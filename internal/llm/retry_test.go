package llm

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunWithRetry_HappyFirstTry(t *testing.T) {
	var calls atomic.Int32
	err := RunWithRetry(context.Background(), RetryPolicy{MaxRetries: 3},
		nil,
		func(ctx context.Context) error {
			calls.Add(1)
			return nil
		})
	if err != nil {
		t.Errorf("err = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestRunWithRetry_SucceedsAfterN(t *testing.T) {
	var calls atomic.Int32
	err := RunWithRetry(context.Background(),
		RetryPolicy{MaxRetries: 5, InitialDelay: 1 * time.Millisecond, Multiplier: 2},
		nil,
		func(ctx context.Context) error {
			n := calls.Add(1)
			if n < 3 {
				return errors.New("transient")
			}
			return nil
		})
	if err != nil {
		t.Errorf("err = %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRunWithRetry_GivesUp(t *testing.T) {
	var calls atomic.Int32
	myErr := errors.New("permanent")
	err := RunWithRetry(context.Background(),
		RetryPolicy{MaxRetries: 2, InitialDelay: 1 * time.Millisecond},
		nil,
		func(ctx context.Context) error {
			calls.Add(1)
			return myErr
		})
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
	// MaxRetries=2 → total attempts 3 (1 original + 2 retries)
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestRunWithRetry_ShouldRetryFalse_StopsEarly(t *testing.T) {
	var calls atomic.Int32
	nonRetry := errors.New("no-retry")
	err := RunWithRetry(context.Background(),
		RetryPolicy{MaxRetries: 5, InitialDelay: 1 * time.Millisecond},
		func(err error) bool { return !errors.Is(err, nonRetry) },
		func(ctx context.Context) error {
			calls.Add(1)
			return nonRetry
		})
	if !errors.Is(err, nonRetry) {
		t.Errorf("err = %v, want %v", err, nonRetry)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (should stop after shouldRetry=false)", got)
	}
}

func TestRunWithRetry_CtxCancelDuringBackoff(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel ctx before the 3rd call after 2 failures
	errCh := make(chan error, 1)
	go func() {
		errCh <- RunWithRetry(ctx,
			RetryPolicy{MaxRetries: 10, InitialDelay: 100 * time.Millisecond},
			nil,
			func(ctx context.Context) error {
				n := calls.Add(1)
				if n == 2 {
					// Cancel immediately after the 2nd call; this will lead to backoff
					go cancel()
				}
				return errors.New("transient")
			})
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error")
		}
		// Should retain the error from the last fn call (not ctx.Err)
		if errors.Is(err, context.Canceled) {
			t.Errorf("expected last fn error, got ctx.Canceled: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithRetry did not return after ctx cancel")
	}
	// Should not attempt more than 3 times (cancelled during the 3rd backoff)
	if got := calls.Load(); got > 3 {
		t.Errorf("calls = %d, want ≤ 3", got)
	}
}

func TestRunWithRetry_CtxAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var calls atomic.Int32
	err := RunWithRetry(ctx, RetryPolicy{MaxRetries: 3},
		nil,
		func(ctx context.Context) error {
			calls.Add(1)
			return nil
		})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls.Load() != 0 {
		t.Errorf("fn should not be called when ctx already cancelled")
	}
}

func TestRunWithRetry_ZeroMaxRetries_NoRetry(t *testing.T) {
	var calls atomic.Int32
	_ = RunWithRetry(context.Background(), RetryPolicy{MaxRetries: 0},
		nil,
		func(ctx context.Context) error {
			calls.Add(1)
			return errors.New("x")
		})
	if got := calls.Load(); got != 1 {
		t.Errorf("MaxRetries=0: calls = %d, want 1", got)
	}
}

func TestRunWithRetry_BackoffBoundedByMax(t *testing.T) {
	// Observe that delay does not exceed MaxDelay
	var delays []time.Duration
	lastCall := time.Now()

	_ = RunWithRetry(context.Background(),
		RetryPolicy{
			MaxRetries:   4,
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     20 * time.Millisecond,
			Multiplier:   10, // Will quickly exceed MaxDelay
		},
		nil,
		func(ctx context.Context) error {
			now := time.Now()
			delays = append(delays, now.Sub(lastCall))
			lastCall = now
			return errors.New("transient")
		})

	// The first delay is approximately 0 (first call), others should be ≤ MaxDelay + tolerance
	for i := 1; i < len(delays); i++ {
		if delays[i] > 60*time.Millisecond {
			t.Errorf("delay[%d] = %v, want ≤ MaxDelay (+ tolerance)", i, delays[i])
		}
	}
}

func TestRunWithRetry_NilShouldRetry_RetriesAll(t *testing.T) {
	// If shouldRetry is nil, any error should be retried
	var calls atomic.Int32
	_ = RunWithRetry(context.Background(),
		RetryPolicy{MaxRetries: 2, InitialDelay: 1 * time.Millisecond},
		nil, // shouldRetry nil
		func(ctx context.Context) error {
			calls.Add(1)
			return errors.New("x")
		})
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (nil shouldRetry should retry all)", got)
	}
}

func TestRetryPolicy_Normalize_ZeroValue(t *testing.T) {
	// Confirm that a zero-value policy also runs (normalize fills in default values)
	var calls atomic.Int32
	err := RunWithRetry(context.Background(), RetryPolicy{},
		nil,
		func(ctx context.Context) error {
			calls.Add(1)
			return nil
		})
	if err != nil {
		t.Errorf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}