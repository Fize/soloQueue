package agent

import (
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

func newDedupCW() *ctxwin.ContextWindow {
	return ctxwin.NewContextWindow(100000, 10000, 0, ctxwin.NewTokenizer())
}

func TestDedupeSkillResult_AlreadyLoaded(t *testing.T) {
	cw := newDedupCW()
	cw.Push(ctxwin.RoleTool, "rendered skill content",
		ctxwin.WithToolName("Skill"), ctxwin.WithToolCallID("call-1"))

	tc := llm.ToolCall{ID: "call-2", Function: llm.FunctionCall{Name: "Skill", Arguments: `{"skill":"pdf","args":"x"}`}}
	got := dedupeSkillResult(cw, tc, "rendered skill content")
	if !strings.Contains(got, "already loaded") {
		t.Errorf("identical content should yield already-loaded note: %q", got)
	}
}

func TestDedupeSkillResult_DifferentContentPassesThrough(t *testing.T) {
	cw := newDedupCW()
	cw.Push(ctxwin.RoleTool, "rendered v1", ctxwin.WithToolName("Skill"), ctxwin.WithToolCallID("call-1"))

	tc := llm.ToolCall{Function: llm.FunctionCall{Name: "Skill"}}
	got := dedupeSkillResult(cw, tc, "rendered v2")
	if got != "rendered v2" {
		t.Errorf("different content should pass through: %q", got)
	}
}

func TestDedupeSkillResult_NonSkillToolNeverDedupes(t *testing.T) {
	cw := newDedupCW()
	cw.Push(ctxwin.RoleTool, "content", ctxwin.WithToolName("Read"), ctxwin.WithToolCallID("call-1"))

	tc := llm.ToolCall{Function: llm.FunctionCall{Name: "Read"}}
	got := dedupeSkillResult(cw, tc, "content")
	if got != "content" {
		t.Errorf("non-Skill tools should never dedupe: %q", got)
	}
}

func TestDedupeSkillResult_EmptyWindow(t *testing.T) {
	cw := newDedupCW()
	tc := llm.ToolCall{Function: llm.FunctionCall{Name: "Skill"}}
	got := dedupeSkillResult(cw, tc, "fresh content")
	if got != "fresh content" {
		t.Errorf("fresh content in empty window should pass through: %q", got)
	}
}
