package skill

import (
	"fmt"
	"strings"
	"testing"
)

func TestSkillToolDescription_NoSkills(t *testing.T) {
	st := NewSkillTool(NewSkillRegistry(), nil)
	got := st.Description()
	if !strings.Contains(got, "No skills are currently available") {
		t.Errorf("no-skills fallback lost: got %q", got)
	}
}

func TestSkillToolDescription_KeepsDirectiveAndHeader(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "x", Description: "d"})
	st := NewSkillTool(reg, nil)
	got := st.Description()
	for _, phrase := range []string{
		"protocol violation",
		"highest-priority",
		"Available skills:",
	} {
		if !strings.Contains(got, phrase) {
			t.Errorf("directive should contain %q: got %q", phrase, got)
		}
	}
}

func TestSkillToolDescription_ListsSkillsOneLineEach(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "alpha", Description: "Alpha description for testing."})
	_ = reg.Register(&Skill{ID: "beta", Description: "Beta description for testing."})
	st := NewSkillTool(reg, nil)
	got := st.Description()
	for _, want := range []string{
		"- alpha: Alpha description for testing.",
		"- beta: Beta description for testing.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestSkillToolDescription_SkipsDisableModelInvocation(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "visible", Description: "d"})
	_ = reg.Register(&Skill{ID: "hidden", Description: "h", DisableModelInvocation: true})
	st := NewSkillTool(reg, nil)
	got := st.Description()
	if strings.Contains(got, "hidden") {
		t.Errorf("disable-model-invocation skill should not be listed: %q", got)
	}
	if !strings.Contains(got, "visible") {
		t.Errorf("normal skill should be listed: %q", got)
	}
}

func TestSkillToolDescription_AllDisabled(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "hidden", Description: "h", DisableModelInvocation: true})
	st := NewSkillTool(reg, nil)
	got := st.Description()
	if !strings.Contains(got, "No skills are currently available") {
		t.Errorf("all-disabled should hit no-skills fallback: got %q", got)
	}
}

func TestSkillToolDescription_ForkSkillsMarked(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "inline-skill", Description: "inline"})
	_ = reg.Register(&Skill{ID: "fork-skill", Description: "fork", Context: "fork"})
	st := NewSkillTool(reg, nil)
	got := st.Description()
	if !strings.Contains(got, "- fork-skill: fork [fork]") {
		t.Errorf("fork skills should be marked: %q", got)
	}
	if strings.Contains(got, "inline-skill: inline [fork]") {
		t.Errorf("inline skill wrongly marked: %q", got)
	}
}

func TestSkillToolDescription_TriggerFirstSummary(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{
		ID:          "pdf",
		Description: "Extracts text and tables from PDF files.",
		Triggers:    []string{"pdf", "OCR"},
	})
	st := NewSkillTool(reg, nil)
	got := st.Description()
	if !strings.Contains(got, "- pdf: 当任务提到 pdf、OCR 时使用 — Extracts text") {
		t.Errorf("trigger-first summary missing: %q", got)
	}
}

func TestSkillToolDescription_BudgetCapsListing(t *testing.T) {
	reg := NewSkillRegistry()
	for i := 0; i < 50; i++ {
		_ = reg.Register(&Skill{
			ID:          fmt.Sprintf("skill-%02d", i),
			Description: strings.Repeat("很长的描述内容", 30),
		})
	}
	st := NewSkillTool(reg, nil)
	got := st.Description()

	// Every ID must survive the budget.
	for i := 0; i < 50; i++ {
		id := fmt.Sprintf("skill-%02d", i)
		if !strings.Contains(got, id) {
			t.Fatalf("ID %s dropped by budget", id)
		}
	}
	// And at least one line must degrade to ID-only.
	hasIDOnly := false
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- ") && !strings.Contains(line, ": ") {
			hasIDOnly = true
			break
		}
	}
	if !hasIDOnly {
		t.Errorf("budget should degrade tail skills to ID-only: %q", got)
	}
}

// ─── markForkSkills ─────────────────────────────────────────────────────────

func TestMarkForkSkills(t *testing.T) {
	skills := map[string]*Skill{
		"fork-skill":   {ID: "fork-skill", Context: "fork"},
		"inline-skill": {ID: "inline-skill"},
	}
	listing := "- fork-skill: fork desc\n- inline-skill: inline desc"
	got := markForkSkills(listing, skills)
	if !strings.Contains(got, "- fork-skill: fork desc [fork]") {
		t.Errorf("fork marker missing: %q", got)
	}
	if strings.Contains(got, "inline-skill: inline desc [fork]") {
		t.Errorf("inline skill wrongly marked: %q", got)
	}
}

func TestMarkForkSkills_IDOnlyLine(t *testing.T) {
	skills := map[string]*Skill{
		"fork-skill": {ID: "fork-skill", Context: "fork"},
	}
	got := markForkSkills("- fork-skill", skills)
	if got != "- fork-skill [fork]" {
		t.Errorf("ID-only fork line should be marked: %q", got)
	}
}
