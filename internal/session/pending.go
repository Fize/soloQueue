package session

import (
	"strings"
	"sync"
)

// PendingQueue holds user messages that arrived while the session is busy.
// They are drained before the next LLM API call inside the agent's tool loop,
// so the LLM sees all queued messages batched together in a single turn.
type PendingQueue struct {
	mu   sync.Mutex
	msgs []string
}

// Enqueue adds a message to the pending queue.
func (q *PendingQueue) Enqueue(prompt string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = append(q.msgs, prompt)
}

// Drain returns all pending messages joined with double newlines, and clears
// the queue. Returns empty string if no messages are pending.
func (q *PendingQueue) Drain() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.msgs) == 0 {
		return ""
	}
	joined := strings.Join(q.msgs, "\n\n")
	q.msgs = q.msgs[:0]
	return joined
}

// HasPending returns true if there are queued messages.
func (q *PendingQueue) HasPending() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.msgs) > 0
}

// Len returns the number of queued messages.
func (q *PendingQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.msgs)
}
