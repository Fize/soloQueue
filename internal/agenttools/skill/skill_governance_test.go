package skill

import (
	"strings"
	"testing"
	"time"
)

func TestGovernanceReport_NeverInvokedSkills(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "used", Description: "d", Triggers: []string{"x"}})
	_ = reg.Register(&Skill{ID: "ignored", Description: "d", Triggers: []string{"y"}})
	_ = reg.Register(&Skill{ID: "hidden", Description: "d", DisableModelInvocation: true})

	stats := &mockStats{counts: map[string]int{"used": 3}}
	rep, err := BuildGovernanceReport(reg, stats, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	if len(rep.NeverInvoked) != 1 || rep.NeverInvoked[0] != "ignored" {
		t.Errorf("never-invoked should list only visible skills without calls: %v", rep.NeverInvoked)
	}
}

func TestGovernanceReport_NoStats(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "a", Description: "d"})

	rep, err := BuildGovernanceReport(reg, nil, time.Now())
	if err != nil {
		t.Fatalf("report without stats should not error: %v", err)
	}
	if len(rep.NeverInvoked) != 1 {
		t.Errorf("without stats, every visible skill counts as never invoked: %v", rep.NeverInvoked)
	}
}

func TestGovernanceReport_DescriptionQuality(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "good", Description: "Use when the user mentions spreadsheets.", Triggers: []string{"xlsx", "csv"}, WhenToUse: "Trigger when the user mentions: spreadsheet"})
	_ = reg.Register(&Skill{ID: "no-triggers", Description: "A description without any trigger information."})
	_ = reg.Register(&Skill{ID: "no-desc", Description: ""})
	_ = reg.Register(&Skill{ID: "long-wtu", Description: "d", WhenToUse: strings.Repeat("word ", 400)}) // >1536 chars

	rep, err := BuildGovernanceReport(reg, nil, time.Now())
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	if len(rep.QualityWarnings) != 3 {
		t.Fatalf("expected 3 quality warnings, got %d: %+v", len(rep.QualityWarnings), rep.QualityWarnings)
	}
	bySkill := map[string]string{}
	for _, w := range rep.QualityWarnings {
		bySkill[w.SkillID] = w.Issue
	}
	if bySkill["no-triggers"] == "" {
		t.Errorf("no-triggers should be flagged: %+v", rep.QualityWarnings)
	}
	if bySkill["no-desc"] == "" {
		t.Errorf("no-desc should be flagged: %+v", rep.QualityWarnings)
	}
	if bySkill["long-wtu"] == "" {
		t.Errorf("oversized when_to_use should be flagged: %+v", rep.QualityWarnings)
	}
	if _, ok := bySkill["good"]; ok {
		t.Errorf("well-formed skill should not be flagged: %+v", rep.QualityWarnings)
	}
}
