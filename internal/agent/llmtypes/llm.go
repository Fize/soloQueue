// Package llmtypes defines the minimal LLM client contract used by the agent
// framework: LLMMessage, LLMRequest, LLMResponse and the LLMClient interface.
//
// It lives in its own subpackage so that test-support packages (e.g.
// internal/agent/agenttest) can implement LLMClient without importing
// internal/agent (which would create an import cycle with package-internal
// tests). The agent package re-exports these types via type aliases, so
// external consumers can keep using agent.LLMClient / agent.LLMRequest etc.
package llmtypes

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// LLMMessage is a message passed to the LLM
//
// Supports tool-calling protocol:
//   - role="system" / "user": Fill Content; user can optionally include Images (multimodal)
//   - role="assistant": Content + optional ToolCalls (allows empty Content, with only tool_calls)
//   - role="tool": ToolCallID + Content (tool execution result)
type LLMMessage struct {
	Role             string
	Content          string
	Images           []llm.ImageContent // Multimodal images (used only in user messages)
	ReasoningContent string             // DeepSeek thinking mode; must be returned when tool_calls are present
	Name             string
	ToolCallID       string         // Required when role="tool"
	ToolCalls        []llm.ToolCall // Optional for role="assistant"
}

// LLMRequest is the input for LLMClient.Chat / ChatStream
type LLMRequest struct {
	ProviderID  string
	Model       string
	Messages    []LLMMessage
	Temperature float64
	MaxTokens   int

	// Extended sampling parameters
	TopP             float64
	FrequencyPenalty float64
	PresencePenalty  float64
	StopSequences    []string

	// Reasoning effort level (V4 model thinking mode)
	// "high" | "max" | "" (empty means this parameter is not sent)
	ReasoningEffort string

	// ThinkingEnabled enables thinking mode
	ThinkingEnabled bool
	// ThinkingType sets the thinking.type value in the API request.
	// "enabled" (default, DeepSeek) or "adaptive" (MiniMax M3 etc.).
	ThinkingType string

	// Tool-calling
	Tools      []llm.ToolDef // Empty means no tool
	ToolChoice string        // "" | "none" | "auto" | "required"

	// Output format
	ResponseJSON bool // Corresponds to response_format: json_object

	// Streaming options (only effective for ChatStream)
	IncludeUsage bool // Corresponds to stream_options.include_usage

	// Vision indicates whether the model supports multimodal image_url content.
	// If false, the wire layer will discard image data and fall back to plain text.
	Vision bool
}

// LLMResponse is the return value of LLMClient.Chat
type LLMResponse struct {
	Content          string
	ReasoningContent string // For deepseek-reasoner only
	ToolCalls        []llm.ToolCall
	FinishReason     llm.FinishReason
	Usage            llm.Usage
}

// LLMClient is the minimal interface for LLM calls
//
// Implementations must be concurrent-safe (multiple goroutines may call Chat / ChatStream simultaneously).
// When ctx is cancelled, it should return ctx.Err() as soon as possible.
type LLMClient interface {
	// Chat is a synchronous call: blocks until a complete response (internally may be accumulated from streaming)
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)

	// ChatStream returns an Event channel
	// When the channel is closed, it means the stream has ended (normally or abnormally); an error event will be delivered before closing
	ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error)
}
