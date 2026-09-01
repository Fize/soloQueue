// Package tools defines the Tool interface and built-in implementations.
//
// Core concepts:
//   - Tool: the smallest callable unit, mapped 1:1 to LLM function calling.
//   - ToolRegistry: a concurrent-safe name → Tool mapping (defined in registry.go).
//
// Dependency direction:
//
//	tools does not depend on others (it defines the Tool interface)
//	skill → tools (SkillRegistry composes ToolRegistry)
//	agent → skill + tools
package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

// ─── Tool interface ──────────────────────────────────────────────────────────

// Tool is an executable unit that can be called by an Agent.
//
// The Name / Description / Parameters methods are read when the Agent builds an LLMRequest,
// and should be treated as read-only constants (they are read before each LLM call, but their
// return values should not change). Execute is invoked serially by the Agent run goroutine
// (only one tool runs at a time for a single Agent), but different Agents may concurrently call
// the same Tool instance, so Execute implementations must be concurrency-safe.
type Tool interface {
	// Name returns the tool name; it must be non-empty and unique within a single Agent.
	Name() string

	// Description provides the natural-language description shown to the LLM; an empty string is allowed but not recommended.
	Description() string

	// Parameters returns a JSON Schema (object type) describing the parameters; nil may be returned
	// to mean "no parameters" (corresponding to omitting the OpenAI function declaration parameters).
	Parameters() json.RawMessage

	// Execute runs the tool.
	//
	// args is the raw JSON string sent by the LLM (for example `""`, `"{}"`, or `{"path":"foo"}`).
	// The tool itself is responsible for unmarshaling it into a concrete struct.
	//
	// Return values:
	//   - result: content for the LLM tool-role message (recommended to be short and structured, text or JSON)
	//   - err: execution error; the Agent will feed "error: "+err.Error() back to the LLM without interrupting the loop.
	//
	// ctx cancellation should be honored promptly; if an Execute implementation does not respond to ctx, the Agent can only rely on the outer timeout.
	Execute(ctx context.Context, args string) (result string, err error)
}

// ─── AsyncTool interface ────────────────────────────────────────────────────

// AsyncAction describes the intent for an asynchronous tool execution.
//
// Returned by AsyncTool.ExecuteAsync. The tool only declares "what I want
// to do asynchronously" — it does not start a goroutine. The framework
// is fully responsible for scheduling.
type AsyncAction struct {
	Target     iface.Locatable // target agent (already located)
	Prompt     string          // task description to send
	Timeout    time.Duration   // delegation timeout
	Context    context.Context // optional target context; nil inherits the caller context
	DispatchID string
	OnEvent    func(iface.AgentEvent) error
	OnFinish   func(error) error
}

// AsyncTool is an optional interface that tools may implement to declare an asynchronous execution intent.
//
// Tools that do not implement this interface use the normal Execute path.
// Tools that implement it are scheduled uniformly by the Agent's execTools:
//
//   - execTools assembles all context (asyncTurnState) before starting the goroutine
//   - it eliminates two-phase registration races entirely
//   - the Tool does not start a goroutine; it only returns an intent, and the framework handles go + registration + cleanup
type AsyncTool interface {
	Tool
	// ExecuteAsync returns an asynchronous execution intent without starting a goroutine.
	// The framework is responsible for assembling asyncTurnState → registering with the Agent → starting the goroutine → listening for results.
	ExecuteAsync(ctx context.Context, args string) (*AsyncAction, error)
}

// ProgressSupervised opts a tool out of the framework's fallback total timeout
// only when its execution is covered by hierarchical progress leases.
type ProgressSupervised interface {
	ProgressSupervised() bool
}

// TurnTerminator is an optional interface that tools may implement to signal
// that an execution should end the current agent turn.
//
// When a tool implements this interface, the agent calls TerminatesTurn(result, err)
// after every Execute call. If it returns true, the agent completes the current
// turn without proceeding to the next LLM iteration — even if err != nil.
//
// This lets orchestration tools receive a structured result without the agent
// calling additional tools or making another LLM API call.
type TurnTerminator interface {
	Tool
	// TerminatesTurn reports whether this execution ends the current turn.
	TerminatesTurn(result string, err error) bool
}

// ─── FallbackTool wrapper ───────────────────────────────────────────────────

// FallbackTool wraps a Tool and prepends a fallback-only prefix to its
// Description, signaling to the LLM that this tool should only be used when
// no delegate_* tool is available. All other methods delegate to the inner Tool.
//
// AsyncTool is handled by the agent layer, so FallbackTool only needs to implement the base Tool.
type FallbackTool struct {
	Tool
	desc string
}

// WithFallbackPrefix wraps each tool in tools with a fallback-only prefix.
// Used by L1 (Session) agent to discourage direct tool usage when delegation
// is available. L2/L3 agents should NOT use this wrapper.
func WithFallbackPrefix(tools []Tool) []Tool {
	out := make([]Tool, len(tools))
	for i, t := range tools {
		if _, requiredTerminalTool := t.(TurnTerminator); requiredTerminalTool {
			out[i] = t
			continue
		}
		out[i] = &FallbackTool{
			Tool: t,
			desc: "[!!! DO NOT USE — protocol violation — call delegate_* instead !!!] " + t.Description(),
		}
	}
	return out
}

func (f *FallbackTool) Description() string { return f.desc }
