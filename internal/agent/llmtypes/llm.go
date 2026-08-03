// Package llmtypes defines the minimal LLM client contract (LLMMessage, LLMRequest, LLMResponse, LLMClient).
// In a subpackage to prevent import cycles with test packages.
package llmtypes

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// LLMMessage is a single message in the LLM conversation history.
type LLMMessage struct {
	Role             string
	Content          string
	Images           []llm.ImageContent // Multimodal images (used only in user messages)
	ReasoningContent string             // DeepSeek thinking mode; must be returned when tool_calls are present
	Name             string
	ToolCallID       string         // required when role="tool"
	ToolCalls        []llm.ToolCall // optional for role="assistant"
}

// LLMRequest is the input for LLMClient.Chat / ChatStream.
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

	ReasoningEffort string // "high" | "max" | ""
	ThinkingEnabled bool
	ThinkingType    string // "enabled" (DeepSeek) | "adaptive" (MiniMax M3)

	Tools      []llm.ToolDef // empty = no tools
	ToolChoice string        // "" | "none" | "auto" | "required"

	ResponseJSON bool // response_format: json_object
	IncludeUsage bool // stream_options.include_usage

	Vision bool // false = discard image data, fall back to text
}

// LLMResponse is the return value of LLMClient.Chat.
type LLMResponse struct {
	Content          string
	ReasoningContent string // For deepseek-reasoner only
	ToolCalls        []llm.ToolCall
	FinishReason     llm.FinishReason
	Usage            llm.Usage
}

// LLMClient is the minimal concurrent-safe interface for LLM calls.
type LLMClient interface {
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)
	ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error)
}
