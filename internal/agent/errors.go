// Package agent implements the actor-model agent: LLM + tools + mailbox + lifecycle.
package agent

import "errors"

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	ErrAgentNil = errors.New("agent: nil")
	ErrEmptyID  = errors.New("agent: empty id")

	ErrAlreadyStarted = errors.New("agent: already started")
	ErrNotStarted     = errors.New("agent: not started")
	ErrStopped        = errors.New("agent: stopped")
	ErrQuarantined    = errors.New("agent: quarantined")
	ErrStopTimeout    = errors.New("agent: stop timeout")

	// ErrMaxIterations signals the tool loop exceeded MaxIterations — likely a loop or misconfiguration.
	ErrMaxIterations = errors.New("agent: too many tool calls without finishing — rephrase your request or split it into smaller steps")

	// ErrCircuitBreakerOpen rejects new tasks after N consecutive fatal failures.
	ErrCircuitBreakerOpen = errors.New("agent: circuit breaker open — too many consecutive failures, task rejected")
)
