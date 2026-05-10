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

// Definition 是 agent 的静态配置
//
// 所有字段都是"起 agent 时一次性写入"的不可变数据。
// 不含 supervision / restart policy —— 本 phase agent 不自管生命周期。
type Definition struct {
	ID           string
	Name         string
	Role         Role
	Kind         Kind
	ModelID      string
	SystemPrompt string
	Temperature  float64
	MaxTokens    int

	// ReasoningEffort 推理努力等级，用于支持思考模式的 V4 模型
	// "high" | "max" | ""（空表示不发送此参数）
	ReasoningEffort string

	// ThinkingEnabled 是否启用思考模式（DeepSeek V4 模型）
	ThinkingEnabled bool

	// MaxIterations 是 tool-use 循环的最大轮数（一次 Ask 内允许的 LLM.Chat 次数）
	//
	// <= 0 使用 DefaultMaxIterations（100）。
	// 无 tools 时循环第一轮就退出（LLM 不返回 tool_calls），此值不生效。
	MaxIterations int

	// ContextWindow 是模型的上下文窗口大小（token 数），用于 Overflow 硬限检查。
	// 对应 config.LLMModel.ContextWindow。
	// <= 0 时使用兜底默认值 1048576 (1M tokens)。
	ContextWindow int

	// ExplicitModel indicates this agent's model was explicitly configured
	// (from agent template YAML). When true, SetModelOverride is a no-op —
	// the template's model takes precedence over task-level routing.
	ExplicitModel bool

	// BypassConfirm skips all tool confirmations for this agent.
	// Set from agent template `permission: true` or global --bypass flag.
	BypassConfirm bool
}

// ─── State ────────────────────────────────────────────────────────────────────

// State 是 agent 运行时的观察态
//
// 仅供外部观察（UI / metrics），内部并不基于 State 做分支决策
// —— 代码流本身即状态机，不存在"迁移表"。
type State int32

const (
	// StateIdle 等待 mailbox 有 job 或 Stop 信号
	StateIdle State = iota
	// StateProcessing 当前正在执行某个 job（Ask 或 Submit 投递的）
	StateProcessing
	// StateStopping 已请求 Stop，正在 drain mailbox
	StateStopping
	// StateStopped run goroutine 已退出
	StateStopped
)

// String 便于日志输出
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

// DefaultMailboxCap 是 mailbox 默认容量
//
// 值的选择：8 足够吸收短时突发；满了的 Ask 会阻塞（有 ctx 兜底），不丢消息。
// 需要更大容量的场景通过 WithMailboxCap(N) 指定。
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
}

// ─── Runtime observability ───────────────────────────────────────────────

// agentRuntime bundles all per-agent mutable runtime state under a single
// sync.RWMutex. This replaces the previous scattered atomic fields and adds
// work-tracking fields for the inspect_agent tool and Watch() method.
type agentRuntime struct {
	state              State
	errCount           int32
	lastErr            string
	consecutiveFailures int32
	exitErr            error

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
