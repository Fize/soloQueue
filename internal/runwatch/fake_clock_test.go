package runwatch

import (
	"sync"
	"time"
)

type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewFakeClock(now time.Time) *FakeClock { return &FakeClock{now: now} }

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}
