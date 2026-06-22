// Package tools defines the Tool interface and built-in implementations.
//
// Core concepts:
//   - Tool: the smallest callable unit, mapped 1:1 to LLM function calling.
//   - Confirmable: an optional interface that supports a "require user confirmation before execution" flow.
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

// Confirmable is an optional interface that tools may implement to support a "require user confirmation before execution" flow.
//
// Tools that do not implement this interface keep the default behavior (direct Execute).
// When a tool implementing this interface returns true from CheckConfirmation, the Agent will:
//  1. emit a ToolNeedsConfirmEvent (with options)
//  2. block until the caller invokes Agent.Confirm(callID, choice)
//  3. if choice != "", call ConfirmArgs to modify args and then Execute
//  4. if choice == "" (cancel/deny), return "error: user denied execution"
type Confirmable interface {
	Tool
	// CheckConfirmation checks whether the given args require user confirmation.
	// It returns (needsConfirm bool, prompt string), where prompt is shown to the user.
	CheckConfirmation(args string) (needsConfirm bool, prompt string)
	// ConfirmationOptions returns a list of available choices.
	// Returning nil or an empty slice indicates binary confirmation (approve/deny), which the UI may represent as "yes"/"".
	// If non-empty, the UI should present the options and pass the selected choice value back.
	ConfirmationOptions(args string) []string
	// ConfirmArgs modifies the original args after the user makes a choice.
	// choice is the user's selected option value; for binary confirmation, ChoiceApprove means approve and ChoiceDeny means deny.
	// The modified args are passed to Execute.
	ConfirmArgs(originalArgs string, choice ConfirmChoice) string
	// SupportsSessionWhitelist returns whether the tool supports the "allow-in-session" option.
	// If false, the UI should not show the "allow for this session" button.
	SupportsSessionWhitelist() bool
}

// ConfirmChoice is the user's choice in the confirmation dialog.
type ConfirmChoice string

const (
	// ChoiceDeny means deny/cancel execution.
	ChoiceDeny ConfirmChoice = ""

	// ChoiceApprove means approve this execution only (without adding a whitelist entry).
	ChoiceApprove ConfirmChoice = "yes"

	// ChoiceAllowInSession means approve this execution and add the tool to the current session allowlist,
	// so future calls in the same session will not trigger confirmation.
	ChoiceAllowInSession ConfirmChoice = "allow-in-session"
)

// ─── AsyncTool interface ────────────────────────────────────────────────────

// AsyncAction describes the intent for an asynchronous tool execution.
//
// Returned by AsyncTool.ExecuteAsync. The tool only declares "what I want
// to do asynchronously" — it does not start a goroutine. The framework
// is fully responsible for scheduling.
type AsyncAction struct {
	Target  iface.Locatable // target agent (already located)
	Prompt  string          // task description to send
	Timeout time.Duration   // delegation timeout
}

// TargetID returns the target agent's identifier for logging and tracing.
// The current Locatable interface does not expose an ID method, so this
// returns an empty string. Record targetID externally if needed.
func (a *AsyncAction) TargetID() string {
	return ""
}

// AsyncTool is an optional interface that tools may implement to declare an asynchronous execution intent.
//
// Tools that do not implement this interface use the normal Execute path.
// Tools that implement it are scheduled uniformly by the Agent's execTools:
//
//   - execTools assembles all context (asyncTurnState) before starting the goroutine
//   - it eliminates two-phase registration races entirely
//   - the Tool does not start a goroutine; it only returns an intent, and the framework handles go + registration + cleanup
//
// The pattern is the same as Confirmable: detection happens in execTools via type assertion.
type AsyncTool interface {
	Tool
	// ExecuteAsync returns an asynchronous execution intent without starting a goroutine.
	// The framework is responsible for assembling asyncTurnState → registering with the Agent → starting the goroutine → listening for results.
	ExecuteAsync(ctx context.Context, args string) (*AsyncAction, error)
}

// ─── FallbackTool wrapper ───────────────────────────────────────────────────

// FallbackTool wraps a Tool and prepends a fallback-only prefix to its
// Description, signaling to the LLM that this tool should only be used when
// no delegate_* tool is available. All other methods delegate to the inner Tool.
//
// Confirmable and AsyncTool interfaces are preserved through type assertion
// in the agent layer, so FallbackTool only needs to implement the base Tool.
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
		out[i] = &FallbackTool{
			Tool: t,
			desc: "[!!! DO NOT USE — protocol violation — call delegate_* instead !!!] " + t.Description(),
		}
	}
	return out
}

func (f *FallbackTool) Description() string { return f.desc }
