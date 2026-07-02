// Package timeline provides persistence and replay for conversational memory within a single session.
//
// Based on Event Sourcing and Append-Only architectural principles:
//   - Underlying storage is an ever-extending JSONL log stream
//   - Message events record conversational content
//   - Control events record state interventions (e.g., /clear)
//   - /clear does not destroy data; it only appends a control event and clears active memory.
package timeline

import "time"

// ─── EventType ───────────────────────────────────────────────────────────────

// EventType defines the type of an event.
type EventType string

const (
	// EventMessage represents a message event: standard conversational interaction.
	EventMessage EventType = "message"
	// EventControl represents a control event: state intervention command.
	EventControl EventType = "control"
)

// ─── Event ───────────────────────────────────────────────────────────────────

// Event represents the top-level structure of each line in the JSONL file.
type Event struct {
	Timestamp string          `json:"ts"`
	EventType EventType       `json:"type"`
	Message   *MessagePayload `json:"msg,omitempty"`
	Control   *ControlPayload `json:"ctrl,omitempty"`
}

// newEvent creates an event with the current timestamp.
func newEvent(et EventType) Event {
	return Event{
		Timestamp: time.Now().Format(time.RFC3339Nano),
		EventType: et,
	}
}

// ─── MessagePayload ─────────────────────────────────────────────────────────

// MessagePayload represents the payload for a message event.
type MessagePayload struct {
	Role             string        `json:"role"`                   // system/user/assistant/tool
	Content          string        `json:"content"`                // Message content
	ReasoningContent string        `json:"reasoning,omitempty"`    // DeepSeek reasoning
	Name             string        `json:"name,omitempty"`         // Tool name (role=tool)
	ToolCallID       string        `json:"tool_call_id,omitempty"` // Tool call ID (role=tool)
	ToolCalls        []ToolCallRec `json:"tool_calls,omitempty"`   // tool_calls when role is assistant
	IsEphemeral      bool          `json:"ephemeral,omitempty"`    // Marks verbose tool output
	AgentID          string        `json:"agent_id,omitempty"`     // Reserved for multi-agent systems
	Timestamp        string        `json:"ts,omitempty"`           // Original message timestamp (RFC3339Nano)
}

// ─── ControlPayload ─────────────────────────────────────────────────────────

// ControlPayload represents the payload for a control event.
type ControlPayload struct {
	Action  string `json:"action"`            // "clear" | "summary"
	Reason  string `json:"reason,omitempty"`  // Reason for trigger
	Content string `json:"content,omitempty"` // Summary text (when action="summary")
}

// ─── ToolCallRec ────────────────────────────────────────────────────────────

// ToolCallRec represents a tool call record (mapped from llm.ToolCall).
type ToolCallRec struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}