package agent

// ─── PriorityMailbox ───────────────────────────────────────────────────────

// prioritizedJob a job with Priority
type prioritizedJob struct {
	job job
}

// PriorityMailbox separates high-priority jobs (delegation callbacks) from
// normal jobs (user messages) to prevent delegation results from being blocked.
type PriorityMailbox struct {
	highCh   chan prioritizedJob // cap 4
	normalCh chan prioritizedJob // cap 8
}

func NewPriorityMailbox() *PriorityMailbox {
	return &PriorityMailbox{
		highCh:   make(chan prioritizedJob, 4),
		normalCh: make(chan prioritizedJob, 8),
	}
}

func (pm *PriorityMailbox) SubmitHigh(jb job) {
	pm.highCh <- prioritizedJob{job: jb}
}

func (pm *PriorityMailbox) trySubmitHigh(jb job) bool {
	select {
	case pm.highCh <- prioritizedJob{job: jb}:
		return true
	default:
		return false
	}
}

func (pm *PriorityMailbox) SubmitNormal(jb job) {
	pm.normalCh <- prioritizedJob{job: jb}
}

func (pm *PriorityMailbox) trySubmitNormal(jb job) bool {
	select {
	case pm.normalCh <- prioritizedJob{job: jb}:
		return true
	default:
		return false
	}
}

func (pm *PriorityMailbox) HighCh() <-chan prioritizedJob {
	return pm.highCh
}

func (pm *PriorityMailbox) NormalCh() <-chan prioritizedJob {
	return pm.normalCh
}

// Len returns approximate queue depth.
func (pm *PriorityMailbox) Len() (high, normal int) {
	return len(pm.highCh), len(pm.normalCh)
}
