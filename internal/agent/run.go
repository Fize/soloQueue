package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// jobWatchdogGrace is the time to wait for a job to finish after ctx
// cancellation before declaring it stuck and continuing the run loop.
const jobWatchdogGrace = 1 * time.Second

// runJob runs fn(ctx) in a goroutine with a watchdog. If the context is
// cancelled and fn doesn't return within jobWatchdogGrace, a warning is
// logged and runJob returns. The fn goroutine will eventually terminate
// on its own (e.g., when orphan processes finish / a.emit detects ctx.Done).
func (a *Agent) runJob(ctx context.Context, fn func(context.Context)) {
	done := make(chan struct{}, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("agent job panic: %v", r)
				a.setRuntimeExitErr(err)
				a.logError(ctx, logger.CatActor, "agent job panic", err)
				a.cancel()
			}
			close(done)
		}()
		fn(ctx)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done:
		case <-time.After(jobWatchdogGrace):
			a.logError(ctx, logger.CatActor, "job did not stop after context cancellation",
				fmt.Errorf("job stuck for %s after ctx.Done", jobWatchdogGrace),
				"grace_period", jobWatchdogGrace.String(),
			)
		}
	}
}

// ─── run goroutine ──────────────────────────────────────────────────────────

// run is the agent's main loop
//
// Accepts ctx / mailbox / done as parameters (instead of reading from the receiver):
// Start constructs them and passes them as local parameters, so run doesn't need to contend for locks with Start/Stop;
// even if Stop resets a.mailbox, this local mailbox still points to the same channel.
func (a *Agent) run(ctx context.Context, mailbox <-chan job, done chan<- struct{}) {
	a.logInfo(ctx, logger.CatActor, "agent run goroutine started")
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.setRuntimeExitErr(err)
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.setRuntimeState(StateStopped)
			close(done)
			// Panic is already recorded in exitErr, caller can retrieve it via Err();
			// no re-panic: re-panic would skip close(done), causing the caller to block indefinitely on Done()
		} else {
			a.logInfo(ctx, logger.CatActor, "agent run goroutine stopped")
			a.setRuntimeState(StateStopped)
			close(done)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			a.setRuntimeState(StateStopping)
			drained := a.drainMailbox(ctx, mailbox)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				"drained_jobs", drained,
			)
			return
		case jb := <-mailbox:
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, jb)
			a.setRuntimeState(StateIdle)
		}
	}
}

// runWithPriorityMailbox is the main loop when PriorityMailbox is enabled
//
// Prioritizes consuming highCh (delegation callbacks, timeout events), then normalCh (user Ask/Submit).
// Ensures that asynchronous delegation results are not blocked by normal messages.
func (a *Agent) runWithPriorityMailbox(ctx context.Context, pm *PriorityMailbox, done chan<- struct{}) {
	a.logInfo(ctx, logger.CatActor, "agent run goroutine started")
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.setRuntimeExitErr(err)
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.setRuntimeState(StateStopped)
			close(done)
		} else {
			a.logInfo(ctx, logger.CatActor, "agent run goroutine stopped")
			a.setRuntimeState(StateStopped)
			close(done)
		}
	}()

	for {
		// First check highCh (non-blocking)
		select {
		case <-ctx.Done():
			a.setRuntimeState(StateStopping)
			drained := a.drainPriorityMailbox(ctx, pm)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				"drained_jobs", drained,
			)
			return
		case pj := <-pm.HighCh():
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
			continue
		default:
		}

		// When highCh has no messages, wait for both highCh + normalCh simultaneously
		select {
		case <-ctx.Done():
			a.setRuntimeState(StateStopping)
			drained := a.drainPriorityMailbox(ctx, pm)
			a.logInfo(ctx, logger.CatActor, "agent run loop exit",
				"drained_jobs", drained,
			)
			return
		case pj := <-pm.HighCh():
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
		case pj := <-pm.NormalCh():
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
		}
	}
}

// drainPriorityMailbox invokes all enqueued jobs with an already canceled ctx
func (a *Agent) drainPriorityMailbox(ctx context.Context, pm *PriorityMailbox) int {
	n := 0
	// First drain highCh
	for {
		select {
		case pj := <-pm.HighCh():
			pj.job(ctx)
			n++
		default:
			goto drainNormal
		}
	}
drainNormal:
	// Then drain normalCh
	for {
		select {
		case pj := <-pm.NormalCh():
			pj.job(ctx)
			n++
		default:
			return n
		}
	}
}

// drainMailbox invokes all enqueued jobs with an already canceled ctx
//
// Purpose: To allow each caller's Ask to receive a result from replyCh (usually ctx.Canceled),
// preventing it from getting stuck indefinitely.
// No further reads from outside the mailbox (the mailbox is never closed; senders will return ErrStopped directly after seeing agentDone is closed).
//
// Returns the number of drained jobs for logging statistics.
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