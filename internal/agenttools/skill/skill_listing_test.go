package skill

import (
	"fmt"
	"strings"
	"testing"
)

// ─── SummarizeSkill ─────────────────────────────────────────────────────────

func TestSummarizeSkill_TriggersPriority(t *testing.T) {
	s := &Skill{
		ID:          "pdf",
		Description: "Extracts text and tables from PDF files.",
		WhenToUse:   "Trigger when the user mentions: PDF file, extract text from PDF",
		Triggers:    []string{"pdf", "提取", "OCR"},
	}
	got := SummarizeSkill(s)
	if !strings.HasPrefix(got, "当任务提到 pdf、提取、OCR 时使用") {
		t.Errorf("triggers should lead the summary: got %q", got)
	}
	if !strings.Contains(got, "Extracts text and tables") {
		t.Errorf("description first line should follow triggers: got %q", got)
	}
}

func TestSummarizeSkill_TriggersCappedAtThree(t *testing.T) {
	s := &Skill{
		ID:          "x",
		Description: "desc",
		Triggers:    []string{"a", "b", "c", "d", "e"},
	}
	got := SummarizeSkill(s)
	// "desc" contains a stray "d", so assert on separator-joined pairs instead.
	for _, banned := range []string{"c、d", "d、e", "、d、"} {
		if strings.Contains(got, banned) {
			t.Errorf("triggers beyond the first 3 should be dropped: got %q", got)
		}
	}
	if !strings.Contains(got, "a、b、c") {
		t.Errorf("first 3 triggers should be joined with 、: got %q", got)
	}
}

func TestSummarizeSkill_WhenToUseFallback(t *testing.T) {
	s := &Skill{
		ID:          "docx",
		Description: "Create and edit Word documents.",
		WhenToUse:   "Use when the user mentions: Word doc, .docx, tracked changes",
	}
	got := SummarizeSkill(s)
	if !strings.Contains(got, "Use when the user mentions") {
		t.Errorf("when_to_use should be used when triggers absent: got %q", got)
	}
}

func TestSummarizeSkill_DescriptionOnly(t *testing.T) {
	s := &Skill{ID: "x", Description: "A generic description without triggers."}
	if got := SummarizeSkill(s); got != "A generic description without triggers." {
		t.Errorf("got %q", got)
	}
}

func TestSummarizeSkill_TruncatesToMaxRunes(t *testing.T) {
	long := strings.Repeat("很长的描述内容", 30) // 180 runes
	s := &Skill{ID: "x", Description: long}
	got := SummarizeSkill(s)
	if n := len([]rune(got)); n > maxSummaryRunes+1 { // +1 for ellipsis
		t.Errorf("summary exceeds %d runes: %d", maxSummaryRunes, n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated summary should end with …: got %q", got)
	}
}

func TestSummarizeSkill_RuneSafeTruncation(t *testing.T) {
	// CJK chars are 3 bytes in UTF-8; byte truncation would corrupt them.
	long := strings.Repeat("字", 100) // 100 runes, 300 bytes
	s := &Skill{ID: "x", Description: long}
	got := SummarizeSkill(s)
	if !strings.HasPrefix(got, "字字字") {
		t.Errorf("rune-safe truncation broken: got %q", got)
	}
}

func TestSummarizeSkill_EmptySkill(t *testing.T) {
	s := &Skill{ID: "x"}
	if got := SummarizeSkill(s); got != "" {
		t.Errorf("empty skill should produce empty summary: got %q", got)
	}
}

// ─── BuildSkillListing ───────────────────────────────────────────────────────

func TestBuildSkillListing_AllWithinBudget(t *testing.T) {
	skills := []*Skill{
		{ID: "b-skill", Description: "BBB desc"},
		{ID: "a-skill", Description: "AAA desc"},
	}
	got := BuildSkillListing(skills, 10000, nil)
	for _, want := range []string{"- a-skill: AAA desc", "- b-skill: BBB desc"} {
		if !strings.Contains(got, want) {
			t.Errorf("listing should contain %q: got %q", want, got)
		}
	}
}

func TestBuildSkillListing_SortedByInvocationCountDesc(t *testing.T) {
	skills := []*Skill{
		{ID: "cold", Description: "cold"},
		{ID: "hot", Description: "hot"},
	}
	counts := map[string]int{"hot": 10, "cold": 0}
	got := BuildSkillListing(skills, 10000, counts)
	if strings.Index(got, "hot") > strings.Index(got, "cold") {
		t.Errorf("most-invoked skill should come first: got %q", got)
	}
}

func TestBuildSkillListing_TieBreaksByIDAsc(t *testing.T) {
	skills := []*Skill{
		{ID: "zulu", Description: "z"},
		{ID: "alpha", Description: "a"},
	}
	got := BuildSkillListing(skills, 10000, nil)
	if strings.Index(got, "alpha") > strings.Index(got, "zulu") {
		t.Errorf("same count should sort by ID asc: got %q", got)
	}
}

func TestBuildSkillListing_BudgetOverflowKeepsAllIDs(t *testing.T) {
	var skills []*Skill
	for i := 0; i < 50; i++ {
		skills = append(skills, &Skill{
			ID:          fmt.Sprintf("skill-%02d", i),
			Description: strings.Repeat("描述内容", 20), // 80 runes each
		})
	}
	got := BuildSkillListing(skills, 300, nil)

	// Every ID must remain, even when descriptions are dropped.
	for _, s := range skills {
		if !strings.Contains(got, s.ID) {
			t.Fatalf("ID %s dropped when budget overflowed", s.ID)
		}
	}
	// Some lines must degrade to ID-only (line starts with "- " and has no ": ").
	hasIDOnly := false
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- ") && !strings.Contains(line, ": ") {
			hasIDOnly = true
			break
		}
	}
	if !hasIDOnly {
		t.Errorf("budget overflow should produce ID-only lines: got %q", got)
	}
	// Full lines (with descriptions) must respect the budget.
	fullLen := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, ": ") {
			fullLen += len([]rune(line)) + 1
		}
	}
	if fullLen > 300 {
		t.Errorf("full lines exceed budget: %d > 300", fullLen)
	}
}

func TestBuildSkillListing_TinyBudgetMeansIDOnly(t *testing.T) {
	// budget=1 cannot fit any full line → everything degrades to ID-only.
	skills := []*Skill{{ID: "only-id", Description: "desc"}}
	got := BuildSkillListing(skills, 1, nil)
	if got != "- only-id" {
		t.Errorf("tiny budget should yield ID-only line: got %q", got)
	}
}

func TestBuildSkillListing_EmptySkills(t *testing.T) {
	if got := BuildSkillListing(nil, 10000, nil); got != "" {
		t.Errorf("empty skill list should yield empty listing: got %q", got)
	}
}
