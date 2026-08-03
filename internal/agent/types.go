package agent

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

type Role string

const (
	RoleUser Role = "user"
)

type Kind string

const (
	KindCustom Kind = "custom"
)

// Definition is the static, immutable configuration of an agent.
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

	// ReasoningEffort: "high" | "max" | "" (not sent)
	ReasoningEffort string

	ThinkingEnabled bool
	// ThinkingType: "enabled" (DeepSeek) or "adaptive" (MiniMax M3 etc.).
	ThinkingType string

	// MaxIterations caps tool-use loop iterations per Ask. <= 0 uses DefaultMaxIterations.
	MaxIterations int

	// ContextWindow is the model's context window (tokens). <= 0 falls back to 1M.
	ContextWindow int

	// ExplicitModel: template pinned this model — router cannot override.
	ExplicitModel bool

	// BypassConfirm: from template `permission: true` or global --bypass.
	BypassConfirm bool

	Vision bool

	// Channels: channel_type → instance_id. Each type appears at most once.
	Channels map[string]string

	// NotifyChannel: channel for cron notifications. Defaults to first Channels entry.
	NotifyChannel string
}

// ─── State ────────────────────────────────────────────────────────────────────

// State is the observable runtime state (for UI/metrics only, not control flow).
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

// DefaultMailboxCap absorbs short-term bursts; a full Ask blocks with ctx fallback.
const DefaultMailboxCap = 16

// DefaultMaxIterations: 200 accommodates complex multi-step tasks.
// Exceeding this strongly suggests a loop or misconfiguration.
const DefaultMaxIterations = 200

// DefaultContextWindow fallback when Definition.ContextWindow is unset.
const DefaultContextWindow = 1048576

// DefaultToolTimeout prevents indefinite blocking when a tool hangs.
const DefaultToolTimeout = 5 * time.Minute

// DefaultMaxConsecutiveFailures before the circuit breaker opens.
// Excludes context cancellations.
const DefaultMaxConsecutiveFailures = 3

// DefaultCircuitBreakerResetTimeout: cooldown before allowing a recovery probe.
const DefaultCircuitBreakerResetTimeout = time.Minute

// ModelParams captures per-ask model overrides set by the Router.
// Auto-cleared when the ask completes.
type ModelParams struct {
	ProviderID string // empty = use agent default; reserved for multi-provider
	ModelID    string // empty = use Definition's ModelID

	ThinkingEnabled bool
	ReasoningEffort string // "high" | "max" | ""
	ThinkingType    string // "enabled" (DeepSeek) | "adaptive" (MiniMax M3)

	TaskType      string
	ContextWindow int  // 0 = don't change
	Vision        bool // drop image content when false
}

// ToIFaceOverride converts to iface.ModelOverrideParams for cross-package propagation.
func (m *ModelParams) ToIFaceOverride() *iface.ModelOverrideParams {
	if m == nil {
		return nil
	}
	return &iface.ModelOverrideParams{
		ProviderID:      m.ProviderID,
		ModelID:         m.ModelID,
		ThinkingEnabled: m.ThinkingEnabled,
		ReasoningEffort: m.ReasoningEffort,
		ThinkingType:    m.ThinkingType,
		TaskType:        m.TaskType,
		ContextWindow:   m.ContextWindow,
		Vision:          m.Vision,
	}
}

// ─── Runtime observability ───────────────────────────────────────────────

// agentRuntime bundles mutable runtime state under a single RWMutex.
// Replaces scattered atomics; read by inspect_agent and Watch().
type agentRuntime struct {
	state               State
	errCount            int32
	lastErr             string
	consecutiveFailures int32
	lastFailureAt       time.Time
	exitErr             error

	prompt    string
	iter      int32
	tool      string
	toolArgs  string
	startedAt time.Time
}

// WorkStatus is the public snapshot from Agent.CurrentWork().
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
