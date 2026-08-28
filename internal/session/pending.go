package session

import (
	"strings"
	"sync"
	"time"
)

// PendingMessage retains temporal metadata while a busy session delays input.
type PendingMessage struct {
	Prompt          string
	ReceivedAt      time.Time
	ExposeTimestamp bool
}

// PendingDrain is the single aggregate user turn produced by Drain.
type PendingDrain struct {
	Content string
	Parts   []PendingMessage
}

// PendingQueue holds user messages that arrived while the session is busy.
// They are drained before the next LLM API call inside the agent's tool loop,
// so the LLM sees all queued messages batched together in a single turn.
type PendingQueue struct {
	mu   sync.Mutex
	msgs []PendingMessage
}

// Enqueue adds legacy or internal input without temporal exposure.
func (q *PendingQueue) Enqueue(prompt string) {
	q.EnqueueMessage(PendingMessage{Prompt: prompt})
}

// EnqueueMessage adds input while retaining its temporal metadata.
func (q *PendingQueue) EnqueueMessage(msg PendingMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = append(q.msgs, msg)
}

// Drain returns all pending messages as the same single aggregate user turn
// used by the original queue behavior, and clears the queue.
func (q *PendingQueue) Drain() PendingDrain {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.msgs) == 0 {
		return PendingDrain{}
	}
	parts := append([]PendingMessage(nil), q.msgs...)
	contents := make([]string, len(parts))
	for i, part := range parts {
		contents[i] = part.Prompt
	}
	q.msgs = q.msgs[:0]
	return PendingDrain{Content: strings.Join(contents, "\n\n"), Parts: parts}
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
