// Package deepseek implements an HTTP client for the DeepSeek Chat Completions API.
//
// It implements agent.LLMClient (Chat + ChatStream) for external use.
// All HTTP calls use the stream=true path — Chat accumulates streaming events internally into a complete response.
// This ensures a single HTTP logic, error handling, and retry logic.
package deepseek

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── Wire-level request types ────────────────────────────────────────────────
//
// These structs directly correspond to the JSON shape of the DeepSeek HTTP API.
// Not exposed externally — callers use agent.LLMRequest / agent.LLMResponse.
//
// All optional fields are represented by pointers, combined with omitempty, to be fully omitted in JSON
// (e.g., to avoid top_p=0 being mistakenly sent as "not provided").
// Note: DeepSeek V4 no longer supports the temperature parameter; it has been removed from wireRequest.

type wireRequest struct {
	Model            string          `json:"model"`
	Messages         []wireMessage   `json:"messages"`
	TopP             *float64        `json:"top_p,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	Stop             []string        `json:"stop,omitempty"`
	Stream           bool            `json:"stream"`
	StreamOptions    *wireStreamOpts `json:"stream_options,omitempty"`
	Tools            []wireToolDef   `json:"tools,omitempty"`
	ToolChoice       string          `json:"tool_choice,omitempty"`
	ResponseFormat   *wireRespFormat `json:"response_format,omitempty"`
	ReasoningEffort  *string         `json:"reasoning_effort,omitempty"`
	Thinking         *wireThinking   `json:"thinking,omitempty"`
}

// wireContentPart represents an element in an OpenAI-compatible content array
//
// Used only for multimodal messages (when images are present), mutually exclusive with Content string.
// Not exposed to callers.
type wireContentPart struct {
	Type     string          `json:"type"`
	Text     string          `json:"text,omitempty"`
	ImageURL *wireImageURL   `json:"image_url,omitempty"`
}

type wireImageURL struct {
	URL    string `json:"url"`              // data:image/png;base64,... or http(s) URL
	Detail string `json:"detail,omitempty"` // "auto" | "low" | "high"
}

type wireMessage struct {
	Role             string         `json:"role"`
	Content          any            `json:"content"` // string | []wireContentPart
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	Name             string         `json:"name,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
}

type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function wireFunctionCall `json:"function"`
}

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolDef struct {
	Type     string           `json:"type"` // "function"
	Function wireFunctionDecl `json:"function"`
}

type wireFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireThinking struct {
	Type string `json:"type"` // "enabled" | "disabled"
}

type wireRespFormat struct {
	Type string `json:"type"` // "text" | "json_object"
}

// ─── Wire-level response types ───────────────────────────────────────────────

// wireChunk represents a single `data: {...}` JSON object in a streaming response
// For streaming with object="chat.completion.chunk", each delta is a chunk
type wireChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []wireChoice `json:"choices"`
	Usage   *wireUsage   `json:"usage,omitempty"`
}

type wireChoice struct {
	Index        int        `json:"index"`
	Delta        *wireDelta `json:"delta,omitempty"`
	FinishReason *string    `json:"finish_reason"`
}

type wireDelta struct {
	Role             string              `json:"role,omitempty"`
	Content          *string             `json:"content,omitempty"`
	ReasoningContent *string             `json:"reasoning_content,omitempty"`
	ToolCalls        []wireDeltaToolCall `json:"tool_calls,omitempty"`
}

// wireDeltaToolCall is an incremental part of a tool_call in streaming
// Index is the slot number of the tool_call within the choice
type wireDeltaToolCall struct {
	Index    int                  `json:"index"`
	ID       string               `json:"id,omitempty"`
	Type     string               `json:"type,omitempty"`
	Function wireDeltaFunctionArg `json:"function,omitempty"`
}

type wireDeltaFunctionArg struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type wireUsage struct {
	PromptTokens          int                    `json:"prompt_tokens"`
	CompletionTokens      int                    `json:"completion_tokens"`
	TotalTokens           int                    `json:"total_tokens"`
	PromptCacheHitTokens  int                    `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int                    `json:"prompt_cache_miss_tokens,omitempty"`
	CompletionDetails     *wireCompletionDetails `json:"completion_tokens_details,omitempty"`
}

type wireCompletionDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// wireErrorEnvelope is the body shape for HTTP 4xx/5xx errors
type wireErrorEnvelope struct {
	Error wireError `json:"error"`
}

type wireError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
	Param   string `json:"param"`
}

// ─── Request builder ─────────────────────────────────────────────────────────

// buildWireRequest converts an agent.LLMRequest into a wire-level request
//
// The stream parameter determines whether stream=true is set;
// the includeUsage parameter determines stream_options.include_usage (only effective when stream=true).
func buildWireRequest(req agent.LLMRequest, stream, includeUsage bool) wireRequest {
	out := wireRequest{
		Model:    req.Model,
		Messages: buildWireMessages(req.Messages, req.ThinkingEnabled, req.Vision),
		Stream:   stream,
	}
	// Optional sampling parameters: zero values are also valid (e.g., top_p=0 is meaningful),
	// so use pointers + unconditionallly fill them — if the caller doesn't care, let it use API defaults.
	// Note: DeepSeek V4 no longer supports the temperature parameter, so it's no longer sent.
	if req.TopP > 0 {
		p := req.TopP
		out.TopP = &p
	}
	if req.MaxTokens > 0 {
		m := min(req.MaxTokens, 100000) // Output token limit 100k
		out.MaxTokens = &m
	}
	if req.FrequencyPenalty != 0 {
		f := req.FrequencyPenalty
		out.FrequencyPenalty = &f
	}
	if req.PresencePenalty != 0 {
		p := req.PresencePenalty
		out.PresencePenalty = &p
	}
	if len(req.StopSequences) > 0 {
		out.Stop = req.StopSequences
	}
	if stream && includeUsage {
		out.StreamOptions = &wireStreamOpts{IncludeUsage: true}
	}
	if len(req.Tools) > 0 {
		out.Tools = make([]wireToolDef, 0, len(req.Tools))
		for _, td := range req.Tools {
			out.Tools = append(out.Tools, wireToolDef{
				Type: td.Type,
				Function: wireFunctionDecl{
					Name:        td.Function.Name,
					Description: td.Function.Description,
					Parameters:  safeParams(td.Function.Name, td.Function.Parameters),
				},
			})
		}
	}
	if req.ToolChoice != "" {
		out.ToolChoice = req.ToolChoice
	}
	if req.ResponseJSON {
		out.ResponseFormat = &wireRespFormat{Type: "json_object"}
	}
	if req.ReasoningEffort != "" {
		out.ReasoningEffort = &req.ReasoningEffort
	}
	// DeepSeek V4 thinking parameter: required, either enabled or disabled
	if req.ThinkingEnabled {
		out.Thinking = &wireThinking{Type: "enabled"}
	} else {
		out.Thinking = &wireThinking{Type: "disabled"}
	}
	return out
}

// buildWireMessages converts a list of agent.LLMMessage into a list of wire-level messages
//
// When visionEnabled=false, multimodal images are stripped and replaced with text annotations,
// to prevent APIs that don't support multimodal input from returning a 400 error.
func buildWireMessages(msgs []agent.LLMMessage, thinkingEnabled, visionEnabled bool) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		// Build Content: if Images present and vision is enabled, use content array;
		// otherwise plain string. When vision is disabled, strip images and annotate.
		var content any
		if len(m.Images) > 0 && (m.Role == "user" || m.Role == "system") {
			if visionEnabled {
				parts := make([]wireContentPart, 0, len(m.Images)+1)
				if m.Content != "" {
					parts = append(parts, wireContentPart{
						Type: "text",
						Text: m.Content,
					})
				}
				for _, img := range m.Images {
					parts = append(parts, wireContentPart{
						Type: "image_url",
						ImageURL: &wireImageURL{
							URL: "data:" + img.MimeType + ";base64," + img.Data,
						},
					})
				}
				content = parts
			} else {
				// Vision not supported: strip images, add text annotation
				text := m.Content
				if text != "" {
					text += "\n\n"
				}
				text += fmt.Sprintf("[The user included %d images, but the current model does not support multimodal recognition and they have been ignored]", len(m.Images))
				content = text
			}
		} else {
			content = m.Content
		}

		wm := wireMessage{
			Role:       m.Role,
			Content:    content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		// DeepSeek thinking mode: handling reasoning_content for assistant messages
		// When thinking mode is enabled, all assistant messages must include reasoning_content
		// (even if empty, the field needs to be sent to satisfy API validation)
		if m.Role == "assistant" {
			if m.ReasoningContent != "" {
				wm.ReasoningContent = m.ReasoningContent
			} else if thinkingEnabled {
				// Thinking mode requires reasoning_content on all assistant messages.
				// For messages produced without thinking, insert a placeholder.
				wm.ReasoningContent = " "
			}
		}
		if len(m.ToolCalls) > 0 {
			wm.ToolCalls = make([]wireToolCall, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: wireFunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
		out = append(out, wm)
	}
	return out
}

// ─── chunk → Event ───────────────────────────────────────────────────────────

// chunkToEvents converts a wireChunk into zero or more llm.Event
//
// Output order (iterating through choices):
//  1. tool_call delta (one Event per slot)
//  2. content / reasoning_content delta (packaged together into one Event)
//  3. done (when choice's finish_reason is non-empty; may include Usage, see below)
//
// If the chunk itself contains Usage (include_usage=true and it's the final chunk),
// it is preferentially merged into the last Done event; if no Done event exists, a separate Done with Usage is sent.
func chunkToEvents(c wireChunk) []llm.Event {
	var events []llm.Event

	for _, ch := range c.Choices {
		if ch.Delta != nil {
			// tool_call delta first
			for _, tc := range ch.Delta.ToolCalls {
				events = append(events, llm.Event{
					Type: llm.EventDelta,
					ToolCallDelta: &llm.ToolCallDelta{
						Index:     tc.Index,
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
			// content / reasoning_content delta combined into one event
			ev := llm.Event{Type: llm.EventDelta}
			has := false
			if ch.Delta.Content != nil && *ch.Delta.Content != "" {
				ev.ContentDelta = *ch.Delta.Content
				has = true
			}
			if ch.Delta.ReasoningContent != nil && *ch.Delta.ReasoningContent != "" {
				ev.ReasoningContentDelta = *ch.Delta.ReasoningContent
				has = true
			}
			if has {
				events = append(events, ev)
			}
		}

		if ch.FinishReason != nil && *ch.FinishReason != "" {
			events = append(events, llm.Event{
				Type:         llm.EventDone,
				FinishReason: llm.FinishReason(*ch.FinishReason),
			})
		}
	}

	if c.Usage != nil {
		u := wireUsageToLLM(c.Usage)
		// Merge into the last Done event; otherwise, send a separate Done event.
		if n := len(events); n > 0 && events[n-1].Type == llm.EventDone {
			events[n-1].Usage = &u
		} else {
			events = append(events, llm.Event{
				Type:  llm.EventDone,
				Usage: &u,
			})
		}
	}

	return events
}

func wireUsageToLLM(u *wireUsage) llm.Usage {
	out := llm.Usage{
		PromptTokens:          u.PromptTokens,
		CompletionTokens:      u.CompletionTokens,
		TotalTokens:           u.TotalTokens,
		PromptCacheHitTokens:  u.PromptCacheHitTokens,
		PromptCacheMissTokens: u.PromptCacheMissTokens,
	}
	if u.CompletionDetails != nil {
		out.ReasoningTokens = u.CompletionDetails.ReasoningTokens
	}
	return out
}

// ─── Parameter validation guard ──────────────────────────────────────────────

// safeParams validates and returns a json.RawMessage. If the input is invalid
// JSON, it returns a minimal valid schema so DeepSeek's API doesn't reject
// the entire request. This is a defense-in-depth guard against malformed tool
// parameters from MCP servers or other dynamic sources.
func safeParams(name string, raw json.RawMessage) json.RawMessage {
	if raw == nil || len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		// Log which tool has the invalid parameters so the root cause
		// (e.g., a specific MCP server with a malformed inputSchema)
		// can be identified and fixed at the source.
		fmt.Fprintf(os.Stderr, "soloqueue: tool %q has invalid Parameters JSON (len=%d), using default schema instead\n", name, len(raw))
		return defaultSchema
	}
	return raw
}

// defaultSchema is a minimal valid JSON Schema object used when a tool's
// Parameters is invalid JSON.
var defaultSchema = json.RawMessage(`{"type": "object"}`)