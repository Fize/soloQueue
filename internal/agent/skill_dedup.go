package agent

import (
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

// dedupeSkillResult collapses repeated Skill invocations whose rendered
// content is already in the conversation into a short "already loaded" note
// (aligned with Claude Code v2.1.202+). Identical content means same skill +
// same args; the full instructions are already in context, so re-injecting
// them would burn tokens. Different args render differently and pass through.
func dedupeSkillResult(cw *ctxwin.ContextWindow, tc llm.ToolCall, result string) string {
	if tc.Function.Name != "Skill" {
		return result
	}
	if cw.HasToolResult("Skill", result) {
		return "Skill already loaded in this conversation (identical content). Invoke with different args only if you need a different rendering."
	}
	return result
}
