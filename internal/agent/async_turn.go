package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

// delegatedTask represents a single async tool_call delegation task.
type delegatedTask struct {
	replyCh   chan delegateResult
	callID    string          // which tool_call it belongs to
	callIndex int             // position in toolCalls
	turn      *asyncTurnState // reverse reference to the owning turn
}

type delegateResult struct {
	content  string
	err      error
	duration time.Duration
}

// asyncTurnState tracks and aggregates all tool_call results in an async turn.
type asyncTurnState struct {
	agentID   string
	out       chan<- AgentEvent
	cw        *ctxwin.ContextWindow
	iter      int
	toolCalls []llm.ToolCall
	// Concurrency safety: each worker writes to its distinct callIndex index.
	// happens-before established by pending atomic counter hitting 0.
	results   []string
	durations []time.Duration
	pending   atomic.Int32 // number of pending asynchronous calls
	callerCtx context.Context

	// cancelMerged holds the cancel function for the merged context.
	//
	// When streamLoop yields for async delegation, the job closure in
	// AskStreamWithHistory must NOT cancel the merged context (callerCtx)
	// — resumeTurn still needs it. Instead, the cancel is deferred here
	// and invoked after the final streamLoop completes in resumeTurn.
	cancelMerged context.CancelFunc
}

// setDuration assigns d to durations[index], growing slice lazily for tests.
func (t *asyncTurnState) setDuration(index int, d time.Duration) {
	if index < 0 {
		return
	}
	if index >= len(t.durations) {
		needed := index + 1
		if needed <= cap(t.durations) {
			t.durations = t.durations[:needed]
		} else {
			newCap := needed
			if newCap < len(t.toolCalls) {
				newCap = len(t.toolCalls)
			}
			grown := make([]time.Duration, needed, newCap)
			copy(grown, t.durations)
			t.durations = grown
		}
	}
	t.durations[index] = d
}

// execToolsWithAsync identifies AsyncTools, pre-creates asyncTurnState, runs sync tools immediately, and spawns async goroutines.
func (a *Agent) execToolsWithAsync(
	ctx context.Context,
	iter int,
	calls []llm.ToolCall,
	out chan<- AgentEvent,
	cw *ctxwin.ContextWindow,
) []string {
	results := make([]string, len(calls))

	// Phase 1: Identify asynchronous tools + pre-create asyncTurnState
	var (
		turnState    *asyncTurnState
		asyncActions []func() // collect closures that need to be started asynchronously
		tasks        []*delegatedTask
	)

	for i, tc := range calls {
		if err := ctx.Err(); err != nil {
			results[i] = "error: " + err.Error()
			continue
		}

		tool, ok := a.tools.SafeGet(tc.Function.Name)
		if !ok {
			results[i] = a.execToolStream(ctx, iter, tc, out)
			continue
		}

		// Check if it's an AsyncTool
		at, isAsync := tool.(tools.AsyncTool)
		if !isAsync {
			// Synchronous tool: execute normally
			results[i] = a.execToolStream(ctx, iter, tc, out)
			continue
		}

		// Lazy initialize turnState (created for the first asynchronous tool)
		if turnState == nil {
			turnState = &asyncTurnState{
				agentID:   a.Def.ID,
				out:       out,
				cw:        cw,
				iter:      iter,
				toolCalls: calls,
				results:   results,
				durations: make([]time.Duration, len(calls)),
				callerCtx: ctx,
			}
		}

		// Inject model override + workDir + agent name into the context
		asyncCtx := ctx
		if a.WorkDir != "" {
			asyncCtx = iface.ContextWithWorkDir(asyncCtx, a.WorkDir)
		}
		if a.Def.Name != "" {
			asyncCtx = iface.ContextWithAgentName(asyncCtx, a.Def.Name)
		}
		if override := a.modelOverride.Load(); override != nil {
			asyncCtx = iface.ContextWithModelOverride(asyncCtx, override.ToIFaceOverride())
		}

		// Call ExecuteAsync to get the intent (without starting a goroutine)
		action, err := at.ExecuteAsync(asyncCtx, tc.Function.Arguments)
		if err != nil {
			results[i] = "error: " + err.Error()
			continue
		}
		if action == nil {
			// ExecuteAsync returns nil, indicating fallback to synchronous execution
			results[i] = a.execToolStream(ctx, iter, tc, out)
			continue
		}

		results[i] = formatDelegationStarted(tc)

		targetInstanceID := ""
		type instanceIDer interface {
			InstanceID() string
		}
		if idProv, ok := action.Target.(instanceIDer); ok {
			targetInstanceID = idProv.InstanceID()
		}

		a.emit(ctx, out, ToolExecStartEvent{
			Iter:          iter,
			CallID:        tc.ID,
			Name:          tc.Function.Name,
			Args:          tc.Function.Arguments,
			TargetAgentID: targetInstanceID,
		})

		turnState.pending.Add(1)
		replyCh := make(chan delegateResult, 1)

		// Assemble delegatedTask (turnState is 100% ready at this point)
		task := &delegatedTask{
			replyCh:   replyCh,
			callID:    tc.ID,
			callIndex: i,
			turn:      turnState,
		}
		tasks = append(tasks, task)

		// Collect asynchronous start closures (not yet 'go'd!)
		asyncActions = append(asyncActions, func() {
			timeout := action.Timeout
			if timeout <= 0 {
				timeout = tools.DelegateDefaultTimeout
			}
			delCtx := iface.ContextWithBypassConfirm(turnState.callerCtx)
			delCtx, cancel := context.WithTimeout(delCtx, timeout)
			defer cancel()

			a.logInfo(delCtx, logger.CatTool, "async-goroutine: starting, about to call AskStream",
				"timeout", timeout,
			)

			// --- Inject confirm relay (aligned with execToolStream synchronous path) ---
			relayCh := make(chan iface.AgentEvent, 16)

			forwarder := a.routeConfirm()

			relayDone := a.startRelayGoroutine(turnState.callerCtx, relayCh, turnState.out, func(ev iface.AgentEvent) {
				if a.Log != nil {
					a.Log.InfoContext(turnState.callerCtx, logger.CatTool, "relay-goroutine: received event from relayCh",
						"event_type", fmt.Sprintf("%T", ev),
					)
				}
			})

			// --- Use AskStream + manual consumption instead of Ask ---
			start := time.Now()
			evCh, err := action.Target.AskStream(delCtx, action.Prompt)
			if err != nil {
				if delCtx.Err() == context.DeadlineExceeded {
					if s, ok := action.Target.(interface{ Stop(time.Duration) error }); ok {
						go func() { _ = s.Stop(5 * time.Second) }()
					}
				}
				a.logInfo(delCtx, logger.CatTool, "async-goroutine: AskStream failed",
					"err", err.Error(),
				)
				close(relayCh)
				<-relayDone
				// Ensure the target agent is reaped even when AskStream fails
				// (e.g., due to context cancellation). Without this, spawned
				// L2/L3 agents leak when /cancel fires.
				if dn, ok := action.Target.(iface.DoneNotifier); ok {
					dn.OnDelegationDone()
				}
				replyCh <- delegateResult{err: err}
				return
			}
			a.logInfo(delCtx, logger.CatTool, "async-goroutine: AskStream succeeded, consuming events",
			)

			var content string
			var finalErr error
			for ev := range evCh {
				if ev == nil {
					continue
				}

				select {
				case relayCh <- ev:
				case <-delCtx.Done():
				}

				ec, ok := ev.(iface.EventConsumer)
				if !ok {
					if a.Log != nil {
						a.Log.InfoContext(delCtx, logger.CatTool, "async-goroutine: event not EventConsumer, skipping",
							"event_type", fmt.Sprintf("%T", ev),
						)
					}
					continue
				}

				if callID, has := ec.ConfirmRequest(); has {
					if a.Log != nil {
						a.Log.InfoContext(delCtx, logger.CatTool, "async-goroutine: confirm request detected, firing forwarder",
							"call_id", callID,
						)
					}
					go func(fc context.Context, cid string, target iface.Locatable) {
						defer func() {
							if r := recover(); r != nil {
								a.RecordError(fmt.Errorf("forwarder goroutine panic: %v", r))
							}
						}()
						forwarder(fc, cid, target)
					}(delCtx, callID, action.Target)
				}

				if delta, has := ec.ContentDelta(); has {
					content += delta
				}
				if doneContent, has := ec.DoneContent(); has && doneContent != "" {
					content = doneContent
				}
				if errValue, has := ec.Error(); has && errValue != nil {
					finalErr = errValue
				}
			}

			close(relayCh)
			<-relayDone

			// Stop target agent if delegation timed out.
			if delCtx.Err() == context.DeadlineExceeded {
				if s, ok := action.Target.(interface{ Stop(time.Duration) error }); ok {
					go func() { _ = s.Stop(5 * time.Second) }()
				}
			}

			// Notify that delegation is done so the target can be reaped immediately.
			if dn, ok := action.Target.(iface.DoneNotifier); ok {
				dn.OnDelegationDone()
			}

			dur := time.Since(start)
			replyCh <- delegateResult{content: content, err: finalErr, duration: dur}
		})

		results[i] = "" // placeholder
	}

	// Phase 2: If there are asynchronous tools, register state + start goroutines
	if turnState != nil && turnState.pending.Load() > 0 {
		a.logInfo(ctx, logger.CatTool, "execToolsWithAsync: registering async turn and starting goroutines",
			"agent_id", a.Def.ID,
			"iter", iter,
			"num_async", turnState.pending.Load(),
		)
		a.turnMu.Lock()
		a.asyncTurns[iter] = turnState
		a.turnMu.Unlock()

		// Start all asynchronous goroutines (state is now safely persisted)
		for _, action := range asyncActions {
			a.taskWg.Add(1)
			go func(act func()) {
				defer a.taskWg.Done()
				defer func() {
					if r := recover(); r != nil {
						a.RecordError(fmt.Errorf("async action goroutine panic: %v", r))
					}
				}()
				act()
			}(action)
		}

		// Start result collection goroutines (one for each delegatedTask)
		for _, task := range tasks {
			a.taskWg.Add(1)
			go func(t *delegatedTask) {
				defer a.taskWg.Done()
				defer func() {
					if r := recover(); r != nil {
						a.RecordError(fmt.Errorf("watchDelegatedTask goroutine panic: %v", r))
					}
				}()
				a.watchDelegatedTask(t)
			}(task)
		}
	}

	return results
}

// watchDelegatedTask awaits one async result, stores it, and triggers resumeTurn when all pending complete.
func (a *Agent) watchDelegatedTask(task *delegatedTask) {
	select {
	case result := <-task.replyCh:
		// Fill result
		toolResult := result.content
		if result.err != nil {
			toolResult = "error: " + result.err.Error()
			a.RecordError(result.err)
		}
		task.turn.results[task.callIndex] = toolResult
		task.turn.setDuration(task.callIndex, result.duration)

		// Check if all completed
		if task.turn.pending.Add(-1) == 0 {
			// All asynchronous results have arrived, submit a high-priority job to resume the tool loop
			a.submitHighPriority(func(ctx context.Context) {
				a.resumeTurn(task.turn)
			})
		}
	case <-task.turn.callerCtx.Done():
		// Caller context cancelled. Give replyCh a short grace period —
		// the result might already be in-flight and arrive momentarily.
		select {
		case result := <-task.replyCh:
			toolResult := result.content
			if result.err != nil {
				toolResult = "error: " + result.err.Error()
				a.RecordError(result.err)
			}
			task.turn.results[task.callIndex] = toolResult
			task.turn.setDuration(task.callIndex, result.duration)

			if task.turn.pending.Add(-1) == 0 {
				a.submitHighPriority(func(ctx context.Context) {
					a.resumeTurn(task.turn)
				})
			}
		case <-time.After(10 * time.Millisecond):
			// Genuinely cancelled — fill a synthetic error result and
			// ensure resumeTurn is still triggered so out gets closed.
			task.turn.results[task.callIndex] = "error: delegation cancelled"
			a.RecordError(errors.New("delegation cancelled"))
			if task.turn.pending.Add(-1) == 0 {
				a.submitHighPriority(func(ctx context.Context) {
					a.resumeTurn(task.turn)
				})
			}
		}
	}
}

// resumeTurn formats async delegation results, pushes to CW, emits events, and continues the tool loop.
func (a *Agent) resumeTurn(turn *asyncTurnState) {
	// Clean up asyncTurns registration
	a.turnMu.Lock()
	delete(a.asyncTurns, turn.iter)
	a.turnMu.Unlock()

	// Format the actual delegation results as a user message and push to cw
	// Wrap into an assistant(tool_calls) + tool(result) + user(result) structure,
	// to ensure the LLM correctly understands this as a tool call return result.
	resultMsg := formatDelegationCompleted(turn.toolCalls, turn.results)
	if resultMsg != "" {
		turn.cw.Push(ctxwin.RoleUser, resultMsg, ctxwin.WithEphemeral(true))
	}

	// Emit DelegationCompletedEvent
	resultContent := ""
	if len(turn.results) > 0 {
		resultContent = turn.results[0]
	}
	a.emit(turn.callerCtx, turn.out, DelegationCompletedEvent{
		Iter:          turn.iter,
		TargetAgentID: turn.agentID,
		ResultContent: resultContent,
	})

	// Emit ToolExecDoneEvent for each asynchronous delegated tool, allowing the frontend to mark
	// the tool_call segment created by ToolExecStartEvent as complete.
	for i, tc := range turn.toolCalls {
		if !strings.HasPrefix(tc.Function.Name, "delegate_") {
			continue
		}
		result := ""
		if i < len(turn.results) {
			result = turn.results[i]
		}
		var dur time.Duration
		if i < len(turn.durations) {
			dur = turn.durations[i]
		}
		a.emit(turn.callerCtx, turn.out, ToolExecDoneEvent{
			Iter:     turn.iter,
			CallID:   tc.ID,
			Name:     tc.Function.Name,
			Result:   result,
			Duration: dur,
		})
	}

	// Overflow check: async results may have pushed CW over capacity
	// while the agent was handling user messages.
	if turn.cw.Overflow() {
		current, max, _ := turn.cw.TokenUsage()
		err := fmt.Errorf("context overflow after async delegation: %d tokens exceed effective limit %d",
			current, max)
		a.emit(turn.callerCtx, turn.out, ErrorEvent{Err: err})
		close(turn.out)
		if turn.cancelMerged != nil {
			turn.cancelMerged()
		}
		return
	}

	// Continue tool loop
	yielded := a.continueToolLoop(turn.callerCtx, turn.out, turn.cw, turn.iter+1)

	// Manage the merged context lifecycle.
	//
	// Normal path: continueToolLoop completes fully → cancel merged ctx to
	// prevent leak.
	//
	// Nested async path: continueToolLoop yielded again (another async
	// delegation started in this stream loop). The new asyncTurnState has
	// the same callerCtx (merged) but does NOT have cancelMerged set
	// (saveAsyncCancel is only called by the outer AskStreamWithHistory,
	// not by the inner continueToolLoop). Transfer our cancelMerged to
	// the new turn so it can cancel on its own completion.
	if yielded {
		if turn.cancelMerged != nil {
			a.saveAsyncCancel(turn.callerCtx, turn.cancelMerged)
		}
	} else if turn.cancelMerged != nil {
		turn.cancelMerged()
	}
}

// continueToolLoop resumes streamLoop from startIter.
func (a *Agent) continueToolLoop(
	ctx context.Context,
	out chan<- AgentEvent,
	cw *ctxwin.ContextWindow,
	startIter int,
) bool {
	// Reuse the loop body of runOnceStreamWithHistory
	return a.runOnceStreamWithHistoryFromIter(ctx, cw, out, startIter)
}

// saveAsyncCancel stores the cancel function into the asyncTurnState whose
// callerCtx matches ctx. Called by AskStreamWithHistory when streamLoop
// yields so that resumeTurn can cancel the merged context after the final
// streamLoop completes.
//
// If no matching asyncTurnState is found (should not happen), cancel is
// invoked immediately to prevent context leak.
func (a *Agent) saveAsyncCancel(ctx context.Context, cancel context.CancelFunc) {
	a.turnMu.Lock()
	defer a.turnMu.Unlock()
	for _, ts := range a.asyncTurns {
		if ts.callerCtx == ctx {
			ts.cancelMerged = cancel
			return
		}
	}
	// Defensive: no matching turn found — cancel now to prevent leak.
	cancel()
}

// delegationArgs is the parameter structure for delegation tools
type delegationArgs struct {
	Task string `json:"task"`
}

// formatDelegationStarted generates an immediate tool result for async delegation.
// This ensures the assistant(tool_calls) → tool(result) pair is complete in CW,
// preventing interleaved user messages from violating LLM API message ordering.
func formatDelegationStarted(tc llm.ToolCall) string {
	var d delegationArgs
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &d)
	task := d.Task
	if task == "" {
		task = tc.Function.Arguments
	}
	name := tc.Function.Name
	if name == "" {
		name = "delegate"
	}
	return fmt.Sprintf("Delegation started: task assigned via '%s'.\nTask: %s\nWaiting for results...", name, task)
}

// formatDelegationCompleted builds a user message containing completed delegation results.
// It filters only delegate_* tool calls and formats their results as:
//
//	[Delegation Completed]
//
//	Task: {task description}
//	CallID: {tool call ID}
//	Result:
//	{result content}
func formatDelegationCompleted(toolCalls []llm.ToolCall, results []string) string {
	var sb strings.Builder
	sb.WriteString("[Delegation Completed]\n\n")
	hasResults := false
	for i, tc := range toolCalls {
		if !strings.HasPrefix(tc.Function.Name, "delegate_") {
			continue
		}
		result := ""
		if i < len(results) {
			result = results[i]
		}
		var d delegationArgs
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &d)
		task := d.Task
		if task == "" {
			task = tc.Function.Arguments
		}
		sb.WriteString(fmt.Sprintf("Task: %s\n", task))
		sb.WriteString(fmt.Sprintf("CallID: %s\n", tc.ID))
		sb.WriteString("Result:\n")
		sb.WriteString(result)
		sb.WriteString("\n\n")
		hasResults = true
	}
	if !hasResults {
		return ""
	}
	return sb.String()
}
