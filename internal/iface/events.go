// Package iface defines shared interfaces for the agent framework.
//
// This package breaks the circular dependency between agent and tools:
//
//	iface  ← agent (implements interfaces)
//	iface  ← tools (consumes interfaces)
//
// Without iface, tools could not reference agent event types, leading to
// reflect-based field access and interface{} channels.
package iface

import "context"

// AgentEvent is the shared event interface produced by agent streams.
//
// Concrete implementations live in the agent package. The exported marker
// method allows cross-package implementation. The agent package adds its
// own unexported marker for local sealing (exhaustive switch checking).
type AgentEvent interface {
	IsAgentEvent() // exported marker — enables cross-package implementation
}

// EventConsumer extracts typed data from agent events without requiring
// a type switch on concrete types. This allows the tools package to
// consume event data safely without importing the agent package.
//
// Each method returns (value, true) if the event carries that kind of
// data, or (zero, false) otherwise. Only the relevant event type returns
// true for each method:
//
//   - ContentDeltaEvent  → ContentDelta() returns (delta, true)
//   - DoneEvent          → DoneContent() returns (content, true)
//   - ErrorEvent         → Error() returns (err, true)
//   - ToolNeedsConfirmEvent → ConfirmRequest() returns (callID, true)
type EventConsumer interface {
	ContentDelta() (delta string, ok bool)
	DoneContent() (content string, ok bool)
	Error() (err error, ok bool)
	ConfirmRequest() (callID string, ok bool)
}

// Locatable is the minimal Agent abstraction for delegation.
//
// DelegateTool uses this interface to communicate with target agents,
// decoupled from the concrete Agent type.
type Locatable interface {
	// Ask sends a blocking request and returns the final result.
	Ask(ctx context.Context, prompt string) (string, error)

	// AskStream sends a streaming request and returns a typed event channel.
	// The caller must consume the channel until close or cancel ctx.
	AskStream(ctx context.Context, prompt string) (<-chan AgentEvent, error)

	// Confirm responds to a pending tool confirmation request.
	Confirm(callID string, choice string) error
}

// AgentLocator looks up running Agent instances by ID.
//
// Implemented by agent.Registry. Used by DelegateTool to find target agents.
type AgentLocator interface {
	Locate(id string) (Locatable, bool)
}

// ConfirmForwarder routes a child agent's tool confirmation request to
// the parent agent. It blocks until the user confirms or ctx is cancelled.
//
// The function is created as a closure by the agent package and injected
// into the tool execution context. DelegateTool extracts and invokes it
// when it encounters a ToolNeedsConfirmEvent from the child stream.
type ConfirmForwarder func(ctx context.Context, callID string, child Locatable) (string, error)
