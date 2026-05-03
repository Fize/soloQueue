// Package timeline 提供单会话记忆的持久化与回放
//
// 基于事件溯源（Event Sourcing）和只追加（Append-Only）的架构思想：
//   - 底层存储为一条不断延伸的 JSONL 日志流
//   - 消息事件（Message）记录对话内容
//   - 控制事件（Control）记录状态干预（如 /clear）
//   - /clear 不销毁数据，仅追加控制事件并清空活跃内存
package timeline

import "time"

// ─── EventType ───────────────────────────────────────────────────────────────

// EventType 事件类型
type EventType string

const (
	// EventMessage 消息事件：标准对话交互
	EventMessage EventType = "message"
	// EventControl 控制事件：状态干预指令
	EventControl EventType = "control"
)

// ─── Event ───────────────────────────────────────────────────────────────────

// Event 是 JSONL 中每一行的顶层结构
type Event struct {
	Timestamp string         `json:"ts"`
	EventType EventType      `json:"type"`
	Message   *MessagePayload `json:"msg,omitempty"`
	Control   *ControlPayload `json:"ctrl,omitempty"`
}

// newEvent 创建一个带当前时间戳的事件
func newEvent(et EventType) Event {
	return Event{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		EventType: et,
	}
}

// ─── MessagePayload ─────────────────────────────────────────────────────────

// MessagePayload 消息事件载荷
type MessagePayload struct {
	Role             string        `json:"role"`                    // system/user/assistant/tool
	Content          string        `json:"content"`                 // 消息内容
	ReasoningContent string        `json:"reasoning,omitempty"`     // DeepSeek reasoning
	Name             string        `json:"name,omitempty"`          // 工具名（role=tool）
	ToolCallID       string        `json:"tool_call_id,omitempty"`  // 工具调用 ID（role=tool）
	ToolCalls        []ToolCallRec `json:"tool_calls,omitempty"`    // role=assistant 时的 tool_calls
	IsEphemeral      bool          `json:"ephemeral,omitempty"`     // 标记冗长工具输出
	AgentID          string        `json:"agent_id,omitempty"`      // 多智能体预留
}

// ─── ControlPayload ─────────────────────────────────────────────────────────

// ControlPayload 控制事件载荷
type ControlPayload struct {
	Action  string `json:"action"`            // "clear" | "summary"
	Reason  string `json:"reason,omitempty"`  // 触发原因
	Content string `json:"content,omitempty"` // summary 文本（action="summary" 时）
}

// ─── ToolCallRec ────────────────────────────────────────────────────────────

// ToolCallRec 工具调用记录（从 llm.ToolCall 映射）
type ToolCallRec struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
