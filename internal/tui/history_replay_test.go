package tui

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func TestLoadMessagesFromHistory_Empty(t *testing.T) {
	msgs := loadMessagesFromHistory(nil, false)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestLoadMessagesFromHistory_UserOnly(t *testing.T) {
	history := []agent.LLMMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "hello"},
	}
	msgs := loadMessagesFromHistory(history, false)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].role != "user" || msgs[0].content != "hello" {
		t.Errorf("unexpected msg: %+v", msgs[0])
	}
}

func TestLoadMessagesFromHistory_AssistantWithThinking(t *testing.T) {
	history := []agent.LLMMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer", ReasoningContent: "let me think..."},
	}
	msgs := loadMessagesFromHistory(history, false)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	agMsg := msgs[1]
	if agMsg.role != "agent" {
		t.Errorf("expected agent, got %s", agMsg.role)
	}
	if agMsg.content != "" {
		t.Errorf("content should be empty (in timeline), got %q", agMsg.content)
	}
	if len(agMsg.timeline) != 2 {
		t.Fatalf("expected 2 timeline entries (thinking + content), got %d", len(agMsg.timeline))
	}
	if agMsg.timeline[0].kind != timelineThinking || agMsg.timeline[0].text != "let me think..." {
		t.Errorf("unexpected thinking entry: %+v", agMsg.timeline[0])
	}
	if agMsg.timeline[1].kind != timelineContent || agMsg.timeline[1].text != "answer" {
		t.Errorf("unexpected content entry: %+v", agMsg.timeline[1])
	}
}

func TestLoadMessagesFromHistory_WithToolCalls(t *testing.T) {
	history := []agent.LLMMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "read file"},
		{
			Role:             "assistant",
			ReasoningContent: "need to read",
			ToolCalls: []llm.ToolCall{
				{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "file_read", Arguments: `{"path":"/tmp/x"}`}},
			},
		},
		{Role: "tool", ToolCallID: "call_1", Content: "file contents here"},
		{Role: "assistant", Content: "the file says..."},
	}
	msgs := loadMessagesFromHistory(history, false)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user + agent), got %d", len(msgs))
	}
	agMsg := msgs[1]
	if agMsg.role != "agent" {
		t.Errorf("expected agent, got %s", agMsg.role)
	}
	if agMsg.content != "" {
		t.Errorf("content should be empty (in timeline), got %q", agMsg.content)
	}
	if len(agMsg.timeline) < 3 {
		t.Fatalf("expected >=3 timeline entries, got %d", len(agMsg.timeline))
	}
	// First: thinking
	if agMsg.timeline[0].kind != timelineThinking {
		t.Errorf("entry 0 should be thinking, got kind=%d", agMsg.timeline[0].kind)
	}
	// Second: tool
	if agMsg.timeline[1].kind != timelineTool || agMsg.timeline[1].tool == nil {
		t.Fatalf("entry 1 should be tool, got kind=%d tool=%v", agMsg.timeline[1].kind, agMsg.timeline[1].tool)
	}
	if agMsg.timeline[1].tool.name != "file_read" {
		t.Errorf("tool name = %q", agMsg.timeline[1].tool.name)
	}
	if !agMsg.timeline[1].tool.done {
		t.Error("tool should be marked done")
	}
	if agMsg.timeline[1].tool.lineCount != 1 {
		t.Errorf("lineCount = %d, want 1", agMsg.timeline[1].tool.lineCount)
	}
	// Last: content
	lastIdx := len(agMsg.timeline) - 1
	if agMsg.timeline[lastIdx].kind != timelineContent {
		t.Errorf("last entry should be content, got kind=%d", agMsg.timeline[lastIdx].kind)
	}
}

func TestLoadMessagesFromHistory_MultiTurn(t *testing.T) {
	history := []agent.LLMMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2", ReasoningContent: "thinking2"},
	}
	msgs := loadMessagesFromHistory(history, false)
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (2 pairs), got %d", len(msgs))
	}
	if msgs[0].role != "user" || msgs[0].content != "q1" {
		t.Errorf("msg 0: %+v", msgs[0])
	}
	if msgs[1].role != "agent" || msgs[1].timeline[0].text != "a1" {
		t.Errorf("msg 1: %+v", msgs[1])
	}
	if msgs[2].role != "user" || msgs[2].content != "q2" {
		t.Errorf("msg 2: %+v", msgs[2])
	}
	if msgs[3].role != "agent" || msgs[3].content != "" {
		t.Errorf("msg 3 should have empty content (in timeline): %+v", msgs[3])
	}
}
