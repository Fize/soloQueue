package deepseek

import (
	"encoding/json"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── buildWireRequest ────────────────────────────────────────────────────────

func TestBuildWireRequest_Minimal(t *testing.T) {
	req := agent.LLMRequest{
		Model: "deepseek-chat",
		Messages: []agent.LLMMessage{
			{Role: "user", Content: "hi"},
		},
	}
	w := buildWireRequest(req, false, false)

	if w.Model != "deepseek-chat" {
		t.Errorf("model = %q", w.Model)
	}
	if len(w.Messages) != 1 || w.Messages[0].Role != "user" {
		t.Errorf("messages = %+v", w.Messages)
	}
	if w.Stream {
		t.Error("Stream should be false")
	}
	if w.Temperature != nil {
		t.Errorf("Temperature should be nil for zero value, got %v", *w.Temperature)
	}
	if w.MaxTokens != nil {
		t.Errorf("MaxTokens should be nil for zero value")
	}
	if w.StreamOptions != nil {
		t.Error("StreamOptions should be nil without include_usage")
	}
}

func TestBuildWireRequest_FullFields(t *testing.T) {
	req := agent.LLMRequest{
		Model:            "deepseek-reasoner",
		Messages:         []agent.LLMMessage{{Role: "user", Content: "q"}},
		Temperature:      0.6,
		TopP:             0.95,
		MaxTokens:        1024,
		FrequencyPenalty: 0.1,
		PresencePenalty:  -0.1,
		StopSequences:    []string{"END", "STOP"},
		ResponseJSON:     true,
		ToolChoice:       "auto",
		Tools: []llm.ToolDef{
			{Type: "function", Function: llm.FunctionDecl{
				Name:        "get_weather",
				Description: "fetch weather",
				Parameters:  json.RawMessage(`{"type":"object"}`),
			}},
		},
	}
	w := buildWireRequest(req, true, true)

	if !w.Stream {
		t.Error("Stream should be true")
	}
	if w.StreamOptions == nil || !w.StreamOptions.IncludeUsage {
		t.Error("StreamOptions.IncludeUsage should be true")
	}
	if w.Temperature == nil || *w.Temperature != 0.6 {
		t.Errorf("Temperature = %v", w.Temperature)
	}
	if w.TopP == nil || *w.TopP != 0.95 {
		t.Errorf("TopP = %v", w.TopP)
	}
	if w.MaxTokens == nil || *w.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %v", w.MaxTokens)
	}
	if w.FrequencyPenalty == nil || *w.FrequencyPenalty != 0.1 {
		t.Errorf("FrequencyPenalty = %v", w.FrequencyPenalty)
	}
	if w.PresencePenalty == nil || *w.PresencePenalty != -0.1 {
		t.Errorf("PresencePenalty = %v", w.PresencePenalty)
	}
	if len(w.Stop) != 2 || w.Stop[0] != "END" {
		t.Errorf("Stop = %v", w.Stop)
	}
	if w.ResponseFormat == nil || w.ResponseFormat.Type != "json_object" {
		t.Errorf("ResponseFormat = %+v", w.ResponseFormat)
	}
	if w.ToolChoice != "auto" {
		t.Errorf("ToolChoice = %q", w.ToolChoice)
	}
	if len(w.Tools) != 1 || w.Tools[0].Function.Name != "get_weather" {
		t.Errorf("Tools = %+v", w.Tools)
	}
}

func TestBuildWireRequest_MessagesWithToolCalls(t *testing.T) {
	req := agent.LLMRequest{
		Model: "m",
		Messages: []agent.LLMMessage{
			{Role: "system", Content: "sys"},
			{Role: "user", Content: "q"},
			{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
				{ID: "call_1", Type: "function", Function: llm.FunctionCall{
					Name: "f", Arguments: `{"x":1}`,
				}},
			}},
			{Role: "tool", ToolCallID: "call_1", Content: "result"},
		},
	}
	w := buildWireRequest(req, false, false)
	if len(w.Messages) != 4 {
		t.Fatalf("len = %d", len(w.Messages))
	}
	asst := w.Messages[2]
	if asst.Role != "assistant" {
		t.Errorf("msg[2].Role = %q", asst.Role)
	}
	if len(asst.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d", len(asst.ToolCalls))
	}
	tc := asst.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "f" || tc.Function.Arguments != `{"x":1}` {
		t.Errorf("tool_call = %+v", tc)
	}

	toolMsg := w.Messages[3]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" || toolMsg.Content != "result" {
		t.Errorf("tool msg = %+v", toolMsg)
	}
}

func TestBuildWireRequest_JSONOmitEmpty(t *testing.T) {
	// 零值字段不应出现在 JSON 里（验证 omitempty）
	req := agent.LLMRequest{
		Model:    "m",
		Messages: []agent.LLMMessage{{Role: "user", Content: "hi"}},
	}
	w := buildWireRequest(req, true, false)
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	// 这些零值字段应被 omitempty 省略
	for _, not := range []string{"temperature", "top_p", "max_tokens", "tools", "tool_choice", "response_format"} {
		if contains(s, `"`+not+`"`) {
			t.Errorf("JSON should omit %q, got: %s", not, s)
		}
	}
	// stream 应出现（总是显式）
	if !contains(s, `"stream":true`) {
		t.Errorf(`JSON should include "stream":true, got: %s`, s)
	}
}

// ─── chunkToEvents ───────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }

func TestChunkToEvents_ContentDelta(t *testing.T) {
	c := wireChunk{
		Choices: []wireChoice{
			{Delta: &wireDelta{Content: strPtr("hello")}},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 {
		t.Fatalf("len = %d", len(events))
	}
	if events[0].Type != llm.EventDelta || events[0].ContentDelta != "hello" {
		t.Errorf("event = %+v", events[0])
	}
}

func TestChunkToEvents_ReasoningContentDelta(t *testing.T) {
	c := wireChunk{
		Choices: []wireChoice{
			{Delta: &wireDelta{ReasoningContent: strPtr("thinking...")}},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 || events[0].ReasoningContentDelta != "thinking..." {
		t.Errorf("events = %+v", events)
	}
}

func TestChunkToEvents_ContentAndReasoning_CombineOneEvent(t *testing.T) {
	// reasoning 和 content 在同一 delta 里应合并为一个 Event
	c := wireChunk{
		Choices: []wireChoice{
			{Delta: &wireDelta{
				Content:          strPtr("c"),
				ReasoningContent: strPtr("r"),
			}},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 {
		t.Fatalf("len = %d (expected 1 combined)", len(events))
	}
	ev := events[0]
	if ev.ContentDelta != "c" || ev.ReasoningContentDelta != "r" {
		t.Errorf("event = %+v", ev)
	}
}

func TestChunkToEvents_ToolCallDelta(t *testing.T) {
	c := wireChunk{
		Choices: []wireChoice{
			{Delta: &wireDelta{
				ToolCalls: []wireDeltaToolCall{
					{
						Index: 0,
						ID:    "call_1",
						Type:  "function",
						Function: wireDeltaFunctionArg{
							Name:      "get_weather",
							Arguments: `{"loc":`,
						},
					},
				},
			}},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 {
		t.Fatalf("len = %d", len(events))
	}
	ev := events[0]
	if ev.ToolCallDelta == nil {
		t.Fatal("ToolCallDelta nil")
	}
	if ev.ToolCallDelta.Index != 0 || ev.ToolCallDelta.ID != "call_1" ||
		ev.ToolCallDelta.Name != "get_weather" || ev.ToolCallDelta.Arguments != `{"loc":` {
		t.Errorf("delta = %+v", ev.ToolCallDelta)
	}
}

func TestChunkToEvents_FinishReason_ProducesDone(t *testing.T) {
	c := wireChunk{
		Choices: []wireChoice{
			{FinishReason: strPtr("stop")},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 || events[0].Type != llm.EventDone ||
		events[0].FinishReason != llm.FinishStop {
		t.Errorf("events = %+v", events)
	}
}

func TestChunkToEvents_UsageMergedWithDone(t *testing.T) {
	c := wireChunk{
		Choices: []wireChoice{
			{FinishReason: strPtr("stop")},
		},
		Usage: &wireUsage{
			PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30,
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 {
		t.Fatalf("len = %d", len(events))
	}
	done := events[0]
	if done.Type != llm.EventDone {
		t.Fatalf("type = %v", done.Type)
	}
	if done.Usage == nil || done.Usage.TotalTokens != 30 {
		t.Errorf("Usage = %+v", done.Usage)
	}
}

func TestChunkToEvents_UsageWithoutFinish_CreatesDone(t *testing.T) {
	// include_usage=true 时，DeepSeek 会在最后发一个只含 usage 的 chunk
	c := wireChunk{
		Choices: []wireChoice{},
		Usage: &wireUsage{
			PromptTokens: 5, CompletionTokens: 10, TotalTokens: 15,
		},
	}
	events := chunkToEvents(c)
	if len(events) != 1 || events[0].Type != llm.EventDone {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Usage == nil || events[0].Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v", events[0].Usage)
	}
}

func TestChunkToEvents_EmptyDelta_NoEvent(t *testing.T) {
	// 第一个 chunk 通常只有 role="assistant"，没有 content；不应产生 Event
	c := wireChunk{
		Choices: []wireChoice{
			{Delta: &wireDelta{Role: "assistant"}},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 0 {
		t.Errorf("empty delta should produce no events, got: %+v", events)
	}
}

func TestChunkToEvents_MultipleToolCallsInOneDelta(t *testing.T) {
	c := wireChunk{
		Choices: []wireChoice{
			{Delta: &wireDelta{
				ToolCalls: []wireDeltaToolCall{
					{Index: 0, ID: "a", Function: wireDeltaFunctionArg{Arguments: "x"}},
					{Index: 1, ID: "b", Function: wireDeltaFunctionArg{Arguments: "y"}},
				},
			}},
		},
	}
	events := chunkToEvents(c)
	if len(events) != 2 {
		t.Fatalf("len = %d", len(events))
	}
	if events[0].ToolCallDelta.Index != 0 || events[1].ToolCallDelta.Index != 1 {
		t.Errorf("indices = %d, %d", events[0].ToolCallDelta.Index, events[1].ToolCallDelta.Index)
	}
}

// ─── wireUsageToLLM ──────────────────────────────────────────────────────────

func TestWireUsageToLLM(t *testing.T) {
	wu := &wireUsage{
		PromptTokens:          10,
		CompletionTokens:      20,
		TotalTokens:           30,
		PromptCacheHitTokens:  3,
		PromptCacheMissTokens: 7,
		CompletionDetails:     &wireCompletionDetails{ReasoningTokens: 5},
	}
	got := wireUsageToLLM(wu)
	if got.PromptTokens != 10 || got.CompletionTokens != 20 || got.TotalTokens != 30 {
		t.Errorf("basic tokens wrong: %+v", got)
	}
	if got.PromptCacheHitTokens != 3 || got.PromptCacheMissTokens != 7 {
		t.Errorf("cache tokens wrong: %+v", got)
	}
	if got.ReasoningTokens != 5 {
		t.Errorf("ReasoningTokens = %d", got.ReasoningTokens)
	}
}

func TestWireUsageToLLM_NilCompletionDetails(t *testing.T) {
	wu := &wireUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}
	got := wireUsageToLLM(wu)
	if got.ReasoningTokens != 0 {
		t.Errorf("ReasoningTokens should be 0")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
