package agent

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/iface"
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

	// bypassConfirm 跳过所有工具确认；来自 agent 模板 permission 字段或全局 --bypass。
	bypassConfirm bool

	// confirmStore 是会话级工具放行存储；默认内存实现，可通过 WithConfirmStore 替换。
	confirmStore SessionConfirmStore

	// InstanceID 是 Agent 实例的唯一标识（UUID），与 Def.ID（模板/角色标识）分离。
	// 支持同一模板的多个 Agent 实例共存（并行调度）。
	InstanceID string

	// 异步委托追踪（L1 专用）
	turnMu     sync.RWMutex
	asyncTurns map[int]*asyncTurnState // iter → 轮次异步状态

	// 优先级 mailbox（L1 启用；nil 表示普通 chan job）
	priorityMailbox *PriorityMailbox

	// modelOverride is a per-ask model parameter override.
	// Set by the router before submitting an ask job, consumed by streamLoop,
	// and auto-cleared when the ask completes. Thread-safe via atomic pointer.
	modelOverride atomic.Pointer[ModelParams]

	// runtime bundles all mutable runtime observability state under a single
	// RWMutex. Includes lifecycle state, error tracking, circuit breaker,
	// exit error, and work-tracking fields. Read by TUI and inspect_agent tool;
	// written by the agent's own goroutine (single writer).
	runtimeMu sync.RWMutex
	runtime   agentRuntime

	watchSlots []watchSlot
	watchMu    sync.RWMutex
	watchSeq   int64

	onStateChange func(State) // called after every state transition
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
// Skill 是可执行的技能定义，LLM 通过 Skill 内置工具调用。
// 多次调用 WithSkills 会累加（同一 SkillRegistry），同名 Skill 仍会 panic。
func WithSkills(skills ...*skill.Skill) Option {
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
//	    agent.WithToolTimeout("Bash", 30*time.Second),
//	    agent.WithToolTimeout("WebFetch", 10*time.Second),
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

// WithInstanceID overrides the auto-generated UUID instance ID.
// Primarily for deterministic testing.
func WithInstanceID(id string) Option {
	return func(a *Agent) {
		a.InstanceID = id
	}
}

// SetDelegateSpawnFn replaces the SpawnFn on the DelegateTool with the given
// leaderID. This is used after Supervisor creation to wire L2→L3 delegation
// through the Supervisor so spawned L3 children are tracked.
//
// Returns true if a DelegateTool with that name was found and updated.
func (a *Agent) SetDelegateSpawnFn(leaderID string, spawnFn func(ctx context.Context, task string) (iface.Locatable, error)) bool {
	if a.tools == nil {
		return false
	}
	t, ok := a.tools.Get("delegate_" + leaderID)
	if !ok {
		return false
	}
	dt, ok := t.(*tools.DelegateTool)
	if !ok {
		return false
	}
	dt.SpawnFn = spawnFn
	return true
}

// RegisterTool registers a tool into the agent's ToolRegistry at runtime.
func (a *Agent) RegisterTool(t tools.Tool) error {
	if a.tools == nil {
		return fmt.Errorf("agent %q: ToolRegistry is nil", a.Def.ID)
	}
	return a.tools.Register(t)
}

// PendingDelegations 返回当前等待中的异步委托轮次数量
func (a *Agent) PendingDelegations() int {
	a.turnMu.RLock()
	defer a.turnMu.RUnlock()
	return len(a.asyncTurns)
}

// MailboxDepth 返回 mailbox 队列深度
//
// 对 PriorityMailbox 返回 (high, normal)；对普通 mailbox 返回 (0, len(mailbox))。
// 值为近似值（channel 长度非精确锁定）。
func (a *Agent) MailboxDepth() (high, normal int) {
	a.mu.Lock()
	pm := a.priorityMailbox
	mb := a.mailbox
	a.mu.Unlock()

	if pm != nil {
		return pm.Len()
	}
	if mb != nil {
		return 0, len(mb)
	}
	return 0, 0
}

// SetModelOverride sets per-ask model parameters that take precedence over
// Definition defaults. Called by the router BEFORE AskStream/Ask.
// The override is automatically cleared when the ask completes.
//
// If the agent has an explicitly configured model (from template), this is
// a no-op — template model takes precedence over task-level routing.
//
// Thread-safe (atomic pointer store). Calling with nil clears the override.
func (a *Agent) SetModelOverride(params *ModelParams) {
	if a.Def.ExplicitModel {
		return
	}
	a.modelOverride.Store(params)
}

// ClearModelOverride removes the per-ask override, reverting to Definition defaults.
func (a *Agent) ClearModelOverride() {
	a.modelOverride.Store(nil)
}

// EffectiveModelID returns the model ID actually in use.
// It prefers the per-ask override (set by the router) and falls back to the
// Definition default. Thread-safe (atomic pointer load).
func (a *Agent) EffectiveModelID() string {
	if mp := a.modelOverride.Load(); mp != nil && mp.ModelID != "" {
		return mp.ModelID
	}
	return a.Def.ModelID
}

// EffectiveTaskLevel returns the task classification level from the current
// per-ask override. Returns "" if no override is active or no level is set.
// Thread-safe (atomic pointer load).
func (a *Agent) EffectiveTaskLevel() string {
	if mp := a.modelOverride.Load(); mp != nil {
		return mp.Level
	}
	return ""
}

// RecordError increments the error counter and stores the error message.
// Called from streamLoop (LLM errors) and watchDelegatedTask (delegation failures).
func (a *Agent) RecordError(err error) {
	a.runtimeMu.Lock()
	a.runtime.errCount++
	a.runtime.lastErr = err.Error()
	a.runtimeMu.Unlock()
}

// ResetErrors clears the per-job error counters. Called at the start of
// each new job in run.go.
func (a *Agent) ResetErrors() {
	a.runtimeMu.Lock()
	a.runtime.errCount = 0
	a.runtime.lastErr = ""
	a.runtimeMu.Unlock()
}

// ErrorCount returns the number of errors recorded in the current job.
func (a *Agent) ErrorCount() int32 {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.errCount
}

// LastError returns the most recent error message, or "" if none.
func (a *Agent) LastError() string {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.lastErr
}

// ─── Circuit breaker ────────────────────────────────────────────────────

// IncrementConsecutiveFailures increments the circuit breaker counter and
// returns the new value. Called by streamLoop on fatal errors.
func (a *Agent) IncrementConsecutiveFailures() int32 {
	a.runtimeMu.Lock()
	a.runtime.consecutiveFailures++
	v := a.runtime.consecutiveFailures
	a.runtimeMu.Unlock()
	return v
}

func (a *Agent) ResetConsecutiveFailures() {
	a.runtimeMu.Lock()
	a.runtime.consecutiveFailures = 0
	a.runtimeMu.Unlock()
}

func (a *Agent) ConsecutiveFailures() int32 {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.consecutiveFailures
}

// CurrentWork returns a consistent snapshot of the agent's current runtime
// observability state including lifecycle state, work tracking, errors, and
// delegation count.
func (a *Agent) CurrentWork() WorkStatus {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	var elapsed string
	if !a.runtime.startedAt.IsZero() {
		elapsed = time.Since(a.runtime.startedAt).Truncate(time.Millisecond).String()
	}
	return WorkStatus{
		State:               a.runtime.state,
		Prompt:              a.runtime.prompt,
		Iteration:           int(a.runtime.iter),
		CurrentTool:         a.runtime.tool,
		CurrentToolArgs:     a.runtime.toolArgs,
		Elapsed:             elapsed,
		ErrorCount:          int(a.runtime.errCount),
		LastError:           a.runtime.lastErr,
		ConsecutiveFailures: int(a.runtime.consecutiveFailures),
		PendingDelegations:  a.pendingDelegationsLocked(),
	}
}

func (a *Agent) pendingDelegationsLocked() int {
	return len(a.asyncTurns)
}

// ─── Runtime state helpers (agent goroutine only) ────────────────────────

// SetOnStateChange registers a callback invoked (outside any lock) after every
// state transition. Must be set before Start. The callback receives the new state.
func (a *Agent) SetOnStateChange(fn func(State)) {
	a.mu.Lock()
	a.onStateChange = fn
	a.mu.Unlock()
}

func (a *Agent) setRuntimeState(s State) {
	a.runtimeMu.Lock()
	a.runtime.state = s
	a.runtimeMu.Unlock()
	if fn := a.onStateChange; fn != nil {
		fn(s)
	}
}

func (a *Agent) setRuntimeExitErr(err error) {
	a.runtimeMu.Lock()
	a.runtime.exitErr = err
	a.runtimeMu.Unlock()
}

func (a *Agent) setWorkStart(prompt string) {
	a.runtimeMu.Lock()
	a.runtime.state = StateProcessing
	a.runtime.prompt = prompt
	a.runtime.startedAt = time.Now()
	a.runtimeMu.Unlock()
}

func (a *Agent) setWorkIter(iter int) {
	a.runtimeMu.Lock()
	a.runtime.iter = int32(iter)
	a.runtimeMu.Unlock()
}

func (a *Agent) setWorkTool(name, args string) {
	a.runtimeMu.Lock()
	a.runtime.tool = name
	a.runtime.toolArgs = args
	a.runtimeMu.Unlock()
}

func (a *Agent) clearWorkTool() {
	a.runtimeMu.Lock()
	a.runtime.tool = ""
	a.runtime.toolArgs = ""
	a.runtimeMu.Unlock()
}

func (a *Agent) clearWork() {
	a.runtimeMu.Lock()
	a.runtime.prompt = ""
	a.runtime.iter = 0
	a.runtime.tool = ""
	a.runtime.toolArgs = ""
	a.runtime.state = StateIdle
	a.runtimeMu.Unlock()
}

// ─── Event subscription (Watch mode) ────────────────────────────────────

type watchSlot struct {
	ch chan AgentEvent
	id int64
}

// Watch returns a buffered channel that receives a copy of all AgentEvent
// produced by this agent's current (or next) job execution. Returns a cancel
// function to unsubscribe and close the channel.
//
// The channel has a 64-slot buffer. If the watcher falls behind, events are
// silently dropped (non-blocking fan-out) to avoid backpressure on the
// primary consumer.
//
// Only events from the current/next streamLoop are broadcast; idle agents
// produce no events. Call after Ask/AskStream has started.
func (a *Agent) Watch() (<-chan AgentEvent, func()) {
	ch := make(chan AgentEvent, 64)
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	a.watchSeq++
	id := a.watchSeq
	a.watchSlots = append(a.watchSlots, watchSlot{ch: ch, id: id})
	cancel := func() {
		a.watchMu.Lock()
		defer a.watchMu.Unlock()
		for i, s := range a.watchSlots {
			if s.id == id {
				a.watchSlots = append(a.watchSlots[:i], a.watchSlots[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

// emitToWatchers fans out an event to all registered watchers. Non-blocking;
// slow watchers silently drop events.
func (a *Agent) emitToWatchers(ev AgentEvent) {
	a.watchMu.RLock()
	defer a.watchMu.RUnlock()
	for _, s := range a.watchSlots {
		select {
		case s.ch <- ev:
		default:
		}
	}
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
		InstanceID:     uuid.NewString(),
		mailboxCap:     DefaultMailboxCap,
		asyncTurns:     make(map[int]*asyncTurnState),
		pendingConfirm: make(map[string]*confirmSlot),
		confirmStore:   NewMemoryConfirmStore(),
		bypassConfirm:  def.BypassConfirm,
	}
	for _, opt := range opts {
		opt(a)
	}
	a.runtime.state = StateStopped
	return a
}
