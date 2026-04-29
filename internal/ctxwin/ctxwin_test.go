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
	cw.Push(RoleTool, `{"exit_code":0,"stdout":"hello"}`, WithEphemeral(true), WithToolCallID("call_1"), WithToolName("shell_exec"))

	msg, _ := cw.MessageAt(0)
	if !msg.IsEphemeral {
		t.Error("IsEphemeral = false, want true")
	}
	if msg.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want %q", msg.ToolCallID, "call_1")
	}
	if msg.Name != "shell_exec" {
		t.Errorf("Name = %q, want %q", msg.Name, "shell_exec")
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

	// Calibrate 后：currentTokens 被精确值覆盖，可能不等于 sum
	cw.Calibrate(42)
	current, _, _ = cw.TokenUsage()
	if current != 42 {
		t.Errorf("After Calibrate: currentTokens = %d, want 42", current)
	}
	// ⚠️ Calibrate 后 sum(messages.Tokens) 不一定等于 currentTokens — 这是正常的漂移
	// 不应断言 cw.Recalculate() == current
}

func TestOverflow(t *testing.T) {
	cw := newTestCW(100, 20)
	cw.Push(RoleUser, "Hello")

	// Overflow with a very large limit should be false
	if cw.Overflow(1000000) {
		t.Error("Overflow(1000000) should be false")
	}
	// Overflow with hardLimit=0 should be true (we have tokens after push)
	if !cw.Overflow(0) {
		t.Error("Overflow(0) should be true when we have messages")
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
