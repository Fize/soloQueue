package agent

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// AgentEvent is the sealed event interface for agent streams.
//
// AskStream produces events implementing this interface. The sealed
// pattern (unexported marker method) ensures only this package can
// define event types, enabling exhaustive switch checking via linters.
//
// All concrete types also implement iface.AgentEvent (for cross-package
// use) and iface.EventConsumer (for type-safe field extraction without
// reflection).
type AgentEvent interface {
	iface.AgentEvent
	agentEvent() // package-level seal — external packages cannot implement
}

// ContentDeltaEvent carries an incremental LLM response content fragment.
type ContentDeltaEvent struct {
	Iter  int
	Delta string
}

// ReasoningDeltaEvent carries an incremental reasoning_content fragment
// (DeepSeek reasoner models).
type ReasoningDeltaEvent struct {
	Iter  int
	Delta string
}

// ToolCallDeltaEvent carries an incremental tool_call arguments fragment.
//
// The first delta for a given call typically carries CallID and Name;
// subsequent deltas for the same CallID carry only ArgsDelta.
type ToolCallDeltaEvent struct {
	Iter      int
	CallID    string
	Name      string
	ArgsDelta string
}

// ToolExecStartEvent signals that the agent has started executing a tool.
type ToolExecStartEvent struct {
	Iter   int
	CallID string
	Name   string
	Args   string // complete JSON arguments
}

// ToolExecDoneEvent signals that a tool execution has completed.
//
// When Err != nil, Result is typically empty. The error text has already
// been formatted as "error: ..." and fed back to the LLM.
type ToolExecDoneEvent struct {
	Iter     int
	CallID   string
	Name     string
	Result   string
	Err      error
	Duration time.Duration
}

// IterationDoneEvent signals the end of an LLM iteration (one Chat stream
// fully consumed). Emitted before tool execution begins.
type IterationDoneEvent struct {
	Iter         int
	FinishReason llm.FinishReason
	Usage        llm.Usage
}

// DoneEvent signals successful completion of the entire AskStream.
// Content is the final assistant response.
type DoneEvent struct {
	Content          string
	ReasoningContent string
}

// ToolNeedsConfirmEvent signals that a tool requires user confirmation
// before execution.
//
// UI should display Prompt and Options (if any). The user's choice is
// injected via Agent.Confirm(callID, choice). Empty Options means binary
// confirm/deny; use "yes" to confirm or "" to deny.
type ToolNeedsConfirmEvent struct {
	Iter           int
	CallID         string
	Name           string
	Args           string
	Prompt         string
	Options        []string
	AllowInSession bool
}

// ErrorEvent signals that AskStream has terminated due to an error.
// Always the last event before channel close.
type ErrorEvent struct {
	Err error
}

// DelegationStartedEvent signals that async delegation has begun.
// Emitted when at least one tool call in the current iteration is async.
type DelegationStartedEvent struct {
	Iter     int
	NumTasks int
}

// DelegationCompletedEvent signals that all async delegations have completed
// and results have been injected into the context window.
type DelegationCompletedEvent struct {
	Iter          int
	TargetAgentID string
}

// --- iface.AgentEvent marker (all types satisfy iface.AgentEvent) ---

func (ContentDeltaEvent) IsAgentEvent()        {}
func (ReasoningDeltaEvent) IsAgentEvent()      {}
func (ToolCallDeltaEvent) IsAgentEvent()       {}
func (ToolExecStartEvent) IsAgentEvent()       {}
func (ToolExecDoneEvent) IsAgentEvent()        {}
func (ToolNeedsConfirmEvent) IsAgentEvent()    {}
func (IterationDoneEvent) IsAgentEvent()       {}
func (DoneEvent) IsAgentEvent()                {}
func (ErrorEvent) IsAgentEvent()               {}
func (DelegationStartedEvent) IsAgentEvent()   {}
func (DelegationCompletedEvent) IsAgentEvent() {}

// --- agent-internal sealed marker ---

func (ContentDeltaEvent) agentEvent()        {}
func (ReasoningDeltaEvent) agentEvent()      {}
func (ToolCallDeltaEvent) agentEvent()       {}
func (ToolExecStartEvent) agentEvent()       {}
func (ToolExecDoneEvent) agentEvent()        {}
func (ToolNeedsConfirmEvent) agentEvent()    {}
func (IterationDoneEvent) agentEvent()       {}
func (DoneEvent) agentEvent()                {}
func (ErrorEvent) agentEvent()               {}
func (DelegationStartedEvent) agentEvent()   {}
func (DelegationCompletedEvent) agentEvent() {}

// --- iface.EventConsumer implementation ---
//
// Each event type implements all four methods. Only the relevant method
// returns (value, true); all others return (zero, false).

// ContentDeltaEvent → ContentDelta
func (e ContentDeltaEvent) ContentDelta() (string, bool)   { return e.Delta, true }
func (e ContentDeltaEvent) DoneContent() (string, bool)    { return "", false }
func (e ContentDeltaEvent) Error() (error, bool)           { return nil, false }
func (e ContentDeltaEvent) ConfirmRequest() (string, bool) { return "", false }

// ReasoningDeltaEvent → none (not consumed by DelegateTool)
func (e ReasoningDeltaEvent) ContentDelta() (string, bool)   { return "", false }
func (e ReasoningDeltaEvent) DoneContent() (string, bool)    { return "", false }
func (e ReasoningDeltaEvent) Error() (error, bool)           { return nil, false }
func (e ReasoningDeltaEvent) ConfirmRequest() (string, bool) { return "", false }

// ToolCallDeltaEvent → none
func (e ToolCallDeltaEvent) ContentDelta() (string, bool)   { return "", false }
func (e ToolCallDeltaEvent) DoneContent() (string, bool)    { return "", false }
func (e ToolCallDeltaEvent) Error() (error, bool)           { return nil, false }
func (e ToolCallDeltaEvent) ConfirmRequest() (string, bool) { return "", false }

// ToolExecStartEvent → none
func (e ToolExecStartEvent) ContentDelta() (string, bool)   { return "", false }
func (e ToolExecStartEvent) DoneContent() (string, bool)    { return "", false }
func (e ToolExecStartEvent) Error() (error, bool)           { return nil, false }
func (e ToolExecStartEvent) ConfirmRequest() (string, bool) { return "", false }

// ToolExecDoneEvent → none
func (e ToolExecDoneEvent) ContentDelta() (string, bool)   { return "", false }
func (e ToolExecDoneEvent) DoneContent() (string, bool)    { return "", false }
func (e ToolExecDoneEvent) Error() (error, bool)           { return nil, false }
func (e ToolExecDoneEvent) ConfirmRequest() (string, bool) { return "", false }

// IterationDoneEvent → none
func (e IterationDoneEvent) ContentDelta() (string, bool)   { return "", false }
func (e IterationDoneEvent) DoneContent() (string, bool)    { return "", false }
func (e IterationDoneEvent) Error() (error, bool)           { return nil, false }
func (e IterationDoneEvent) ConfirmRequest() (string, bool) { return "", false }

// DoneEvent → DoneContent
func (e DoneEvent) ContentDelta() (string, bool)   { return "", false }
func (e DoneEvent) DoneContent() (string, bool)    { return e.Content, true }
func (e DoneEvent) Error() (error, bool)           { return nil, false }
func (e DoneEvent) ConfirmRequest() (string, bool) { return "", false }

// ToolNeedsConfirmEvent → ConfirmRequest
func (e ToolNeedsConfirmEvent) ContentDelta() (string, bool)   { return "", false }
func (e ToolNeedsConfirmEvent) DoneContent() (string, bool)    { return "", false }
func (e ToolNeedsConfirmEvent) Error() (error, bool)           { return nil, false }
func (e ToolNeedsConfirmEvent) ConfirmRequest() (string, bool) { return e.CallID, true }

// ErrorEvent → Error
func (e ErrorEvent) ContentDelta() (string, bool)   { return "", false }
func (e ErrorEvent) DoneContent() (string, bool)    { return "", false }
func (e ErrorEvent) Error() (error, bool)           { return e.Err, true }
func (e ErrorEvent) ConfirmRequest() (string, bool) { return "", false }

// DelegationStartedEvent → none
func (e DelegationStartedEvent) ContentDelta() (string, bool)   { return "", false }
func (e DelegationStartedEvent) DoneContent() (string, bool)    { return "", false }
func (e DelegationStartedEvent) Error() (error, bool)           { return nil, false }
func (e DelegationStartedEvent) ConfirmRequest() (string, bool) { return "", false }

// DelegationCompletedEvent → none
func (e DelegationCompletedEvent) ContentDelta() (string, bool)   { return "", false }
func (e DelegationCompletedEvent) DoneContent() (string, bool)    { return "", false }
func (e DelegationCompletedEvent) Error() (error, bool)           { return nil, false }
func (e DelegationCompletedEvent) ConfirmRequest() (string, bool) { return "", false }
