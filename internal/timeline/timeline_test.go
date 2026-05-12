package timeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── Writer: AppendMessage ───────────────────────────────────────────────────

func TestWriter_AppendMessage(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	err = w.AppendMessage(&MessagePayload{
		Role:    "user",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	events := readEventsFromFile(t, timelineFile(dir))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]
	if evt.EventType != EventMessage {
		t.Errorf("type = %q, want %q", evt.EventType, EventMessage)
	}
	if evt.Message == nil {
		t.Fatal("message is nil")
	}
	if evt.Message.Role != "user" || evt.Message.Content != "hello" {
		t.Errorf("message = %+v, want role=user content=hello", evt.Message)
	}
	if evt.Control != nil {
		t.Error("control should be nil for message event")
	}
}

func TestWriter_AppendMessage_AllFields(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	err = w.AppendMessage(&MessagePayload{
		Role:             "assistant",
		Content:          "result",
		ReasoningContent: "thinking...",
		Name:             "tool_a",
		ToolCallID:       "tc-1",
		ToolCalls: []ToolCallRec{
			{ID: "tc-1", Type: "function", Name: "read_file", Arguments: `{"path":"/tmp"}`},
		},
		IsEphemeral: true,
		AgentID:     "agent-001",
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	events := readEventsFromFile(t, timelineFile(dir))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	msg := events[0].Message
	if msg.Role != "assistant" {
		t.Errorf("role = %q", msg.Role)
	}
	if msg.Content != "result" {
		t.Errorf("content = %q", msg.Content)
	}
	if msg.ReasoningContent != "thinking..." {
		t.Errorf("reasoning = %q", msg.ReasoningContent)
	}
	if msg.Name != "tool_a" {
		t.Errorf("name = %q", msg.Name)
	}
	if msg.ToolCallID != "tc-1" {
		t.Errorf("tool_call_id = %q", msg.ToolCallID)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "read_file" {
		t.Errorf("tool_calls = %+v", msg.ToolCalls)
	}
	if !msg.IsEphemeral {
		t.Error("ephemeral should be true")
	}
	if msg.AgentID != "agent-001" {
		t.Errorf("agent_id = %q", msg.AgentID)
	}
}

// ─── Writer: AppendControl ───────────────────────────────────────────────────

func TestWriter_AppendControl(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	err = w.AppendControl(&ControlPayload{
		Action: "clear",
		Reason: "user_command",
	})
	if err != nil {
		t.Fatalf("AppendControl: %v", err)
	}

	events := readEventsFromFile(t, timelineFile(dir))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	evt := events[0]
	if evt.EventType != EventControl {
		t.Errorf("type = %q, want %q", evt.EventType, EventControl)
	}
	if evt.Control == nil {
		t.Fatal("control is nil")
	}
	if evt.Control.Action != "clear" {
		t.Errorf("action = %q, want clear", evt.Control.Action)
	}
	if evt.Control.Reason != "user_command" {
		t.Errorf("reason = %q", evt.Control.Reason)
	}
	if evt.Message != nil {
		t.Error("message should be nil for control event")
	}
}

// ─── Writer: Timestamp ───────────────────────────────────────────────────────

func TestWriter_EventHasTimestamp(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	w.AppendMessage(&MessagePayload{Role: "user", Content: "hi"})

	events := readEventsFromFile(t, timelineFile(dir))
	if events[0].Timestamp == "" {
		t.Error("timestamp is empty")
	}
}

// ─── Writer: Multiple events ─────────────────────────────────────────────────

func TestWriter_MultipleEvents(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	w.AppendMessage(&MessagePayload{Role: "user", Content: "q1"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a1"})
	w.AppendControl(&ControlPayload{Action: "clear"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q2"})

	events := readEventsFromFile(t, timelineFile(dir))
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Message.Content != "q1" {
		t.Errorf("event[0] content = %q", events[0].Message.Content)
	}
	if events[1].Message.Content != "a1" {
		t.Errorf("event[1] content = %q", events[1].Message.Content)
	}
	if events[2].Control.Action != "clear" {
		t.Errorf("event[2] action = %q", events[2].Control.Action)
	}
	if events[3].Message.Content != "q2" {
		t.Errorf("event[3] content = %q", events[3].Message.Content)
	}
}

// ─── Writer: Rotation ────────────────────────────────────────────────────────

func TestWriter_Rotation(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir, "timeline", 50, 3) // 50 bytes per file, keep 3 days
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	for i := 0; i < 30; i++ {
		w.AppendMessage(&MessagePayload{Role: "user", Content: "hello world"})
	}

	// 主文件应存在
	if _, err := os.Stat(timelineFile(dir)); err != nil {
		t.Errorf("active file missing: %v", err)
	}
	// 至少有一个轮转文件
	if _, err := os.Stat(timelineRotatedFile(dir, 2)); err != nil {
		t.Errorf("rotated file missing: %v", err)
	}
}

// ─── ReadTail ──────────────────────────────────────────────────────────────

func TestReadTail_NoFiles(t *testing.T) {
	dir := t.TempDir()
	segs, _, err := ReadTail(dir, "timeline", 10)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if len(segs) != 0 {
		t.Errorf("expected 0 segments, got %d", len(segs))
	}
}

func TestReadTail_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	segs, _, err := ReadTail(dir, "timeline", 10)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if len(segs) != 0 {
		t.Errorf("expected 0 segments for empty dir, got %d", len(segs))
	}
}

func TestReadTail_ReturnsMessages(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q1"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a1"})
	w.Close()

	segs, _, err := ReadTail(dir, "timeline", 10)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if len(segs[0].Messages) != 2 {
		t.Errorf("segment has %d messages, want 2", len(segs[0].Messages))
	}
}

func TestReadTail_MixedLegacyAndDateSizeFiles(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "timeline.jsonl")
	dateSize := timelineFile(dir)
	if err := os.WriteFile(legacy, []byte(
		`{"ts":"2026-01-01T00:00:00Z","type":"message","msg":{"role":"user","content":"q1","ts":"2026-01-01T00:00:00Z"}}`+"\n"+
			`{"ts":"2026-01-01T00:00:01Z","type":"message","msg":{"role":"assistant","content":"a1","ts":"2026-01-01T00:00:01Z"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if err := os.WriteFile(dateSize, []byte(
		`{"ts":"2026-01-02T00:00:00Z","type":"message","msg":{"role":"user","content":"q2","ts":"2026-01-02T00:00:00Z"}}`+"\n"+
			`{"ts":"2026-01-02T00:00:01Z","type":"message","msg":{"role":"assistant","content":"a2","ts":"2026-01-02T00:00:01Z"}}`+"\n"), 0o644); err != nil {
		t.Fatalf("write datesize: %v", err)
	}

	segs, _, err := ReadTail(dir, "timeline", 10)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if got := segs[0].Messages[0].Content; got != "q1" {
		t.Fatalf("first message = %q, want q1", got)
	}
	if got := segs[0].Messages[3].Content; got != "a2" {
		t.Fatalf("last message = %q, want a2", got)
	}
}

func TestReadTail_RespectsMaxTurns(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	// 3 user turns
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q1"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a1"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q2"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a2"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q3"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a3"})
	w.Close()

	segs, _, err := ReadTail(dir, "timeline", 2) // only last 2 turns
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	// Should have last 2 turns: q2,a2 q3,a3
	if len(segs[0].Messages) != 4 {
		t.Errorf("segment has %d messages, want 4 (last 2 turns)", len(segs[0].Messages))
	}
	if segs[0].Messages[0].Content != "q2" {
		t.Errorf("msg[0] = %q, want q2", segs[0].Messages[0].Content)
	}
	if segs[0].Messages[2].Content != "q3" {
		t.Errorf("msg[2] = %q, want q3", segs[0].Messages[2].Content)
	}
}

func TestReadTail_SkipsIdentitySystem(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	w.AppendMessage(&MessagePayload{Role: "system", Content: "<identity>\nyou are bot"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "hello"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "hi"})
	w.Close()

	segs, _, err := ReadTail(dir, "timeline", 1)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	// System message with <identity> should be skipped
	if len(segs[0].Messages) != 2 {
		t.Errorf("segment has %d messages, want 2 (user, assistant)", len(segs[0].Messages))
	}
	if segs[0].Messages[0].Role != "user" {
		t.Errorf("msg[0].Role = %q, want user", segs[0].Messages[0].Role)
	}
}

func TestReadTail_SkipsSummarySystem(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	w.AppendMessage(&MessagePayload{Role: "system", Content: "[Conversation Summary]\n..."})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "hello"})
	w.Close()

	segs, _, err := ReadTail(dir, "timeline", 1)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if len(segs[0].Messages) != 1 || segs[0].Messages[0].Content != "hello" {
		t.Errorf("expected only 'hello', got %+v", segs[0].Messages)
	}
}

// ─── ReplayInto ──────────────────────────────────────────────────────────────

func TestReplayInto_SystemMessagesPassedThrough(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "system", Content: "you are helpful"},
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi there"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	// System messages in the segment (e.g. compaction summaries) are pushed to CW.
	// The factory handles pushing the initial system prompt separately.
	if cw.Len() != 3 {
		t.Errorf("cw.Len() = %d, want 3 (system + user + assistant)", cw.Len())
	}
}

func TestReplayInto_WithToolCalls(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "user", Content: "read file"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCallRec{
						{ID: "tc-1", Type: "function", Name: "read_file", Arguments: `{"path":"/tmp"}`},
					},
				},
				{Role: "tool", Content: "file contents", Name: "read_file", ToolCallID: "tc-1"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	if cw.Len() != 3 {
		t.Fatalf("cw.Len() = %d, want 3", cw.Len())
	}

	// Check assistant message has tool calls
	msg, ok := cw.MessageAt(1)
	if !ok {
		t.Fatal("message at index 1 not found")
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool_calls len = %d, want 1", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("tool_call name = %q", msg.ToolCalls[0].Function.Name)
	}
	if msg.ToolCalls[0].Function.Arguments != `{"path":"/tmp"}` {
		t.Errorf("tool_call arguments = %q", msg.ToolCalls[0].Function.Arguments)
	}

	// Check tool message has correct fields
	toolMsg, ok := cw.MessageAt(2)
	if !ok {
		t.Fatal("message at index 2 not found")
	}
	if toolMsg.Name != "read_file" {
		t.Errorf("tool name = %q", toolMsg.Name)
	}
	if toolMsg.ToolCallID != "tc-1" {
		t.Errorf("tool_call_id = %q", toolMsg.ToolCallID)
	}
}

func TestReplayInto_OrphanedToolCalls_Skipped(t *testing.T) {
	// When assistant(tool_calls) has no corresponding tool results,
	// the entire assistant message should be skipped to prevent LLM API 400.
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "user", Content: "do something"},
				{
					Role:    "assistant",
					Content: "let me delegate",
					ToolCalls: []ToolCallRec{
						{ID: "tc-1", Type: "function", Name: "delegate_dev", Arguments: `{"task":"find json"}`},
					},
				},
				// NO tool result for tc-1 — orphaned!
				{Role: "user", Content: "next question"},
				{Role: "assistant", Content: "here is the answer"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	// Should have: user("do something"), user("next question"), assistant("here is the answer")
	// The orphaned assistant(tool_calls) is skipped
	if cw.Len() != 3 {
		t.Fatalf("cw.Len() = %d, want 3 (orphaned assistant skipped)", cw.Len())
	}
	msg0, _ := cw.MessageAt(0)
	if msg0.Role != "user" || msg0.Content != "do something" {
		t.Errorf("msg0 = %q %q, want user/do something", msg0.Role, msg0.Content)
	}
	msg1, _ := cw.MessageAt(1)
	if msg1.Role != "user" || msg1.Content != "next question" {
		t.Errorf("msg1 = %q %q, want user/next question", msg1.Role, msg1.Content)
	}
	msg2, _ := cw.MessageAt(2)
	if msg2.Role != "assistant" || msg2.Content != "here is the answer" {
		t.Errorf("msg2 = %q %q, want assistant/here is the answer", msg2.Role, msg2.Content)
	}
}

func TestReplayInto_PartialToolResults_Skipped(t *testing.T) {
	// When assistant(tool_calls) has 2 tool calls but only 1 tool result,
	// both the assistant message and the partial result should be skipped.
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "user", Content: "do two things"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCallRec{
						{ID: "tc-1", Type: "function", Name: "grep", Arguments: `{}`},
						{ID: "tc-2", Type: "function", Name: "delegate_dev", Arguments: `{}`},
					},
				},
				{Role: "tool", Content: "grep result", ToolCallID: "tc-1"},
				// NO tool result for tc-2 — partially orphaned!
				{Role: "user", Content: "next question"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	// Should have: user("do two things"), user("next question")
	// The orphaned assistant + partial tool result are both skipped
	if cw.Len() != 2 {
		t.Fatalf("cw.Len() = %d, want 2 (orphaned assistant + partial result skipped)", cw.Len())
	}
}

func TestReplayInto_CompleteToolCalls_Kept(t *testing.T) {
	// When all tool results are present, the assistant(tool_calls) + results are kept.
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "user", Content: "read file"},
				{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCallRec{
						{ID: "tc-1", Type: "function", Name: "read_file", Arguments: `{}`},
						{ID: "tc-2", Type: "function", Name: "grep", Arguments: `{}`},
					},
				},
				{Role: "tool", Content: "file contents", ToolCallID: "tc-1"},
				{Role: "tool", Content: "grep results", ToolCallID: "tc-2"},
				{Role: "assistant", Content: "here is what I found"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	// Should have all 5 messages
	if cw.Len() != 5 {
		t.Fatalf("cw.Len() = %d, want 5 (complete tool_calls kept)", cw.Len())
	}
}

func TestReplayInto_WithReasoningContent(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "user", Content: "think"},
				{Role: "assistant", Content: "answer", ReasoningContent: "let me think..."},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	msg, ok := cw.MessageAt(1)
	if !ok {
		t.Fatal("message at index 1 not found")
	}
	if msg.ReasoningContent != "let me think..." {
		t.Errorf("reasoning = %q", msg.ReasoningContent)
	}
}

func TestReplayInto_WithEphemeral(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "assistant", Content: "", ToolCalls: []ToolCallRec{{ID: "tc1", Type: "function", Name: "search", Arguments: "{}"}}},
				{Role: "tool", Content: "large output", IsEphemeral: true, ToolCallID: "tc1"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	msg, ok := cw.MessageAt(1)
	if !ok {
		t.Fatal("tool message not found at index 1")
	}
	if !msg.IsEphemeral {
		t.Error("ephemeral flag not set")
	}
}

func TestReplayInto_MultipleSegments(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{Messages: []MessagePayload{
			{Role: "user", Content: "q1"},
			{Role: "assistant", Content: "a1"},
		}},
		{Messages: []MessagePayload{
			{Role: "user", Content: "q2"},
			{Role: "assistant", Content: "a2"},
		}},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	if cw.Len() != 4 {
		t.Errorf("cw.Len() = %d, want 4", cw.Len())
	}
}

func TestReplayInto_SkipsEmptyAssistant(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	segments := []Segment{
		{
			Messages: []MessagePayload{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "", ReasoningContent: "thinking only, no output"},
				{Role: "user", Content: "continue"},
			},
		},
	}
	ReplayInto(cw, segments)
	cw.SetReplayMode(false)

	if cw.Len() != 2 {
		t.Fatalf("cw.Len() = %d, want 2 (empty assistant skipped)", cw.Len())
	}
	msg, ok := cw.MessageAt(0)
	if !ok || msg.Role != "user" || msg.Content != "hello" {
		t.Errorf("msg[0] = %+v, want user:hello", msg)
	}
	msg, ok = cw.MessageAt(1)
	if !ok || msg.Role != "user" || msg.Content != "continue" {
		t.Errorf("msg[1] = %+v, want user:continue", msg)
	}
}

func TestReplayInto_EmptySegments(t *testing.T) {
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)

	ReplayInto(cw, nil)
	cw.SetReplayMode(false)

	if cw.Len() != 0 {
		t.Errorf("cw.Len() = %d, want 0", cw.Len())
	}
}

// ─── End-to-end: Write then Read ────────────────────────────────────────────

func TestWriteThenRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	w.AppendMessage(&MessagePayload{Role: "system", Content: "be helpful"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "hello"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "world"})
	w.AppendControl(&ControlPayload{Action: "clear"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "new topic"})
	w.Close()

	segs, _, err := ReadTail(dir, "timeline", 10)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	// ReadTail ignores /clear cut points; returns last N turns.
	// 2 user turns → all 4 conversation messages (system passes through,
	// control events are ignored).
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if len(segs[0].Messages) != 4 {
		t.Errorf("segment has %d messages, want 4", len(segs[0].Messages))
	}
	// First user message is "hello"
	if segs[0].Messages[1].Content != "hello" {
		t.Errorf("msg[1] = %q, want 'hello'", segs[0].Messages[1].Content)
	}
}

func TestWriteThenReplay_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)

	// 写入完整的对话流程
	w.AppendMessage(&MessagePayload{Role: "system", Content: "system"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q1"})
	w.AppendMessage(&MessagePayload{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCallRec{
			{ID: "tc-1", Type: "function", Name: "read_file", Arguments: `{"path":"/tmp"}`},
		},
	})
	w.AppendMessage(&MessagePayload{Role: "tool", Content: "contents", Name: "read_file", ToolCallID: "tc-1"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a1", ReasoningContent: "thinking..."})
	w.Close()

	segs, _, _ := ReadTail(dir, "timeline", 10)

	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.SetReplayMode(true)
	ReplayInto(cw, segs)
	cw.SetReplayMode(false)

	// 5 messages (system is now passed through)
	if cw.Len() != 5 {
		t.Fatalf("cw.Len() = %d, want 5", cw.Len())
	}

	// 验证 system
	m0, _ := cw.MessageAt(0)
	if m0.Role != ctxwin.RoleSystem || m0.Content != "system" {
		t.Errorf("msg[0] = %+v", m0)
	}

	// 验证 user
	m1, _ := cw.MessageAt(1)
	if m1.Role != ctxwin.RoleUser || m1.Content != "q1" {
		t.Errorf("msg[1] = %+v", m1)
	}

	// 验证 assistant with tool_calls
	m2, _ := cw.MessageAt(2)
	if len(m2.ToolCalls) != 1 {
		t.Fatalf("msg[2] tool_calls len = %d", len(m2.ToolCalls))
	}
	if m2.ToolCalls[0].ID != "tc-1" || m2.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("msg[2] tool_call = %+v", m2.ToolCalls[0])
	}

	// 验证 tool result
	m3, _ := cw.MessageAt(3)
	if m3.Role != ctxwin.RoleTool || m3.ToolCallID != "tc-1" {
		t.Errorf("msg[3] = %+v", m3)
	}

	// 验证 assistant with reasoning
	m4, _ := cw.MessageAt(4)
	if m4.Content != "a1" || m4.ReasoningContent != "thinking..." {
		t.Errorf("msg[4] = %+v", m4)
	}
}

// ─── readFile: corrupted lines ───────────────────────────────────────────────

func TestReadFile_SkipsCorruptedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	// 写入混合内容：有效行 + 损坏行 + 空行
	f, _ := os.Create(path)
	f.WriteString(`{"ts":"2025-01-01T00:00:00Z","type":"message","msg":{"role":"user","content":"q1"}}` + "\n")
	f.WriteString("this is not json\n")
	f.WriteString("\n")
	f.WriteString(`{"ts":"2025-01-01T00:00:00Z","type":"message","msg":{"role":"assistant","content":"a1"}}` + "\n")
	f.Close()

	events, err := readFile(path)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 valid events, got %d", len(events))
	}
}

// ─── newEvent ────────────────────────────────────────────────────────────────

func TestNewEvent_Timestamp(t *testing.T) {
	evt := newEvent(EventMessage)
	if evt.Timestamp == "" {
		t.Error("timestamp should not be empty")
	}
	if evt.EventType != EventMessage {
		t.Errorf("type = %q, want %q", evt.EventType, EventMessage)
	}
}

// ─── ToolCallRec JSON round-trip ─────────────────────────────────────────────

func TestToolCallRec_JSONRoundTrip(t *testing.T) {
	tc := ToolCallRec{
		ID:        "call-abc",
		Type:      "function",
		Name:      "Bash",
		Arguments: `{"command":"ls"}`,
	}
	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got ToolCallRec
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got != tc {
		t.Errorf("round-trip failed: got %+v, want %+v", got, tc)
	}
}

// ─── PushHook integration ────────────────────────────────────────────────────

func TestPushHook_WritesToTimeline(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	defer w.Close()

	pushHook := func(msg ctxwin.Message) {
		var toolCalls []ToolCallRec
		for _, tc := range msg.ToolCalls {
			toolCalls = append(toolCalls, ToolCallRec{
				ID:        tc.ID,
				Type:      tc.Type,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		_ = w.AppendMessage(&MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          "test-agent",
		})
	}

	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer(),
		ctxwin.WithPushHook(pushHook),
	)

	cw.Push(ctxwin.RoleUser, "hello")
	cw.Push(ctxwin.RoleAssistant, "world", ctxwin.WithReasoningContent("thinking..."))
	cw.Push(ctxwin.RoleAssistant, "",
		ctxwin.WithToolCalls([]llm.ToolCall{
			{
				ID:   "tc-1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "read_file",
					Arguments: `{"path":"/tmp"}`,
				},
			},
		}),
	)

	events := readEventsFromFile(t, timelineFile(dir))
	if len(events) != 3 {
		t.Fatalf("expected 3 events from pushHook, got %d", len(events))
	}

	// user
	if events[0].Message.Role != "user" || events[0].Message.Content != "hello" {
		t.Errorf("event[0] = %+v", events[0].Message)
	}
	// assistant with reasoning
	if events[1].Message.ReasoningContent != "thinking..." {
		t.Errorf("event[1] reasoning = %q", events[1].Message.ReasoningContent)
	}
	// assistant with tool_calls
	if len(events[2].Message.ToolCalls) != 1 {
		t.Fatalf("event[2] tool_calls len = %d", len(events[2].Message.ToolCalls))
	}
	if events[2].Message.ToolCalls[0].Name != "read_file" {
		t.Errorf("event[2] tool_call name = %q", events[2].Message.ToolCalls[0].Name)
	}
	if events[2].Message.AgentID != "test-agent" {
		t.Errorf("event[2] agent_id = %q", events[2].Message.AgentID)
	}
}

// ─── Helper ──────────────────────────────────────────────────────────────────

func timelineFile(dir string) string {
	return filepath.Join(dir, "timeline-"+time.Now().Format("2006-01-02")+".jsonl")
}

func timelineRotatedFile(dir string, seq int) string {
	return filepath.Join(dir, "timeline-"+time.Now().Format("2006-01-02")+"-"+strconv.Itoa(seq)+".jsonl")
}

func readEventsFromFile(t *testing.T, path string) []Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}

	var events []Event
	for _, line := range splitLines(string(data)) {
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Errorf("unmarshal line: %v\nline: %s", err, line)
			continue
		}
		events = append(events, evt)
	}
	return events
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func TestReadTailBefore_Pagination(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(dir, "timeline", 0, 0)
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q1", Timestamp: "2026-01-01T00:00:00Z"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a1", Timestamp: "2026-01-01T00:00:01Z"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q2", Timestamp: "2026-01-02T00:00:00Z"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a2", Timestamp: "2026-01-02T00:00:01Z"})
	w.AppendMessage(&MessagePayload{Role: "user", Content: "q3", Timestamp: "2026-01-03T00:00:00Z"})
	w.AppendMessage(&MessagePayload{Role: "assistant", Content: "a3", Timestamp: "2026-01-03T00:00:01Z"})
	w.Close()

	// Read all 3 turns
	segs, cursor, err := ReadTail(dir, "timeline", 10)
	if err != nil {
		t.Fatalf("ReadTail: %v", err)
	}
	if cursor == nil {
		t.Fatal("expected a cursor")
	}
	if len(segs[0].Messages) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(segs[0].Messages))
	}

	// ReadTailBefore with cursor from oldest message should return nothing
	segs2, cursor2, err := ReadTailBefore(dir, "timeline", 10, *cursor)
	if err != nil {
		t.Fatalf("ReadTailBefore: %v", err)
	}
	if len(segs2) != 0 {
		t.Errorf("expected 0 segments (no older), got %d", len(segs2))
	}
	if cursor2 != nil {
		t.Errorf("expected nil cursor, got %v", cursor2)
	}

	// Read only last 1 turn, then page back
	segs3, cursor3, err := ReadTail(dir, "timeline", 1)
	if err != nil {
		t.Fatalf("ReadTail(1): %v", err)
	}
	if segs3[0].Messages[0].Content != "q3" {
		t.Errorf("msg[0] = %q, want q3", segs3[0].Messages[0].Content)
	}

	segs4, _, err := ReadTailBefore(dir, "timeline", 1, *cursor3)
	if err != nil {
		t.Fatalf("ReadTailBefore(1): %v", err)
	}
	if segs4[0].Messages[0].Content != "q2" {
		t.Errorf("msg[0] = %q, want q2", segs4[0].Messages[0].Content)
	}
}
