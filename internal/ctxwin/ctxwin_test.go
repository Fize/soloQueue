package ctxwin

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func newTestCW(maxTokens, bufferTokens int) *ContextWindow {
	return NewContextWindow(maxTokens, bufferTokens, 0, NewTokenizer())
}

func TestNewContextWindow(t *testing.T) {
	cw := newTestCW(10000, 2000)
	if cw.Len() != 0 {
		t.Errorf("Len() = %d, want 0", cw.Len())
	}
	current, max, buffer := cw.TokenUsage()
	if current != 0 {
		t.Errorf("currentTokens = %d, want 0", current)
	}
	if max != 10000 {
		t.Errorf("maxTokens = %d, want 10000", max)
	}
	if buffer != 2000 {
		t.Errorf("bufferTokens = %d, want 2000", buffer)
	}
}

func TestPushBasic(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "You are a helpful assistant.")
	cw.Push(RoleUser, "Hello!")

	if cw.Len() != 2 {
		t.Errorf("Len() = %d, want 2", cw.Len())
	}
	current, _, _ := cw.TokenUsage()
	if current <= 0 {
		t.Errorf("currentTokens = %d, want > 0", current)
	}
	// Calibrate 前：sum(messages.Tokens) == currentTokens
	if cw.Recalculate() != current {
		t.Errorf("Recalculate() = %d, currentTokens = %d, want equal (before Calibrate)", cw.Recalculate(), current)
	}
}

func TestPushWithReasoningContent(t *testing.T) {
	cw := newTestCW(100000, 2000)
	reasoning := "Let me think about this step by step..."
	cw.Push(RoleAssistant, "The answer is 42.", WithReasoningContent(reasoning))

	if cw.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", cw.Len())
	}
	msg, ok := cw.MessageAt(0)
	if !ok {
		t.Fatal("MessageAt(0) returned false")
	}
	// Tokens 应该包含 Content + ReasoningContent
	contentTokens := cw.tokenizer.Count("The answer is 42.")
	reasoningTokens := cw.tokenizer.Count(reasoning)
	expectedMin := contentTokens + reasoningTokens
	if msg.Tokens < expectedMin {
		t.Errorf("Tokens = %d, want >= %d (content=%d + reasoning=%d)",
			msg.Tokens, expectedMin, contentTokens, reasoningTokens)
	}
}

func TestPushWithToolCalls(t *testing.T) {
	cw := newTestCW(100000, 2000)
	tcs := []llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"main.go"}`}},
	}
	cw.Push(RoleAssistant, "", WithToolCalls(tcs))

	msg, _ := cw.MessageAt(0)
	if len(msg.ToolCalls) != 1 {
		t.Errorf("ToolCalls len = %d, want 1", len(msg.ToolCalls))
	}
	// Tokens 应该包含 ToolCalls 的 JSON 表示
	if msg.Tokens <= 0 {
		t.Errorf("Tokens = %d, want > 0", msg.Tokens)
	}
}

func TestPushWithEphemeral(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleTool, `{"exit_code":0,"stdout":"hello"}`, WithEphemeral(true), WithToolCallID("call_1"), WithToolName("Bash"))

	msg, _ := cw.MessageAt(0)
	if !msg.IsEphemeral {
		t.Error("IsEphemeral = false, want true")
	}
	if msg.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want %q", msg.ToolCallID, "call_1")
	}
	if msg.Name != "Bash" {
		t.Errorf("Name = %q, want %q", msg.Name, "Bash")
	}
}

func TestBuildPayload(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System prompt")
	cw.Push(RoleUser, "User message")
	cw.Push(RoleAssistant, "Assistant reply", WithReasoningContent("thinking..."))

	payload := cw.BuildPayload()
	if len(payload) != 3 {
		t.Fatalf("len(payload) = %d, want 3", len(payload))
	}
	if payload[0].Role != "system" {
		t.Errorf("payload[0].Role = %q, want %q", payload[0].Role, "system")
	}
	if payload[1].Role != "user" {
		t.Errorf("payload[1].Role = %q, want %q", payload[1].Role, "user")
	}
	if payload[2].ReasoningContent != "thinking..." {
		t.Errorf("payload[2].ReasoningContent = %q, want %q", payload[2].ReasoningContent, "thinking...")
	}
	// 返回的切片是拷贝，修改不影响原数据
	payload[0].Content = "modified"
	orig, _ := cw.MessageAt(0)
	if orig.Content == "modified" {
		t.Error("BuildPayload should return a copy, not a reference")
	}
}

func TestCalibrate(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleUser, "Hello")
	cw.Push(RoleAssistant, "Hi there")

	// Calibrate 前：sum == currentTokens
	current, _, _ := cw.TokenUsage()
	if cw.Recalculate() != current {
		t.Errorf("Before Calibrate: Recalculate()=%d != currentTokens=%d", cw.Recalculate(), current)
	}

	// Calibrate 后：currentTokens 被精确值覆盖，sum(msg.Tokens) 应等于 currentTokens
	cw.Calibrate(42)
	current, _, _ = cw.TokenUsage()
	if current != 42 {
		t.Errorf("After Calibrate: currentTokens = %d, want 42", current)
	}
	// Calibrate 现在会按比例更新 msg.Tokens，sum(msg.Tokens) 应约等于 currentTokens
	if cw.Recalculate() != current {
		t.Errorf("After Calibrate: Recalculate()=%d != currentTokens=%d (rounding diff only)", cw.Recalculate(), current)
	}
}

func TestOverflow(t *testing.T) {
	cw := newTestCW(100, 20)

	// Empty CW — no overflow
	if cw.Overflow() {
		t.Error("Overflow() should be false for empty CW")
	}

	cw.Push(RoleUser, "Hello")

	// After push within capacity — no overflow
	if cw.Overflow() {
		t.Error("Overflow() should be false when tokens are within capacity")
	}

	// Verify capacity is consistent with expectations
	current, max, buffer := cw.TokenUsage()
	capacity := max - buffer
	if capacity <= 0 {
		t.Fatalf("capacity = %d, should be positive", capacity)
	}
	if current > capacity {
		t.Errorf("currentTokens (%d) should be <= capacity (%d) after normal Push", current, capacity)
	}
}

func TestPopLast(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Hello")

	beforeTokens, _, _ := cw.TokenUsage()

	msg, ok := cw.PopLast()
	if !ok {
		t.Fatal("PopLast returned false")
	}
	if msg.Role != RoleUser {
		t.Errorf("Popped role = %q, want %q", msg.Role, RoleUser)
	}
	if cw.Len() != 1 {
		t.Errorf("Len() = %d, want 1", cw.Len())
	}
	afterTokens, _, _ := cw.TokenUsage()
	if afterTokens >= beforeTokens {
		t.Errorf("Tokens should decrease after PopLast: before=%d, after=%d", beforeTokens, afterTokens)
	}
}

func TestPopLastEmpty(t *testing.T) {
	cw := newTestCW(100000, 2000)
	_, ok := cw.PopLast()
	if ok {
		t.Error("PopLast on empty window should return false")
	}
}

func TestMessageAtOutOfBounds(t *testing.T) {
	cw := newTestCW(100000, 2000)
	_, ok := cw.MessageAt(0)
	if ok {
		t.Error("MessageAt(0) on empty window should return false")
	}
}

func TestBuildPayload_OrphanedToolMessageSkipped(t *testing.T) {
	// A tool message without a matching assistant(tool_calls) should be skipped.
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "What time is it?")
	cw.Push(RoleAssistant, "Let me check.", WithToolCalls([]llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "get_time", Arguments: `{}`}},
	}))
	cw.Push(RoleTool, `{"time":"12:00"}`, WithToolCallID("call_1"), WithToolName("get_time"))
	// Orphaned tool message — no matching assistant(tool_calls)
	cw.Push(RoleTool, `{"temp":"22C"}`, WithToolCallID("call_orphan"), WithToolName("get_temp"))

	payload := cw.BuildPayload()
	// Should have 4 messages: system, user, assistant(tool_calls), tool result.
	// The orphaned tool message with call_orphan is skipped.
	if len(payload) != 4 {
		t.Fatalf("len(payload) = %d, want 4", len(payload))
	}
	last := payload[len(payload)-1]
	if last.Role != string(RoleTool) || last.ToolCallID != "call_1" {
		t.Errorf("last msg = %q/%q, want tool/call_1", last.Role, last.ToolCallID)
	}
}

func TestBuildPayload_OrphanedAssistantToolCallsSkipped(t *testing.T) {
	// An assistant(tool_calls) without any tool result messages should be skipped.
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Question 1")
	cw.Push(RoleAssistant, "Answer 1")
	cw.Push(RoleUser, "Question 2")
	// Assistant makes a tool call, but no tool result ever appears
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "orphan-1", Type: "function", Function: llm.FunctionCall{Name: "search", Arguments: `{}`}},
	}))

	payload := cw.BuildPayload()
	// Should have 4 messages, orphaned assistant skipped
	if len(payload) != 4 {
		t.Fatalf("len(payload) = %d, want 4", len(payload))
	}
	for _, p := range payload {
		if len(p.ToolCalls) > 0 {
			t.Errorf("unexpected tool_calls in payload: %v", p.ToolCalls)
		}
	}
}

func TestBuildPayload_PartialToolResultsSkipped(t *testing.T) {
	// Assistant with 2 tool_calls but only 1 tool result — all skipped.
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Search and read")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "tc-1", Type: "function", Function: llm.FunctionCall{Name: "search", Arguments: `{}`}},
		{ID: "tc-2", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{}`}},
	}))
	cw.Push(RoleTool, "search result", WithToolCallID("tc-1"), WithToolName("search"))
	// tc-2 result missing — partial
	cw.Push(RoleUser, "Next question")
	cw.Push(RoleAssistant, "Final answer")

	payload := cw.BuildPayload()
	// Should have 4 messages: system, user, user, assistant
	// Orphaned assistant tool_calls + partial tool result both skipped
	if len(payload) != 4 {
		t.Fatalf("len(payload) = %d, want 4", len(payload))
	}
	for _, p := range payload {
		if p.Role == string(RoleTool) && p.ToolCallID == "tc-1" {
			t.Errorf("partial tool result tc-1 should have been skipped")
		}
	}
}

func TestBuildPayload_CompleteToolCallsKept(t *testing.T) {
	// Assistant with tool_calls and all tool results present — all kept.
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Read file")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "tc-a", Type: "function", Function: llm.FunctionCall{Name: "read", Arguments: `{}`}},
		{ID: "tc-b", Type: "function", Function: llm.FunctionCall{Name: "search", Arguments: `{}`}},
	}))
	cw.Push(RoleTool, "read result", WithToolCallID("tc-a"), WithToolName("read"))
	cw.Push(RoleTool, "search result", WithToolCallID("tc-b"), WithToolName("search"))
	cw.Push(RoleUser, "Next question")

	payload := cw.BuildPayload()
	// Should have 6 messages: all kept
	if len(payload) != 6 {
		t.Fatalf("len(payload) = %d, want 6", len(payload))
	}
}

func TestBuildPayload_MixedCompleteAndIncomplete(t *testing.T) {
	// First tool call pair is complete, second is incomplete.
	// Only the complete pair should survive.
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Q1")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "good-1", Type: "function", Function: llm.FunctionCall{Name: "get_time", Arguments: `{}`}},
	}))
	cw.Push(RoleTool, "12:00", WithToolCallID("good-1"), WithToolName("get_time"))
	cw.Push(RoleAssistant, "The time is 12:00")
	cw.Push(RoleUser, "Q2 — canceled during tool")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "bad-1", Type: "function", Function: llm.FunctionCall{Name: "search", Arguments: `{}`}},
	}))
	// No tool result for bad-1 — user canceled during execution

	payload := cw.BuildPayload()
	// Should have 6 messages (system, user, assistant w/tool_calls, tool result, assistant, user)
	// Orphaned assistant(bad-1) skipped
	if len(payload) != 6 {
		t.Fatalf("len(payload) = %d, want 6", len(payload))
	}
	// Verify no orphaned assistant with tool_calls remains
	for _, p := range payload {
		for _, tc := range p.ToolCalls {
			if tc.ID == "bad-1" {
				t.Errorf("orphaned assistant tool_calls bad-1 should have been skipped")
			}
		}
	}
}

func TestPushTriggersEviction(t *testing.T) {
	// 小窗口：maxTokens=100, bufferTokens=20, 有效容量=80
	cw := newTestCW(100, 20)

	// Push system prompt（占一点 token）
	cw.Push(RoleSystem, "You are helpful.")

	// Push 足够多的 user/assistant 消息触发淘汰
	for i := 0; i < 20; i++ {
		cw.Push(RoleUser, "Tell me a long story about programming and software engineering.")
		cw.Push(RoleAssistant, "Once upon a time in a far away land, there was a programmer who loved to code all day long.")
	}

	current, _, _ := cw.TokenUsage()
	// 淘汰后应该低于有效容量
	if current > 100 {
		t.Errorf("After eviction: currentTokens=%d, should be <= maxTokens=100", current)
	}
	// system prompt 应该还在
	if cw.Len() == 0 {
		t.Error("Messages should not be empty after eviction")
	}
	first, _ := cw.MessageAt(0)
	if first.Role != RoleSystem {
		t.Errorf("First message role = %q, want system (never evicted)", first.Role)
	}
}

func TestResize(t *testing.T) {
	// Create CW with room for messages — but first push a system prompt
	cw := newTestCW(2000, 200)
	cw.Push(RoleSystem, "You are a helpful assistant.")

	// Push several turns to fill up the CW
	for i := 0; i < 20; i++ {
		cw.Push(RoleUser, "What is the meaning of life, the universe, and everything? A very long question to fill the context window quickly with many tokens.")
		cw.Push(RoleAssistant, "The answer is 42. This is a detailed response that explains the entire concept in great depth with many words to consume context window tokens.")
	}

	tokensBefore, _, _ := cw.TokenUsage()

	// Resize to a smaller window — should trigger eviction
	cw.Resize(300, 0, 0)

	_, maxAfter, _ := cw.TokenUsage()
	if maxAfter != 300 {
		t.Errorf("maxTokens after Resize = %d, want 300", maxAfter)
	}
	tokensAfter := cw.CurrentTokens()
	if tokensAfter >= tokensBefore {
		t.Errorf("tokens after Resize (%d) should be less than before (%d)", tokensAfter, tokensBefore)
	}
	if cw.Len() < 2 {
		t.Error("should have at least system prompt + some messages after Resize")
	}
	sysMsg, _ := cw.MessageAt(0)
	if sysMsg.Role != RoleSystem {
		t.Errorf("first message role = %q, want system (never evicted)", sysMsg.Role)
	}

	// Resize to the same value — should be idempotent (no change)
	tokensAfter2 := cw.CurrentTokens()
	cw.Resize(300, 0, 0)
	if cw.CurrentTokens() != tokensAfter2 {
		t.Error("Resize to same maxTokens should be idempotent")
	}

	// Resize to a larger window — no eviction needed
	cw.Resize(2000, 0, 0)
	_, maxLarger, _ := cw.TokenUsage()
	if maxLarger != 2000 {
		t.Errorf("maxTokens after growing Resize = %d, want 2000", maxLarger)
	}
}

func TestResize_SummaryTokens_Recalculation(t *testing.T) {
	// Small model (< 512k): summaryTokens should be 85% of maxTokens
	cw := newTestCW(100000, 0)
	cw.Resize(100000, 0, 0)
	st := cw.SummaryTokens()
	expected := 100000 * 85 / 100
	if expected == 0 {
		expected = 85000
	}
	if st != expected {
		t.Errorf("summaryTokens for small window = %d, want %d", st, expected)
	}

	// Large model (>= 512k): summaryTokens should be 75% of maxTokens
	cw.Resize(600000, 0, 0)
	st = cw.SummaryTokens()
	expected = 600000 * 75 / 100
	if expected == 0 {
		expected = 450000
	}
	if st != expected {
		t.Errorf("summaryTokens for large window = %d, want %d", st, expected)
	}
}

func TestResize_BufferTokens_AutoCalculate(t *testing.T) {
	cw := newTestCW(1000, 100)
	cw.Resize(500, 0, 0)
	_, _, buffer := cw.TokenUsage()
	if buffer != 50 {
		t.Errorf("auto bufferTokens = %d, want 50 (maxTokens/10)", buffer)
	}
}
