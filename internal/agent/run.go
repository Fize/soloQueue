package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// jobWatchdogGrace: bounded wait for async goroutines after ctx cancellation.
const jobWatchdogGrace = 10 * time.Second

// runJob runs fn(ctx), then waits for all async goroutines (taskWg) before closing done.
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
			a.taskWg.Wait()
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
				fmt.Errorf("job stuck for %s after ctx.Done (async goroutines may be orphaned)", jobWatchdogGrace),
				"grace_period", jobWatchdogGrace.String(),
			)
		}
	}
}

// ─── run goroutine ──────────────────────────────────────────────────────────

// run is the agent's main loop. Parameters are local copies from Start,
// so run doesn't contend for locks with Start/Stop.
func (a *Agent) run(ctx context.Context, mailbox <-chan job, done chan<- struct{}) {
	a.logInfo(ctx, logger.CatActor, "agent run goroutine started")
	defer func() {
		if r := recover(); r != nil {
			err := fmt.Errorf("agent panic: %v", r)
			a.setRuntimeExitErr(err)
			a.logError(ctx, logger.CatActor, "agent run goroutine panic", err)
			a.setRuntimeState(StateStopped)
			close(done)
			// Record in exitErr, don't re-panic: that would skip close(done) and hang the caller.
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

// runWithPriorityMailbox prioritizes highCh (delegation callbacks) over normalCh (user messages).
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

// drainMailbox runs pending jobs with a cancelled ctx so blocked Ask callers
// receive ctx.Canceled instead of hanging forever.
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