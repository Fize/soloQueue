package agent

import (
	"context"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// Start launches the agent's run goroutine.
//
// Calling Start repeatedly returns ErrAlreadyStarted. After Stop, it can be Started again (resetting mailbox and exitErr).
// parent is typically context.Background() or a process-level context; if parent is canceled, the agent will automatically exit.
func (a *Agent) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// If the previous 'done' is not yet closed and 'ctx' is not nil, it means the agent is still running.
	if a.ctx != nil {
		select {
		case <-a.done:
			// Previously exited, can restart.
		default:
			return ErrAlreadyStarted
		}
	}

	a.ctx, a.cancel = context.WithCancel(parent)
	// The agent's own ctx also injects actor_id, so run/drain logs automatically include it.
	a.ctx = a.ctxWithAgentAttrs(a.ctx)
	a.done = make(chan struct{})
	a.setRuntimeExitErr(nil)
	a.setRuntimeState(StateIdle)

	// Clear session-level confirmation whitelist on each Start (corresponds to a new session).
	a.confirmStore.Clear()

	// Choose run function based on whether PriorityMailbox is enabled.
	if a.priorityMailbox != nil {
		go a.runWithPriorityMailbox(a.ctx, a.priorityMailbox, a.done)
	} else {
		a.mailbox = make(chan job, a.mailboxCap)
		go a.run(a.ctx, a.mailbox, a.done)
	}

	a.logInfo(a.ctx, logger.CatActor, "agent started",
		"kind", string(a.Def.Kind),
		"role", string(a.Def.Role),
		"model_id", a.Def.ModelID,
		"mailbox_cap", a.mailboxCap,
		"priority_mailbox", a.priorityMailbox != nil,
	)
	return nil
}

// Stop requests the agent to stop.
//
//  1. Cancels agent ctx → the run goroutine exits in the next select iteration.
//  2. The ctx of the currently executing job is also canceled (job should listen to ctx.Done).
//  3. Enqueued pending jobs will be drained (each job called with an already canceled ctx),
//     allowing Ask calls stuck on the reply channel to return ctx.Canceled.
//  4. Waits for the run goroutine to exit; timeout <= 0 means infinite wait.
//
// Returns ErrStopTimeout on timeout, but the goroutine will eventually exit.
// Calling Stop without Start first returns ErrNotStarted.
func (a *Agent) Stop(timeout time.Duration) error {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	// Snapshot a.ctx: its value (actor_id) is still readable after cancel, used for Stop logs.
	stopCtx := a.ctx
	a.mu.Unlock()

	if cancel == nil || done == nil {
		return ErrNotStarted
	}

	a.logInfo(stopCtx, logger.CatActor, "agent stop requested",
		"timeout_ms", timeout.Milliseconds(),
	)

	cancel()

	start := time.Now()
	if timeout <= 0 {
		<-done
		a.logInfo(stopCtx, logger.CatActor, "agent stopped",
			"wait_ms", time.Since(start).Milliseconds(),
		)
		return nil
	}
	select {
	case <-done:
		a.logInfo(stopCtx, logger.CatActor, "agent stopped",
			"wait_ms", time.Since(start).Milliseconds(),
		)
		return nil
	case <-time.After(timeout):
		a.logError(stopCtx, logger.CatActor, "agent stop timeout", ErrStopTimeout)
		return ErrStopTimeout
	}
}

// Done returns a channel that is closed after the run goroutine exits.
//
// Semantically similar to context.Context.Done: can be used in a select statement to wait for the agent to exit.
// When not Started, returns an already closed channel (immediately readable).
func (a *Agent) Done() <-chan struct{} {
	a.mu.Lock()
	d := a.done
	a.mu.Unlock()
	if d == nil {
		// Not Started: return an already closed channel.
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return d
}

// Err returns the reason for the agent's exit.
//
//   - nil: Not Started / Running / Successfully Stopped.
//   - non-nil: an internal panic occurred in the run goroutine, the value is a wrapped error.
//
// Only definitive after <-Done() returns.
func (a *Agent) Err() error {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.exitErr
}

func (a *Agent) State() State {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.state
}