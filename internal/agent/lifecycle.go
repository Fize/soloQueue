package agent

import (
	"context"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Lifecycle ──────────────────────────────────────────────────────────────

// Start launches the run goroutine. Idempotent: returns ErrAlreadyStarted if running.
// After Stop, can be restarted. parent cancellation causes automatic exit.
func (a *Agent) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Previous 'done' not yet closed + ctx non-nil = still running.
	if a.ctx != nil {
		select {
		case <-a.done:
			// Previously exited, can restart.
		default:
			return ErrAlreadyStarted
		}
	}

	a.ctx, a.cancel = context.WithCancel(parent)
	// Inject actor_id for structured logging in run/drain.
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

// Stop cancels the agent context, drains pending jobs, and waits for the run
// goroutine to exit. timeout <= 0 means infinite wait.
func (a *Agent) Stop(timeout time.Duration) error {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	// Snapshot ctx for stop logs (its actor_id is still readable after cancel).
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

// Done returns a channel closed after the run goroutine exits.
// Not Started: returns an already-closed channel.
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

// Err returns the run goroutine's exit reason. Only definitive after <-Done().
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