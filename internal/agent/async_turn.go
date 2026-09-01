package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
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

type asyncTurnPhase uint8

const (
	asyncTurnPending asyncTurnPhase = iota
	asyncTurnQueued
	asyncTurnResuming
	asyncTurnHandedOff
	asyncTurnTerminal
)

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

	terminalMu    sync.Mutex
	phase         asyncTurnPhase
	ownershipDone chan struct{}
	ownershipOnce sync.Once
	terminalOnce  sync.Once

	// Test-only scheduling hook used to deterministically exercise cancellation
	// after queued->resuming but before the first persistent mutation.
	beforeResumeMutation func()
}

func (t *asyncTurnState) claimResume() bool {
	if t == nil {
		return false
	}
	t.terminalMu.Lock()
	defer t.terminalMu.Unlock()
	if t.phase != asyncTurnPending {
		return false
	}
	t.phase = asyncTurnQueued
	t.ensureOwnershipDoneLocked()
	return true
}

func (t *asyncTurnState) recordCancellation(callIndex int) bool {
	if t == nil {
		return false
	}
	t.terminalMu.Lock()
	defer t.terminalMu.Unlock()
	if t.phase != asyncTurnPending {
		return false
	}
	if callIndex >= 0 && callIndex < len(t.results) {
		t.results[callIndex] = "error: delegation cancelled"
	}
	t.pending.Add(-1)
	return true
}

func (t *asyncTurnState) recordResult(callIndex int, result string, duration time.Duration) (accepted, allDone bool) {
	if t == nil {
		return false, false
	}
	t.terminalMu.Lock()
	defer t.terminalMu.Unlock()
	if t.phase != asyncTurnPending {
		return false, false
	}
	if callIndex >= 0 && callIndex < len(t.results) {
		t.results[callIndex] = result
		t.setDuration(callIndex, duration)
	}
	return true, t.pending.Add(-1) == 0
}

func (t *asyncTurnState) beginResume() bool {
	if t == nil {
		return false
	}
	t.terminalMu.Lock()
	defer t.terminalMu.Unlock()
	if t.phase != asyncTurnQueued && t.phase != asyncTurnPending {
		return false
	}
	t.phase = asyncTurnResuming
	return true
}

func (t *asyncTurnState) ensureOwnershipDoneLocked() chan struct{} {
	if t.ownershipDone == nil {
		t.ownershipDone = make(chan struct{})
	}
	return t.ownershipDone
}

func (t *asyncTurnState) ownershipSignal() <-chan struct{} {
	t.terminalMu.Lock()
	ch := t.ensureOwnershipDoneLocked()
	if t.phase == asyncTurnHandedOff || t.phase == asyncTurnTerminal {
		t.ownershipOnce.Do(func() { close(ch) })
	}
	t.terminalMu.Unlock()
	return ch
}

func (t *asyncTurnState) signalOwnershipLocked() {
	ch := t.ensureOwnershipDoneLocked()
	t.ownershipOnce.Do(func() { close(ch) })
}

// handoffInitialMutation makes cancellation and the first resumed persistence
// mutation one ordered transition. The winner of terminalMu owns the boundary.
func (t *asyncTurnState) handoffInitialMutation(ctx context.Context, mutate func()) bool {
	if t == nil {
		return false
	}
	t.terminalMu.Lock()
	if t.phase != asyncTurnResuming || ctx.Err() != nil {
		t.terminalMu.Unlock()
		return false
	}
	if mutate != nil {
		mutate()
	}
	t.phase = asyncTurnHandedOff
	t.signalOwnershipLocked()
	t.terminalMu.Unlock()
	return true
}

func (t *asyncTurnState) setCancelMerged(cancel context.CancelFunc) bool {
	if t == nil || cancel == nil {
		return false
	}
	t.terminalMu.Lock()
	if t.phase == asyncTurnTerminal {
		t.terminalMu.Unlock()
		cancel()
		return false
	}
	t.cancelMerged = cancel
	t.terminalMu.Unlock()
	return true
}

func (t *asyncTurnState) takeCancelMerged() context.CancelFunc {
	if t == nil {
		return nil
	}
	t.terminalMu.Lock()
	cancel := t.cancelMerged
	t.cancelMerged = nil
	t.terminalMu.Unlock()
	return cancel
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
				results:   append([]string(nil), results...),
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
		if override := modelOverrideForContext(ctx, a); override != nil {
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
		if action.DispatchID != "" {
			results[i] += "\nDispatch ID: " + action.DispatchID
		}

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
			start := time.Now()
			result := delegateResult{}
			var completeOnce sync.Once
			complete := func() {
				completeOnce.Do(func() {
					if action.OnFinish != nil {
						var callbackErr error
						func() {
							defer func() {
								if recovered := recover(); recovered != nil {
									callbackErr = fmt.Errorf("async finish callback panicked: %v", recovered)
								}
							}()
							callbackErr = action.OnFinish(result.err)
						}()
						result.err = errors.Join(result.err, callbackErr)
					}
					result.duration = time.Since(start)
					replyCh <- result
				})
			}
			defer func() {
				if recovered := recover(); recovered != nil {
					panicErr := fmt.Errorf("async delegation panicked: %v", recovered)
					a.RecordError(panicErr)
					result.err = errors.Join(result.err, panicErr)
				}
				complete()
			}()

			delCtx := turnState.callerCtx
			if action.Context != nil {
				delCtx = action.Context
			}
			if action.Timeout > 0 {
				var cancel context.CancelFunc
				delCtx, cancel = context.WithTimeout(delCtx, action.Timeout)
				defer cancel()
			}

			a.logInfo(delCtx, logger.CatTool, "async-goroutine: starting, about to call AskStream",
				"timeout", action.Timeout,
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
			defer func() {
				close(relayCh)
				<-relayDone
			}()

			// --- Use AskStream + manual consumption instead of Ask ---
			evCh, err := action.Target.AskStream(delCtx, action.Prompt)
			if err != nil {
				result.err = err
				if delCtx.Err() == context.DeadlineExceeded {
					if s, ok := action.Target.(interface{ Stop(time.Duration) error }); ok {
						go func() { _ = s.Stop(5 * time.Second) }()
					}
				}
				a.logInfo(delCtx, logger.CatTool, "async-goroutine: AskStream failed",
					"err", err.Error(),
				)
				// Ensure the target agent is reaped even when AskStream fails
				// (e.g., due to context cancellation). Without this, spawned
				// L2/L3 agents leak when /cancel fires.
				if dn, ok := action.Target.(iface.DoneNotifier); ok {
					dn.OnDelegationDone()
				}
				return
			}
			a.logInfo(delCtx, logger.CatTool, "async-goroutine: AskStream succeeded, consuming events")

			var content string
			var finalErr error
		eventLoop:
			for {
				var ev iface.AgentEvent
				var ok bool
				select {
				case <-delCtx.Done():
					finalErr = errors.Join(finalErr, delCtx.Err())
					break eventLoop
				case ev, ok = <-evCh:
					if !ok {
						break eventLoop
					}
				}
				if ev == nil {
					continue
				}
				// Cancellation owns the persistence boundary. A target may publish
				// buffered events after observing cancellation; those events must
				// not reach persistence, relay, or response aggregation.
				if delCtx.Err() != nil {
					finalErr = errors.Join(finalErr, delCtx.Err())
					break eventLoop
				}
				if action.OnEvent != nil {
					finalErr = errors.Join(finalErr, action.OnEvent(ev))
				}

				select {
				case relayCh <- ev:
				case <-delCtx.Done():
					finalErr = errors.Join(finalErr, delCtx.Err())
					break eventLoop
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

			if finalErr == nil && delCtx.Err() != nil {
				finalErr = delCtx.Err()
			}

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

			result.content = content
			result.err = finalErr
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
	recordResult := func(result delegateResult) {
		toolResult := result.content
		if result.err != nil {
			toolResult = "error: " + result.err.Error()
			a.RecordError(result.err)
		}
		accepted, allDone := task.turn.recordResult(task.callIndex, toolResult, result.duration)
		if accepted && allDone {
			a.submitAsyncResume(task.turn, func(ctx context.Context) {
				a.resumeTurn(task.turn)
			})
		}
	}
	select {
	case result := <-task.replyCh:
		recordResult(result)
	case <-task.turn.callerCtx.Done():
		// Preserve the established short grace for a result already in flight,
		// but never wait on mailbox capacity after cancellation.
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		select {
		case result := <-task.replyCh:
			recordResult(result)
		case <-timer.C:
			if task.turn.recordCancellation(task.callIndex) {
				a.RecordError(errors.New("delegation cancelled"))
			}
			a.terminateAsyncTurn(task.turn)
		}
	}
}

func (a *Agent) submitAsyncResume(turn *asyncTurnState, resume job) {
	if !turn.claimResume() {
		return
	}
	ownershipDone := turn.ownershipSignal()
	if err := a.submitHighPriority(turn.callerCtx, resume); err == nil {
		a.observeAsyncTurnCancellation(turn, ownershipDone)
		return
	}
	a.terminateAsyncTurn(turn)
}

func (a *Agent) observeAsyncTurnCancellation(turn *asyncTurnState, ownershipDone <-chan struct{}) {
	if a == nil || turn == nil {
		return
	}
	ctx := turn.callerCtx
	if ctx == nil {
		ctx = context.Background()
	}
	a.taskWg.Add(1)
	go func() {
		defer a.taskWg.Done()
		select {
		case <-ctx.Done():
			a.terminateAsyncTurn(turn)
		case <-ownershipDone:
		}
	}()
}

func (a *Agent) removeAsyncTurnIfSame(turn *asyncTurnState) {
	if a == nil || turn == nil {
		return
	}
	a.turnMu.Lock()
	if a.asyncTurns[turn.iter] == turn {
		delete(a.asyncTurns, turn.iter)
	}
	a.turnMu.Unlock()
}

// terminateAsyncTurn owns every abnormal terminal side effect exactly once.
// Successful resume transfers close/tracker ownership back to streamLoop.
func (a *Agent) terminateAsyncTurn(turn *asyncTurnState) {
	a.transitionAsyncTurnToTerminal(turn, false)
}

func (a *Agent) finishHandedOffAsyncTurn(turn *asyncTurnState) {
	a.transitionAsyncTurnToTerminal(turn, true)
}

func (a *Agent) transitionAsyncTurnToTerminal(turn *asyncTurnState, includeHandedOff bool) {
	if a == nil || turn == nil {
		return
	}
	turn.terminalMu.Lock()
	if turn.phase == asyncTurnTerminal || (turn.phase == asyncTurnHandedOff && !includeHandedOff) {
		turn.terminalMu.Unlock()
		return
	}
	turn.phase = asyncTurnTerminal
	turn.signalOwnershipLocked()
	cancel := turn.cancelMerged
	turn.cancelMerged = nil
	turn.terminalMu.Unlock()
	turn.terminalOnce.Do(func() {
		a.removeAsyncTurnIfSame(turn)
		if cancel != nil {
			cancel()
		}
		if tracker := jobTrackerFromContext(turn.callerCtx); tracker != nil {
			tracker.finish()
		}
		if turn.out != nil {
			close(turn.out)
		}
	})
}

// abandonAsyncTurnForOutput transfers stream close ownership to the exact
// registered async turn. It never affects another yielded request on L1.
func (a *Agent) abandonAsyncTurnForOutput(out chan<- AgentEvent) bool {
	a.turnMu.RLock()
	var turn *asyncTurnState
	for _, candidate := range a.asyncTurns {
		if candidate != nil && candidate.out == out {
			turn = candidate
			break
		}
	}
	a.turnMu.RUnlock()
	if turn == nil {
		return false
	}
	a.terminateAsyncTurn(turn)
	return true
}

// resumeTurn formats async delegation results, pushes to CW, emits events, and continues the tool loop.
func (a *Agent) resumeTurn(turn *asyncTurnState) {
	if !turn.beginResume() {
		return
	}
	if turn.beforeResumeMutation != nil {
		turn.beforeResumeMutation()
	}

	// Format the actual delegation results as a user message and push to cw
	// Wrap into an assistant(tool_calls) + tool(result) + user(result) structure,
	// to ensure the LLM correctly understands this as a tool call return result.
	resultMsg := formatDelegationCompleted(turn.toolCalls, turn.results)
	if !turn.handoffInitialMutation(turn.callerCtx, func() {
		if resultMsg != "" {
			turn.cw.Push(ctxwin.RoleUser, resultMsg, ctxwin.WithEphemeral(true))
		}
	}) {
		a.terminateAsyncTurn(turn)
		return
	}
	a.removeAsyncTurnIfSame(turn)
	if err := turn.callerCtx.Err(); err != nil {
		a.finishHandedOffAsyncTurn(turn)
		return
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
		if tc.Function.Name != "delegate" && !strings.HasPrefix(tc.Function.Name, "delegate_") {
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
		a.finishHandedOffAsyncTurn(turn)
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
	cancelMerged := turn.takeCancelMerged()
	if yielded {
		if cancelMerged != nil {
			a.saveAsyncCancel(turn.callerCtx, cancelMerged)
		}
	} else if cancelMerged != nil {
		cancelMerged()
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
	a.turnMu.RLock()
	var turn *asyncTurnState
	for _, ts := range a.asyncTurns {
		if ts.callerCtx == ctx {
			turn = ts
			break
		}
	}
	a.turnMu.RUnlock()
	if turn != nil && turn.setCancelMerged(cancel) {
		return
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
		if tc.Function.Name != "delegate" && !strings.HasPrefix(tc.Function.Name, "delegate_") {
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
