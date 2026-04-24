package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
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
	tools         *ToolRegistry // nil 表示无 tools（ToolSpecs 返回 nil）
	parallelTools bool          // true 时一轮多个 tool_call 用 errgroup 并发执行

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

// WithTools 注册一批工具到 agent
//
// 重名 / Tool.Name() 为空 / nil tool 会 panic —— 属于构造期编程错误，
// 必须早炸。生产代码应保证 tool name 唯一；若动态注册需要 error，
// 直接构造 ToolRegistry 后用 Register() 检查返回值。
//
// 多次调用 WithTools 会累加（同一 registry），同名仍会 panic。
func WithTools(tools ...Tool) Option {
	return func(a *Agent) {
		if len(tools) == 0 {
			return
		}
		if a.tools == nil {
			a.tools = NewToolRegistry()
		}
		for _, t := range tools {
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
	}
	for _, opt := range opts {
		opt(a)
	}
	a.state.Store(int32(StateStopped)) // 未 Start 时视为 Stopped
	a.exitErr.Store(errHolder{})       // 初始无 error
	return a
}

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// Start 启动 agent 的 run goroutine
//
// 重复 Start 返回 ErrAlreadyStarted。Stop 后可以再次 Start（重置 mailbox 和 exitErr）。
// parent 通常是 context.Background() 或进程级 ctx；parent 取消会让 agent 自动退出。
func (a *Agent) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// 如果上次的 done 还没 close 并且 ctx 非 nil，说明 agent 还在运行
	if a.ctx != nil {
		select {
		case <-a.done:
			// 上次已退出，可以重启
		default:
			return ErrAlreadyStarted
		}
	}

	a.ctx, a.cancel = context.WithCancel(parent)
	// agent 自己的 ctx 也注入 actor_id，这样 run/drain 的日志也自动带
	a.ctx = a.ctxWithAgentAttrs(a.ctx)
	a.mailbox = make(chan job, a.mailboxCap)
	a.done = make(chan struct{})
	a.exitErr.Store(errHolder{})
	a.state.Store(int32(StateIdle))

	go a.run(a.ctx, a.mailbox, a.done)

	a.logInfo(a.ctx, logger.CatActor, "agent started",
		slog.String("kind", string(a.Def.Kind)),
		slog.String("role", string(a.Def.Role)),
		slog.String("model_id", a.Def.ModelID),
		slog.Int("mailbox_cap", a.mailboxCap),
	)
	return nil
}

// Stop 请求 agent 停止
//
//  1. cancel agent ctx → run goroutine 下轮 select 退出
//  2. 正在执行的 job 其 ctx 也被取消（job 应监听 ctx.Done）
//  3. 已入队的 pending job 会被 drain（每个 job 以已 canceled 的 ctx 调用）
//     使得卡在 reply chan 的 Ask 能返回 ctx.Canceled
//  4. 等待 run goroutine 退出；timeout <= 0 表示无限等待
//
// 超时返回 ErrStopTimeout，但 goroutine 仍会最终退出。
// 未 Start 直接调 Stop 返回 ErrNotStarted。
func (a *Agent) Stop(timeout time.Duration) error {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	// 快照 a.ctx：cancel 之后它的 value（actor_id）仍可读，用于 Stop 日志
	stopCtx := a.ctx
	a.mu.Unlock()

	if cancel == nil || done == nil {
		return ErrNotStarted
	}

	a.logInfo(stopCtx, logger.CatActor, "agent stop requested",
		slog.Int64("timeout_ms", timeout.Milliseconds()),
	)

	cancel()

	start := time.Now()
	if timeout <= 0 {
		<-done
		a.logInfo(stopCtx, logger.CatActor, "agent stopped",
			slog.Int64("wait_ms", time.Since(start).Milliseconds()),
		)
		return nil
	}
	select {
	case <-done:
		a.logInfo(stopCtx, logger.CatActor, "agent stopped",
			slog.Int64("wait_ms", time.Since(start).Milliseconds()),
		)
		return nil
	case <-time.After(timeout):
		a.logError(stopCtx, logger.CatActor, "agent stop timeout", ErrStopTimeout)
		return ErrStopTimeout
	}
}

// Done 返回一个 channel，run goroutine 退出后 close
//
// 语义类似 context.Context.Done：可用于 select 等待 agent 退出。
// 未 Start 时返回一个已 close 的 channel（立即可读）。
func (a *Agent) Done() <-chan struct{} {
	a.mu.Lock()
	d := a.done
	a.mu.Unlock()
	if d == nil {
		// 未 Start：返回一个已 close 的 channel
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return d
}

// Err 返回 agent 退出原因
//
//   - nil：未 Start / 正在运行 / 已正常 Stop
//   - non-nil：run goroutine 内部 panic，值为封装的 error
//
// 仅在 <-Done() 之后读取才有定论。
func (a *Agent) Err() error {
	if v, ok := a.exitErr.Load().(errHolder); ok {
		return v.err
	}
	return nil
}

// State 返回当前观察状态（并发安全）
func (a *Agent) State() State {
	return State(a.state.Load())
}

// Confirm 向 agent 注入用户对某个待确认 tool_call 的响应。
//
// 由外部系统（UI / TUI / WebSocket）在用户做出选择后调用。
// choice 为用户选择的选项值；二元确认用 "yes"（确认）或 ""（拒绝）。
// 若 callID 不存在或已响应，返回错误。
func (a *Agent) Confirm(callID string, choice string) error {
	a.confirmMu.RLock()
	slot, ok := a.pendingConfirm[callID]
	a.confirmMu.RUnlock()
	if !ok {
		return fmt.Errorf("agent: no pending confirmation for %s", callID)
	}
	if !slot.done.CompareAndSwap(false, true) {
		return fmt.Errorf("agent: confirmation %s already resolved", callID)
	}
	select {
	case slot.ch <- choice:
		return nil
	default:
		return fmt.Errorf("agent: confirmation %s channel blocked", callID)
	}
}

// ToolSpecs 返回当前 agent 注册的所有 tool 的 llm.ToolDef 快照
//
// 未调 WithTools 时返回 nil；LLMRequest.Tools = nil 在 DeepSeek wire 层
// 会被 omitempty 省略，行为等价于"没注册工具"。
func (a *Agent) ToolSpecs() []llm.ToolDef {
	if a.tools == nil {
		return nil
	}
	return a.tools.Specs()
}

// ─── Ask / Submit ───────────────────────────────────────────────────────────

// Ask 向 agent 投递一次 LLM 请求并等结果
//
// 行为：内部走 AskStream 累积所有事件 → 返回最终 content + 首个错误
//   - 投递阶段：若 mailbox 满，阻塞直到有空位 / ctx 取消 / agent 退出
//   - 执行阶段：job 在 agent goroutine 中串行执行（一次只处理一条）
//   - 取消：caller ctx 或 agent ctx 任一取消都会中断在途 LLM 调用
//
// 错误：
//   - ErrNotStarted：agent 未 Start
//   - ErrStopped：投递时或等待时 agent 已退出
//   - ctx.Err()：caller 主动取消
//   - LLM 返回的 error 透传
//
// 向后兼容：签名不变，原来所有调用都继续工作；但内部路径从
// "runOnce 同步 Chat" 变为 "runOnceStream 消费事件流"。
func (a *Agent) Ask(ctx context.Context, prompt string) (string, error) {
	ch, err := a.AskStream(ctx, prompt)
	if err != nil {
		return "", err
	}

	var (
		b            strings.Builder
		finalContent string
		finalErr     error
	)
	for ev := range ch {
		switch e := ev.(type) {
		case ContentDeltaEvent:
			b.WriteString(e.Delta)
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			finalErr = e.Err
		}
	}
	if finalErr != nil {
		return "", finalErr
	}
	if finalContent != "" {
		return finalContent, nil
	}
	return b.String(), nil
}

// AskStream 投递一次流式 Ask 并立即返回事件通道
//
// 返回通道由 agent goroutine 内部的 runOnceStream close。
// caller 必须持续 range 直到通道关闭；中途放弃 range 会触发背压
// （runOnceStream 在发送事件时阻塞），因此放弃前必须 cancel ctx。
//
// 错误：
//   - ErrNotStarted / ErrStopped：入队失败时直接返回 (nil, err)
//   - 入队后的错误：通过 ErrorEvent 下发（此时第一返回值 non-nil 通道仍可 range）
func (a *Agent) AskStream(ctx context.Context, prompt string) (<-chan AgentEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// 注入 trace_id（有则用、无则自生）+ actor_id，供全链路日志提取
	ctx = ensureTraceID(ctx)
	ctx = a.ctxWithAgentAttrs(ctx)

	// buffer 64：能缓冲单轮典型的 delta 风暴；满了阻塞（不丢事件）+ ctx 兜底
	out := make(chan AgentEvent, 64)

	jb := func(jobCtx context.Context) {
		// 合并 caller ctx（带 trace_id）和 agent jobCtx（Stop 时 cancel）
		// ctx 放前面是关键：合并后 ctx 的 value（trace_id / actor_id）仍可读
		merged, cancel := mergeCtx(ctx, jobCtx)
		defer cancel()
		a.runOnceStream(merged, prompt, out)
	}

	if err := a.submit(ctx, jb); err != nil {
		// submit 失败（ErrNotStarted / ErrStopped / ctx.Err）→ 关闭 out 后返回 err
		// 关闭是为了防止 caller 误以为 channel 还会有事件来而悬挂
		close(out)
		return nil, err
	}
	return out, nil
}

// Submit 投递任意自定义 job
//
// fn 接收 agent 的 ctx（Stop 时会被 cancel）。
// Submit 只等入队，不等 fn 完成；返回 nil 表示成功入队。
// 要同步等待结果，请用 Ask；或在 fn 内部使用 caller 的 chan。
//
// caller ctx 语义：
//   - 仅控制"入队等待"：mailbox 满时 caller ctx 取消会让 Submit 返回 ctx.Err()
//   - 不控制 fn 执行：fn 运行时完全由 agent ctx 控制（Stop 时取消）
//   - trace_id / actor_id 会从 caller ctx 拷贝到 fn ctx，保持跨 goroutine 日志链路
//
// 错误：
//   - ErrNotStarted / ErrStopped
//   - ctx.Err()：caller 在入队等待中取消
func (a *Agent) Submit(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return fmt.Errorf("agent: nil fn")
	}
	// 注入 trace_id + actor_id（供入队等待日志用，同时用于拷贝到 fn ctx）
	ctx = ensureTraceID(ctx)
	ctx = a.ctxWithAgentAttrs(ctx)
	traceID := logger.TraceIDFromContext(ctx)

	jb := func(jobCtx context.Context) {
		// 把 trace_id / actor_id 拷到 jobCtx（actor_id 已由 Start 注入 a.ctx）
		// jobCtx 源自 a.ctx，所以 actor_id 已有；trace_id 从 caller ctx 补上
		fnCtx := jobCtx
		if traceID != "" {
			fnCtx = logger.WithTraceID(fnCtx, traceID)
		}
		if err := fn(fnCtx); err != nil {
			a.logError(fnCtx, logger.CatActor, "submit job returned error", err)
		}
	}
	return a.submit(ctx, jb)
}

// submit 是 Ask / Submit 共享的入队实现
func (a *Agent) submit(ctx context.Context, jb job) error {
	a.mu.Lock()
	mailbox := a.mailbox
	agentDone := a.done
	a.mu.Unlock()

	if mailbox == nil || agentDone == nil {
		return ErrNotStarted
	}

	// 快速路径：agent 已退出（避免送进一个没人消费的 mailbox）
	select {
	case <-agentDone:
		return ErrStopped
	default:
	}

	select {
	case mailbox <- jb:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-agentDone:
		return ErrStopped
	}
}

// ─── run goroutine ──────────────────────────────────────────────────────────

// run 是 agent 的主循环
//
// 接受 ctx / mailbox / done 作为参数（而非从 receiver 读）：
// Start 构造它们并作为局部参数传入，run 就不需要和 Start/Stop 抢锁；
// 即使 Stop 重置了 a.mailbox，这里的局部 mailbox 还指向同一个 chan。
func (a *Agent) run(ctx context.Context, mailbox <-chan job, done chan<- struct{}) {
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.exitErr.Store(errHolder{err: err})
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
		}
		a.state.Store(int32(StateStopped))
		close(done)
	}()

	for {
		select {
		case <-ctx.Done():
			a.state.Store(int32(StateStopping))
			drained := a.drainMailbox(ctx, mailbox)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				slog.Int("drained_jobs", drained),
			)
			return
		case jb := <-mailbox:
			a.state.Store(int32(StateProcessing))
			jb(ctx)
			a.state.Store(int32(StateIdle))
		}
	}
}

// drainMailbox 把已入队的 job 全部以已 canceled 的 ctx 调用一遍
//
// 目的：让每个 caller 的 Ask 能从 replyCh 拿到结果（通常是 ctx.Canceled），
// 不会永远卡住。
// 不会再从 mailbox 之外读（mailbox 永不 close，send 方会看到 agentDone
// 已 close 后直接返回 ErrStopped）。
//
// 返回 drain 的 job 数量，用于日志统计。
func (a *Agent) drainMailbox(ctx context.Context, mailbox <-chan job) int {
	n := 0
	for {
		select {
		case jb := <-mailbox:
			jb(ctx)
			n++
		default:
			return n
		}
	}
}

// ─── runOnceStream / RunOnce ─────────────────────────────────────────────────

// runOnceStream 是 AskStream 的执行主体（在 agent goroutine 中串行运行）
//
// 职责：
//   - 构造 LLMRequest（含累积 msgs + ToolSpecs）→ 调 LLM.ChatStream
//   - 从 llm.Event 累积 content / reasoning / tool_call 并 re-emit AgentEvent
//   - 每轮结束后：若有 tool_calls 则执行并喂回，否则 DoneEvent 终止
//   - 守护 MaxIterations 上限；超限 → ErrorEvent(ErrMaxIterations)
//   - 始终 defer close(out)：无论哪条返回路径都保证 channel 关闭
//
// 日志（都带 trace_id / actor_id）：
//   - info  "llm chat start"   iter / model / prompt_len / messages / tools
//   - error "llm chat failed"  iter / err / duration_ms
//   - info  "llm chat done"    iter / response_len / reasoning_len / tool_calls /
//                              finish_reason / token 统计 / duration_ms
//   - info  "tool exec start"  tool_name / tool_call_id / arg_len
//   - info  "tool exec done"   tool_name / tool_call_id / arg_len / result_len / duration_ms
//   - error "tool exec failed" 同上 + err
//   - error "tool not found"   tool_name / tool_call_id
//   - error "max tool iterations exceeded"  max_iter
func (a *Agent) runOnceStream(ctx context.Context, prompt string, out chan<- AgentEvent) {
	// 注意 defer 栈（LIFO）：
	//   1. close(out) 最先注册 → panic 时**最后**执行，保证 ErrorEvent
	//      已投递后再关闭 channel（Ask 消费端才能收到错误）
	//   2. recover+re-panic 后注册 → 最先执行：捕获 panic、emit ErrorEvent、
	//      re-panic 让上层 run goroutine 的 recover 正常记录 exitErr 并置 Stopped
	defer close(out)
	defer func() {
		if r := recover(); r != nil {
			a.emit(ctx, out, ErrorEvent{
				Err: fmt.Errorf("agent panic: %v", r),
			})
			panic(r) // 继续冒泡给 run goroutine 的 recover
		}
	}()

	if a.LLM == nil {
		a.emit(ctx, out, ErrorEvent{
			Err: fmt.Errorf("agent %q: llm client is nil", a.Def.ID),
		})
		return
	}

	msgs := buildMessages(a.Def.SystemPrompt, prompt)
	specs := a.ToolSpecs() // nil-safe

	maxIter := a.Def.MaxIterations
	if maxIter <= 0 {
		maxIter = DefaultMaxIterations
	}

	for iter := 0; iter < maxIter; iter++ {
		if err := ctx.Err(); err != nil {
			a.emit(ctx, out, ErrorEvent{Err: err})
			return
		}

		req := LLMRequest{
			Model:       a.Def.ModelID,
			Temperature: a.Def.Temperature,
			MaxTokens:   a.Def.MaxTokens,
			Messages:    msgs,
			Tools:       specs,
		}

		a.logInfo(ctx, logger.CatLLM, "llm chat start",
			slog.Int("iter", iter),
			slog.String("model", req.Model),
			slog.Int("prompt_len", len(prompt)),
			slog.Int("messages", len(msgs)),
			slog.Int("tools", len(specs)),
		)

		start := time.Now()
		evCh, err := a.LLM.ChatStream(ctx, req)
		if err != nil {
			durMs := time.Since(start).Milliseconds()
			a.logError(ctx, logger.CatLLM, "llm chat failed", err,
				slog.Int("iter", iter),
				slog.Int64("duration_ms", durMs),
			)
			a.emit(ctx, out, ErrorEvent{Err: err})
			return
		}

		// 本轮累积器
		var (
			content   strings.Builder
			reasoning strings.Builder
			tcSlots   = map[int]*llm.ToolCall{} // by ToolCallDelta.Index
			finish    llm.FinishReason
			usage     llm.Usage
		)

		streamDone := false
		for !streamDone {
			select {
			case <-ctx.Done():
				a.emit(ctx, out, ErrorEvent{Err: ctx.Err()})
				return
			case ev, ok := <-evCh:
				if !ok {
					// channel close 但没收到 Done/Error —— 视为异常结束
					streamDone = true
					break
				}
				switch ev.Type {
				case llm.EventDelta:
					if ev.ContentDelta != "" {
						content.WriteString(ev.ContentDelta)
						if !a.emit(ctx, out, ContentDeltaEvent{
							Iter: iter, Delta: ev.ContentDelta,
						}) {
							return
						}
					}
					if ev.ReasoningContentDelta != "" {
						reasoning.WriteString(ev.ReasoningContentDelta)
						if !a.emit(ctx, out, ReasoningDeltaEvent{
							Iter: iter, Delta: ev.ReasoningContentDelta,
						}) {
							return
						}
					}
					if ev.ToolCallDelta != nil {
						d := ev.ToolCallDelta
						accumulateToolCall(tcSlots, d)
						if !a.emit(ctx, out, ToolCallDeltaEvent{
							Iter:      iter,
							CallID:    d.ID,
							Name:      d.Name,
							ArgsDelta: d.Arguments,
						}) {
							return
						}
					}
				case llm.EventDone:
					finish = ev.FinishReason
					if ev.Usage != nil {
						usage = *ev.Usage
					}
					streamDone = true
				case llm.EventError:
					durMs := time.Since(start).Milliseconds()
					a.logError(ctx, logger.CatLLM, "llm chat failed", ev.Err,
						slog.Int("iter", iter),
						slog.Int64("duration_ms", durMs),
					)
					a.emit(ctx, out, ErrorEvent{Err: ev.Err})
					return
				}
			}
		}

		durMs := time.Since(start).Milliseconds()
		toolCalls := sortedToolCalls(tcSlots)

		a.logInfo(ctx, logger.CatLLM, "llm chat done",
			slog.Int("iter", iter),
			slog.Int("response_len", content.Len()),
			slog.Int("reasoning_len", reasoning.Len()),
			slog.Int("tool_calls", len(toolCalls)),
			slog.String("finish_reason", string(finish)),
			slog.Int("prompt_tokens", usage.PromptTokens),
			slog.Int("completion_tokens", usage.CompletionTokens),
			slog.Int("total_tokens", usage.TotalTokens),
			slog.Int64("duration_ms", durMs),
		)

		// IterationDoneEvent：UI 可据此显示"LLM 说完了、正在执行工具..."
		if !a.emit(ctx, out, IterationDoneEvent{
			Iter:         iter,
			FinishReason: finish,
			Usage:        usage,
		}) {
			return
		}

		// 退出条件 1：LLM 不再要工具 → 返回 content
		if len(toolCalls) == 0 {
			a.emit(ctx, out, DoneEvent{Content: content.String()})
			return
		}

		// 追加 assistant(tool_calls) 消息到对话历史
		msgs = append(msgs, LLMMessage{
			Role:      "assistant",
			Content:   content.String(),
			ToolCalls: toolCalls,
		})

		// 执行本轮所有 tool_call（串行 / errgroup 并发，由 WithParallelTools 决定）
		// 返回的 results 顺序严格等于 toolCalls 原顺序
		results := a.execTools(ctx, iter, toolCalls, out)
		for i, tc := range toolCalls {
			msgs = append(msgs, LLMMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    results[i],
			})
		}
	}

	// 退出条件 2：迭代超上限
	a.logError(ctx, logger.CatLLM, "max tool iterations exceeded", ErrMaxIterations,
		slog.Int("max_iter", maxIter),
	)
	a.emit(ctx, out, ErrorEvent{Err: ErrMaxIterations})
}

// emit 向 out 发送一个 AgentEvent；ctx 取消时放弃发送并返回 false
//
// 关键语义（见 plan R10：防止 AskStream 泄漏 goroutine）：
//   - buffer 有空位时立即发送（不阻塞，不检查 ctx）
//   - buffer 满时 select { ch <- ev; <-ctx.Done() } —— 任一就绪都退出
//   - 返回 false 表示 ctx 取消；调用方应立刻 return（通常配合 defer close(out)）
func (a *Agent) emit(ctx context.Context, out chan<- AgentEvent, ev AgentEvent) bool {
	// 非阻塞快速路径（不检查 ctx，优先把事件发出去）
	select {
	case out <- ev:
		return true
	default:
	}
	// 阻塞路径：优先发送事件，ctx 取消作为兜底
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		// ctx 已取消，但仍尝试最后一次非阻塞发送
		select {
		case out <- ev:
			return true
		default:
			return false
		}
	}
}

// accumulateToolCall 把 streaming ToolCallDelta 按 Index 归位到 slots
//
// 规则（与 llm.ToolCallDelta 文档一致）：
//   - 首次出现该 Index 时初始化 slot；携带 ID/Name
//   - 后续只把 Arguments 片段追加到 slot.Function.Arguments
func accumulateToolCall(slots map[int]*llm.ToolCall, d *llm.ToolCallDelta) {
	tc, ok := slots[d.Index]
	if !ok {
		tc = &llm.ToolCall{Type: "function"}
		slots[d.Index] = tc
	}
	if d.ID != "" {
		tc.ID = d.ID
	}
	if d.Name != "" {
		tc.Function.Name = d.Name
	}
	if d.Arguments != "" {
		tc.Function.Arguments += d.Arguments
	}
}

// sortedToolCalls 按 Index 升序输出 slots 中的完整 ToolCall 列表
//
// 保证：结果顺序严格等于 LLM 原始 tool_calls 顺序（即使 slot map 乱序）
func sortedToolCalls(slots map[int]*llm.ToolCall) []llm.ToolCall {
	if len(slots) == 0 {
		return nil
	}
	maxIdx := -1
	for i := range slots {
		if i > maxIdx {
			maxIdx = i
		}
	}
	out := make([]llm.ToolCall, 0, len(slots))
	for i := 0; i <= maxIdx; i++ {
		if tc, ok := slots[i]; ok {
			out = append(out, *tc)
		}
	}
	return out
}

// execTools 执行本轮所有 tool_call，返回与 calls 同序的结果切片
//
// 分派策略：
//   - len(calls) <= 1 或 parallelTools=false → 串行执行
//     （单 tool 并发无收益；串行简化是共识路径）
//   - 否则走 errgroup 并发：每个 call 一个 goroutine，
//     gctx 由 errgroup 共享（任一 ctx 取消传播到所有未完成的 tool）
//
// 错误语义：
//   - execToolStream 已经把 tool 错误格式化为 "error: ..." 字符串返回，
//     所以每个 goroutine 返回 nil —— errgroup **永不短路**
//   - 即使某个 tool 失败或超时，其他 tool 继续跑完
//   - 上游 ctx 取消时：**正在跑的** tool 会收到 ctx.Done（由 gctx 传播），
//     但未完成的 slot 会被 execToolStream 写 "error: ..." 字符串占位
//
// 结果顺序保证：results[i] 严格对应 calls[i]，与 goroutine 完成顺序无关。
func (a *Agent) execTools(
	ctx context.Context,
	iter int,
	calls []llm.ToolCall,
	out chan<- AgentEvent,
) []string {
	results := make([]string, len(calls))

	// 串行路径：单 tool、或未启用 parallel
	if len(calls) <= 1 || !a.parallelTools {
		for i, tc := range calls {
			if err := ctx.Err(); err != nil {
				results[i] = "error: " + err.Error()
				continue
			}
			results[i] = a.execToolStream(ctx, iter, tc, out)
		}
		return results
	}

	// 并行路径：errgroup，**永不返回非 nil error**
	// 理由见上：tool 错误是 "LLM 要处理的业务态"，不是 agent 要终止的系统态。
	g, gctx := errgroup.WithContext(ctx)
	for i, tc := range calls {
		i, tc := i, tc // capture loop vars
		g.Go(func() error {
			results[i] = a.execToolStream(gctx, iter, tc, out)
			return nil
		})
	}
	_ = g.Wait() // 永不会 return 非 nil
	return results
}

// execToolStream 执行一个 tool_call 并沿 out 发 Start/Done 事件
//
// 总返回 string（塞回 LLM 的 tool-role 消息内容）：
//   - 成功：tool.Execute 的 result
//   - 工具不存在 / Execute 返回 error：`"error: " + err.Error()`，LLM 自行决定是否重试
//
// 错误**不中断循环** —— 这是"tool error 反馈给 LLM"策略。
func (a *Agent) execToolStream(ctx context.Context, iter int, tc llm.ToolCall, out chan<- AgentEvent) string {
	name := tc.Function.Name
	args := tc.Function.Arguments

	tool, ok := a.tools.safeGet(name)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrToolNotFound, name)
		a.logError(ctx, logger.CatTool, "tool not found", err,
			slog.String("tool_name", name),
			slog.String("tool_call_id", tc.ID),
		)
		result := "error: " + err.Error()
		a.emit(ctx, out, ToolExecDoneEvent{
			Iter: iter, CallID: tc.ID, Name: name, Result: result, Err: err,
		})
		return result
	}

	a.logInfo(ctx, logger.CatTool, "tool exec start",
		slog.String("tool_name", name),
		slog.String("tool_call_id", tc.ID),
		slog.Int("arg_len", len(args)),
	)
	a.emit(ctx, out, ToolExecStartEvent{
		Iter: iter, CallID: tc.ID, Name: name, Args: args,
	})

	// ── Confirmable 检查 ───────────────────────────────────────────────
	// 若工具实现了 Confirmable 且 CheckConfirmation 返回 true，
	// 发送 ToolNeedsConfirmEvent（含 Options）并阻塞等待外部 Confirm 调用。
	if c, ok := tool.(Confirmable); ok {
		needsConfirm, prompt := c.CheckConfirmation(args)
		if needsConfirm {
			options := c.ConfirmationOptions(args)
			if !a.emit(ctx, out, ToolNeedsConfirmEvent{
				Iter:    iter,
				CallID:  tc.ID,
				Name:    name,
				Args:    args,
				Prompt:  prompt,
				Options: options,
			}) {
				return "error: " + ctx.Err().Error()
			}

			slot := &confirmSlot{ch: make(chan string, 1)}
			a.confirmMu.Lock()
			a.pendingConfirm[tc.ID] = slot
			a.confirmMu.Unlock()

			var choice string
			select {
			case choice = <-slot.ch:
			case <-ctx.Done():
				a.confirmMu.Lock()
				delete(a.pendingConfirm, tc.ID)
				a.confirmMu.Unlock()
				return "error: " + ctx.Err().Error()
			}

			a.confirmMu.Lock()
			delete(a.pendingConfirm, tc.ID)
			a.confirmMu.Unlock()

			if choice == "" {
				result := "error: user denied execution"
				a.emit(ctx, out, ToolExecDoneEvent{
					Iter:   iter,
					CallID: tc.ID,
					Name:   name,
					Result: result,
					Err:    errors.New("user denied"),
				})
				return result
			}
			args = c.ConfirmArgs(args, choice)
		}
	}

	// 按 tool name 叠加超时（WithToolTimeout 注入）
	// 父 ctx 取消仍优先生效（WithTimeout 是附加 deadline）
	execCtx := ctx
	var timeoutDur time.Duration
	if d, ok := a.toolTimeouts[name]; ok && d > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
		timeoutDur = d
	}

	start := time.Now()
	result, err := tool.Execute(execCtx, args)
	dur := time.Since(start)

	if err != nil {
		// 超时检测：
		//   - 只有当 **父 ctx 未取消** 且 execCtx 已 DeadlineExceeded 时，才归因为 tool timeout
		//   - 父 ctx 取消（Stop / caller cancel）走普通错误路径，保持原错误文本
		isToolTimeout := timeoutDur > 0 &&
			ctx.Err() == nil &&
			execCtx.Err() == context.DeadlineExceeded
		a.logError(ctx, logger.CatTool, "tool exec failed", err,
			slog.String("tool_name", name),
			slog.String("tool_call_id", tc.ID),
			slog.Int("arg_len", len(args)),
			slog.Int64("duration_ms", dur.Milliseconds()),
			slog.Bool("timeout", isToolTimeout),
		)
		var errResult string
		if isToolTimeout {
			errResult = fmt.Sprintf("error: tool timeout after %s", timeoutDur)
		} else {
			errResult = "error: " + err.Error()
		}
		a.emit(ctx, out, ToolExecDoneEvent{
			Iter: iter, CallID: tc.ID, Name: name,
			Result: errResult, Err: err, Duration: dur,
		})
		return errResult
	}

	a.logInfo(ctx, logger.CatTool, "tool exec done",
		slog.String("tool_name", name),
		slog.String("tool_call_id", tc.ID),
		slog.Int("arg_len", len(args)),
		slog.Int("result_len", len(result)),
		slog.Int64("duration_ms", dur.Milliseconds()),
	)
	a.emit(ctx, out, ToolExecDoneEvent{
		Iter: iter, CallID: tc.ID, Name: name,
		Result: result, Duration: dur,
	})
	return result
}

// RunOnce 是包级一次性调用：不启动 goroutine、不经过 mailbox
//
// 适合脚本 / CLI / 单元测试等只需调一次 LLM 的场景。
// 内部消费 runOnceStream 的事件流累积成 content，保持旧 API 签名不变。
//
// def 仅用于 LLMRequest 构造（ModelID / SystemPrompt / 等）。log 可以 nil。
func RunOnce(ctx context.Context, def Definition, client LLMClient, log *logger.Logger, prompt string) (string, error) {
	a := &Agent{Def: def, LLM: client, Log: log}

	out := make(chan AgentEvent, 64)
	go a.runOnceStream(ctx, prompt, out)

	var (
		b            strings.Builder
		finalContent string
		finalErr     error
	)
	for ev := range out {
		switch e := ev.(type) {
		case ContentDeltaEvent:
			b.WriteString(e.Delta)
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			finalErr = e.Err
		}
	}
	if finalErr != nil {
		return "", finalErr
	}
	if finalContent != "" {
		return finalContent, nil
	}
	return b.String(), nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// buildMessages 组装 system + user 两条消息
//
// 如 systemPrompt 为空，跳过 system 消息（避免 `{"role":"system","content":""}`）
func buildMessages(systemPrompt, userPrompt string) []LLMMessage {
	msgs := make([]LLMMessage, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, LLMMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, LLMMessage{Role: "user", Content: userPrompt})
	return msgs
}

// logInfo / logError 是 nil-safe 的日志包装
func (a *Agent) logInfo(ctx context.Context, cat logger.Category, msg string, args ...any) {
	if a.Log == nil {
		return
	}
	a.Log.InfoContext(ctx, cat, msg, args...)
}

func (a *Agent) logError(ctx context.Context, cat logger.Category, msg string, err error, args ...any) {
	if a.Log == nil {
		return
	}
	allArgs := append([]any{slog.String("err", err.Error())}, args...)
	a.Log.ErrorContext(ctx, cat, msg, allArgs...)
}

// mergeCtx 返回一个 context，a 或 b 任一取消都会取消返回的 context
//
// 实现：起一个 goroutine 监听两个源；返回的 cancel func 保证 goroutine
// 总能退出（调用 cancel 或任一源取消都会让 goroutine 退出），无泄漏。
func mergeCtx(a, b context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancel(a)
	if b == nil || b.Done() == nil {
		return merged, cancel
	}
	go func() {
		select {
		case <-b.Done():
			cancel()
		case <-merged.Done():
			// a 取消或 caller 调 cancel；不需要额外动作，goroutine 退出即可
		}
	}()
	return merged, cancel
}

// ─── Trace / actor_id injection ─────────────────────────────────────────────

// ensureTraceID 保证 ctx 里带 trace_id
//
// 策略（用户确认）：有则用、无则自生
// 新生成的是 8 字节 hex（16 个字符），足够在单个进程内区分并发 Ask。
func ensureTraceID(ctx context.Context) context.Context {
	if logger.TraceIDFromContext(ctx) != "" {
		return ctx
	}
	return logger.WithTraceID(ctx, newTraceID())
}

// ctxWithAgentAttrs 把 actor_id 注入 ctx，供后续 Logger 自动从 ctx 提取
//
// Agent 构造时 Def.ID 就固定了；每次 Ask/Submit/lifecycle 日志都应该带。
func (a *Agent) ctxWithAgentAttrs(ctx context.Context) context.Context {
	if a.Def.ID != "" {
		ctx = logger.WithActorID(ctx, a.Def.ID)
	}
	return ctx
}

// newTraceID 返回一个 8 字节 hex 编码的随机 trace ID（16 字符）
func newTraceID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
