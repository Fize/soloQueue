package ctxwin

import (
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// skillMsg builds an assistant(tool_calls)+tool(result) pair for a Skill call.
func skillMsg(t *testing.T, callID, skillID, rendered string, ts time.Time) []Message {
	t.Helper()
	return []Message{
		{
			Role:      RoleAssistant,
			Content:   "",
			ToolCalls: []llm.ToolCall{{ID: callID, Function: llm.FunctionCall{Name: "Skill", Arguments: `{"skill":"` + skillID + `"}`}}},
			Timestamp: ts,
		},
		{
			Role:       RoleTool,
			Content:    rendered,
			Name:       "Skill",
			ToolCallID: callID,
			Timestamp:  ts,
		},
	}
}

func bigBudget(t *testing.T) *Tokenizer {
	t.Helper()
	return NewTokenizer()
}

func TestRestoreRecentSkillContent_KeepsLatestPerSkill(t *testing.T) {
	base := time.Now()
	tok := bigBudget(t)
	msgs := []Message{}
	msgs = append(msgs, skillMsg(t, "c1", "docx", "docx instructions v1", base)...)
	msgs = append(msgs, skillMsg(t, "c2", "pdf", "pdf instructions", base.Add(time.Minute))...)
	msgs = append(msgs, skillMsg(t, "c3", "docx", "docx instructions v2", base.Add(2*time.Minute))...)

	got := RestoreRecentSkillContent(msgs, tok, 100000, 100000)
	if !strings.Contains(got, "pdf instructions") {
		t.Errorf("pdf should be restored: %q", got)
	}
	if !strings.Contains(got, "docx instructions v2") {
		t.Errorf("latest docx rendering should be restored: %q", got)
	}
	if strings.Contains(got, "docx instructions v1") {
		t.Errorf("stale docx rendering must not be restored (only latest per skill): %q", got)
	}
	// Most recent first.
	if strings.Index(got, "docx instructions v2") > strings.Index(got, "pdf instructions") {
		t.Errorf("most recent skill should come first: %q", got)
	}
}

func TestRestoreRecentSkillContent_SkipsAlreadyLoadedNotes(t *testing.T) {
	base := time.Now()
	tok := bigBudget(t)
	msgs := append([]Message{},
		skillMsg(t, "c1", "docx", "Skill already loaded in this conversation (identical content). Invoke with different args only if you need a different rendering.", base)...,
	)
	got := RestoreRecentSkillContent(msgs, tok, 100000, 100000)
	if got != "" {
		t.Errorf("already-loaded notes should not be restored: %q", got)
	}
}

func TestRestoreRecentSkillContent_SkipsForkResults(t *testing.T) {
	base := time.Now()
	tok := bigBudget(t)
	// Fork results are answers, not instructions — never restored. The inline
	// skill rendering must still be restored.
	msgs := append([]Message{},
		skillMsg(t, "c1", "market", forkResultPrefix+"skill \"market\" executed in a sub-agent:\n\nAAPL quote: $250", base)...,
	)
	msgs = append(msgs, skillMsg(t, "c2", "pdf", "pdf instructions", base.Add(time.Minute))...)

	got := RestoreRecentSkillContent(msgs, tok, 100000, 100000)
	if strings.Contains(got, "AAPL quote") {
		t.Errorf("fork result must not be restored: %q", got)
	}
	if !strings.Contains(got, "pdf instructions") {
		t.Errorf("inline rendering should still be restored: %q", got)
	}
}

func TestRestoreRecentSkillContent_EmptyHistory(t *testing.T) {
	got := RestoreRecentSkillContent(nil, NewTokenizer(), 100000, 100000)
	if got != "" {
		t.Errorf("empty history should yield empty restore: %q", got)
	}
}

func TestRestoreRecentSkillContent_NonSkillToolsIgnored(t *testing.T) {
	base := time.Now()
	tok := bigBudget(t)
	msgs := []Message{
		{
			Role:      RoleAssistant,
			Content:   "",
			ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "Read", Arguments: `{"path":"a.go"}`}}},
			Timestamp: base,
		},
		{Role: RoleTool, Content: "file content", Name: "Read", ToolCallID: "c1", Timestamp: base},
	}
	got := RestoreRecentSkillContent(msgs, tok, 100000, 100000)
	if got != "" {
		t.Errorf("non-Skill tool output must not be restored: %q", got)
	}
}

func TestRestoreRecentSkillContent_TotalBudgetTruncatesToFit(t *testing.T) {
	base := time.Now()
	tok := bigBudget(t)
	aContent := strings.Repeat("A", 100)
	bContent := strings.Repeat("B", 100)
	msgs := []Message{}
	msgs = append(msgs, skillMsg(t, "c1", "a", aContent, base)...)
	msgs = append(msgs, skillMsg(t, "c2", "b", bContent, base.Add(time.Minute))...)

	// Total budget fits the most recent skill fully plus ~half of the older
	// one — the older entry is truncated into the remaining space, not dropped.
	budget := tok.Count(bContent) + tok.Count(aContent)/2
	got := RestoreRecentSkillContent(msgs, tok, 100000, budget)
	if !strings.Contains(got, strings.Repeat("B", 100)) {
		t.Errorf("most recent content should fit the budget: %q", got)
	}
	if !strings.Contains(got, strings.Repeat("A", 25)) {
		t.Errorf("older content should be truncated into remaining budget: %q", got)
	}
	if tok.Count(got) > budget+2 {
		t.Errorf("restored content exceeds total budget: %d > %d", tok.Count(got), budget)
	}
}

func TestRestoreRecentSkillContent_PerSkillTruncation(t *testing.T) {
	base := time.Now()
	tok := bigBudget(t)
	content := strings.Repeat("X", 200)
	msgs := append([]Message{},
		skillMsg(t, "c1", "docx", content, base)...,
	)

	// Per-skill budget set to half the content's token count → truncation.
	perSkill := tok.Count(content) / 2
	if perSkill < 1 {
		t.Fatalf("test content too short to truncate: %d tokens", perSkill)
	}
	got := RestoreRecentSkillContent(msgs, tok, perSkill, 100000)
	if tok.Count(got) > perSkill+2 {
		t.Errorf("per-skill content should be truncated to %d tokens: got %d (%q)", perSkill, tok.Count(got), got)
	}
}
