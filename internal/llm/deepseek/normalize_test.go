package deepseek

import (
	"encoding/json"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func tc(id, name, args string) llm.ToolCall {
	return llm.ToolCall{
		ID:   id,
		Type: "function",
		Function: llm.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func toolMsg(id, name, content string) agent.LLMMessage {
	return agent.LLMMessage{Role: "tool", ToolCallID: id, Name: name, Content: content}
}

func asstMsg(content string, calls ...llm.ToolCall) agent.LLMMessage {
	return agent.LLMMessage{Role: "assistant", Content: content, ToolCalls: calls}
}

func TestNormalizeMessages_Passthrough(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "hello"},
		asstMsg("ok"),
	}
	out := normalizeMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	if &out[0] != &msgs[0] {
		t.Error("passthrough should return same backing array")
	}
}

func TestNormalizeMessages_WellFormedToolTurn(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do task"},
		asstMsg("thinking...", tc("call_1", "read", `{"path":"f"}`)),
		toolMsg("call_1", "read", "file contents"),
		asstMsg("done"),
	}
	out := normalizeMessages(msgs)
	if len(out) != 4 {
		t.Fatalf("len = %d", len(out))
	}
	if &out[0] != &msgs[0] {
		t.Error("well-formed should passthrough same slice")
	}
}

func TestNormalizeMessages_UnansweredToolCall(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do"},
		asstMsg("", tc("call_1", "write", `{"path":"f"}`)),
		// no tool result
		{Role: "user", Content: "next"},
	}
	out := normalizeMessages(msgs)
	if len(out) != 4 {
		t.Fatalf("len = %d, want 4 (user + asst + placeholder + user)", len(out))
	}
	if out[2].Role != "tool" {
		t.Error("expected placeholder tool message")
	}
	if out[2].ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q", out[2].ToolCallID)
	}
	if out[2].Content != interruptedToolResult {
		t.Errorf("Content = %q", out[2].Content)
	}
}

func TestNormalizeMessages_PartialToolResults(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do"},
		asstMsg("", tc("a", "f1", "{}"), tc("b", "f2", "{}")),
		toolMsg("a", "f1", "result1"),
		// "b" missing
		{Role: "user", Content: "next"},
	}
	out := normalizeMessages(msgs)
	if len(out) != 5 {
		t.Fatalf("len = %d", len(out))
	}
	if out[2].Content != "result1" {
		t.Error("first result should be preserved")
	}
	if out[3].Content != interruptedToolResult {
		t.Errorf("second should be placeholder, got %q", out[3].Content)
	}
}

func TestNormalizeMessages_OrphanToolMessage(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "hi"},
		toolMsg("orphan", "x", "result"), // no preceding assistant with tool_calls
		{Role: "user", Content: "next"},
	}
	out := normalizeMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("orphan tool should be dropped, len = %d", len(out))
	}
}

func TestNormalizeMessages_EmptyToolCallName_BackfillByID(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do"},
		asstMsg("", tc("call_1", "", `{}`), tc("call_2", "", `{}`)),
		toolMsg("call_1", "read", "r1"),
		toolMsg("call_2", "write", "w1"),
	}
	out := normalizeMessages(msgs)
	if len(out) != 4 {
		t.Fatalf("len = %d", len(out))
	}
	if out[1].ToolCalls[0].Function.Name != "read" {
		t.Errorf("first backfill: %q", out[1].ToolCalls[0].Function.Name)
	}
	if out[1].ToolCalls[1].Function.Name != "write" {
		t.Errorf("second backfill: %q", out[1].ToolCalls[1].Function.Name)
	}
}

func TestNormalizeMessages_TruncatedJSON_Repaired(t *testing.T) {
	broken := `{"path":"/tmp/f`
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do"},
		asstMsg("", tc("c", "write", broken)),
		toolMsg("c", "write", "done"),
	}
	out := normalizeMessages(msgs)
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if !json.Valid([]byte(out[1].ToolCalls[0].Function.Arguments)) {
		t.Errorf("arguments should be valid JSON, got: %s", out[1].ToolCalls[0].Function.Arguments)
	}
}

func TestNormalizeMessages_MultipleAssistantBlocks(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "task1"},
		asstMsg("", tc("a", "f1", "{}")),
		toolMsg("a", "f1", "r1"),
		asstMsg("ok"),
		{Role: "user", Content: "task2"},
		asstMsg("", tc("b", "f2", "{}")),
		// no result for b
		{Role: "user", Content: "next"},
	}
	out := normalizeMessages(msgs)
	// Expected: u, a(tc), t(r1), a(ok), u, a(tc), t(placeholder), u
	if len(out) != 8 {
		t.Fatalf("len = %d", len(out))
	}
	if out[6].Role != "tool" {
		t.Error("6th msg should be placeholder tool")
	}
}

func TestNormalizeMessages_ReorderedResults_IDPairing(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do"},
		asstMsg("", tc("a", "f1", "{}"), tc("b", "f2", "{}")),
		toolMsg("b", "f2", "r2"), // arrived before "a"
		toolMsg("a", "f1", "r1"),
	}
	out := normalizeMessages(msgs)
	if len(out) != 4 {
		t.Fatalf("len = %d", len(out))
	}
	if out[2].ToolCallID != "a" || out[2].Name != "f1" {
		t.Errorf("first result should be for 'a', got id=%q name=%q", out[2].ToolCallID, out[2].Name)
	}
	if out[3].ToolCallID != "b" || out[3].Name != "f2" {
		t.Errorf("second result should be for 'b', got id=%q name=%q", out[3].ToolCallID, out[3].Name)
	}
}

func TestNormalizeMessages_NoID_FallsBackToPositional(t *testing.T) {
	msgs := []agent.LLMMessage{
		{Role: "user", Content: "do"},
		asstMsg("", tc("", "f1", "{}"), tc("", "f2", "{}")),
		toolMsg("", "f1", "r1"),
		toolMsg("", "f2", "r2"),
	}
	out := normalizeMessages(msgs)
	// Positional pairing: rewrite IDs
	if out[2].ToolCallID != "" {
		t.Error("positional: ID should stay empty")
	}
}

func TestCloseTruncatedJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		valid    bool // must be valid JSON after closing
		contains string
	}{
		{"already valid", `{"a":1}`, true, `"a"`},
		{"unterminated string", `{"name":"foo`, true, `foo`},
		{"open object with key", `{"path":`, true, `null`},
		{"dangling comma", `{"a":1,"b":2,`, true, `2`},
		{"open array", `[1,2`, true, `1`},
		{"unterminated nested array", `{"items":[1,2`, true, `[1,2]`},
		{"escape char at end", `{"path":"/tmp/\\`, true, `tmp`},    // esc removes trailing backslash
		{"empty returns empty", "", false, `{}`},  // empty not valid JSON, degrades to {}
		{"garbage degrades", `not json at all`, false, `{}`},       // defaults to {}
		{"nested open braces", `{"a":{"b":`, true, `"b"`},          // closes both
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := closeTruncatedJSON(tt.input)
			if tt.valid && !json.Valid([]byte(result)) {
				t.Errorf("result is not valid JSON: %s", result)
			}
			if tt.contains != "" && !contains(result, tt.contains) {
				t.Errorf("result %q does not contain %q", result, tt.contains)
			}
			if result == "{}" && tt.valid && tt.input != "" {
				t.Errorf("valid input %q degraded to {}", tt.input)
			}
		})
	}
}

func TestCloseTruncatedJSON_UnterminatedStringAtEnd(t *testing.T) {
	result := closeTruncatedJSON(`{"name":"foo`)
	if !json.Valid([]byte(result)) {
		t.Errorf("not valid: %s", result)
	}
}

func TestCloseTruncatedJSON_TrailingColon(t *testing.T) {
	result := closeTruncatedJSON(`{"a":`)
	if !json.Valid([]byte(result)) {
		t.Errorf("not valid: %s", result)
	}
	if result != `{"a":null}` {
		t.Errorf("got %q", result)
	}
}

func TestCloseTruncatedJSON_EscapedQuote(t *testing.T) {
	result := closeTruncatedJSON(`{"msg":"say \\\"hello`)
	if !json.Valid([]byte(result)) {
		t.Errorf("not valid: %s", result)
	}
}

func TestIdDistinct(t *testing.T) {
	if !idDistinct(nil) {
		t.Error("nil should be true")
	}
	if idDistinct([]llm.ToolCall{{}}) {
		t.Error("empty ID should be false")
	}
	if !idDistinct([]llm.ToolCall{{ID: "a"}, {ID: "b"}}) {
		t.Error("distinct IDs should be true")
	}
	if idDistinct([]llm.ToolCall{{ID: "a"}, {ID: "a"}}) {
		t.Error("duplicate IDs should be false")
	}
}

func TestBackfillToolCallNames_NoResults(t *testing.T) {
	calls := []llm.ToolCall{tc("a", "", "{}")}
	out := backfillToolCallNames(calls, nil)
	if out[0].Function.Name != "" {
		t.Error("no results → name stays empty")
	}
}

func TestBackfillToolCallNames_AlreadyFilled(t *testing.T) {
	calls := []llm.ToolCall{tc("a", "f1", "{}")}
	out := backfillToolCallNames(calls, nil)
	if &out[0] != &calls[0] {
		t.Error("already filled should return same slice")
	}
}
