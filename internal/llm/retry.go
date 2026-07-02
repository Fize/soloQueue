package llm

import (
	"context"
	"time"
)

// RetryPolicy Exponential backoff configuration
//
// Behavior:
//   - Waits for InitialDelay after the first failure
//   - Multiplies by Multiplier each time, capped at MaxDelay
//   - Attempts at most MaxRetries additional retries (total attempts = MaxRetries + 1)
//   - MaxRetries = 0 → No retries
type RetryPolicy struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// normalize normalizes the policy (primarily to prevent issues like zero-value division).
func (p RetryPolicy) normalize() RetryPolicy {
	if p.Multiplier <= 1.0 {
		p.Multiplier = 2.0
	}
	if p.InitialDelay <= 0 {
		p.InitialDelay = 500 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.MaxRetries < 0 {
		p.MaxRetries = 0
	}
	return p
}

// RunWithRetry executes fn, retrying according to the policy.
//
// Parameters:
//
//	ctx         The caller's context; a cancellation will immediately stop the retry (without waiting for backoff).
//	policy      The backoff policy.
//	shouldRetry Determines whether a specific failure should be retried; nil = retry all.
//	fn          The actual work to be executed.
//
// Returns: The error from the last fn execution (returns nil on success); ctx cancellation returns ctx.Err().
//
// Idempotency: Assumes fn is idempotent — the client must only call this helper during retry-safe phases (e.g., before an HTTP response).
// Calling this helper; retrying should not occur after the request body has started being read.
func RunWithRetry(
	ctx context.Context,
	policy RetryPolicy,
	shouldRetry func(error) bool,
	fn func(ctx context.Context) error,
) error {
	return RunWithRetryHooks(ctx, policy, shouldRetry, nil, fn)
}

// RunWithRetryHooks is similar to RunWithRetry but allows injecting an onRetry callback.
//
// onRetry(attempt, delay, err): Called after deciding to retry and before backoff starts.
//   - attempt: The current attempt number that just failed (1-indexed).
//   - delay: The backoff duration before the next attempt.
//   - err: The error from the failed attempt.
//
// The callback is only triggered on the "decided to retry" path; if shouldRetry=false or attempt==MaxRetries
// no further retries will occur, and onRetry will not be called.
func RunWithRetryHooks(
	ctx context.Context,
	policy RetryPolicy,
	shouldRetry func(error) bool,
	onRetry func(attempt int, delay time.Duration, err error),
	fn func(ctx context.Context) error,
) error {
	p := policy.normalize()
	delay := p.InitialDelay

	var lastErr error
	for attempt := 0; attempt <= p.MaxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// Last attempt: no more retries.
		if attempt == p.MaxRetries {
			break
		}
		// Not retryable.
		if shouldRetry != nil && !shouldRetry(err) {
			break
		}

		// Callback: inform the caller that a retry will occur, for logging / metrics.
		if onRetry != nil {
			onRetry(attempt+1, delay, err)
		}

		// Wait for delay or ctx cancel.
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return lastErr
		}

		// Delay for the next round (exponential increase, capped at MaxDelay).
		next := time.Duration(float64(delay) * p.Multiplier)
		if next > p.MaxDelay {
			next = p.MaxDelay
		}
		delay = next
	}
	return lastErr
}