package agent

// ─── PriorityMailbox ───────────────────────────────────────────────────────

// Priority distinguishes job priorities in the mailbox
type Priority int

const (
	PriorityNormal Priority = iota // Normal user Ask / Submit
	PriorityHigh                   // Delegation callback / Timeout events / Cancel commands
)

// prioritizedJob a job with Priority
type prioritizedJob struct {
	priority Priority
	job      job
}

// PriorityMailbox a mailbox that supports priorities
//
// Core design:
//   - Two channels: highCh (capacity 4) and normalCh (capacity 8)
//   - The run goroutine prioritizes checking highCh (delegation callbacks are processed first)
//   - Prevents delegation results from waiting behind normal user messages
//
// Compatibility with chan job:
//   - When Agent is initialized, priorityMailbox == nil indicates that a normal chan job is used
//   - Enable priority mode via WithPriorityMailbox() Option
type PriorityMailbox struct {
	highCh   chan prioritizedJob
	normalCh chan prioritizedJob
}

// NewPriorityMailbox creates a new PriorityMailbox
func NewPriorityMailbox() *PriorityMailbox {
	return &PriorityMailbox{
		highCh:   make(chan prioritizedJob, 4),
		normalCh: make(chan prioritizedJob, 8),
	}
}

// SubmitHigh submits a high-priority job
func (pm *PriorityMailbox) SubmitHigh(jb job) {
	pm.highCh <- prioritizedJob{priority: PriorityHigh, job: jb}
}

// SubmitNormal submits a normal-priority job
func (pm *PriorityMailbox) SubmitNormal(jb job) {
	pm.normalCh <- prioritizedJob{priority: PriorityNormal, job: jb}
}

// HighCh returns the high-priority channel (for consumption by the run goroutine)
func (pm *PriorityMailbox) HighCh() <-chan prioritizedJob {
	return pm.highCh
}

// NormalCh returns the normal-priority channel (for consumption by the run goroutine)
func (pm *PriorityMailbox) NormalCh() <-chan prioritizedJob {
	return pm.normalCh
}

// Len returns the current queue depth (approximate value, channel length is not precisely locked)
func (pm *PriorityMailbox) Len() (high, normal int) {
	return len(pm.highCh), len(pm.normalCh)
}