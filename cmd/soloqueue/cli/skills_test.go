package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func TestSkillsReportCmd_PrintsGovernanceReport(t *testing.T) {
	workDir := t.TempDir()

	// One installed skill with clean metadata, one without triggers.
	writeSkill := func(id, desc string, withTriggers bool) {
		dir := filepath.Join(workDir, "skills", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		triggers := ""
		if withTriggers {
			triggers = "triggers:\n  - docx\n"
		}
		content := "---\nname: " + id + "\ndescription: " + desc + "\n" + triggers + "---\n\nInstructions for " + id + ".\n"
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill("used-skill", "Creates Word documents. Use when the user needs docx output.", true)
	writeSkill("ignored-skill", "A vague description.", false)

	// Seed one invocation for used-skill.
	dbPath := filepath.Join(workDir, "soloqueue.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	stats := skill.NewSQLiteInvocationStats(d)
	if err := stats.Record(t.Context(), skill.InvocationEvent{SkillID: "used-skill", Result: skill.InvocationOK}); err != nil {
		t.Fatalf("seed invocation: %v", err)
	}
	d.Close()

	cmd := skillsReportCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--work-dir", workDir, "--db", dbPath, "--days", "30"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "ignored-skill") {
		t.Errorf("never-invoked skill should appear in report: %q", got)
	}
	if strings.Contains(got, `"used-skill"`) && !strings.Contains(got, "used-skill") {
		t.Errorf("used skill should be listed as invoked (or absent from never-invoked): %q", got)
	}
	if !strings.Contains(got, "QualityWarnings") || !strings.Contains(got, "ignored-skill") {
		t.Errorf("quality warnings should be reported: %q", got)
	}
}

