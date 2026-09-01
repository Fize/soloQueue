package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

// --- Ask / Submit -----------------------------------------------------------

// Ask delivers an LLM request to the agent and waits for the result.
//
// Behavior: Internally uses AskStream to accumulate all events → returns final content + the first error.
//   - Delivery phase: If mailbox is full, blocks until a slot is available / ctx cancelled / agent exits.
//   - Execution phase: Job executes serially in the agent goroutine (only one job processed at a time).
//   - Cancellation: Either caller ctx or agent ctx cancellation will interrupt an in-progress LLM call.
//
// Errors:
//   - ErrNotStarted: Agent not started.
//   - ErrStopped: Agent exited during delivery or while waiting.
//   - ctx.Err(): Caller explicitly cancelled.
//   - LLM-returned error is passed through.
//
// Backward compatibility: Signature remains unchanged, all existing calls continue to work; however, the internal path changes from
// "runOnce synchronous Chat" to "runOnceStream consuming event stream".
func (a *Agent) Ask(ctx context.Context, prompt string) (string, error) {
	if a.Log != nil {
		a.Log.InfoContext(ctx, logger.CatActor, "ask: starting synchronous ask",
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

// AskStream delivers a streaming Ask request and immediately returns an event channel.
//
// The returned channel is closed by runOnceStream within the agent goroutine.
// The caller must continuously range until the channel is closed; abandoning the range midway will trigger backpressure
// (runOnceStream blocks when sending events), so the ctx must be cancelled before abandoning.
//
// Errors:
//   - ErrNotStarted / ErrStopped: Returns (nil, err) directly if enqueueing fails.
//   - Errors after enqueueing: Delivered via ErrorEvent (at this point, the non-nil channel can still be ranged).
func (a *Agent) AskStream(ctx context.Context, prompt string) (<-chan AgentEvent, error) {
	out, _, err := a.AskStreamTracked(ctx, prompt)
	return out, err
}

// AskStreamTracked is the request-scoped form of AskStream. The returned
// handle lets the owning Session fence only the request that tripped a
// watchdog, without confusing it with a later mailbox job.
func (a *Agent) AskStreamTracked(ctx context.Context, prompt string) (<-chan AgentEvent, *JobHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Injects trace_id (uses existing one if present, generates new one if not) + actor_id, for full-link logging extraction.
	ctx = ensureTraceID(ctx)
	ctx = a.ctxWithAgentAttrs(ctx)
	// Freeze a compatibility/global override into the request context at the
	// submission boundary. Execution reads the request-scoped value, so an
	// async yield or a later request cannot change this job's route.
	if iface.ModelOverrideFromContext(ctx) == nil {
		if override := a.modelOverride.Load(); override != nil {
			ctx = iface.ContextWithModelOverride(ctx, override.ToIFaceOverride())
			a.modelOverride.CompareAndSwap(override, nil)
		}
	}
	tracker := &jobTracker{id: a.jobSeq.Add(1), done: make(chan struct{}), override: modelOverrideForContext(ctx, nil)}
	ctx = withJobTracker(ctx, tracker)
	handle := &JobHandle{agent: a, tracker: tracker}

	if a.Log != nil {
		a.Log.InfoContext(ctx, logger.CatActor, "askstream: enqueueing request",
			"agent_id", a.Def.ID,
			"prompt_len", len(prompt),
		)
	}

	// Buffer 64: can buffer a typical delta storm for a single turn; if full, blocks (events are not lost) + ctx fallback.
	out := make(chan AgentEvent, 64)

	jb := func(jobCtx context.Context) {
		// Merge caller ctx (with trace_id) and agent jobCtx (cancelled on Stop).
		// Putting ctx first is crucial: the merged ctx's values (trace_id / actor_id) remain readable.
		merged, cancel := mergeCtx(ctx, jobCtx)
		defer cancel()

		a.taskCancel.Store(cancel)
		defer a.taskCancel.Store(context.CancelFunc(func() {}))

		if a.Log != nil {
			a.Log.DebugContext(merged, logger.CatActor, "askstream: execution starting",
				"agent_id", a.Def.ID,
			)
		}

		a.beginTrackedJob(tracker)
		defer tracker.finish()
		defer a.endTrackedJob(tracker)
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
		// If submit fails (ErrNotStarted / ErrStopped / ctx.Err) → close 'out' and return err.
		// Closing is to prevent the caller from mistakenly thinking the channel will still have events and hanging.
		close(out)
		tracker.finish()
		return nil, nil, err
	}

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "askstream: request enqueued successfully",
			"agent_id", a.Def.ID,
		)
	}

	return out, handle, nil
}

// Submit sends an arbitrary job to the agent's mailbox.
//
// fn receives the agent's ctx (which will be cancelled on Stop).
// Submit only waits for enqueueing, not for fn to complete; returns nil on successful enqueue.
// To wait for results synchronously, use Ask; or use the caller's channel inside fn.
//
// Caller ctx semantics:
//   - Only controls "enqueue waiting": if the mailbox is full, cancellation of the caller ctx will cause Submit to return ctx.Err().
//   - Does not control fn execution: fn execution is fully controlled by the agent ctx (cancelled on Stop).
//   - trace_id / actor_id are copied from caller ctx to fn ctx to maintain log traceability across goroutines.
//
// Errors:
//   - ErrNotStarted / ErrStopped
//   - ctx.Err(): Caller cancelled during enqueue waiting.
func (a *Agent) Submit(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return fmt.Errorf("agent: nil fn")
	}
	// Injects trace_id + actor_id (for enqueue waiting logs, and also for copying to fn ctx).
	ctx = ensureTraceID(ctx)
	ctx = a.ctxWithAgentAttrs(ctx)
	traceID := logger.TraceIDFromContext(ctx)

	if a.Log != nil {
		a.Log.InfoContext(ctx, logger.CatActor, "submit: enqueueing custom job",
			"agent_id", a.Def.ID,
		)
	}

	jb := func(jobCtx context.Context) {
		// Copies trace_id / actor_id to jobCtx (actor_id already injected into a.ctx by Start).
		// jobCtx originates from a.ctx, so actor_id is already present; trace_id is added from the caller ctx.
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

// submit is the shared enqueueing implementation for Ask / Submit.
func (a *Agent) submit(ctx context.Context, jb job) error {
	a.scheduleMu.Lock()
	blocked := a.quarantined.Load() || a.recoveryFenced()
	a.scheduleMu.Unlock()
	if blocked {
		return ErrQuarantined
	}
	a.mu.Lock()
	mailbox := a.mailbox
	pm := a.priorityMailbox
	agentDone := a.done
	a.mu.Unlock()

	if agentDone == nil {
		return ErrNotStarted
	}

	// Fast path: agent already exited.
	select {
	case <-agentDone:
		return ErrStopped
	default:
	}

	if pm == nil && mailbox == nil {
		return ErrNotStarted
	}
	// Check the recovery fence and enqueue in one short synchronization
	// domain. A full mailbox releases the lock before waiting, so watchdog
	// fencing can never be delayed by backpressure.
	retry := time.NewTicker(time.Millisecond)
	defer retry.Stop()
	for {
		a.scheduleMu.Lock()
		if a.quarantined.Load() || a.recoveryFenced() {
			a.scheduleMu.Unlock()
			return ErrQuarantined
		}
		enqueued := false
		if pm != nil {
			enqueued = pm.trySubmitNormal(jb)
		} else {
			select {
			case mailbox <- jb:
				enqueued = true
			default:
			}
		}
		a.scheduleMu.Unlock()
		if enqueued {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-agentDone:
			return ErrStopped
		case <-retry.C:
		}
	}
}

// submitHighPriority delivers high-priority jobs (delegation callbacks, timeout events).
//
// Only effective when Agent has PriorityMailbox enabled.
// Asynchronous delegation results are delivered via this path to ensure they are not blocked by normal user messages.
func (a *Agent) submitHighPriority(ctx context.Context, jb job) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	pm := a.priorityMailbox
	agentDone := a.done
	a.mu.Unlock()

	if agentDone == nil {
		return ErrNotStarted
	}

	// Fast path: agent already exited. Without this check, async delegation
	// callbacks (resumeTurn) can be posted to the priority mailbox after the
	// run goroutine has already drained and exited, causing the job to be
	// silently dropped and never executed.
	select {
	case <-agentDone:
		return ErrStopped
	default:
	}

	if pm != nil {
		retry := time.NewTicker(time.Millisecond)
		defer retry.Stop()
		for {
			a.scheduleMu.Lock()
			if a.quarantined.Load() || a.recoveryFenced() {
				a.scheduleMu.Unlock()
				return ErrQuarantined
			}
			enqueued := pm.trySubmitHigh(jb)
			a.scheduleMu.Unlock()
			if enqueued {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-agentDone:
				return ErrStopped
			case <-retry.C:
			}
		}
	}

	// PriorityMailbox not enabled: falls back to regular submit.
	return a.submit(ctx, jb)
}

// --- AskWithHistory / AskStreamWithHistory -----------------------------------

// AskWithHistory delivers an LLM request with conversational history to the agent and waits for the result.
//
// Unlike Ask, this method uses ContextWindow to provide complete conversation history,
// and pushes intermediate messages to the ContextWindow during tool calls.
// Returns content and reasoningContent (DeepSeek thinking mode must be returned across turns).
//
// ⚠️ The caller (typically Session) should push the user prompt to cw before calling,
// push the assistant reply (including reasoningContent) to cw upon successful call, and PopLast to remove the user prompt if it fails.
func (a *Agent) AskWithHistory(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (content string, reasoningContent string, err error) {
	if a.Log != nil {
		ctxCurrent, _, _ := cw.TokenUsage()
		a.Log.InfoContext(ctx, logger.CatActor, "ask_with_history: starting with context window",
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

// AskStreamWithHistory delivers a streaming Ask request with conversational history.
//
// The returned channel is closed by runOnceStreamWithHistory within the agent goroutine.
func (a *Agent) AskStreamWithHistory(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (<-chan AgentEvent, error) {
	out, _, err := a.AskStreamWithHistoryTracked(ctx, cw, prompt)
	return out, err
}

// AskStreamWithHistoryTracked is the request-scoped variant used by Session
// watchdog recovery. The returned handle remains tied to this exact stream,
// including async delegation resumes, until its output channel closes.
func (a *Agent) AskStreamWithHistoryTracked(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (<-chan AgentEvent, *JobHandle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = ensureTraceID(ctx)
	ctx = a.ctxWithAgentAttrs(ctx)
	if iface.ModelOverrideFromContext(ctx) == nil {
		if override := a.modelOverride.Load(); override != nil {
			ctx = iface.ContextWithModelOverride(ctx, override.ToIFaceOverride())
			a.modelOverride.CompareAndSwap(override, nil)
		}
	}
	tracker := &jobTracker{id: a.jobSeq.Add(1), done: make(chan struct{}), override: modelOverrideForContext(ctx, nil)}
	ctx = withJobTracker(ctx, tracker)
	handle := &JobHandle{agent: a, tracker: tracker}

	if a.Log != nil {
		ctxCurrent, _, _ := cw.TokenUsage()
		a.Log.InfoContext(ctx, logger.CatActor, "askstreamwithhistory: enqueueing request with context",
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
		tracker.finish()
		return nil, nil, err
	}

	if a.Log != nil {
		a.Log.DebugContext(ctx, logger.CatActor, "askstreamwithhistory: request enqueued successfully",
			"agent_id", a.Def.ID,
		)
	}

	return out, handle, nil
}
