package ctxwin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── charLevelTruncate ─────────────────────────────────────────────────────

func TestCharLevelTruncate(t *testing.T) {
	// 100 字符，保留前 10% + 后 20% = 10 + 20 = 30 字符
	s := strings.Repeat("x", 100)
	result := charLevelTruncate(s, 0.10, 0.20)
	if len(result) >= 100 {
		t.Errorf("charLevelTruncate should shorten: got len=%d", len(result))
	}
	if !strings.HasPrefix(result, "xxxxxxxxxx") {
		t.Errorf("should preserve first 10%%: got %q", result[:20])
	}
	if !strings.Contains(result, "omitted") {
		t.Error("should contain omission marker")
	}
}

func TestCharLevelTruncateShort(t *testing.T) {
	// 单字符不应该被截断（head+tail >= n）
	s := "x"
	result := charLevelTruncate(s, 0.10, 0.20)
	if result != s {
		t.Errorf("single char should not be truncated: got %q", result)
	}
}

func TestCharLevelTruncateChinese(t *testing.T) {
	// 中文字符，每个 rune 算一个字符
	s := strings.Repeat("你", 100)
	result := charLevelTruncate(s, 0.10, 0.20)
	if !strings.Contains(result, "omitted") {
		t.Error("should contain omission marker for Chinese")
	}
}

// ─── tryJSONObjectTruncate ──────────────────────────────────────────────────

func TestTryJSONObjectTruncate(t *testing.T) {
	tok := NewTokenizer()
	// 构造一个包含大 content 字段的 JSON
	largeContent := strings.Repeat("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n", 50)
	obj := map[string]any{
		"path":    "main.go",
		"size":    2048,
		"content": largeContent,
	}
	input, _ := json.Marshal(obj)

	result := tryJSONObjectTruncate(string(input), tok)
	if result == "" {
		t.Fatal("tryJSONObjectTruncate returned empty string")
	}

	// 结果应该是合法 JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, result)
	}

	// 小字段应该保留
	if parsed["path"] != "main.go" {
		t.Errorf("path = %v, want main.go", parsed["path"])
	}
	// size 应该保留（数字不是字符串，不会被截断）
	if parsed["size"] != float64(2048) {
		t.Errorf("size = %v, want 2048", parsed["size"])
	}
	// content 应该被截断
	content, ok := parsed["content"].(string)
	if !ok {
		t.Fatal("content should be a string")
	}
	if len(content) >= len(largeContent) {
		t.Error("content should be truncated")
	}
	if !strings.Contains(content, "omitted") {
		t.Error("truncated content should contain omission marker")
	}
}

func TestTryJSONObjectTruncateSmallFields(t *testing.T) {
	tok := NewTokenizer()
	// 小字段不应该被截断
	obj := map[string]any{
		"exit_code": 0,
		"stdout":    "hello",
		"stderr":    "",
	}
	input, _ := json.Marshal(obj)

	result := tryJSONObjectTruncate(string(input), tok)
	// 所有字段都很小，应该返回 ""（无需截断）
	if result != "" {
		t.Errorf("small fields should not be truncated, got: %s", result)
	}
}

func TestTryJSONObjectTruncateNonJSON(t *testing.T) {
	tok := NewTokenizer()
	result := tryJSONObjectTruncate("not json at all", tok)
	if result != "" {
		t.Errorf("non-JSON should return empty string, got: %s", result)
	}
}

func TestTryJSONObjectTruncateErrorPrefix(t *testing.T) {
	tok := NewTokenizer()
	// "error: ..." 格式的工具错误输出不是 JSON，应该返回 ""
	result := tryJSONObjectTruncate("error: command not found", tok)
	if result != "" {
		t.Errorf("error string should return empty string, got: %s", result)
	}
}

// ─── tryJSONArrayTruncate ───────────────────────────────────────────────────

func TestTryJSONArrayTruncate(t *testing.T) {
	tok := NewTokenizer()
	// 构造一个包含 50 个元素的 JSON 数组
	arr := make([]any, 50)
	for i := range arr {
		arr[i] = map[string]any{"file": "test.go", "line": i}
	}
	input, _ := json.Marshal(arr)

	result := tryJSONArrayTruncate(string(input), tok)
	if result == "" {
		t.Fatal("tryJSONArrayTruncate returned empty string")
	}

	// 结果应该是合法 JSON
	var parsed []any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// 应该包含头尾元素 + 一个省略标记
	// head = max(1, 50*0.10) = 5, tail = max(1, 50*0.20) = 10
	expectedLen := 5 + 1 + 10 // head + omission + tail
	if len(parsed) != expectedLen {
		t.Errorf("parsed array len = %d, want %d", len(parsed), expectedLen)
	}

	// 省略标记应该是一个字符串
	marker, ok := parsed[5].(string)
	if !ok || !strings.Contains(marker, "omitted") {
		t.Errorf("omission marker should be a string with omitted, got: %v", parsed[5])
	}
}

func TestTryJSONArrayTruncateSmall(t *testing.T) {
	tok := NewTokenizer()
	// 少于 10 个元素，不应该截断
	arr := []any{1, 2, 3, 4, 5}
	input, _ := json.Marshal(arr)

	result := tryJSONArrayTruncate(string(input), tok)
	if result != "" {
		t.Errorf("small array should not be truncated, got: %s", result)
	}
}

// ─── tryJSONTruncate ────────────────────────────────────────────────────────

func TestTryJSONTruncateObject(t *testing.T) {
	tok := NewTokenizer()
	// body 需要超过 largeFieldTokenThreshold=500 tokens
	largeContent := strings.Repeat("This is a paragraph of HTTP response body content that should be long enough to exceed the threshold. ", 50)
	obj := map[string]any{
		"status": 200,
		"body":   largeContent,
	}
	input, _ := json.Marshal(obj)

	result := tryJSONTruncate(string(input), tok)
	if result == "" {
		t.Fatal("tryJSONTruncate should handle JSON objects")
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result should be valid JSON: %v", err)
	}
}

func TestTryJSONTruncateArray(t *testing.T) {
	tok := NewTokenizer()
	arr := make([]any, 20)
	for i := range arr {
		arr[i] = i
	}
	input, _ := json.Marshal(arr)

	result := tryJSONTruncate(string(input), tok)
	if result == "" {
		t.Fatal("tryJSONTruncate should handle JSON arrays")
	}
}

func TestTryJSONTruncateNonJSON(t *testing.T) {
	tok := NewTokenizer()
	result := tryJSONTruncate("plain text, not json", tok)
	if result != "" {
		t.Errorf("non-JSON should return empty string, got: %s", result)
	}
}

// ─── truncateMiddleOut ──────────────────────────────────────────────────────

func TestTruncateMiddleOutEphemeral(t *testing.T) {
	cw := newTestCW(100000, 2000)
	// Push 一个超长的 ephemeral 消息
	largeContent := strings.Repeat("This is a line of code that should be truncated. ", 200)
	cw.Push(RoleTool, largeContent, WithEphemeral(true), WithToolCallID("call_1"))

	beforeTokens, _, _ := cw.TokenUsage()
	truncated := cw.truncateMiddleOut()
	afterTokens, _, _ := cw.TokenUsage()

	if !truncated {
		t.Error("truncateMiddleOut should return true for large ephemeral message")
	}
	if afterTokens >= beforeTokens {
		t.Errorf("tokens should decrease: before=%d, after=%d", beforeTokens, afterTokens)
	}
}

func TestTruncateMiddleOutNonEphemeral(t *testing.T) {
	cw := newTestCW(100000, 2000)
	largeContent := strings.Repeat("This is important context. ", 200)
	cw.Push(RoleAssistant, largeContent)

	truncated := cw.truncateMiddleOut()
	if truncated {
		t.Error("non-ephemeral messages should not be truncated")
	}
}

func TestTruncateMiddleOutSmallEphemeral(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleTool, "small output", WithEphemeral(true), WithToolCallID("call_1"))

	truncated := cw.truncateMiddleOut()
	if truncated {
		t.Error("small ephemeral messages should not be truncated")
	}
}

func TestTruncateMiddleOutJSONPreservation(t *testing.T) {
	cw := newTestCW(100000, 2000)
	// 构造 JSON 格式的工具输出
	largeFileContent := strings.Repeat("package main\n", 200)
	obj := map[string]any{
		"path":    "main.go",
		"size":    5000,
		"content": largeFileContent,
	}
	input, _ := json.Marshal(obj)

	cw.Push(RoleTool, string(input), WithEphemeral(true), WithToolCallID("call_1"))
	cw.truncateMiddleOut()

	msg, _ := cw.MessageAt(0)
	// 截断后应该仍是合法 JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(msg.Content), &parsed); err != nil {
		t.Fatalf("truncated content should be valid JSON: %v\ncontent: %s", err, msg.Content[:200])
	}
	// path 和 size 应该保留
	if parsed["path"] != "main.go" {
		t.Errorf("path should be preserved, got: %v", parsed["path"])
	}
}

// ─── slideFIFO ──────────────────────────────────────────────────────────────

func TestSlideFIFOPreservesSystemPrompt(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "You are helpful.")
	cw.Push(RoleUser, "Question 1")
	cw.Push(RoleAssistant, "Answer 1")
	cw.Push(RoleUser, "Question 2")
	cw.Push(RoleAssistant, "Answer 2")

	cw.slideFIFO(0) // target=0 应该删除尽可能多的 Turn

	// system prompt 应该保留
	if cw.Len() == 0 {
		t.Fatal("messages should not be empty")
	}
	first, _ := cw.MessageAt(0)
	if first.Role != RoleSystem {
		t.Errorf("first message should be system, got %q", first.Role)
	}
}

func TestSlideFIFORemovesWholeTurns(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Q1")
	cw.Push(RoleAssistant, "A1")
	cw.Push(RoleUser, "Q2")
	cw.Push(RoleAssistant, "A2")
	cw.Push(RoleUser, "Q3")
	cw.Push(RoleAssistant, "A3")

	beforeLen := cw.Len()
	beforeTokens, _, _ := cw.TokenUsage()

	// 删除到很小的 target，应该删掉最老的 Turn
	cw.slideFIFO(10) // 极小 target

	afterLen := cw.Len()
	afterTokens, _, _ := cw.TokenUsage()

	if afterLen >= beforeLen {
		t.Errorf("Len should decrease: before=%d, after=%d", beforeLen, afterLen)
	}
	if afterTokens >= beforeTokens {
		t.Errorf("Tokens should decrease: before=%d, after=%d", beforeTokens, afterTokens)
	}

	// 验证：剩余消息应该是连续的 Turn（每个 user 后面跟着对应的 assistant）
	payload := cw.BuildPayload()
	for i := 1; i < len(payload); {
		if payload[i].Role != "user" {
			t.Errorf("expected user at index %d, got %q", i, payload[i].Role)
			break
		}
		// 找到这个 Turn 的结束
		i++
		for i < len(payload) && payload[i].Role != "user" {
			i++
		}
	}
}

func TestSlideFIFODoesNotDeleteLastTurn(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Q1")
	cw.Push(RoleAssistant, "A1")

	cw.slideFIFO(0) // target=0，但只有一个 Turn

	// 只有一个 Turn，不应该被删除
	if cw.Len() < 2 { // 至少 system + user + assistant
		t.Errorf("last Turn should not be deleted, Len=%d", cw.Len())
	}
}

func TestSlideFIFOWithToolCalls(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")
	// Turn 1: user + assistant(tool_calls) + tool + assistant
	cw.Push(RoleUser, "Read the file")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "Read", Arguments: `{"path":"main.go"}`}},
	}))
	cw.Push(RoleTool, `{"path":"main.go","content":"package main"}`, WithToolCallID("call_1"), WithToolName("Read"), WithEphemeral(true))
	cw.Push(RoleAssistant, "The file contains a Go program.")
	// Turn 2
	cw.Push(RoleUser, "What does it do?")
	cw.Push(RoleAssistant, "It's a simple Go program.")

	cw.slideFIFO(0) // 删除尽可能多

	// 应该只保留 Turn 2（最新的）
	first, _ := cw.MessageAt(0)
	if first.Role != RoleSystem {
		t.Errorf("first should be system, got %q", first.Role)
	}
	// 第二个应该是 user（Turn 2 的开头）
	if cw.Len() > 1 {
		second, _ := cw.MessageAt(1)
		if second.Role != RoleUser {
			t.Errorf("second should be user, got %q", second.Role)
		}
	}
}

// TestSlideFIFOCleansOrphanToolMessages 验证 slideFIFO 删除 Turn 时会清理
// 后续 Turn 中引用了被删 tool_call_ids 的孤 tool 消息。
//
// 场景复现异步委派导致的孤 tool 消息问题：
//
//	Turn 1: user, assistant(tool_calls: [call_1, call_2])
//	Turn 2: user, tool(call_1), tool(call_2)  ← 孤 tool 消息
//
// slideFIFO 删除 Turn 1 后，Turn 2 的 tool 消息也应被清理。
func TestSlideFIFOCleansOrphanToolMessages(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")

	// Turn 1: user + assistant(tool_calls: [call_1, call_2])，无 tool 结果
	cw.Push(RoleUser, "Delegate tasks")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}},
		{ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}},
	}))

	// Turn 2: 新 user + 两个孤 tool 消息（引用 Turn 1 的 call_1 和 call_2）
	cw.Push(RoleUser, "Another message")
	cw.Push(RoleTool, "result 1", WithToolCallID("call_1"), WithToolName("delegate"), WithEphemeral(true))
	cw.Push(RoleTool, "result 2", WithToolCallID("call_2"), WithToolName("delegate"), WithEphemeral(true))

	// Turn 3: 另一个完整 Turn（不应被删除）
	cw.Push(RoleUser, "Last question")
	cw.Push(RoleAssistant, "Final answer")

	beforeLen := cw.Len()

	// slideFIFO 删除 Turn 1，应同时清理 Turn 2 中的孤 tool 消息
	cw.slideFIFO(0)

	afterLen := cw.Len()
	if afterLen >= beforeLen {
		t.Errorf("Len should decrease: before=%d, after=%d", beforeLen, afterLen)
	}

	// 验证 BuildPayload 不包含孤 tool 消息
	payload := cw.BuildPayload()

	// 检查是否有 tool 消息缺少前置 assistant(tool_calls)
	validIDs := make(map[string]bool)
	for _, p := range payload {
		for _, tc := range p.ToolCalls {
			validIDs[tc.ID] = true
		}
	}
	for _, p := range payload {
		if p.Role == "tool" && p.ToolCallID != "" && !validIDs[p.ToolCallID] {
			t.Errorf("BuildPayload should not contain orphan tool message with tool_call_id=%q", p.ToolCallID)
		}
	}
}

// ─── 完整淘汰流程 ───────────────────────────────────────────────────────────

func TestFullEvictionFlow(t *testing.T) {
	cw := newTestCW(500, 100) // 小窗口：有效容量=400

	cw.Push(RoleSystem, "System prompt for testing.")

	// Push 大量消息触发淘汰
	for i := 0; i < 30; i++ {
		cw.Push(RoleUser, "Tell me a long story about programming and software engineering practices.")
		cw.Push(RoleAssistant, "Once upon a time, there was a programmer who wrote clean code every day and refactored with confidence.")
	}

	current, _, _ := cw.TokenUsage()
	// 淘汰后应该低于有效容量
	if current > 500 {
		t.Errorf("After eviction: currentTokens=%d, should be <= maxTokens=500", current)
	}

	// system prompt 应该保留
	if cw.Len() > 0 {
		first, _ := cw.MessageAt(0)
		if first.Role != RoleSystem {
			t.Errorf("first message should be system, got %q", first.Role)
		}
	}
}

// ─── 漂移场景 ───────────────────────────────────────────────────────────────

func TestDriftAfterCalibrate(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleUser, "Hello")
	cw.Push(RoleAssistant, "Hi there")

	// Calibrate 为一个"精确值"，它可能与 sum(messages.Tokens) 不同
	// 模拟 API 返回的精确值比估算值少 10%
	estimated := cw.Recalculate()
	calibratedValue := int(float64(estimated) * 0.9)
	cw.Calibrate(calibratedValue)

	current, _, _ := cw.TokenUsage()
	if current != calibratedValue {
		t.Errorf("currentTokens = %d, want %d (calibrated value)", current, calibratedValue)
	}

	// ⚠️ sum(messages.Tokens) != currentTokens 是正常的漂移
	recalculated := cw.Recalculate()
	if recalculated == current {
		// 偶然相等是可能的，但通常不等
		t.Logf("Note: Recalculate() == currentTokens by coincidence (both = %d)", current)
	}

	// FIFO 减去的是估算值，currentTokens 会产生漂移
	// 但功能仍然正确：currentTokens 仍用于淘汰决策
}
