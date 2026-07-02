// Package llm defines types and utilities shared across providers:
//
//   - Shared structures like ToolCall / ToolDef / FunctionCall / Usage used in messages / requests / responses
//   - Event (tagged struct) for streaming
//   - Provider-agnostic APIError (implements error + IsRetryable)
//   - RunWithRetry: A general-purpose retry helper with exponential backoff
//
// Specific provider implementations (e.g., DeepSeek) are placed in sub-packages (e.g., llm/deepseek/).
//
// This package itself does not introduce net/http — HTTP clients are implemented by sub-packages.
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ─── Multimodal types ────────────────────────────────────────────────────────

// ImageContent represents a piece of image content (base64 encoded)
//
// Used for multimodal models (e.g., Kimi K2.6), sent in an OpenAI-compatible image_url format.
// Data is the raw base64 bytes without the "data:image/...;base64," prefix.
type ImageContent struct {
	Data     string // base64-encoded image bytes
	MimeType string // e.g., "image/png", "image/jpeg"
}

// ─── Tool-calling shared types ───────────────────────────────────────────────

// ToolCall is a single tool call request within an assistant message.
//
// Universal for both request and response directions:
//   - Response: LLM tells us which tool it wants to call
//   - Request: Replays previous LLM tool_calls in assistant history messages
//
// Function.Arguments is a JSON-encoded string (**not a JSON object**),
// passed through as-is according to OpenAI-compat specification.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // Fixed to "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall is a specific function call
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string; parsed into a concrete object by the caller
}

// ToolDef tells the LLM which tools are available in the request
type ToolDef struct {
	Type     string       `json:"type"` // Fixed to "function"
	Function FunctionDecl `json:"function"`
}

// FunctionDecl is the declaration of a function (without runtime data)
type FunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema; client does not perform validation
}

// ─── Usage ───────────────────────────────────────────────────────────────────

// Usage represents token counts
//
// Standard fields (PromptTokens/CompletionTokens/TotalTokens) are present for all providers;
// the following are DeepSeek-specific; other providers can leave them as zero.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// DeepSeek specific
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
	ReasoningTokens       int `json:"reasoning_tokens,omitempty"`
}

// ─── FinishReason ────────────────────────────────────────────────────────────

// FinishReason is the termination reason for a response
//
// Standard values:
//
//	"stop"                           Normal termination
//	"length"                         Reached max_tokens
//	"tool_calls"                     LLM generated a tool_call, waiting for caller execution
//	"content_filter"                 Intercepted by content filter
//	"insufficient_system_resource"   DeepSeek specific, insufficient service resources
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
)

// ─── Streaming Event ─────────────────────────────────────────────────────────

// EventType is the discriminant field for Event
type EventType int

const (
	// EventDelta Incremental content (content / reasoning_content / tool_call delta)
	EventDelta EventType = iota
	// EventDone Stream ended normally; includes FinishReason, possibly Usage
	EventDone
	// EventError Stream encountered an error midway; includes Err
	EventError
)

// String for debugging purposes
func (t EventType) String() string {
	switch t {
	case EventDelta:
		return "delta"
	case EventDone:
		return "done"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}

// Event is an event flowing on a streaming channel
//
// Uses a tagged struct (Type field + fields filled according to type) instead of an interface — zero allocation,
// caller can just switch, and it's zero-value safe.
//
// Field rules:
//   - Delta  event: Read only ContentDelta / ReasoningContentDelta / ToolCallDelta
//   - Done   event: Read only FinishReason / Usage
//   - Error  event: Read only Err
type Event struct {
	Type EventType

	// Delta
	ContentDelta          string
	ReasoningContentDelta string
	ToolCallDelta         *ToolCallDelta

	// Done
	FinishReason FinishReason
	Usage        *Usage

	// Error
	Err error
}

// ToolCallDelta is an incremental part of a tool_call in streaming
//
// Accumulation rules (caller implementation):
//   - Position by Index as a slot
//   - First occurrence includes ID + Name; subsequent occurrences only include Arguments fragments
//   - Arguments strings are concatenated to form the complete JSON
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// ─── APIError ────────────────────────────────────────────────────────────────

// APIError is a structured error returned by the provider
//
// Corresponds to the common error body for OpenAI / DeepSeek:
//
//	{"error": {"message": ..., "type": ..., "code": ..., "param": ...}}
type APIError struct {
	StatusCode int
	Type       string // "authentication_error" / "rate_limit_reached" / "insufficient_balance" / ...
	Code       string // "invalid_api_key" / ...
	Message    string
	Param      string
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := fmt.Sprintf("llm: http %d", e.StatusCode)
	if e.Type != "" {
		parts += ": " + e.Type
	}
	if e.Code != "" {
		parts += " (" + e.Code + ")"
	}
	if e.Message != "" {
		parts += ": " + e.Message
	}
	return parts
}

// IsRetryable checks if this error is worth retrying
//
// Strategy:
//   - 5xx: Server-side issue, should retry
//   - 429: Rate limit, should retry (exponential backoff)
//   - 4xx (non-429): Client-side error, do not retry
//
// Unknown status (0, network error) is not treated as APIError by default — if the caller cannot extract
// an APIError using errors.As, it follows another path (usually network errors should also retry).
func (e *APIError) IsRetryable() bool {
	if e == nil {
		return false
	}
	if e.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return e.StatusCode >= 500 && e.StatusCode < 600
}

// IsRetryableErr is a general check: network errors are retryable; APIError depends on status; others are not retryable
//
// This is the standard shouldRetry implementation for RunWithRetry provided by a provider client.
func IsRetryableErr(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsRetryable()
	}
	// Others (network / EOF / timeout) default to retry
	return true
}