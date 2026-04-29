package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Context Keys for Tool Execution ───────────────────────────────────────
//
// 注意：toolEventChannelCtxKey 和 confirmForwarderCtxKey 统一定义在 tools 包中，
// 由 tools.WithToolEventChannel / tools.WithConfirmForwarder 提供注入。
// agent 包通过这些导出 helper 使用，避免跨包 context key 类型不匹配。

// ─── Stream Event Accumulator ───────────────────────────────────────────────
//
// streamAccumulator holds accumulated state during a single LLM streaming
// iteration. Used to eliminate 123-line duplication across runOnceStream,
// runOnceStreamWithHistory, and runOnceStreamWithHistoryFromIter.
type streamAccumulator struct {
	content   strings.Builder
	reasoning strings.Builder
	tcSlots   map[int]*llm.ToolCall // by ToolCallDelta.Index
	finish    llm.FinishReason
	usage     llm.Usage
}

// processStreamEvents processes a single LLM streaming event channel and
// accumulates response into acc. This extracts the 123-line identical
// event processing loop from all 3 run* functions.
//
// Returns after stream is exhausted (normally or on error).
// On context cancel or LLM error, emits ErrorEvent before returning.
func (a *Agent) processStreamEvents(
	ctx context.Context,
	iter int,
	evCh <-chan llm.Event,
	out chan<- AgentEvent,
	start time.Time,
	acc *streamAccumulator,
) error {
	streamDone := false
	for !streamDone {
		select {
		case <-ctx.Done():
			a.emit(ctx, out, ErrorEvent{Err: ctx.Err()})
			return ctx.Err()
		case ev, ok := <-evCh:
			if !ok {
				// channel close but no Done/Error — treat as abnormal end
				streamDone = true
				break
			}
			switch ev.Type {
			case llm.EventDelta:
				if ev.ContentDelta != "" {
					acc.content.WriteString(ev.ContentDelta)
					if !a.emit(ctx, out, ContentDeltaEvent{
						Iter: iter, Delta: ev.ContentDelta,
					}) {
					return ctx.Err()
					}
				}
				if ev.ReasoningContentDelta != "" {
					acc.reasoning.WriteString(ev.ReasoningContentDelta)
					if !a.emit(ctx, out, ReasoningDeltaEvent{
						Iter: iter, Delta: ev.ReasoningContentDelta,
					}) {
						return ctx.Err()
					}
				}
				if ev.ToolCallDelta != nil {
					d := ev.ToolCallDelta
					accumulateToolCall(acc.tcSlots, d)
					if !a.emit(ctx, out, ToolCallDeltaEvent{
						Iter:      iter,
						CallID:    d.ID,
						Name:      d.Name,
						ArgsDelta: d.Arguments,
					}) {
						return ctx.Err()
					}
				}
			case llm.EventDone:
				acc.finish = ev.FinishReason
				if ev.Usage != nil {
					acc.usage = *ev.Usage
				}
				streamDone = true
			case llm.EventError:
				durMs := time.Since(start).Milliseconds()
				a.logError(ctx, logger.CatLLM, "llm chat failed", ev.Err,
					slog.Int("iter", iter),
					slog.Int64("duration_ms", durMs),
				)
				a.emit(ctx, out, ErrorEvent{Err: ev.Err})
				return ev.Err
			}
		}
	}
	return nil
}

// --- Strategy pattern for stream loop deduplication ---
//
// The three run* functions (runOnceStream, runOnceStreamWithHistory,
// runOnceStreamWithHistoryFromIter) share ~90% identical code. The only
// differences are:
//   - How messages are built (in-memory vs ContextWindow)
//   - Whether overflow checking / calibration applies
//   - Which execTools variant is called (sync vs async-aware)
//   - How tool results are stored (in-memory msgs vs ContextWindow push)
//
// streamStrategy encapsulates these differences so streamLoop contains
// the shared logic exactly once.

// streamStrategy defines the variant behavior injected into streamLoop.
type streamStrategy interface {
	// buildMessages returns the LLM messages for this iteration.
	// Returns an error for overflow or other pre-flight failures.
	buildMessages(a *Agent, iter int) ([]LLMMessage, error)

	// execTools runs all tool calls and returns per-call result strings.
	execTools(a *Agent, ctx context.Context, iter int, calls []llm.ToolCall, out chan<- AgentEvent) []string

	// postIteration handles post-LLM-response processing: calibration,
	// context window push, async delegation check, tool result storage.
	// Returns true if the loop should yield (async delegation started).
	postIteration(a *Agent, ctx context.Context, iter int, acc *streamAccumulator, calls []llm.ToolCall, results []string, out chan<- AgentEvent) (yield bool)

	// promptLen returns the original prompt length for logging.
	promptLen() int
}

// recoverAndEmit catches panics, emits ErrorEvent, and re-panics.
// Used as a deferred call in streamLoop to ensure errors are always
// reported before the channel closes.
func (a *Agent) recoverAndEmit(ctx context.Context, out chan<- AgentEvent) {
	if r := recover(); r != nil {
		a.emit(ctx, out, ErrorEvent{
			Err: fmt.Errorf("agent panic: %v", r),
		})
		panic(r) // propagate to run goroutine's outer recover
	}
}

// streamLoop is the unified LLM tool-use loop.
//
// GOROUTINE SAFETY CONTRACT:
// The caller MUST either:
//  1. Consume the out channel until close (range), OR
//  2. Cancel ctx before abandoning the channel
//
// If ctx is cancelled, emit() returns false and streamLoop exits,
// triggering defer close(out). The 64-slot buffer absorbs transient
// backpressure.
func (a *Agent) streamLoop(ctx context.Context, out chan<- AgentEvent, strat streamStrategy, startIter int) (yielded bool) {
	// Only close out when the stream finishes normally (yielded == false).
	// When async delegation starts, streamLoop yields (returns true) and out
	// must stay open so resumeTurn can continue writing events and launch the
	// next streamLoop call which will close out on its final exit.
	defer func() {
		if !yielded {
			close(out)
		}
	}()
	defer a.recoverAndEmit(ctx, out)

	if a.LLM == nil {
		a.emit(ctx, out, ErrorEvent{
			Err: fmt.Errorf("agent %q: llm client is nil", a.Def.ID),
		})
		return
	}

	specs := a.ToolSpecs()

	maxIter := a.Def.MaxIterations
	if maxIter <= 0 {
		maxIter = DefaultMaxIterations
	}

	for iter := startIter; iter < maxIter; iter++ {
		if err := ctx.Err(); err != nil {
			a.emit(ctx, out, ErrorEvent{Err: err})
			return
		}

		msgs, err := strat.buildMessages(a, iter)
		if err != nil {
			a.logError(ctx, logger.CatLLM, "build messages failed", err,
				slog.Int("iter", iter),
			)
			a.emit(ctx, out, ErrorEvent{Err: err})
			return
		}

		req := LLMRequest{
			Model:           a.Def.ModelID,
			Temperature:     a.Def.Temperature,
			MaxTokens:       a.Def.MaxTokens,
			Messages:        msgs,
			Tools:           specs,
			ReasoningEffort: a.Def.ReasoningEffort,
			ThinkingEnabled: a.Def.ThinkingEnabled,
			IncludeUsage:    true,
		}

		a.logInfo(ctx, logger.CatLLM, "llm chat start",
			slog.Int("iter", iter),
			slog.String("model", req.Model),
			slog.Int("prompt_len", strat.promptLen()),
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

		acc := &streamAccumulator{
			tcSlots: make(map[int]*llm.ToolCall),
		}
		if err := a.processStreamEvents(ctx, iter, evCh, out, start, acc); err != nil {
			return
		}

		durMs := time.Since(start).Milliseconds()
		toolCalls := sortedToolCalls(acc.tcSlots)

		a.logInfo(ctx, logger.CatLLM, "llm chat done",
			slog.Int("iter", iter),
			slog.Int("response_len", acc.content.Len()),
			slog.Int("reasoning_len", acc.reasoning.Len()),
			slog.Int("tool_calls", len(toolCalls)),
			slog.String("finish_reason", string(acc.finish)),
			slog.Int("prompt_tokens", acc.usage.PromptTokens),
			slog.Int("completion_tokens", acc.usage.CompletionTokens),
			slog.Int("total_tokens", acc.usage.TotalTokens),
			slog.Int64("duration_ms", durMs),
		)

		if !a.emit(ctx, out, IterationDoneEvent{
			Iter:         iter,
			FinishReason: acc.finish,
			Usage:        acc.usage,
		}) {
			return
		}

		// Exit: LLM produced no tool calls → return final content
		if len(toolCalls) == 0 {
			a.emit(ctx, out, DoneEvent{
				Content:          acc.content.String(),
				ReasoningContent: acc.reasoning.String(),
			})
			return
		}

		results := strat.execTools(a, ctx, iter, toolCalls, out)
		if strat.postIteration(a, ctx, iter, acc, toolCalls, results, out) {
			return true // async delegation started, loop yields — out stays open
		}
	}

	// Max iterations exceeded
	a.logError(ctx, logger.CatLLM, "max tool iterations exceeded", ErrMaxIterations,
		slog.Int("max_iter", maxIter),
	)
	a.emit(ctx, out, ErrorEvent{Err: ErrMaxIterations})
	return false
}

// --- simpleStrategy: in-memory message accumulation (runOnceStream) ---

type simpleStrategy struct {
	systemPrompt string
	prompt       string
	msgs         []LLMMessage // accumulated across iterations
}

func (s *simpleStrategy) buildMessages(a *Agent, iter int) ([]LLMMessage, error) {
	if iter == 0 {
		s.msgs = buildMessages(s.systemPrompt, s.prompt)
	}
	return s.msgs, nil
}

func (s *simpleStrategy) execTools(a *Agent, ctx context.Context, iter int, calls []llm.ToolCall, out chan<- AgentEvent) []string {
	return a.execTools(ctx, iter, calls, out)
}

func (s *simpleStrategy) postIteration(a *Agent, ctx context.Context, iter int, acc *streamAccumulator, calls []llm.ToolCall, results []string, out chan<- AgentEvent) bool {
	// Append assistant + tool results to in-memory message list
	s.msgs = append(s.msgs, LLMMessage{
		Role:             "assistant",
		Content:          acc.content.String(),
		ReasoningContent: acc.reasoning.String(),
		ToolCalls:        calls,
	})
	for i, tc := range calls {
		s.msgs = append(s.msgs, LLMMessage{
			Role:       "tool",
			ToolCallID: tc.ID,
			Name:       tc.Function.Name,
			Content:    results[i],
		})
	}
	return false
}

func (s *simpleStrategy) promptLen() int { return len(s.prompt) }

// --- historyStrategy: ContextWindow-based (runOnceStreamWithHistory) ---

type historyStrategy struct {
	cw     *ctxwin.ContextWindow
	prompt string
}

func (s *historyStrategy) buildMessages(a *Agent, iter int) ([]LLMMessage, error) {
	// Overflow hard-limit check: prevent API 400
	hardLimit := a.Def.ContextWindow
	if hardLimit <= 0 {
		hardLimit = DefaultContextWindow // fallback default
	}
	if s.cw.Overflow(hardLimit) {
		current, _, _ := s.cw.TokenUsage()
		return nil, fmt.Errorf("context overflow: current %d tokens exceed hard limit %d, please start a new session", current, hardLimit)
	}
	payload := s.cw.BuildPayload()
	return payloadToLLMMessages(payload), nil
}

func (s *historyStrategy) execTools(a *Agent, ctx context.Context, iter int, calls []llm.ToolCall, out chan<- AgentEvent) []string {
	return a.execToolsWithAsync(ctx, iter, calls, out, s.cw)
}

func (s *historyStrategy) postIteration(a *Agent, ctx context.Context, iter int, acc *streamAccumulator, calls []llm.ToolCall, results []string, out chan<- AgentEvent) bool {
	// Strict ordering: calibrate first (align to API exact value), then push
	if acc.usage.PromptTokens > 0 {
		s.cw.Calibrate(acc.usage.PromptTokens)
	}

	// Push assistant(tool_calls) to ContextWindow
	s.cw.Push(ctxwin.RoleAssistant, acc.content.String(),
		ctxwin.WithReasoningContent(acc.reasoning.String()),
		ctxwin.WithToolCalls(calls),
	)

	// Check for async delegation (tool loop must pause)
	a.turnMu.RLock()
	_, hasAsync := a.asyncTurns[iter]
	a.turnMu.RUnlock()

	if hasAsync {
		// Async path:
		// - assistant(tool_calls) already pushed to cw
		// - tool results NOT pushed (wait for async results)
		// - out NOT closed (streamLoop returns yielded=true, resumeTurn → streamLoop will close on final exit)
		// - emit DelegationStartedEvent
		var numTasks int
		a.turnMu.RLock()
		if ts := a.asyncTurns[iter]; ts != nil {
			numTasks = int(ts.pending.Load())
		}
		a.turnMu.RUnlock()
		a.emit(ctx, out, DelegationStartedEvent{
			Iter:     iter,
			NumTasks: numTasks,
		})
		return true // yield — release run goroutine for new messages
	}

	// Sync path: push tool results to ContextWindow
	for i, tc := range calls {
		s.cw.Push(ctxwin.RoleTool, results[i],
			ctxwin.WithToolCallID(tc.ID),
			ctxwin.WithToolName(tc.Function.Name),
			ctxwin.WithEphemeral(true),
		)
	}
	return false
}

func (s *historyStrategy) promptLen() int { return len(s.prompt) }

// --- Public entry points (thin wrappers) ---

// runOnceStream is the execution body of AskStream (runs in the agent
// goroutine). Uses in-memory message accumulation without ContextWindow.
func (a *Agent) runOnceStream(ctx context.Context, prompt string, out chan<- AgentEvent) {
	a.streamLoop(ctx, out, &simpleStrategy{
		systemPrompt: a.Def.SystemPrompt,
		prompt:       prompt,
	}, 0)
}

// runOnceStreamWithHistory is the execution body of AskStreamWithHistory.
// Uses ContextWindow for full conversation history, calibration, and
// async delegation support.
//
// Returns true if the stream loop yielded (async delegation started);
// the caller must keep the context alive until resumeTurn completes.
func (a *Agent) runOnceStreamWithHistory(ctx context.Context, cw *ctxwin.ContextWindow, prompt string, out chan<- AgentEvent) bool {
	return a.streamLoop(ctx, out, &historyStrategy{cw: cw, prompt: prompt}, 0)
}

// runOnceStreamWithHistoryFromIter resumes the tool loop from a given
// iteration. Called by resumeTurn after async delegation completes.
func (a *Agent) runOnceStreamWithHistoryFromIter(
	ctx context.Context,
	cw *ctxwin.ContextWindow,
	out chan<- AgentEvent,
	startIter int,
) {
	a.streamLoop(ctx, out, &historyStrategy{cw: cw}, startIter)
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

	var tool tools.Tool
	var ok bool
	if a.tools != nil {
		tool, ok = a.tools.SafeGet(name)
	}
	if !ok {
		err := fmt.Errorf("%w: %s", tools.ErrToolNotFound, name)
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
	// 若工具实现了 Confirmable：
	//   1. 先查会话级白名单，命中则跳过确认直接注入 confirmed=true；
	//   2. 否则 CheckConfirmation，需要确认时发 ToolNeedsConfirmEvent 并阻塞等待；
	//   3. 用户选择 ChoiceAllowInSession → 加入白名单并按 ChoiceApprove 处理。
	if c, ok := tool.(tools.Confirmable); ok {
		if a.confirmStore.IsConfirmed(name) {
			args = c.ConfirmArgs(args, choiceApprove)
		} else {
			needsConfirm, prompt := c.CheckConfirmation(args)
			if needsConfirm {
				options := c.ConfirmationOptions(args)
				if !a.emit(ctx, out, ToolNeedsConfirmEvent{
					Iter:           iter,
					CallID:         tc.ID,
					Name:           name,
					Args:           args,
					Prompt:         prompt,
					Options:        options,
					AllowInSession: c.SupportsSessionWhitelist(),
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
				cc := confirmChoice(choice)
				if cc == choiceAllowInSession {
					a.confirmStore.Confirm(name)
					cc = choiceApprove
				}
				args = c.ConfirmArgs(args, cc)
			}
		}
	}

	// Apply per-tool timeout (WithToolTimeout), falling back to DefaultToolTimeout.
	// Parent ctx cancellation still takes priority (WithTimeout adds a deadline).
	execCtx := ctx
	var timeoutDur time.Duration
	if d, ok := a.toolTimeouts[name]; ok && d > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
		timeoutDur = d
	} else {
		// Global fallback timeout — prevents indefinite blocking
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, DefaultToolTimeout)
		defer cancel()
		timeoutDur = DefaultToolTimeout
	}

	start := time.Now()

	// Build tool execution context: inject event relay + confirm routing.
	//
	// 1. Create typed relay channel for child agent events
	// 2. Start relay goroutine: filter ToolNeedsConfirmEvent and emit to parent
	// 3. Inject ConfirmForwarder closure: registers proxy slot on this agent,
	//    blocks until user confirms, then forwards to child
	relayCh := make(chan iface.AgentEvent, 16)
	toolCtx := tools.WithToolEventChannel(execCtx, relayCh)

	// Inject confirm forwarder: when DelegateTool sees a ToolNeedsConfirmEvent
	// from the child stream, it invokes this closure to route the request.
	forwarder := iface.ConfirmForwarder(func(fwdCtx context.Context, callID string, child iface.Locatable) (string, error) {
		// Register proxy confirmSlot on this agent (L2)
		slot := &confirmSlot{ch: make(chan string, 1)}
		a.confirmMu.Lock()
		a.pendingConfirm[callID] = slot
		a.confirmMu.Unlock()

		defer func() {
			a.confirmMu.Lock()
			delete(a.pendingConfirm, callID)
			a.confirmMu.Unlock()
		}()

		// Block until TUI calls L2.Confirm(callID, choice) → slot.ch receives choice
		select {
		case choice := <-slot.ch:
			// Forward to child agent's (L3) original confirmSlot
			if err := child.Confirm(callID, choice); err != nil {
				return "", err
			}
			return choice, nil
		case <-fwdCtx.Done():
			return "", fwdCtx.Err()
		}
	})
	toolCtx = tools.WithConfirmForwarder(toolCtx, forwarder)

	// Start relay goroutine: only forward ToolNeedsConfirmEvent to parent.
	//
	// IMPORTANT: Only relay ToolNeedsConfirmEvent! Relaying ContentDeltaEvent
	// or DoneEvent would pollute the parent's out channel, causing Session
	// to mistake L3's DoneEvent for L2's → ContextWindow sequence corruption
	// → LLM API HTTP 400.
	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		for ev := range relayCh {
			if _, isConfirm := ev.(ToolNeedsConfirmEvent); isConfirm {
				a.emit(ctx, out, ev.(AgentEvent))
			}
		}
	}()

	result, err := tool.Execute(toolCtx, args)
	close(relayCh) // signal relay goroutine to exit
	<-relayDone    // wait for relay to drain all events
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
