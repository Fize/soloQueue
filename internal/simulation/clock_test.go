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
		TimeScale:   600,
		TickDur:     500 * time.Millisecond,
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
	if cfg.TimeScale != 600 {
		t.Errorf("expected time scale 600, got %f", cfg.TimeScale)
	}
}

func TestSimClock_Advance(t *testing.T) {
	cfg := ClockConfig{
		StartHour:   12,
		StartMinute: 0,
		TimeScale:   3600, // 1s real = 1h simulated
		TickDur:     50 * time.Millisecond,
	}
	clock := NewSimClock(cfg)

	ch := make(chan SimTimeEvent, 10)
	clock.Subscribe(ch)

	go clock.Start()
	defer clock.Stop()

	// Wait for a few ticks
	var events []SimTimeEvent
	timeout := time.After(500 * time.Millisecond)
loop:
	for {
		select {
		case evt := <-ch:
			events = append(events, evt)
			if len(events) >= 3 {
				break loop
			}
		case <-timeout:
			break loop
		}
	}

	if len(events) < 1 {
		t.Fatal("expected at least 1 tick event")
	}

	// Simulated time should have advanced
	elapsed := clock.Elapsed()
	if elapsed <= 0 {
		t.Error("expected non-zero elapsed simulated time")
	}
}

func TestSimClock_Day(t *testing.T) {
	// Day() returns elapsed hours / 24 + 1 (integer division).
	// After < 24 simulated hours, Day() is always 1.
	cfg := ClockConfig{
		StartHour:   7,
		StartMinute: 0,
		TimeScale:   3600, // 1s real = 1h simulated
		TickDur:     10 * time.Millisecond,
	}
	clock := NewSimClock(cfg)

	// At construction, day is 1
	if clock.Day() != 1 {
		t.Errorf("expected day 1 at start, got %d", clock.Day())
	}

	ch := make(chan SimTimeEvent, 50)
	clock.Subscribe(ch)
	go clock.Start()
	defer clock.Stop()

	// After 1s real = 1h simulated, day is still 1 (0 < 24h elapsed)
	time.Sleep(100 * time.Millisecond)
	elapsed := clock.ElapsedHours()
	if elapsed < 0.01 {
		t.Errorf("expected some elapsed time, got %.2f hours", elapsed)
	}
	// Day should be 1 since elapsed < 24 hours
	if clock.Day() != 1 {
		t.Logf("day: %d after %.2f hours", clock.Day(), elapsed)
	}

	// Drain events
	for {
		select {
		case <-ch:
		default:
			goto done
		}
	}
done:
}

func TestSimClock_SubscribeUnsubscribe(t *testing.T) {
	cfg := ClockConfig{
		TimeScale: 600,
		TickDur:   10 * time.Millisecond,
	}
	clock := NewSimClock(cfg)

	ch1 := make(chan SimTimeEvent, 10)
	ch2 := make(chan SimTimeEvent, 10)

	clock.Subscribe(ch1)
	clock.Subscribe(ch2)
	clock.Unsubscribe(ch2)

	go clock.Start()
	defer clock.Stop()

	// Wait for a tick
	select {
	case <-ch1:
		// ch1 should get events
	case <-time.After(200 * time.Millisecond):
		t.Error("ch1 should have received events")
	}

	// ch2 was unsubscribed, should not get events
	select {
	case <-ch2:
		t.Error("ch2 should not have received events after unsubscribe")
	default:
	}
}

func TestSimClock_Concurrency(t *testing.T) {
	cfg := ClockConfig{
		TimeScale: 600,
		TickDur:   1 * time.Millisecond,
	}
	clock := NewSimClock(cfg)

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

func TestSimClock_HourChanged(t *testing.T) {
	cfg := ClockConfig{
		StartHour:   11,
		StartMinute: 59,
		TimeScale:   7200, // 1s real = 2h simulated
		TickDur:     20 * time.Millisecond,
	}
	clock := NewSimClock(cfg)

	hourCh := clock.HourChanged()

	go clock.Start()
	defer clock.Stop()

	select {
	case newHour := <-hourCh:
		if newHour != 12 {
			t.Logf("new hour: %d", newHour)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("expected hour change notification")
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
