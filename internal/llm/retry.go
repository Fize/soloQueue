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
	MaxRetries          int
	RateLimitMaxRetries int // separate retry budget for 429; 0 means use MaxRetries
	InitialDelay        time.Duration
	MaxDelay            time.Duration
	Multiplier          float64
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
	if p.RateLimitMaxRetries < 0 {
		p.RateLimitMaxRetries = 0
	}
	return p
}

// rateLimitMaxRetries returns the effective max retries for rate-limit (429) errors.
// If RateLimitMaxRetries is explicitly set (>0), use it; otherwise fall back to MaxRetries.
func (p RetryPolicy) rateLimitMaxRetries() int {
	if p.RateLimitMaxRetries > 0 {
		return p.RateLimitMaxRetries
	}
	return p.MaxRetries
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
	return RunWithRetryHooks(ctx, policy, shouldRetry, nil, nil, fn)
}

// RunWithRetryHooks is similar to RunWithRetry but allows injecting an onRetry callback
// and an isRateLimit predicate for per-error-type retry budgets.
//
// onRetry(attempt, delay, err): Called after deciding to retry and before backoff starts.
//   - attempt: The current attempt number that just failed (1-indexed).
//   - delay: The backoff duration before the next attempt.
//   - err: The error from the failed attempt.
//
// isRateLimit(err) bool: If true, uses RateLimitMaxRetries instead of MaxRetries. nil = never.
//
// The callback is only triggered on the "decided to retry" path; if shouldRetry=false or attempt==MaxRetries
// no further retries will occur, and onRetry will not be called.
func RunWithRetryHooks(
	ctx context.Context,
	policy RetryPolicy,
	shouldRetry func(error) bool,
	isRateLimit func(error) bool,
	onRetry func(attempt int, delay time.Duration, err error),
	fn func(ctx context.Context) error,
) error {
	p := policy.normalize()
	delay := p.InitialDelay

	// Determine effective max retries: 429 errors get a separate (larger) budget.
	rateLimitMax := p.rateLimitMaxRetries()

	// The loop bound is the larger of the two, so we don't prematurely exit for 429.
	loopMax := p.MaxRetries
	if isRateLimit != nil && rateLimitMax > loopMax {
		loopMax = rateLimitMax
	}

	var lastErr error
	for attempt := 0; attempt <= loopMax; attempt++ {
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

		// Determine the effective max for this particular error.
		max := p.MaxRetries
		if isRateLimit != nil && isRateLimit(err) {
			max = rateLimitMax
		}

		// Last attempt: no more retries.
		if attempt >= max {
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
