package simulation

import (
	"sync"
	"time"
)

// SimClock drives simulated time for the agent-based simulation.
// Unlike the previous goroutine-based ticker design, this clock is
// advanced explicitly by the simulation engine's barrier loop.
// Each Advance() call advances simulated time by StepSize, ensuring
// that all agents process every tick before time moves forward.
type SimClock struct {
	tickStart time.Time // simulated time at simulation start (default 07:00)
	current   time.Time // current simulated time
	stepSize  time.Duration // simulated time per Advance() call
	stepCount int
	mu        sync.RWMutex
}

// SimTimeEvent carries time-change notifications.
type SimTimeEvent struct {
	SimTime time.Time `json:"sim_time"`
	Hour    int       `json:"hour"`
	Minute  int       `json:"minute"`
	Day     int       `json:"day"` // day number since simulation start
}

// ClockConfig configures the simulated clock.
type ClockConfig struct {
	StartHour   int           // simulated hour at simulation start (default 7)
	StartMinute int           // simulated minute at simulation start (default 0)
	StepSize    time.Duration // simulated time per Advance() call (default 5min)
}

// DefaultClockConfig returns sensible defaults: start at 07:00, 5 minutes per step.
func DefaultClockConfig() ClockConfig {
	return ClockConfig{
		StartHour:   7,
		StartMinute: 0,
		StepSize:    5 * time.Minute,
	}
}

// NewSimClock creates a new simulated clock.
func NewSimClock(cfg ClockConfig) *SimClock {
	now := time.Now()
	simStart := time.Date(now.Year(), now.Month(), now.Day(), cfg.StartHour, cfg.StartMinute, 0, 0, now.Location())

	if cfg.StepSize <= 0 {
		cfg.StepSize = 5 * time.Minute
	}

	return &SimClock{
		tickStart: simStart,
		current:   simStart,
		stepSize:  cfg.StepSize,
	}
}

// Advance advances the simulated time by one step and returns the event.
// Called by the main barrier loop after all agents have completed a round.
func (c *SimClock) Advance() SimTimeEvent {
	c.mu.Lock()
	c.current = c.current.Add(c.stepSize)
	c.stepCount++
	evt := SimTimeEvent{
		SimTime: c.current,
		Hour:    c.current.Hour(),
		Minute:  c.current.Minute(),
		Day:     int(c.current.Sub(c.tickStart).Hours()/24) + 1,
	}
	c.mu.Unlock()
	return evt
}

// SetStepSize changes the step size for subsequent Advance() calls.
// The change takes effect at the next Advance() call. If d <= 0, no change.
func (c *SimClock) SetStepSize(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stepSize = d
}

// StepSize returns the current step size used by Advance().
func (c *SimClock) StepSize() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stepSize
}

// Now returns the current simulated time.
func (c *SimClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// Hour returns the current simulated hour (0-23).
func (c *SimClock) Hour() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Hour()
}

// Day returns the day number since simulation start (1-indexed).
func (c *SimClock) Day() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return int(c.current.Sub(c.tickStart).Hours()/24) + 1
}

// TimeString returns the current simulated time as "HH:MM".
func (c *SimClock) TimeString() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Format("15:04")
}

// Elapsed returns the simulated duration since start.
func (c *SimClock) Elapsed() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Sub(c.tickStart)
}

// ElapsedHours returns the simulated hours since start.
func (c *SimClock) ElapsedHours() float64 {
	return c.Elapsed().Hours()
}

// StepCount returns the number of Advance() calls made.
func (c *SimClock) StepCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stepCount
}
