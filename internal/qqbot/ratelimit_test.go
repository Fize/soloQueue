package qqbot

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestMessageQueue_PushAndDrain(t *testing.T) {
	// Use a very short interval so the test completes quickly.
	mq := NewMessageQueue(10*time.Millisecond, 10)
	defer mq.Stop()

	var count atomic.Int32
	for i := 0; i < 5; i++ {
		mq.Push(func() {
			count.Add(1)
		})
	}

	// Wait for all messages to be drained
	time.Sleep(100 * time.Millisecond)

	if got := count.Load(); got != 5 {
		t.Errorf("expected 5 messages sent, got %d", got)
	}
}

func TestMessageQueue_StopDrainsRemaining(t *testing.T) {
	mq := NewMessageQueue(50*time.Millisecond, 10)

	var count atomic.Int32
	for i := 0; i < 5; i++ {
		mq.Push(func() {
			count.Add(1)
		})
	}

	// Stop immediately should drain remaining
	mq.Stop()

	if got := count.Load(); got != 5 {
		t.Errorf("expected 5 messages drained on stop, got %d", got)
	}
}

func TestMessageQueue_DropOnFull(t *testing.T) {
	// Cap of 1 — second push should be dropped because the first is still
	// in the channel waiting for the ticker.
	mq := NewMessageQueue(50*time.Millisecond, 1)
	defer mq.Stop()

	var count atomic.Int32
	mq.Push(func() {
		count.Add(1)
	})
	mq.Push(func() {
		count.Add(1)
	})

	time.Sleep(200 * time.Millisecond)

	// Only one should have been sent since the second was dropped (cap 1)
	if got := count.Load(); got != 1 {
		t.Errorf("expected 1 message (cap 1, second dropped), got %d", got)
	}
}

func TestMessageQueue_Interval(t *testing.T) {
	// 50ms interval. 3 pushes, 3 ticks needed = ~150ms total.
	// Wait long enough for all ticks to fire.
	mq := NewMessageQueue(50*time.Millisecond, 10)
	defer mq.Stop()

	var count atomic.Int32
	for i := 0; i < 3; i++ {
		mq.Push(func() {
			count.Add(1)
		})
	}

	// Wait for 3 ticks (~150ms), plus generous margin
	time.Sleep(500 * time.Millisecond)

	if got := count.Load(); got != 3 {
		t.Errorf("expected 3 messages sent after 500ms, got %d", got)
	}
}
