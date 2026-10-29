package skill

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// ─── SummarizeSkill ─────────────────────────────────────────────────────────

func TestSummarizeSkill_DescriptionOnly(t *testing.T) {
	for _, tt := range []struct {
		name        string
		description string
	}{
		{"single-line", "Extract text from PDF files."},
		{"multi-line", "Testing strategy.\n\nUse for integration checks.\nDo not use for translation."},
		{"empty", ""},
		{"whitespace", " \n\t "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Skill{
				ID:          "x",
				Description: tt.description,
				WhenToUse:   "Legacy condition must not be listed.",
				Triggers:    []string{"legacy-trigger", "OCR"},
			}
			if got, want := SummarizeSkill(s), strings.TrimSpace(tt.description); got != want {
				t.Errorf("description = %q, want %q", got, want)
			}
		})
	}
}

func TestSummarizeSkill_DescriptionCharacterLimit(t *testing.T) {
	for _, char := range []string{"a", testfixtures.Chinese("skill_listing_test_001"), "🛠"} {
		for _, size := range []int{1535, 1536, 1537} {
			t.Run(fmt.Sprintf("%s-%d", char, size), func(t *testing.T) {
				description := strings.Repeat(char, size)
				got := SummarizeSkill(&Skill{ID: "x", Description: description})
				want := description
				if size > 1536 {
					want = strings.Repeat(char, 1535) + "…"
				}
				if got != want {
					t.Errorf("description length = %d, want %d; content matches: %v", utf8.RuneCountInString(got), utf8.RuneCountInString(want), got == want)
				}
				if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 1536 {
					t.Errorf("description must be valid UTF-8 and at most 1536 characters")
				}
			})
		}
	}
}

func TestSummarizeSkill_NilSkill(t *testing.T) {
	if got := SummarizeSkill(nil); got != "" {
		t.Errorf("nil skill should produce empty summary: got %q", got)
	}
}

func TestBuildSkillListing_MultilineDescriptionBudget(t *testing.T) {
	description := "Testing strategy.\nUse for integration checks."
	skills := []*Skill{{ID: "qa", Description: description}}
	want := "- qa: " + description
	budget := utf8.RuneCountInString(want) + 1 // Include the listing's newline.
	if got := BuildSkillListing(skills, budget, nil); got != want {
		t.Errorf("description should fit exactly: got %q, want %q", got, want)
	}
	if got := BuildSkillListing(skills, budget-1, nil); got != "- qa" {
		t.Errorf("budget must count all description lines: got %q", got)
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
			Description: strings.Repeat(testfixtures.Chinese("skill_listing_test_002"), 20), // 80 runes each
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
