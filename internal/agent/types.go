package agent

import (
	"time"
)

type Role string

const (
	RoleUser Role = "user"
)

type Kind string

const (
	KindCustom Kind = "custom"
)

// Definition is the static configuration of an agent
//
// All fields are immutable data "written once when the agent starts".
// Does not include supervision / restart policy — in this phase, the agent does not manage its own lifecycle.
type Definition struct {
	ID           string
	Name         string
	Role         Role
	Kind         Kind
	ProviderID   string
	ModelID      string
	SystemPrompt string
	Temperature  float64
	MaxTokens    int

	// ReasoningEffort is the reasoning effort level, used to support thinking mode of V4 models
	// "high" | "max" | "" (empty means this parameter is not sent)
	ReasoningEffort string

	// ThinkingEnabled indicates whether thinking mode is enabled (DeepSeek V4 models)
	ThinkingEnabled bool

	// MaxIterations is the maximum number of tool-use loop iterations (LLM.Chat calls allowed within one Ask)
	//
	// If <= 0, DefaultMaxIterations (100) is used.
	// If no tools are present, the loop exits after the first round (LLM returns no tool_calls), making this value ineffective.
	MaxIterations int

	// ContextWindow is the model's context window size (in tokens), used for overflow hard limit checks.
	// Corresponds to config.LLMModel.ContextWindow.
	// If <= 0, the fallback default value 1048576 (1M tokens) is used.
	ContextWindow int

	// ExplicitModel indicates this agent's model was explicitly configured
	// (from agent template YAML). When true, SetModelOverride is a no-op —
	// the template's model takes precedence over task-level routing.
	ExplicitModel bool

	// BypassConfirm skips all tool confirmations for this agent.
	// Set from agent template `permission: true` or global --bypass flag.
	BypassConfirm bool

	// Vision indicates the model supports multimodal image_url content parts.
	Vision bool
}

// ─── State ────────────────────────────────────────────────────────────────────

// State is the observable runtime state of an agent
//
// For external observation only (UI / metrics), internally no branching decisions are made based on State
// — the code flow itself is the state machine; there is no "transition table".
type State int32

const (
	// StateIdle waits for a job in the mailbox or a Stop signal
	StateIdle State = iota
	// StateProcessing is currently executing a job (submitted via Ask or Submit)
	StateProcessing
	// StateStopping Stop has been requested, draining mailbox
	StateStopping
	// StateStopped run goroutine has exited
	StateStopped
)

// String provides a convenient string representation for logging
func (s State) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateProcessing:
		return "processing"
	case StateStopping:
		return "stopping"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ─── Defaults ────────────────────────────────────────────────────────────────

// DefaultMailboxCap is the default mailbox capacity
//
// Value choice: 8 is sufficient to absorb short-term bursts; a full Ask will block (with ctx fallback), no messages are lost.
// For scenarios requiring larger capacity, specify via WithMailboxCap(N).
const DefaultMailboxCap = 16

// DefaultMaxIterations is the default maximum number of tool-use loop
// iterations per Ask call.
//
// 100 accommodates complex multi-step tasks and delegation resumption.
// Typical tasks: 2-4 rounds; with delegation: 10-20 rounds.
// Exceeding 100 strongly suggests the LLM is looping or tools are misconfigured.
const DefaultMaxIterations = 200

// DefaultContextWindow is the fallback context window size (tokens).
// Used when Definition.ContextWindow is unset (<= 0).
const DefaultContextWindow = 1048576

// DefaultToolTimeout is the fallback timeout for tools that do not have
// an explicit per-tool timeout via WithToolTimeout. Prevents indefinite
// blocking when a tool hangs.
const DefaultToolTimeout = 5 * time.Minute

// DefaultMaxConsecutiveFailures is the number of consecutive fatal streamLoop
// failures before the circuit breaker opens and rejects new tasks.
// Fatal failures include ChatStream errors, buildMessages errors, and
// MaxIterations exceeded. Context cancellations are excluded.
const DefaultMaxConsecutiveFailures = 3

// ─── ModelParams (per-ask override) ─────────────────────────────────────────

// ModelParams captures per-ask model parameter overrides.
//
// When set on an Agent via SetModelOverride, the streamLoop uses these values
// instead of Definition defaults for that specific ask cycle. After the ask
// completes, the override is automatically cleared.
//
// This enables the Router to dynamically select different models based on
// task complexity without recreating the Agent.
type ModelParams struct {
	// ProviderID identifies which LLM provider to use (e.g., "deepseek", "openai").
	// Empty means use the agent's default provider.
	// Reserved for future multi-provider support — currently the Agent has a single LLMClient.
	ProviderID string

	// ModelID is the actual API model name (e.g., "deepseek-v4-pro").
	// Empty means use the agent Definition's ModelID.
	ModelID string

	// ThinkingEnabled controls whether thinking/reasoning mode is activated.
	ThinkingEnabled bool

	// ReasoningEffort controls the reasoning depth: "high", "max", or "" (disabled).
	ReasoningEffort string

	// Level is the classification level label for this task (e.g., "L1-SimpleSingleFile").
	// Set by the task router for L1; may be set from delegation context for L2/L3.
	Level string

	// ContextWindow is the model's context window capacity (tokens).
	// Used to resize the ContextWindow when the model changes.
	// 0 means don't change (backward compatible).
	ContextWindow int

	// Vision indicates the model supports multimodal image_url content parts.
	// When false, image content is dropped and replaced with text annotations.
	Vision bool
}

// ─── Runtime observability ───────────────────────────────────────────────

// agentRuntime bundles all per-agent mutable runtime state under a single
// sync.RWMutex. This replaces the previous scattered atomic fields and adds
// work-tracking fields for the inspect_agent tool and Watch() method.
type agentRuntime struct {
	state               State
	errCount            int32
	lastErr             string
	consecutiveFailures int32
	exitErr             error

	prompt    string
	iter      int32
	tool      string
	toolArgs  string
	startedAt time.Time
}

// WorkStatus is the public snapshot returned by Agent.CurrentWork().
type WorkStatus struct {
	State               State  `json:"state"`
	Prompt              string `json:"prompt"`
	Iteration           int    `json:"iteration"`
	CurrentTool         string `json:"current_tool,omitempty"`
	CurrentToolArgs     string `json:"current_tool_args,omitempty"`
	Elapsed             string `json:"elapsed"`
	ErrorCount          int    `json:"error_count"`
	LastError           string `json:"last_error,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	PendingDelegations  int    `json:"pending_delegations"`
}