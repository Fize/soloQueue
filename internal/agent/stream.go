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
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── runOnceStream ──────────────────────────────────────────────────────────

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
			Model:           a.Def.ModelID,
			Temperature:     a.Def.Temperature,
			MaxTokens:       a.Def.MaxTokens,
			Messages:        msgs,
			Tools:           specs,
			ReasoningEffort: a.Def.ReasoningEffort,
			ThinkingEnabled: a.Def.ThinkingEnabled,
			ThinkingType:    a.Def.ThinkingType,
			IncludeUsage:    true,
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
			a.emit(ctx, out, DoneEvent{Content: content.String(), ReasoningContent: reasoning.String()})
			return
		}

		// 追加 assistant(tool_calls) 消息到对话历史
		msgs = append(msgs, LLMMessage{
			Role:             "assistant",
			Content:          content.String(),
			ReasoningContent: reasoning.String(),
			ToolCalls:        toolCalls,
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

// runOnceStreamWithHistory 是 AskWithHistory 的执行主体
//
// 与 runOnceStream 的关键区别：
//   - 使用 cw.BuildPayload() 代替 buildMessages()
//   - 每轮 API 返回后先 Calibrate 再 Push（严格时序）
//   - API 请求前检查 Overflow
//   - 工具循环中 push 中间消息到 cw
//   - 最终 assistant 回复不 push（由 Session 负责）
func (a *Agent) runOnceStreamWithHistory(ctx context.Context, cw *ctxwin.ContextWindow, prompt string, out chan<- AgentEvent) {
	defer close(out)
	defer func() {
		if r := recover(); r != nil {
			a.emit(ctx, out, ErrorEvent{
				Err: fmt.Errorf("agent panic: %v", r),
			})
			panic(r)
		}
	}()

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

	for iter := 0; iter < maxIter; iter++ {
		if err := ctx.Err(); err != nil {
			a.emit(ctx, out, ErrorEvent{Err: err})
			return
		}

		// ★ 从 ContextWindow 构建 payload（包含完整历史）
		payload := cw.BuildPayload()
		msgs := payloadToLLMMessages(payload)

		// ★ Overflow 硬限检查：防止 API 400
		hardLimit := a.Def.ContextWindow
		if hardLimit <= 0 {
			hardLimit = 128000 // 兜底默认值
		}
		if cw.Overflow(hardLimit) {
			current, _, _ := cw.TokenUsage()
			a.logError(ctx, logger.CatLLM, "context overflow", fmt.Errorf("tokens %d exceed hard limit %d", current, hardLimit))
			a.emit(ctx, out, ErrorEvent{
				Err: fmt.Errorf("context overflow: current %d tokens exceed hard limit %d, please start a new session", current, hardLimit),
			})
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
			ThinkingType:    a.Def.ThinkingType,
			IncludeUsage:    true,
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
			tcSlots   = map[int]*llm.ToolCall{}
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

		// ★ 严格时序：先 Calibrate（对齐到 API 精确值），再 Push 新消息
		if usage.PromptTokens > 0 {
			cw.Calibrate(usage.PromptTokens)
		}

		// IterationDoneEvent
		if !a.emit(ctx, out, IterationDoneEvent{
			Iter:         iter,
			FinishReason: finish,
			Usage:        usage,
		}) {
			return
		}

		// 退出条件：LLM 不再要工具
		if len(toolCalls) == 0 {
			a.emit(ctx, out, DoneEvent{Content: content.String(), ReasoningContent: reasoning.String()})
			return
		}

		// ★ Push assistant(tool_calls) 到 ContextWindow
		cw.Push(ctxwin.RoleAssistant, content.String(),
			ctxwin.WithReasoningContent(reasoning.String()),
			ctxwin.WithToolCalls(toolCalls),
		)

	// 执行本轮所有 tool_call
	results := a.execToolsWithAsync(ctx, iter, toolCalls, out, cw)

	// 检查是否有异步委托（本轮 tool loop 需要暂停）
	a.turnMu.RLock()
	_, hasAsync := a.asyncTurns[iter]
	a.turnMu.RUnlock()

	if hasAsync {
		// 异步路径：
		// - assistant(tool_calls) 已 push 到 cw（上面已做）
		// - tool result 不 push（等结果回来再 push）
		// - out 不 close（由 resumeTurn 最终 close）
		// - 发射 DelegationStartedEvent
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
		return // ← 释放 run goroutine，L1 可处理新消息
	}

	// 同步路径：push tool results 到 cw
	for i, tc := range toolCalls {
		cw.Push(ctxwin.RoleTool, results[i],
			ctxwin.WithToolCallID(tc.ID),
			ctxwin.WithToolName(tc.Function.Name),
			ctxwin.WithEphemeral(true),
		)
	}
}

	// 迭代超上限
	a.logError(ctx, logger.CatLLM, "max tool iterations exceeded", ErrMaxIterations,
		slog.Int("max_iter", maxIter),
	)
	a.emit(ctx, out, ErrorEvent{Err: ErrMaxIterations})
}

// runOnceStreamWithHistoryFromIter 从指定 iter 开始继续工具循环
//
// 由 resumeTurn 调用，复用 runOnceStreamWithHistory 的循环体。
func (a *Agent) runOnceStreamWithHistoryFromIter(
	ctx context.Context,
	cw *ctxwin.ContextWindow,
	out chan<- AgentEvent,
	startIter int,
) {
	defer close(out)
	defer func() {
		if r := recover(); r != nil {
			a.emit(ctx, out, ErrorEvent{
				Err: fmt.Errorf("agent panic: %v", r),
			})
			panic(r)
		}
	}()

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

		// 从 ContextWindow 构建 payload
		payload := cw.BuildPayload()
		msgs := payloadToLLMMessages(payload)

		// Overflow 硬限检查
		hardLimit := a.Def.ContextWindow
		if hardLimit <= 0 {
			hardLimit = 128000
		}
		if cw.Overflow(hardLimit) {
			current, _, _ := cw.TokenUsage()
			a.logError(ctx, logger.CatLLM, "context overflow", fmt.Errorf("tokens %d exceed hard limit %d", current, hardLimit))
			a.emit(ctx, out, ErrorEvent{
				Err: fmt.Errorf("context overflow: current %d tokens exceed hard limit %d, please start a new session", current, hardLimit),
			})
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
			ThinkingType:    a.Def.ThinkingType,
			IncludeUsage:    true,
		}

		a.logInfo(ctx, logger.CatLLM, "llm chat start",
			slog.Int("iter", iter),
			slog.String("model", req.Model),
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

		var (
			content   strings.Builder
			reasoning strings.Builder
			tcSlots   = map[int]*llm.ToolCall{}
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

		// 严格时序：先 Calibrate，再 Push
		if usage.PromptTokens > 0 {
			cw.Calibrate(usage.PromptTokens)
		}

		// IterationDoneEvent
		if !a.emit(ctx, out, IterationDoneEvent{
			Iter:         iter,
			FinishReason: finish,
			Usage:        usage,
		}) {
			return
		}

		// 退出条件：LLM 不再要工具
		if len(toolCalls) == 0 {
			a.emit(ctx, out, DoneEvent{Content: content.String(), ReasoningContent: reasoning.String()})
			return
		}

		// Push assistant(tool_calls) 到 ContextWindow
		cw.Push(ctxwin.RoleAssistant, content.String(),
			ctxwin.WithReasoningContent(reasoning.String()),
			ctxwin.WithToolCalls(toolCalls),
		)

		// 执行本轮所有 tool_call
		results := a.execToolsWithAsync(ctx, iter, toolCalls, out, cw)

		// 检查是否有异步委托
		a.turnMu.RLock()
		_, hasAsync := a.asyncTurns[iter]
		a.turnMu.RUnlock()

		if hasAsync {
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
			return
		}

		// 同步路径：push tool results
		for i, tc := range toolCalls {
			cw.Push(ctxwin.RoleTool, results[i],
				ctxwin.WithToolCallID(tc.ID),
				ctxwin.WithToolName(tc.Function.Name),
				ctxwin.WithEphemeral(true),
			)
		}
	}

	// 迭代超上限
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
