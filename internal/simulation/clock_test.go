package simulation

import (
	"sync"
	"testing"
	"time"
)

func TestSimClock_New(t *testing.T) {
	cfg := ClockConfig{
		StartHour:   9,
		StartMinute: 30,
		StepSize:    10 * time.Minute,
	}
	clock := NewSimClock(cfg)

	now := clock.Now()
	if now.Hour() != 9 || now.Minute() != 30 {
		t.Errorf("expected 09:30, got %s", now.Format("15:04"))
	}
}

func TestSimClock_DefaultConfig(t *testing.T) {
	cfg := DefaultClockConfig()
	if cfg.StartHour != 7 {
		t.Errorf("expected start hour 7, got %d", cfg.StartHour)
	}
	if cfg.StepSize != 5*time.Minute {
		t.Errorf("expected step size 5m, got %s", cfg.StepSize)
	}
}

func TestSimClock_Advance(t *testing.T) {
	cfg := ClockConfig{
		StartHour:   12,
		StartMinute: 0,
		StepSize:    1 * time.Hour,
	}
	clock := NewSimClock(cfg)

	// Advance 3 times
	for i := 0; i < 3; i++ {
		evt := clock.Advance()
		if evt.Hour != 12+i+1 {
			t.Errorf("step %d: expected hour %d, got %d", i, 12+i+1, evt.Hour)
		}
	}

	if clock.StepCount() != 3 {
		t.Errorf("expected step count 3, got %d", clock.StepCount())
	}

	// Simulated time should have advanced by 3 hours
	elapsed := clock.Elapsed()
	if elapsed != 3*time.Hour {
		t.Errorf("expected elapsed 3h, got %s", elapsed)
	}

	if clock.ElapsedHours() != 3.0 {
		t.Errorf("expected elapsed hours 3.0, got %f", clock.ElapsedHours())
	}
}

func TestSimClock_Day(t *testing.T) {
	cfg := ClockConfig{
		StartHour:   7,
		StartMinute: 0,
		StepSize:    6 * time.Hour, // 4 advances = 1 day
	}
	clock := NewSimClock(cfg)

	// At construction, day is 1
	if clock.Day() != 1 {
		t.Errorf("expected day 1 at start, got %d", clock.Day())
	}

	// After 3 advances (18h), still day 1
	for i := 0; i < 3; i++ {
		clock.Advance()
	}
	if clock.Day() != 1 {
		t.Errorf("expected day 1 after 18h, got %d", clock.Day())
	}

	// After 4th advance (24h), day 2
	clock.Advance()
	if clock.Day() != 2 {
		t.Errorf("expected day 2 after 24h, got %d", clock.Day())
	}
}

func TestSimClock_ConcurrentReads(t *testing.T) {
	clock := NewSimClock(DefaultClockConfig())

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = clock.Now()
				_ = clock.Hour()
				_ = clock.Day()
				_ = clock.TimeString()
				_ = clock.Elapsed()
				_ = clock.ElapsedHours()
			}
		}()
	}
	wg.Wait()
}

func TestSimClock_ConcurrentAdvanceAndRead(t *testing.T) {
	clock := NewSimClock(DefaultClockConfig())

	var wg sync.WaitGroup
	// Writer: advance 10 times
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			clock.Advance()
		}
	}()

	// Readers: read concurrently
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = clock.Now()
				_ = clock.ElapsedHours()
			}
		}()
	}
	wg.Wait()

	if clock.StepCount() != 10 {
		t.Errorf("expected 10 steps, got %d", clock.StepCount())
	}
}

func TestSimTimeEvent(t *testing.T) {
	now := time.Now()
	evt := SimTimeEvent{
		SimTime: now,
		Hour:    14,
		Minute:  30,
		Day:     3,
	}
	if evt.Hour != 14 {
		t.Errorf("expected hour 14, got %d", evt.Hour)
	}
	if evt.Day != 3 {
		t.Errorf("expected day 3, got %d", evt.Day)
	}
}

func TestSimClock_AdvanceReturnsCorrectDay(t *testing.T) {
	cfg := ClockConfig{
		StartHour:   0,
		StartMinute: 0,
		StepSize:    12 * time.Hour,
	}
	clock := NewSimClock(cfg)

	evt := clock.Advance()
	if evt.Day != 1 {
		t.Errorf("expected day 1 after first advance, got %d", evt.Day)
	}

	evt = clock.Advance()
	if evt.Day != 2 {
		t.Errorf("expected day 2 after 24h, got %d", evt.Day)
	}
}
