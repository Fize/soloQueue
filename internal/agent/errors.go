// Package agent provides the skeleton for an agent:
//
//   - Agent: A long-running unit holding LLM + config + logs. Start launches an internal
//     goroutine to serially consume jobs from the mailbox; Ask is a synchronous
//     wrapper for "send prompt → wait for reply"; Submit is a low-level escape hatch
//     for submitting any custom job.
//   - Registry: A concurrent-safe map from ID to Agent, supporting batch Start/Stop/Shutdown.
//   - LLMClient: The minimal interface for LLM calls (integrating with DeepSeek / FakeLLM).
//   - Tool / ToolRegistry: Tool abstraction for LLM calls; an agent's runOnce automatically
//     loops and dispatches upon receiving tool_calls — execute → feed back → Chat again,
//     until the LLM no longer requests tools (capped at MaxIterations).
//   - FakeLLM: An LLMClient implementation for testing / demos (supports ToolCallsByTurn
//     for scripting multi-turn tool-use scenarios).
//   - RunOnce: A one-off call that does not launch a goroutine (for scripting / simple CLI scenarios).
//
// This phase does not include: parallel tool execution, per-tool timeouts, ChatStream tool looping,
// supervisor / restart. These are left for subsequent phases.
package agent

import "errors"

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	// ErrAgentNotFound Target agent not found in Registry
	ErrAgentNotFound = errors.New("agent: not found")
	// ErrAgentAlreadyExists ID already exists during Register
	ErrAgentAlreadyExists = errors.New("agent: already exists")
	// ErrAgentNil nil agent passed to Register
	ErrAgentNil = errors.New("agent: nil")
	// ErrEmptyID agent's Definition.ID is empty
	ErrEmptyID = errors.New("agent: empty id")

	// ErrAlreadyStarted Start was called but agent is already running
	ErrAlreadyStarted = errors.New("agent: already started")
	// ErrNotStarted Ask / Submit / Stop was called but agent has never Started, or has Stopped after Starting
	ErrNotStarted = errors.New("agent: not started")
	// ErrStopped agent has entered Stopped state; pending Ask / Submit calls return with this error
	ErrStopped = errors.New("agent: stopped")
	// ErrStopTimeout Stop timed out when called with a timeout; the goroutine will eventually exit
	ErrStopTimeout = errors.New("agent: stop timeout")

	// ErrMaxIterations runOnce's tool-use loop exceeded Definition.MaxIterations
	ErrMaxIterations = errors.New("agent: too many tool calls without finishing — rephrase your request or split it into smaller steps")

	// ErrCircuitBreakerOpen is returned when the agent refuses to execute a new
	// task because the previous N consecutive attempts all failed with fatal
	// errors (e.g., ChatStream failure). This breaks infinite retry loops.
	ErrCircuitBreakerOpen = errors.New("agent: circuit breaker open — too many consecutive failures, task rejected")
)