package agent

import "time"

// Role 区分系统内置 agent 和用户创建 agent
type Role string

const (
	RoleSystem Role = "system"
	RoleUser   Role = "user"
)

// Kind 描述 agent 的行为类型
//
// 本 phase 仅保留 KindChat / KindCustom 作为占位；真正的行为分支
// （code / planner / evaluator 等）等到 tool 系统落地时按需扩展。
type Kind string

const (
	KindChat   Kind = "chat"
	KindCustom Kind = "custom"
)

// Definition 是 agent 的静态配置
//
// 所有字段都是"起 agent 时一次性写入"的不可变数据。
// 不含 supervision / restart policy —— 本 phase agent 不自管生命周期。
type Definition struct {
	ID           string
	Name         string
	TeamID       string
	Role         Role
	Kind         Kind
	ModelID      string
	ProviderID   string
	SystemPrompt string
	Temperature  float64
	MaxTokens    int
	CreatedAt    time.Time

	// ReasoningEffort 推理努力等级，用于支持思考模式的 V4 模型
	// "high" | "max" | ""（空表示不发送此参数）
	ReasoningEffort string

	// ThinkingEnabled 是否启用思考模式（DeepSeek V4 模型）
	ThinkingEnabled bool

	// ThinkingType 思考类型，用于支持思考模式的 V4 模型
	// "reasoning" | "extended_thinking" | ""
	ThinkingType string

	// MaxIterations 是 tool-use 循环的最大轮数（一次 Ask 内允许的 LLM.Chat 次数）
	//
	// <= 0 使用 DefaultMaxIterations（10）。
	// 无 tools 时循环第一轮就退出（LLM 不返回 tool_calls），此值不生效。
	MaxIterations int

	// ContextWindow 是模型的上下文窗口大小（token 数），用于 Overflow 硬限检查。
	// 对应 config.LLMModel.ContextWindow。
	// <= 0 时使用兜底默认值 128000。
	ContextWindow int
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
const DefaultMailboxCap = 8

// DefaultMaxIterations 是 tool-use 循环的默认最大轮数
//
// 值的选择：10 对大多数 tool-use 场景足够（经验值：常见任务 2-4 轮）；
// 超过 10 说明 LLM 陷入循环或 tool 设计有问题，需要上限兜底。
const DefaultMaxIterations = 10

