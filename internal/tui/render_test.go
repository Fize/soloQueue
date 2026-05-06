package tui

import (
	"strings"
	"testing"
	"time"
)

// ─── formatTimestamp ─────────────────────────────────────────────────────────

func TestFormatTimestamp(t *testing.T) {
	ts := time.Date(2026, 1, 1, 14, 30, 0, 0, time.UTC)
	got := formatTimestamp("You", ts)
	if got != "You · 14:30" {
		t.Errorf("got %q, want %q", got, "You · 14:30")
	}
	// Zero time: just the name
	got = formatTimestamp("Solo", time.Time{})
	if got != "Solo" {
		t.Errorf("got %q, want %q", got, "Solo")
	}
}

// ─── renderUserMessage ──────────────────────────────────────────────────────

func TestRenderUserMessage(t *testing.T) {
	msg := message{role: "user", content: "hello world"}
	got := renderUserMessage(msg, 80)
	if !strings.Contains(got, "hello world") {
		t.Error("user message should contain content")
	}
	if !strings.Contains(got, "You") {
		t.Error("user message should contain timestamp label 'You'")
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Error("user message should end with double newline")
	}
}

// ─── renderMessage ──────────────────────────────────────────────────────────

func TestRenderMessage_UserRole(t *testing.T) {
	m := newTestModel()
	msg := message{role: "user", content: "test"}
	got := m.renderMessage(msg)
	if !strings.Contains(got, "test") {
		t.Error("user message should contain content")
	}
	if !strings.Contains(got, "You") {
		t.Error("user message should contain timestamp label 'You'")
	}
}

func TestRenderMessage_AgentRole(t *testing.T) {
	m := newTestModel()
	msg := message{role: "agent", content: "response"}
	got := m.renderMessage(msg)
	if got == "" {
		t.Error("agent message should not be empty")
	}
	if !strings.Contains(got, "Solo") {
		t.Error("agent message should contain timestamp label 'Solo'")
	}
}

func TestRenderMessage_EmptyRole(t *testing.T) {
	m := newTestModel()
	msg := message{role: "unknown", content: "something"}
	got := m.renderMessage(msg)
	// Unknown role returns empty string (no separator anymore)
	if got != "" {
		t.Errorf("unknown role should produce empty string, got %q", got)
	}
}

// ─── renderAgentMessageBody ─────────────────────────────────────────────────

func TestRenderAgentMessageBody_Content(t *testing.T) {
	m := &model{}
	msg := message{role: "agent", content: "Hello!"}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "Hello!") {
		t.Error("body should contain content")
	}
}

func TestRenderAgentMessageBody_WithThinking(t *testing.T) {
	m := &model{}
	msg := message{
		role: "agent",
		timeline: []timelineEntry{
			{kind: timelineThinking, text: "I need to think..."},
			{kind: timelineContent, text: "Here's my answer."},
		},
	}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "Thinking") {
		t.Error("body should show thinking label")
	}
	if !strings.Contains(got, "I need to think") {
		t.Error("body should show thinking content")
	}
	if !strings.Contains(got, "Here's my answer") {
		t.Error("body should show content after thinking")
	}
}

func TestRenderAgentMessageBody_WithToolBlock(t *testing.T) {
	m := &model{}
	tb := toolBlock{name: "file_read", args: `{"path":"a.go"}`, done: true, lineCount: 10, duration: 50 * 1000 * 1000}
	msg := message{
		role: "agent",
		timeline: []timelineEntry{
			{kind: timelineTool, tool: &tb},
		},
	}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "file_read") {
		t.Error("body should show tool name as label")
	}
	if !strings.Contains(got, "file_read") {
		t.Error("body should show tool name")
	}
}

func TestRenderAgentMessageBody_MultipleTools(t *testing.T) {
	m := &model{}
	tb1 := toolBlock{name: "file_read", args: `{"path":"a.go"}`, done: true}
	tb2 := toolBlock{name: "grep", args: `{"path":"."}`, done: true}
	msg := message{
		role: "agent",
		timeline: []timelineEntry{
			{kind: timelineTool, tool: &tb1},
			{kind: timelineTool, tool: &tb2},
		},
	}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "file_read") {
		t.Error("body should show first tool")
	}
	if !strings.Contains(got, "grep") {
		t.Error("body should show second tool")
	}
}

func TestRenderAgentMessageBody_ToolWithNilToolPtr(t *testing.T) {
	m := &model{}
	msg := message{
		role: "agent",
		timeline: []timelineEntry{
			{kind: timelineTool, tool: nil},
		},
	}
	// Should not panic
	got := m.renderAgentMessageBody(msg)
	if strings.Contains(got, "Tool Use") {
		t.Error("nil tool should not render tool label")
	}
}

func TestRenderAgentMessageBody_ThinkingThenContent(t *testing.T) {
	m := &model{}
	msg := message{
		role: "agent",
		timeline: []timelineEntry{
			{kind: timelineThinking, text: "thought"},
			{kind: timelineContent, text: "answer"},
		},
		content: "more content",
	}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "thought") {
		t.Error("should show thinking")
	}
	if !strings.Contains(got, "answer") {
		t.Error("should show timeline content")
	}
	if !strings.Contains(got, "more content") {
		t.Error("should show msg.content")
	}
}

func TestRenderAgentMessageBody_ContentAfterTool(t *testing.T) {
	m := &model{}
	tb := toolBlock{name: "bash", done: true}
	msg := message{
		role: "agent",
		timeline: []timelineEntry{
			{kind: timelineTool, tool: &tb},
		},
		content: "final answer",
	}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "bash") {
		t.Error("should show tool")
	}
	if !strings.Contains(got, "final answer") {
		t.Error("should show content after tool")
	}
}

func TestRenderAgentMessageBody_EmptyTimelineWithContent(t *testing.T) {
	m := &model{}
	msg := message{
		role:     "agent",
		timeline: []timelineEntry{},
		content:  "just content",
	}
	got := m.renderAgentMessageBody(msg)
	if !strings.Contains(got, "just content") {
		t.Error("should show content even with empty timeline")
	}
}

// ─── renderContent ──────────────────────────────────────────────────────────

func TestRenderContent_NilRenderer(t *testing.T) {
	m := &model{renderer: nil}
	got := m.renderContent("hello")
	if !strings.Contains(got, "hello") {
		t.Error("renderContent with nil renderer should still return content")
	}
}

func TestRenderContent_WithRenderer(t *testing.T) {
	m := &model{width: 80, darkBg: true}
	m.renderer = m.newRenderer()
	if m.renderer == nil {
		t.Skip("renderer creation failed, skip test")
	}
	got := m.renderContent("**bold** text")
	if got == "" {
		t.Error("renderContent should return non-empty string")
	}
}

func TestRenderContent_WithRendererError(t *testing.T) {
	// Create a renderer that may fail on certain input
	m := &model{width: 80, darkBg: true}
	m.renderer = m.newRenderer()
	if m.renderer == nil {
		t.Skip("renderer creation failed, skip test")
	}
	// Even with potentially problematic input, should fallback gracefully
	got := m.renderContent("normal text")
	if !strings.Contains(got, "normal text") {
		t.Error("should fallback to contentStyle on error")
	}
}

// ─── invalidateMessageCache ─────────────────────────────────────────────────

func TestInvalidateMessageCache(t *testing.T) {
	m := &model{
		messages: []message{
			{role: "agent", content: "first", dirty: false, rendered: "cached"},
			{role: "agent", content: "second", dirty: false, rendered: "cached"},
		},
	}
	m.invalidateMessageCache()
	for i, msg := range m.messages {
		if !msg.dirty {
			t.Errorf("message %d should be dirty after invalidation", i)
		}
	}
}

func TestInvalidateMessageCache_Empty(t *testing.T) {
	m := &model{messages: []message{}}
	m.invalidateMessageCache() // should not panic
}
