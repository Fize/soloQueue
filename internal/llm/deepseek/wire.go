// Package deepseek 实现 DeepSeek Chat Completions API 的 HTTP 客户端
//
// 对外实现 agent.LLMClient（Chat + ChatStream）。
// 所有 HTTP 调用使用 stream=true 路径 —— Chat 内部累积 streaming event 成完整响应。
// 这保证只有一份 HTTP 逻辑、一份错误处理、一份 retry 逻辑。
package deepseek

import (
	"encoding/json"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── Wire-level request types ────────────────────────────────────────────────
//
// 这些结构体直接对应 DeepSeek HTTP API 的 JSON shape。
// 不向外暴露 —— caller 用 agent.LLMRequest / agent.LLMResponse。
//
// 所有 optional 字段用指针表示，配合 omitempty，能在 JSON 里完整省略
// （避免 top_p=0 被误发为 "不填"）。
// 注意：DeepSeek V4 不再支持 temperature 参数，wireRequest 中已移除。

type wireRequest struct {
	Model            string           `json:"model"`
	Messages         []wireMessage    `json:"messages"`
	TopP             *float64         `json:"top_p,omitempty"`
	MaxTokens        *int             `json:"max_tokens,omitempty"`
	FrequencyPenalty *float64         `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64         `json:"presence_penalty,omitempty"`
	Stop             []string         `json:"stop,omitempty"`
	Stream           bool             `json:"stream"`
	StreamOptions    *wireStreamOpts  `json:"stream_options,omitempty"`
	Tools            []wireToolDef    `json:"tools,omitempty"`
	ToolChoice       string           `json:"tool_choice,omitempty"`
	ResponseFormat   *wireRespFormat  `json:"response_format,omitempty"`
	ReasoningEffort  *string          `json:"reasoning_effort,omitempty"`
}

type wireMessage struct {
	Role            string           `json:"role"`
	Content         string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Name            string           `json:"name,omitempty"`
	ToolCallID      string           `json:"tool_call_id,omitempty"`
	ToolCalls       []wireToolCall  `json:"tool_calls,omitempty"`
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

type wireRespFormat struct {
	Type string `json:"type"` // "text" | "json_object"
}

// ─── Wire-level response types ───────────────────────────────────────────────

// wireChunk 是 streaming 一条 `data: {...}` 的 JSON
// 对于 object="chat.completion.chunk" 的流式，每个 delta 是一个 chunk
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

// wireDeltaToolCall 是 streaming 中 tool_call 的一个增量
// Index 是 tool_call 在 choice 里的槽位号
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
	PromptTokens          int                      `json:"prompt_tokens"`
	CompletionTokens      int                      `json:"completion_tokens"`
	TotalTokens           int                      `json:"total_tokens"`
	PromptCacheHitTokens  int                      `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int                      `json:"prompt_cache_miss_tokens,omitempty"`
	CompletionDetails     *wireCompletionDetails   `json:"completion_tokens_details,omitempty"`
}

type wireCompletionDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// wireErrorEnvelope 是 HTTP 4xx/5xx 的 body shape
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

// buildWireRequest 把 agent.LLMRequest 转成 wire-level 请求
//
// stream 参数决定是否设置 stream=true；
// includeUsage 参数决定 stream_options.include_usage（仅 stream=true 有效）。
func buildWireRequest(req agent.LLMRequest, stream, includeUsage bool) wireRequest {
	out := wireRequest{
		Model:    req.Model,
		Messages: buildWireMessages(req.Messages),
		Stream:   stream,
	}
	// Optional 采样参数：零值也是合法值（top_p=0 有意义），
	// 所以用指针 + 无脑填 —— caller 不关心就让它用 API 默认值
	// 注意：DeepSeek V4 不再支持 temperature 参数，因此不再发送
	if req.TopP > 0 {
		p := req.TopP
		out.TopP = &p
	}
	if req.MaxTokens > 0 {
		m := req.MaxTokens
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
					Parameters:  td.Function.Parameters,
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
		re := req.ReasoningEffort
		out.ReasoningEffort = &re
	}
	return out
}

func buildWireMessages(msgs []agent.LLMMessage) []wireMessage {
	out := make([]wireMessage, 0, len(msgs))
	for _, m := range msgs {
		wm := wireMessage{
			Role:       m.Role,
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		// 只在有 tool_calls 时回传 reasoning_content（满足 API 要求 + 省 token）
		if len(m.ToolCalls) > 0 && m.ReasoningContent != "" {
			wm.ReasoningContent = m.ReasoningContent
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

// chunkToEvents 把一个 wireChunk 转成 0 个或多个 llm.Event
//
// 输出顺序（按 choice 遍历）：
//  1. tool_call delta（每个 slot 一个 Event）
//  2. content / reasoning_content delta（一起打包到一个 Event）
//  3. done（choice 的 finish_reason 非空时；可能带 Usage，见下）
//
// 如果 chunk 自身带 Usage（include_usage=true 且是 final chunk），
// 优先合并到末尾的 Done event；若没有 Done 则独立发一条 Done with Usage。
func chunkToEvents(c wireChunk) []llm.Event {
	var events []llm.Event

	for _, ch := range c.Choices {
		if ch.Delta != nil {
			// tool_call delta 先出
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
			// content / reasoning_content delta 合一个 event
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
		// 合并到末尾 Done；否则独立 Done
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
