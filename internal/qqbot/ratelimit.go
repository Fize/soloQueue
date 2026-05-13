package qqbot

import (
	"sync"
	"time"
)

// ─── MessageQueue ────────────────────────────────────────────────────────────

// MessageQueue provides rate-limited delivery of active messages for the QQ Bot.
// It accepts send functions via Push() (non-blocking) and executes them at a
// fixed interval via a background goroutine. On Stop(), any remaining queued
// messages are drained and executed before returning.
//
// This is used for "active" messages (those without a msg_id/msg_seq reference),
// which QQ rate-limits at 3 messages per 5 seconds per user (1 message per
// ~1.667s). A 1.7s interval provides a safe margin.
//
// Passive replies (ReplyMessage) are NOT routed through this queue — they are
// sent directly and count toward QQ's per-user rate limit as well, but they
// are infrequent (one per incoming message) and typically within limits.
type MessageQueue struct {
	ch     chan func()
	ticker *time.Ticker
	done   chan struct{}
	wg     sync.WaitGroup
}

// NewMessageQueue creates a MessageQueue with the given interval between sends
// and the given channel capacity. interval should be >= 1.7s for QQ Bot.
// cap sets the buffer size; once full, Push() silently drops messages.
func NewMessageQueue(interval time.Duration, cap int) *MessageQueue {
	mq := &MessageQueue{
		ch:     make(chan func(), cap),
		ticker: time.NewTicker(interval),
		done:   make(chan struct{}),
	}
	mq.wg.Add(1)
	go mq.loop()
	return mq
}

// Push enqueues a send function for rate-limited execution.
// Non-blocking: if the channel is full, the message is silently dropped.
func (mq *MessageQueue) Push(fn func()) {
	select {
	case mq.ch <- fn:
	default:
	}
}

// Stop signals the background goroutine to stop, drains any remaining queued
// messages (executing them immediately), and waits for the goroutine to finish.
// After Stop returns, the MessageQueue must not be used.
func (mq *MessageQueue) Stop() {
	close(mq.done)
	mq.wg.Wait()
	mq.ticker.Stop()
}

// loop is the background goroutine. On each tick, it pops one function from the
// channel and executes it. When done is closed, it drains remaining messages.
func (mq *MessageQueue) loop() {
	defer mq.wg.Done()
	for {
		select {
		case <-mq.done:
			// Drain remaining messages
			for {
				select {
				case fn := <-mq.ch:
					fn()
				default:
					return
				}
			}
		case <-mq.ticker.C:
			select {
			case fn := <-mq.ch:
				fn()
			default:
				// nothing queued; skip this tick
			}
		}
	}
}
