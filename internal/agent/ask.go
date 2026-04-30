package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Ask / Submit ─────────────────────────────────────────────────────────--

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
	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "ask: starting synchronous ask",
			"agent_id", a.Def.ID,
			"prompt_len", len(prompt),
		)
	}

	ch, err := a.AskStream(ctx, prompt)
	if err != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "ask: askstream failed",
				"agent_id", a.Def.ID,
				"err", err.Error(),
			)
		}
		return "", err
	}

	var (
		b            strings.Builder
		finalContent string
		finalErr     error
		eventCount   int
	)
	for ev := range ch {
		eventCount++
		switch e := ev.(type) {
		case ContentDeltaEvent:
			b.WriteString(e.Delta)
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			finalErr = e.Err
		}
	}

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "ask: event stream consumed",
			"agent_id", a.Def.ID,
			"events_received", eventCount,
			"final_content_len", len(finalContent),
			"has_error", finalErr != nil,
		)
	}

	if finalErr != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "ask: finished with error",
				"agent_id", a.Def.ID,
				"err", finalErr.Error(),
			)
		}
		return "", finalErr
	}
	if finalContent != "" {
		if a.Log != nil {
			a.Log.InfoContext(ctx, logger.CatActor, "ask: completed successfully",
				"agent_id", a.Def.ID,
				"content_len", len(finalContent),
				"events_received", eventCount,
			)
		}
		return finalContent, nil
	}
	if a.Log != nil {
		a.Log.InfoContext(ctx, logger.CatActor, "ask: completed successfully (from buffer)",
			"agent_id", a.Def.ID,
			"content_len", b.Len(),
			"events_received", eventCount,
		)
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

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "askstream: enqueueing request",
			"agent_id", a.Def.ID,
			"prompt_len", len(prompt),
		)
	}

	// buffer 64：能缓冲单轮典型的 delta 风暴；满了阻塞（不丢事件）+ ctx 兜底
	out := make(chan AgentEvent, 64)

	jb := func(jobCtx context.Context) {
		// 合并 caller ctx（带 trace_id）和 agent jobCtx（Stop 时 cancel）
		// ctx 放前面是关键：合并后 ctx 的 value（trace_id / actor_id）仍可读
		merged, cancel := mergeCtx(ctx, jobCtx)
		defer cancel()

		if a.Log != nil {
			a.Log.DebugContext(merged, logger.CatActor, "askstream: execution starting",
				"agent_id", a.Def.ID,
			)
		}

		a.runOnceStream(merged, prompt, out)

		if a.Log != nil {
			a.Log.DebugContext(merged, logger.CatActor, "askstream: execution completed",
				"agent_id", a.Def.ID,
			)
		}
	}

	if err := a.submit(ctx, jb); err != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "askstream: submit failed",
				"agent_id", a.Def.ID,
				"err", err.Error(),
			)
		}
		// submit 失败（ErrNotStarted / ErrStopped / ctx.Err）→ 关闭 out 后返回 err
		// 关闭是为了防止 caller 误以为 channel 还会有事件来而悬挂
		close(out)
		return nil, err
	}

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "askstream: request enqueued successfully",
			"agent_id", a.Def.ID,
		)
	}

	return out, nil
}

// Submit sends an arbitrary job to the agent's mailbox.
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

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "submit: enqueueing custom job",
			"agent_id", a.Def.ID,
		)
	}

	jb := func(jobCtx context.Context) {
		// 把 trace_id / actor_id 拷到 jobCtx（actor_id 已由 Start 注入 a.ctx）
		// jobCtx 源自 a.ctx，所以 actor_id 已有；trace_id 从 caller ctx 补上
		fnCtx := jobCtx
		if traceID != "" {
			fnCtx = logger.WithTraceID(fnCtx, traceID)
		}

		if a.Log != nil {
			a.Log.DebugContext(fnCtx, logger.CatActor, "submit: job execution starting",
				"agent_id", a.Def.ID,
			)
		}

		if err := fn(fnCtx); err != nil {
			a.logError(fnCtx, logger.CatActor, "submit job returned error", err)
		}

		if a.Log != nil {
			a.Log.DebugContext(fnCtx, logger.CatActor, "submit: job execution completed",
				"agent_id", a.Def.ID,
			)
		}
	}

	if err := a.submit(ctx, jb); err != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "submit: failed to enqueue",
				"agent_id", a.Def.ID,
				"err", err.Error(),
			)
		}
		return err
	}

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "submit: job enqueued successfully",
			"agent_id", a.Def.ID,
		)
	}

	return nil
}

// submit 是 Ask / Submit 共享的入队实现
func (a *Agent) submit(ctx context.Context, jb job) error {
	a.mu.Lock()
	mailbox := a.mailbox
	pm := a.priorityMailbox
	agentDone := a.done
	a.mu.Unlock()

	if agentDone == nil {
		return ErrNotStarted
	}

	// 快速路径：agent 已退出
	select {
	case <-agentDone:
		return ErrStopped
	default:
	}

	// 使用 PriorityMailbox（L1 模式）
	if pm != nil {
		pm.SubmitNormal(jb)
		return nil
	}

	// 使用普通 mailbox（L2/L3 模式）
	if mailbox == nil {
		return ErrNotStarted
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

// submitHighPriority 投递高优先级 job（委托回传、超时事件）
//
// 仅当 Agent 启用了 PriorityMailbox 时有效。
// 异步委托结果通过此路径投递，确保不被普通用户消息阻塞。
func (a *Agent) submitHighPriority(jb job) error {
	a.mu.Lock()
	pm := a.priorityMailbox
	agentDone := a.done
	a.mu.Unlock()

	if agentDone == nil {
		return ErrNotStarted
	}

	if pm != nil {
		pm.SubmitHigh(jb)
		return nil
	}

	// 未启用 PriorityMailbox：降级为普通 submit
	return a.submit(context.Background(), jb)
}

// ─── AskWithHistory / AskStreamWithHistory ──────────────────────────────────

// AskWithHistory 向 agent 投递一次带有上下文历史的 LLM 请求并等结果
//
// 与 Ask 不同的是，此方法使用 ContextWindow 提供完整对话历史，
// 并在工具循环中将中间消息 push 到 ContextWindow。
// 返回 content 和 reasoningContent（DeepSeek thinking mode 跨轮必须回传）。
//
// ⚠️ 调用方（通常是 Session）应在调用前将 user prompt push 到 cw，
// 调用成功后将 assistant reply（含 reasoningContent）push 到 cw，失败时 PopLast 移除 user prompt。
func (a *Agent) AskWithHistory(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (content string, reasoningContent string, err error) {
	if a.Log != nil {
		ctxCurrent, _, _ := cw.TokenUsage()
		a.Log.DebugContext(ctx, logger.CatActor, "ask_with_history: starting with context window",
			"agent_id", a.Def.ID,
			"prompt_len", len(prompt),
			"context_window_tokens", ctxCurrent,
			"context_window_messages", cw.Len(),
		)
	}

	ch, err := a.AskStreamWithHistory(ctx, cw, prompt)
	if err != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "ask_with_history: askstreamwithhistory failed",
				"agent_id", a.Def.ID,
				"err", err.Error(),
			)
		}
		return "", "", err
	}

	var (
		b              strings.Builder
		finalContent   string
		finalReasoning string
		finalErr       error
		eventCount     int
	)
	for ev := range ch {
		eventCount++
		switch e := ev.(type) {
		case ContentDeltaEvent:
			b.WriteString(e.Delta)
		case DoneEvent:
			finalContent = e.Content
			finalReasoning = e.ReasoningContent
		case ErrorEvent:
			finalErr = e.Err
		}
	}

	if a.Log != nil {
		ctxCurrent, _, _ := cw.TokenUsage()
		a.Log.DebugContext(ctx, logger.CatActor, "ask_with_history: event stream consumed",
			"agent_id", a.Def.ID,
			"events_received", eventCount,
			"final_content_len", len(finalContent),
			"has_reasoning", len(finalReasoning) > 0,
			"has_error", finalErr != nil,
			"context_window_tokens_final", ctxCurrent,
			"context_window_messages_final", cw.Len(),
		)
	}

	if finalErr != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "ask_with_history: finished with error",
				"agent_id", a.Def.ID,
				"err", finalErr.Error(),
			)
		}
		return "", "", finalErr
	}
	if finalContent != "" {
		if a.Log != nil {
			a.Log.InfoContext(ctx, logger.CatActor, "ask_with_history: completed successfully",
				"agent_id", a.Def.ID,
				"content_len", len(finalContent),
				"reasoning_len", len(finalReasoning),
				"events_received", eventCount,
			)
		}
		return finalContent, finalReasoning, nil
	}
	if a.Log != nil {
		a.Log.InfoContext(ctx, logger.CatActor, "ask_with_history: completed successfully (from buffer)",
			"agent_id", a.Def.ID,
			"content_len", b.Len(),
			"reasoning_len", len(finalReasoning),
			"events_received", eventCount,
		)
	}
	return b.String(), finalReasoning, nil
}

// AskStreamWithHistory 投递一次带有上下文历史的流式 Ask
//
// 返回通道由 agent goroutine 内部的 runOnceStreamWithHistory close。
func (a *Agent) AskStreamWithHistory(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (<-chan AgentEvent, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = ensureTraceID(ctx)
	ctx = a.ctxWithAgentAttrs(ctx)

	if a.Log != nil {
		ctxCurrent, _, _ := cw.TokenUsage()
		a.Log.DebugContext(ctx, logger.CatActor, "askstreamwithhistory: enqueueing request with context",
			"agent_id", a.Def.ID,
			"prompt_len", len(prompt),
			"context_window_tokens", ctxCurrent,
			"context_window_messages", cw.Len(),
		)
	}

	out := make(chan AgentEvent, 64)

	jb := func(jobCtx context.Context) {
		merged, cancel := mergeCtx(ctx, jobCtx)

		if a.Log != nil {
			a.Log.DebugContext(merged, logger.CatActor, "askstreamwithhistory: execution starting",
				"agent_id", a.Def.ID,
			)
		}

		yielded := a.runOnceStreamWithHistory(merged, cw, prompt, out)

		if a.Log != nil {
			a.Log.DebugContext(merged, logger.CatActor, "askstreamwithhistory: execution completed",
				"agent_id", a.Def.ID,
				"async_delegation_started", yielded,
			)
		}

		if yielded {
			// streamLoop yielded for async delegation — merged ctx must
			// stay alive because resumeTurn (and the subsequent streamLoop)
			// still need it. Save the cancel into the asyncTurnState so
			// resumeTurn can call it after the final streamLoop completes.
			a.saveAsyncCancel(merged, cancel)
		} else {
			cancel()
		}
	}

	if err := a.submit(ctx, jb); err != nil {
		if a.Log != nil {
			a.Log.WarnContext(ctx, logger.CatActor, "askstreamwithhistory: submit failed",
				"agent_id", a.Def.ID,
				"err", err.Error(),
			)
		}
		close(out)
		return nil, err
	}

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "askstreamwithhistory: request enqueued successfully",
			"agent_id", a.Def.ID,
		)
	}

	return out, nil
}
