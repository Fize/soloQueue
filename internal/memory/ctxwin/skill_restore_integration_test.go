package ctxwin

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// TestAsyncCompactRestoresSkillContent verifies the end-to-end compaction path:
// after compaction replaces history with a summary, the most recent rendering
// of each invoked skill is re-attached to the summary message (Claude Code-style
// skill re-attach).
//
// asyncCompact is invoked synchronously after all messages are pushed so the
// snapshot is deterministic (relying on the auto-trigger goroutine would race
// the tool-result push when summaryTokens is tiny).
func TestAsyncCompactRestoresSkillContent(t *testing.T) {
	mc := &mockCompactor{
		compactFn: func(ctx context.Context, msgs []Message) (string, error) {
			return "Brief summary.", nil
		},
	}
	// Large summaryTokens: Push alone must NOT auto-trigger compaction.
	cw := NewContextWindow(100000, 2000, 50000, NewTokenizer(), WithCompactor(mc))

	cw.Push(RoleSystem, "System prompt for testing.")
	cw.Push(RoleUser, "Do the docx task.")

	// Skill invocation pair: assistant(tool_calls) + tool(result).
	callID := "call-1"
	cw.Push(RoleAssistant, "",
		WithToolCalls([]llm.ToolCall{{
			ID:       callID,
			Function: llm.FunctionCall{Name: "Skill", Arguments: `{"skill":"docx","args":"build report"}`},
		}}),
	)
	cw.Push(RoleTool, "Docx skill instructions: create heading structure.",
		WithToolName("Skill"), WithToolCallID(callID))

	cw.Push(RoleAssistant, "Finished the docx task.")
	cw.asyncCompact() // synchronous, deterministic snapshot

	if cw.Len() < 2 {
		t.Fatalf("expected summary message after compact, got %d messages", cw.Len())
	}
	summary, _ := cw.MessageAt(1)
	if summary.Role != RoleSystem {
		t.Fatalf("second message should be the summary, got role %q", summary.Role)
	}
	if !strings.Contains(summary.Content, "Docx skill instructions: create heading structure") {
		t.Errorf("skill instructions should be re-attached to the summary: %q", summary.Content)
	}
	if !strings.Contains(summary.Content, "# Active Skill Instructions") {
		t.Errorf("summary should carry the active-skill section header: %q", summary.Content)
	}
}
