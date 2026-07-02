package ctxwin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── charLevelTruncate ─────────────────────────────────────────────────────

func TestCharLevelTruncate(t *testing.T) {
	// 100 characters, preserve first 10% + last 20% = 10 + 20 = 30 characters
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
	// Single character should not be truncated (head+tail >= n)
	s := "x"
	result := charLevelTruncate(s, 0.10, 0.20)
	if result != s {
		t.Errorf("single char should not be truncated: got %q", result)
	}
}

func TestCharLevelTruncateChinese(t *testing.T) {
	// Chinese characters, each rune counts as one character
	s := strings.Repeat("you", 100)
	result := charLevelTruncate(s, 0.10, 0.20)
	if !strings.Contains(result, "omitted") {
		t.Error("should contain omission marker for Chinese")
	}
}

// ─── tryJSONObjectTruncate ──────────────────────────────────────────────────

func TestTryJSONObjectTruncate(t *testing.T) {
	tok := NewTokenizer()
	// Construct a JSON object with a large 'content' field
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

	// The result should be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v\n%s", err, result)
	}

	// Small fields should be preserved
	if parsed["path"] != "main.go" {
		t.Errorf("path = %v, want main.go", parsed["path"])
	}
	// 'size' should be preserved (numbers are not strings, so they won't be truncated)
	if parsed["size"] != float64(2048) {
		t.Errorf("size = %v, want 2048", parsed["size"])
	}
	// 'content' should be truncated
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
	// Small fields should not be truncated
	obj := map[string]any{
		"exit_code": 0,
		"stdout":    "hello",
		"stderr":    "",
	}
	input, _ := json.Marshal(obj)

	result := tryJSONObjectTruncate(string(input), tok)
	// All fields are small, should return "" (no truncation needed)
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
	// Tool error output in "error: ..." format is not JSON, should return ""
	result := tryJSONObjectTruncate("error: command not found", tok)
	if result != "" {
		t.Errorf("error string should return empty string, got: %s", result)
	}
}

// ─── tryJSONArrayTruncate ───────────────────────────────────────────────────

func TestTryJSONArrayTruncate(t *testing.T) {
	tok := NewTokenizer()
	// Construct a JSON array with 50 elements
	arr := make([]any, 50)
	for i := range arr {
		arr[i] = map[string]any{"file": "test.go", "line": i}
	}
	input, _ := json.Marshal(arr)

	result := tryJSONArrayTruncate(string(input), tok)
	if result == "" {
		t.Fatal("tryJSONArrayTruncate returned empty string")
	}

	// The result should be valid JSON
	var parsed []any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// Should contain head and tail elements + an omission marker
	// head = max(1, 50*0.10) = 5, tail = max(1, 50*0.20) = 10
	expectedLen := 5 + 1 + 10 // head + omission + tail
	if len(parsed) != expectedLen {
		t.Errorf("parsed array len = %d, want %d", len(parsed), expectedLen)
	}

	// Omission marker should be a string
	marker, ok := parsed[5].(string)
	if !ok || !strings.Contains(marker, "omitted") {
		t.Errorf("omission marker should be a string with omitted, got: %v", parsed[5])
	}
}

func TestTryJSONArrayTruncateSmall(t *testing.T) {
	tok := NewTokenizer()
	// Fewer than 10 elements, should not be truncated
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
	// 'body' needs to exceed largeFieldTokenThreshold=500 tokens
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
	// Push a very long ephemeral message
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
	// Construct JSON-formatted tool output
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
	// Should still be valid JSON after truncation
	var parsed map[string]any
	if err := json.Unmarshal([]byte(msg.Content), &parsed); err != nil {
		t.Fatalf("truncated content should be valid JSON: %v\ncontent: %s", err, msg.Content[:200])
	}
	// 'path' and 'size' should be preserved
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

	cw.slideFIFO(0) // target=0 should remove as many turns as possible

	// The system prompt should be preserved
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

	// Deleting to a very small target should remove the oldest turn
	cw.slideFIFO(10) // very small target

	afterLen := cw.Len()
	afterTokens, _, _ := cw.TokenUsage()

	if afterLen >= beforeLen {
		t.Errorf("Len should decrease: before=%d, after=%d", beforeLen, afterLen)
	}
	if afterTokens >= beforeTokens {
		t.Errorf("Tokens should decrease: before=%d, after=%d", beforeTokens, afterTokens)
	}

	// Verification: remaining messages should be continuous turns (each user followed by a corresponding assistant)
	payload := cw.BuildPayload()
	for i := 1; i < len(payload); {
		if payload[i].Role != "user" {
			t.Errorf("expected user at index %d, got %q", i, payload[i].Role)
			break
		}
		// Find the end of this turn
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

	cw.slideFIFO(0) // target=0, but only one turn

	// Only one turn, should not be deleted
	if cw.Len() < 2 { // At least system + user + assistant
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

	cw.slideFIFO(0) // Delete as much as possible

	// Only Turn 2 (the latest) should be preserved
	first, _ := cw.MessageAt(0)
	if first.Role != RoleSystem {
		t.Errorf("first should be system, got %q", first.Role)
	}
	// The second should be a user message (start of Turn 2)
	if cw.Len() > 1 {
		second, _ := cw.MessageAt(1)
		if second.Role != RoleUser {
			t.Errorf("second should be user, got %q", second.Role)
		}
	}
}

// TestSlideFIFOCleansOrphanToolMessages verifies that slideFIFO cleans up
// orphan tool messages in subsequent turns that reference deleted tool_call_ids.
//
// Scenario reproducing orphan tool message issue due to async delegation:
//
//	Turn 1: user, assistant(tool_calls: [call_1, call_2])
//	Turn 2: user, tool(call_1), tool(call_2)  ← Orphan tool messages
//
// After slideFIFO deletes Turn 1, Turn 2's tool messages should also be cleaned up.
func TestSlideFIFOCleansOrphanToolMessages(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleSystem, "System")

	// Turn 1: user + assistant(tool_calls: [call_1, call_2]), no tool results
	cw.Push(RoleUser, "Delegate tasks")
	cw.Push(RoleAssistant, "", WithToolCalls([]llm.ToolCall{
		{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}},
		{ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}},
	}))

	// Turn 2: new user + two orphan tool messages (referencing call_1 and call_2 from Turn 1)
	cw.Push(RoleUser, "Another message")
	cw.Push(RoleTool, "result 1", WithToolCallID("call_1"), WithToolName("delegate"), WithEphemeral(true))
	cw.Push(RoleTool, "result 2", WithToolCallID("call_2"), WithToolName("delegate"), WithEphemeral(true))

	// Turn 3: another complete turn (should not be deleted)
	cw.Push(RoleUser, "Last question")
	cw.Push(RoleAssistant, "Final answer")

	beforeLen := cw.Len()

	// slideFIFO deleting Turn 1 should also clean up orphan tool messages in Turn 2
	cw.slideFIFO(0)

	afterLen := cw.Len()
	if afterLen >= beforeLen {
		t.Errorf("Len should decrease: before=%d, after=%d", beforeLen, afterLen)
	}

	// Verify BuildPayload does not contain orphan tool messages
	payload := cw.BuildPayload()

	// Check if there are tool messages missing preceding assistant(tool_calls)
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

// ─── Full Eviction Flow ───────────────────────────────────────────────────────────

func TestFullEvictionFlow(t *testing.T) {
	cw := newTestCW(500, 100) // Small window: effective capacity = 400

	cw.Push(RoleSystem, "System prompt for testing.")

	// Push many messages to trigger eviction
	for i := 0; i < 30; i++ {
		cw.Push(RoleUser, "Tell me a long story about programming and software engineering practices.")
		cw.Push(RoleAssistant, "Once upon a time, there was a programmer who wrote clean code every day and refactored with confidence.")
	}

	current, _, _ := cw.TokenUsage()
	// After eviction, should be below effective capacity
	if current > 500 {
		t.Errorf("After eviction: currentTokens=%d, should be <= maxTokens=500", current)
	}

	// The system prompt should be preserved
	if cw.Len() > 0 {
		first, _ := cw.MessageAt(0)
		if first.Role != RoleSystem {
			t.Errorf("first message should be system, got %q", first.Role)
		}
	}
}

// ─── Drift Scenario ───────────────────────────────────────────────────────────────

func TestDriftAfterCalibrate(t *testing.T) {
	cw := newTestCW(100000, 2000)
	cw.Push(RoleUser, "Hello")
	cw.Push(RoleAssistant, "Hi there")

	// Calibrate to an "exact value", which may differ from sum(messages.Tokens)
	// Simulate API returning an exact value that is 10% less than the estimated value
	estimated := cw.Recalculate()
	calibratedValue := int(float64(estimated) * 0.9)
	cw.Calibrate(calibratedValue)

	current, _, _ := cw.TokenUsage()
	if current != calibratedValue {
		t.Errorf("currentTokens = %d, want %d (calibrated value)", current, calibratedValue)
	}

	// sum(messages.Tokens) != currentTokens is normal drift
	recalculated := cw.Recalculate()
	if recalculated == current {
		// Coincidental equality is possible, but usually unequal
		t.Logf("Note: Recalculate() == currentTokens by coincidence (both = %d)", current)
	}

	// FIFO subtraction uses the estimated value, currentTokens will drift
	// But functionality remains correct: currentTokens is still used for eviction decisions
}

func TestAggressiveTruncateLastTurn(t *testing.T) {
	tok := NewTokenizer()
	cw := NewContextWindow(1000, 100, 0, tok)

	// Build a single turn that exceeds capacity
	cw.Push(RoleSystem, "system prompt")

	// Push a very large ephemeral message
	bigContent := strings.Repeat("a", 5000) // ~1250 tokens
	cw.Push(RoleUser, bigContent)

	// Push assistant reply
	cw.Push(RoleAssistant, "result")

	// Push automatically triggers eviction.  Because there is only one
	// turn, slideFIFO cannot delete it; it falls back to
	// aggressiveTruncateLastTurn, which truncates the big ephemeral
	// message so the window fits.
	if cw.Overflow() {
		t.Errorf("still overflow after automatic eviction: current=%d, max=%d", cw.CurrentTokens(), cw.maxTokens)
	}

	// System prompt should be preserved
	if cw.Len() < 1 {
		t.Fatal("system prompt was removed")
	}
	m0, _ := cw.MessageAt(0)
	if m0.Role != RoleSystem {
		t.Errorf("msg[0].Role = %q, want system", m0.Role)
	}
}

func TestAggressiveTruncateLastTurn_OmitsContent(t *testing.T) {
	tok := NewTokenizer()
	cw := NewContextWindow(100, 10, 0, tok)

	cw.Push(RoleSystem, "system")
	cw.Push(RoleUser, strings.Repeat("x", 2000)) // huge user message
	cw.Push(RoleAssistant, strings.Repeat("y", 2000)) // huge assistant message

	target := cw.maxTokens - cw.bufferTokens // 90
	cw.slideFIFO(target)

	// Should fit now
	if cw.Overflow() {
		t.Errorf("still overflow after aggressive truncation")
	}

	// At least system prompt should remain
	m0, _ := cw.MessageAt(0)
	if m0.Role != RoleSystem {
		t.Errorf("system prompt removed")
	}
}

func TestPruneOlderTurnsEphemeralContent(t *testing.T) {
	tok := NewTokenizer()
	// Create context window with plenty of space to avoid triggering automatic evictions during pushes
	cw := NewContextWindow(100000, 1000, 0, tok)

	// Push messages
	cw.Push(RoleSystem, "system prompt") // idx 0

	// Turn 1 (Oldest):
	cw.Push(RoleUser, "read file A") // idx 1
	cw.Push(RoleAssistant, "tool call") // idx 2
	// Ephemeral JSON output
	cw.Push(RoleTool, `{"path":"a.txt","content":"huge file content of A"}`, WithEphemeral(true), WithToolCallID("call_a")) // idx 3
	cw.Push(RoleAssistant, "Answer A") // idx 4

	// Turn 2:
	cw.Push(RoleUser, "read file B") // idx 5
	cw.Push(RoleAssistant, "tool call") // idx 6
	// Ephemeral non-JSON output
	cw.Push(RoleTool, "huge plain text content of B", WithEphemeral(true), WithToolCallID("call_b")) // idx 7
	cw.Push(RoleAssistant, "Answer B") // idx 8

	// Turn 3 (Newest):
	cw.Push(RoleUser, "read file C") // idx 9
	cw.Push(RoleAssistant, "tool call") // idx 10
	// Ephemeral JSON output
	cw.Push(RoleTool, `{"path":"c.txt","content":"huge file content of C"}`, WithEphemeral(true), WithToolCallID("call_c")) // idx 11
	cw.Push(RoleAssistant, "Answer C") // idx 12

	// We protect the last 2 user turns (Turn 3 and Turn 2).
	// So the 2nd user from the end is Turn 2 User ("read file B" at idx 5).
	// Everything before idx 5 (i.e. Turn 1) should be pruned if ephemeral.
	// Turn 1 ephemeral tool output is at idx 3.
	cw.pruneOlderTurnsEphemeralContent(2)

	// Verify Turn 1 tool result is pruned/evicted
	msgA, _ := cw.MessageAt(3)
	var parsedA map[string]any
	if err := json.Unmarshal([]byte(msgA.Content), &parsedA); err != nil {
		t.Fatalf("Turn 1 tool output should still be valid JSON, got: %s", msgA.Content)
	}
	if parsedA["path"] != "a.txt" {
		t.Errorf("Turn 1 metadata 'path' should be preserved, got %v", parsedA["path"])
	}
	if parsedA["content"] != "[evicted]" {
		t.Errorf("Turn 1 'content' should be pruned to '[evicted]', got %v", parsedA["content"])
	}

	// Verify Turn 2 and Turn 3 tool results are NOT pruned
	msgB, _ := cw.MessageAt(7)
	if msgB.Content != "huge plain text content of B" {
		t.Errorf("Turn 2 tool output should not be pruned, got %q", msgB.Content)
	}

	msgC, _ := cw.MessageAt(11)
	var parsedC map[string]any
	if err := json.Unmarshal([]byte(msgC.Content), &parsedC); err != nil {
		t.Fatalf("Turn 3 tool output should be valid JSON, got: %s", msgC.Content)
	}
	if parsedC["content"] != "huge file content of C" {
		t.Errorf("Turn 3 tool output should not be pruned, got %q", parsedC["content"])
	}

	// Now try protecting only 1 turn (Turn 3 is protected).
	// Boundary is Turn 3 User ("read file C" at idx 9).
	// Everything before idx 9 (Turn 1 and Turn 2) should be pruned.
	cw.pruneOlderTurnsEphemeralContent(1)

	// Turn 2 tool result is at idx 7, it is plain text.
	// Since it's not JSON, it should be replaced with "[evicted to save space]".
	msgB2, _ := cw.MessageAt(7)
	if msgB2.Content != "[evicted to save space]" {
		t.Errorf("Turn 2 tool output should be evicted to save space, got %q", msgB2.Content)
	}
}