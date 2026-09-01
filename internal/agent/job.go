package agent

import (
	"context"
	"sync"
)

// JobHandle identifies one history-stream request. It is intentionally
// request-scoped: a later request on the same Agent can never satisfy or be
// mistaken for this handle.
type JobHandle struct {
	agent   *Agent
	tracker *jobTracker
}

type jobTracker struct {
	id       uint64
	done     chan struct{}
	once     sync.Once
	override *ModelParams
}

type jobTrackerKey struct{}

func withJobTracker(ctx context.Context, tracker *jobTracker) context.Context {
	return context.WithValue(ctx, jobTrackerKey{}, tracker)
}

func jobTrackerFromContext(ctx context.Context) *jobTracker {
	if ctx == nil {
		return nil
	}
	tracker, _ := ctx.Value(jobTrackerKey{}).(*jobTracker)
	return tracker
}

func (t *jobTracker) finish() {
	if t != nil {
		t.once.Do(func() { close(t.done) })
	}
}

// Done closes when this request's stream has reached its terminal state.
func (h *JobHandle) Done() <-chan struct{} {
	if h == nil || h.tracker == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return h.tracker.done
}

// Active reports whether this exact request is still the Agent's executing
// job. A completed request, a yielded delegation, or a later request returns
// false, preventing delayed recovery timers from acting on the wrong job.
func (h *JobHandle) Active() bool {
	if h == nil || h.agent == nil || h.tracker == nil {
		return false
	}
	h.agent.scheduleMu.Lock()
	defer h.agent.scheduleMu.Unlock()
	h.agent.jobMu.Lock()
	defer h.agent.jobMu.Unlock()
	return h.agent.currentJob == h.tracker
}

// Fence prevents this Agent generation from accepting new work while this
// exact unfinished request is being given a chance to observe cancellation.
// It refuses to fence a generation that has already moved on to another job.
func (h *JobHandle) Fence() bool {
	if h == nil || h.agent == nil || h.tracker == nil {
		return false
	}
	h.agent.scheduleMu.Lock()
	defer h.agent.scheduleMu.Unlock()
	h.agent.jobMu.Lock()
	defer h.agent.jobMu.Unlock()
	select {
	case <-h.tracker.done:
		return false
	default:
	}
	// A yielded request has no actor-blocking current job. Fencing it would
	// unnecessarily stop the leader from serving unrelated foreground work.
	if h.agent.currentJob != h.tracker {
		return false
	}
	if h.agent.recoveryFence != nil && h.agent.recoveryFence != h.tracker {
		return false
	}
	h.agent.recoveryFence = h.tracker
	return true
}

// ReleaseFence makes the generation schedulable again only after the exact
// fenced request has reached its terminal state.
func (h *JobHandle) ReleaseFence() bool {
	if h == nil || h.agent == nil || h.tracker == nil {
		return false
	}
	h.agent.scheduleMu.Lock()
	defer h.agent.scheduleMu.Unlock()
	h.agent.jobMu.Lock()
	defer h.agent.jobMu.Unlock()
	select {
	case <-h.tracker.done:
	default:
		return false
	}
	if h.agent.recoveryFence != h.tracker {
		return false
	}
	h.agent.recoveryFence = nil
	return true
}

// QuarantineIfStillBlocking atomically validates the recovery timer against
// the exact actor-blocking request. A timer that races with completion, yield,
// fence replacement, or generation recovery merely releases its own stale
// fence and cannot quarantine later work.
func (h *JobHandle) QuarantineIfStillBlocking(cause error) bool {
	if h == nil || h.agent == nil || h.tracker == nil {
		return false
	}
	a := h.agent
	a.scheduleMu.Lock()
	a.jobMu.Lock()
	done := false
	select {
	case <-h.tracker.done:
		done = true
	default:
	}
	if done || a.recoveryFence != h.tracker || a.currentJob != h.tracker || a.quarantined.Load() {
		if a.recoveryFence == h.tracker {
			a.recoveryFence = nil
		}
		a.jobMu.Unlock()
		a.scheduleMu.Unlock()
		return false
	}
	a.recoveryFence = nil
	a.quarantined.Store(true)
	a.jobMu.Unlock()
	a.scheduleMu.Unlock()
	a.finishQuarantine(cause)
	return true
}

func (a *Agent) recoveryFenced() bool {
	a.jobMu.Lock()
	defer a.jobMu.Unlock()
	return a.recoveryFence != nil
}

func (a *Agent) beginTrackedJob(tracker *jobTracker) {
	if a == nil || tracker == nil {
		return
	}
	a.jobMu.Lock()
	a.currentJob = tracker
	a.jobMu.Unlock()
	if tracker.override != nil {
		a.activeModelOverride.Store(tracker.override)
	}
}

func (a *Agent) endTrackedJob(tracker *jobTracker) {
	if a == nil || tracker == nil {
		return
	}
	a.jobMu.Lock()
	if a.currentJob == tracker {
		a.currentJob = nil
	}
	a.jobMu.Unlock()
	if tracker.override != nil {
		a.activeModelOverride.CompareAndSwap(tracker.override, nil)
	}
}
