package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Types ───────────────────────────────────────────────────────────────────

// job 是 mailbox 里流动的"一单活"
//
// 它是闭包，封装了"这次要做什么"（包括调用方的数据和 reply chan）。
// 所有上层 API（Ask / Submit / 未来的 Session）都构造不同语义的 job 投递。
// 不对外暴露。
type job func(ctx context.Context)

// errHolder 包装 error 以便存进 atomic.Value（后者不允许存 nil）
type errHolder struct{ err error }

// ─── Agent ───────────────────────────────────────────────────────────────────

// Agent 是一个绑定 LLM + 配置 + 日志的长期运行单元
//
// 生命周期：
//
//	NewAgent → Start → [ Ask / Submit ]* → Stop → Stopped
//	                                                 ↓
//	                                                 Start 可重启
//
// 并发安全：
//   - Ask / Submit / State / Done / Err / Stop 可被多个 goroutine 并发调用
//   - mailbox 里的 job 在 run goroutine 中串行执行（天然互斥）
//   - Start 和 Stop 互斥（同一时刻只有一个在改生命周期字段）
type Agent struct {
	Def Definition
	LLM LLMClient
	Log *logger.Logger

	// 配置（构造后不变）
	mailboxCap    int
	tools         *tools.ToolRegistry  // 底层执行原语
	skills        *skill.SkillRegistry // 上下文注入机制
	parallelTools bool                 // true 时一轮多个 tool_call 用 errgroup 并发执行

	// toolTimeouts 按 tool.Name() 指定 Execute 的超时时长（0/nil = 无单 tool 超时）
	// execToolStream 会用 context.WithTimeout 包裹 ctx，超时错误被格式化
	// 为 "error: tool timeout after Xs" 喂回 LLM（不中断循环）。
	toolTimeouts map[string]time.Duration

	// runtime 字段：Start 时分配，Stop 后保留 done 直到下次 Start 覆盖
	// mu 仅在 Start/Stop 路径上互斥；热路径（Ask/Submit）通过快照读取
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	mailbox chan job
	done    chan struct{}

	// 确认状态机：execToolStream 阻塞等待，外部通过 Confirm 注入结果
	confirmMu      sync.RWMutex
	pendingConfirm map[string]*confirmSlot

	// confirmStore 是会话级工具放行存储；默认内存实现，可通过 WithConfirmStore 替换。
	confirmStore SessionConfirmStore

	// 异步委托追踪（L1 专用）
	turnMu     sync.RWMutex
	asyncTurns map[int]*asyncTurnState // iter → 轮次异步状态

	// 优先级 mailbox（L1 启用；nil 表示普通 chan job）
	priorityMailbox *PriorityMailbox

	// ephemeral 标记（L3 阅后即焚）
	ephemeral bool

	// 观察（无锁）
	state   atomic.Int32 // State
	exitErr atomic.Value // errHolder
}

// confirmSlot 是单次 tool_call 的待确认槽位
type confirmSlot struct {
	done atomic.Bool
	ch   chan string // 用户选择的选项值；"" 表示拒绝/取消
}

// Option 是 NewAgent 的 functional option
type Option func(*Agent)

// WithMailboxCap 设置 mailbox 容量（默认 DefaultMailboxCap = 8）
// cap <= 0 会被忽略（使用默认值）
func WithMailboxCap(cap int) Option {
	return func(a *Agent) {
		if cap > 0 {
			a.mailboxCap = cap
		}
	}
}

// WithSkills 注册一批 Skill 到 agent 的 SkillRegistry
//
// Skill 是上下文加载机制，激活时将 Instructions 注入 system prompt。
// 多次调用 WithSkills 会累加（同一 SkillRegistry），同名 Skill 仍会 panic。
func WithSkills(skills ...skill.Skill) Option {
	return func(a *Agent) {
		if len(skills) == 0 {
			return
		}
		if a.skills == nil {
			a.skills = skill.NewSkillRegistry()
		}
		for _, s := range skills {
			if err := a.skills.Register(s); err != nil {
				panic(fmt.Sprintf("agent: WithSkills: %v", err))
			}
		}
	}
}

// WithTools 注册一批 Tool 到 agent 的 ToolRegistry
//
// Tool 是底层执行原语，LLM 通过 function calling 调用。
// 重名 / Tool.Name() 为空 / nil tool 会 panic —— 属于构造期编程错误，
// 必须早炸。生产代码应保证 tool name 唯一。
//
// 多次调用 WithTools 会累加（同一 registry），同名仍会 panic。
func WithTools(ts ...tools.Tool) Option {
	return func(a *Agent) {
		if len(ts) == 0 {
			return
		}
		if a.tools == nil {
			a.tools = tools.NewToolRegistry()
		}
		for _, t := range ts {
			if err := a.tools.Register(t); err != nil {
				panic(fmt.Sprintf("agent: WithTools: %v", err))
			}
		}
	}
}

// WithParallelTools 打开/关闭同一轮多个 tool_call 的并发执行
//
// 默认 false（串行，保持现状行为）。
//
// 打开后：runOnceStream 内 len(toolCalls) > 1 时走 errgroup 并发路径；
// 单个 tool_call 仍走串行（无并发收益，省 goroutine 创建）。
//
// 事件顺序保证：
//   - 并发场景下各 `ToolExecStart` / `ToolExecDone` 事件相对顺序
//     由 goroutine 调度决定；对任一 `call_id`，`Start` 一定先于 `Done`。
//   - 塞回 LLM 的 `role=tool` 消息**严格按 LLM 原始 tool_calls 顺序**，
//     不被 goroutine 完成次序打乱。
//
// 错误语义与串行一致：tool 执行错误（含 panic / ctx 取消）被格式化为
// `"error: ..."` 喂回 LLM，不中断循环；errgroup 永不短路。
func WithParallelTools(enabled bool) Option {
	return func(a *Agent) {
		a.parallelTools = enabled
	}
}

// WithConfirmStore 替换默认的内存会话确认存储。
//
// 用于测试 mock，或未来接入 Redis/DB 持久化。
// nil store 会被忽略（保持默认内存实现）。
func WithConfirmStore(store SessionConfirmStore) Option {
	return func(a *Agent) {
		if store != nil {
			a.confirmStore = store
		}
	}
}

// WithToolTimeout 给指定 tool.Name() 设置 Execute 的超时时长
//
// 语义：
//   - d > 0：在 execToolStream 内用 context.WithTimeout 包裹 ctx；超时触发
//     后被 **格式化为** `"error: tool timeout after <d>"` 喂回 LLM，
//     不中断整个 Ask 循环（与普通 tool 错误一致）
//   - d <= 0：删除该 tool 的超时配置（回退到无超时，继承上游 ctx）
//   - 同一 name 多次调用：以最后一次为准
//
// 例：
//
//	agent.NewAgent(def, llm, log,
//	    agent.WithTools(tools...),
//	    agent.WithToolTimeout("shell_exec", 30*time.Second),
//	    agent.WithToolTimeout("http_fetch", 10*time.Second),
//	)
//
// 不影响 ctx 取消路径：caller ctx 取消（Stop / Ask ctx）仍按原语义
// 立即传播到当前 tool（WithTimeout 是**附加** deadline，不覆盖父 ctx）。
func WithToolTimeout(name string, d time.Duration) Option {
	return func(a *Agent) {
		if a.toolTimeouts == nil {
			a.toolTimeouts = make(map[string]time.Duration)
		}
		if d <= 0 {
			delete(a.toolTimeouts, name)
			return
		}
		a.toolTimeouts[name] = d
	}
}

// WithEphemeral 将 Agent 标记为阅后即焚（L3 执行单元）
//
// 语义：
//   - MailboxCap 设为 1（只接收一个任务）
//   - 无 Timeline 持久化（factory 中跳过 timeline 创建）
//   - 完成后由 Supervisor.ReapChild 回收
func WithEphemeral() Option {
	return func(a *Agent) {
		a.ephemeral = true
		if a.mailboxCap <= 0 || a.mailboxCap == DefaultMailboxCap {
			a.mailboxCap = 1
		}
	}
}

// WithPriorityMailbox 启用优先级 mailbox（L1 专用）
//
// 启用后：
//   - 高优先级 job（委托回传、超时事件）通过 highCh 投递
//   - 普通优先级 job（用户 Ask/Submit）通过 normalCh 投递
//   - run goroutine 优先消费 highCh，确保委托结果不被新消息阻塞
func WithPriorityMailbox() Option {
	return func(a *Agent) {
		a.priorityMailbox = NewPriorityMailbox()
	}
}

// IsEphemeral 返回 Agent 是否为阅后即焚模式
func (a *Agent) IsEphemeral() bool {
	return a.ephemeral
}

// NewAgent 构造未 Start 的 agent
//
// log 可以为 nil（此时日志调用被跳过）。
// 必须调用 Start 才能开始接收 Ask / Submit。
func NewAgent(def Definition, llm LLMClient, log *logger.Logger, opts ...Option) *Agent {
	a := &Agent{
		Def:            def,
		LLM:            llm,
		Log:            log,
		mailboxCap:     DefaultMailboxCap,
		pendingConfirm: make(map[string]*confirmSlot),
		confirmStore:   NewMemoryConfirmStore(),
	}
	for _, opt := range opts {
		opt(a)
	}
	a.state.Store(int32(StateStopped)) // 未 Start 时视为 Stopped
	a.exitErr.Store(errHolder{})       // 初始无 error
	return a
}
