package simulation

import (
	"sync"
	"time"
)

// SimClock drives simulated time for the agent-based simulation.
// Agents operate on logical time rather than wall clock, enabling
// daily schedules, time-of-day awareness, and time-scaled execution.
type SimClock struct {
	startTime  time.Time // wall clock when simulation started
	tickStart  time.Time // simulated time at simulation start (default 07:00)
	current    time.Time // current simulated time
	timeScale  float64   // multiplier: 1 wall second = timeScale simulated seconds
	tickDur    time.Duration // duration of one simulated tick
	mu         sync.RWMutex

	hourChanged chan int // notifies listeners on hour change (carries new hour)
	stopCh      chan struct{}
	listeners   []chan SimTimeEvent
	listenersMu sync.RWMutex
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
	StartHour   int     // simulated hour at simulation start (default 7)
	StartMinute int     // simulated minute at simulation start (default 0)
	TimeScale   float64 // simulated seconds per wall second (default 600 = 10min per sec)
	TickDur     time.Duration // wall time per tick advance (default 500ms)
}

// DefaultClockConfig returns sensible defaults: start at 07:00, 10min per real second.
func DefaultClockConfig() ClockConfig {
	return ClockConfig{
		StartHour:   7,
		StartMinute: 0,
		TimeScale:   600, // 1s real = 10min simulated
		TickDur:     500 * time.Millisecond,
	}
}

// NewSimClock creates a new simulated clock.
func NewSimClock(cfg ClockConfig) *SimClock {
	now := time.Now()
	simStart := time.Date(now.Year(), now.Month(), now.Day(), cfg.StartHour, cfg.StartMinute, 0, 0, now.Location())

	return &SimClock{
		startTime:   now,
		tickStart:   simStart,
		current:     simStart,
		timeScale:   cfg.TimeScale,
		tickDur:     cfg.TickDur,
		hourChanged: make(chan int, 24),
		stopCh:      make(chan struct{}),
	}
}

// Start begins the clock ticker. Call in a goroutine.
func (c *SimClock) Start() {
	ticker := time.NewTicker(c.tickDur)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.advance()
		}
	}
}

// Stop halts the clock.
func (c *SimClock) Stop() {
	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}
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

// Subscribe adds a listener channel that receives time events.
func (c *SimClock) Subscribe(ch chan SimTimeEvent) {
	c.listenersMu.Lock()
	c.listeners = append(c.listeners, ch)
	c.listenersMu.Unlock()
}

// Unsubscribe removes a listener.
func (c *SimClock) Unsubscribe(ch chan SimTimeEvent) {
	c.listenersMu.Lock()
	for i, l := range c.listeners {
		if l == ch {
			c.listeners = append(c.listeners[:i], c.listeners[i+1:]...)
			break
		}
	}
	c.listenersMu.Unlock()
}

func (c *SimClock) advance() {
	c.mu.Lock()
	oldHour := c.current.Hour()
	advance := time.Duration(float64(c.tickDur) * c.timeScale)
	c.current = c.current.Add(advance)
	newHour := c.current.Hour()
	c.mu.Unlock()

	evt := SimTimeEvent{
		SimTime: c.current,
		Hour:    newHour,
		Minute:  c.current.Minute(),
		Day:     c.Day(),
	}

	c.listenersMu.RLock()
	for _, ch := range c.listeners {
		select {
		case ch <- evt:
		default:
		}
	}
	c.listenersMu.RUnlock()

	if oldHour != newHour {
		select {
		case c.hourChanged <- newHour:
		default:
		}
	}
}

// HourChanged returns a channel that receives the new hour on each hour change.
func (c *SimClock) HourChanged() <-chan int {
	return c.hourChanged
}
