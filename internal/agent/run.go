package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// JobWatchdogGrace is the bounded recovery window before a non-cooperative
// Agent generation is quarantined by its owning Session/Supervisor.
const JobWatchdogGrace = 10 * time.Second

const jobWatchdogGrace = JobWatchdogGrace

// runJob releases the actor loop as soon as a job yields or completes.
// Async tasks are awaited only during agent shutdown so delegation can run in
// the background while the priority mailbox serves new L1 requests.
func (a *Agent) runJob(ctx context.Context, fn func(context.Context)) {
	jobDone := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("agent job panic: %v", r)
				a.setRuntimeExitErr(err)
				a.logError(ctx, logger.CatActor, "agent job panic", err)
				a.cancel()
			}
			close(jobDone)
		}()
		fn(ctx)
	}()

	select {
	case <-jobDone:
		if ctx.Err() == nil {
			return
		}
	case <-ctx.Done():
	}

	cleanupDone := make(chan struct{})
	go func() {
		// Waiting for the job first ensures every taskWg.Add for this turn has
		// happened before Wait begins.
		<-jobDone
		a.taskWg.Wait()
		close(cleanupDone)
	}()
	select {
	case <-cleanupDone:
	case <-time.After(jobWatchdogGrace):
		a.logError(ctx, logger.CatActor, "job did not stop after context cancellation",
			fmt.Errorf("job stuck for %s after ctx.Done (async goroutines may be orphaned)", jobWatchdogGrace),
			"grace_period", jobWatchdogGrace.String(),
		)
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
			if !a.waitForRecoveryFence(ctx) {
				jb(ctx)
				a.setRuntimeState(StateStopping)
				a.drainMailbox(ctx, mailbox)
				return
			}
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
			if !a.waitForRecoveryFence(ctx) {
				pj.job(ctx)
				a.setRuntimeState(StateStopping)
				a.drainPriorityMailbox(ctx, pm)
				return
			}
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
			if !a.waitForRecoveryFence(ctx) {
				pj.job(ctx)
				a.setRuntimeState(StateStopping)
				a.drainPriorityMailbox(ctx, pm)
				return
			}
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
		case pj := <-pm.NormalCh():
			if !a.waitForRecoveryFence(ctx) {
				pj.job(ctx)
				a.setRuntimeState(StateStopping)
				a.drainPriorityMailbox(ctx, pm)
				return
			}
			a.setRuntimeState(StateProcessing)
			a.ResetErrors()
			a.runJob(ctx, pj.job)
			a.setRuntimeState(StateIdle)
		}
	}
}

// waitForRecoveryFence gates work already dequeued before a watchdog fence was
// installed. Enqueue checks alone are insufficient because both mailbox lanes
// may already contain unrelated jobs. The actor waits without holding a lock;
// permanent quarantine wakes it through the Agent context.
func (a *Agent) waitForRecoveryFence(ctx context.Context) bool {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		a.scheduleMu.Lock()
		blocked := a.quarantined.Load() || a.recoveryFenced()
		a.scheduleMu.Unlock()
		if !blocked {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
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
