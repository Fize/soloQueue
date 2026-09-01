package skill

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/iface"
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

// forkLocatableStub implements iface.Locatable with a canned stream result.
type forkLocatableStub struct{ content string }

func (s *forkLocatableStub) Ask(context.Context, string) (string, error) { return s.content, nil }
func (s *forkLocatableStub) AskStream(_ context.Context, _ string) (<-chan iface.AgentEvent, error) {
	ch := make(chan iface.AgentEvent, 1)
	ch <- forkContentEvent(s.content)
	close(ch)
	return ch, nil
}
func (s *forkLocatableStub) ErrorCount() int32 { return 0 }
func (s *forkLocatableStub) LastError() string { return "" }

// forkContentEvent is an AgentEvent carrying a content delta.
type forkContentEvent string

func (e forkContentEvent) ContentDelta() (string, bool) { return string(e), true }
func (e forkContentEvent) IsAgentEvent()                {}

func TestSkillToolExecute_ForkResultPrefixed(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "market", Context: "fork", Instructions: "query the quote"})
	st := NewSkillTool(reg, func(_ context.Context, s *Skill, content, args string) (iface.Locatable, func(), error) {
		return &forkLocatableStub{content: "AAPL quote: $250"}, func() {}, nil
	})

	got, err := st.Execute(context.Background(), `{"skill":"market","args":"AAPL"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.HasPrefix(got, ForkResultPrefix) {
		t.Errorf("fork result should carry %q prefix: got %q", ForkResultPrefix, got)
	}
	if !strings.Contains(got, "AAPL quote: $250") {
		t.Errorf("fork result body lost: got %q", got)
	}
}

func TestSkillToolExecute_InlineResultUnprefixed(t *testing.T) {
	// Inline results must NOT carry the fork marker — dedup (and
	// post-compaction restore) rely on the boundary: marked = repeatable
	// answer, unmarked = idempotent instructions.
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "pdf", Instructions: "extract the tables"})
	st := NewSkillTool(reg, nil) // forkSpawn nil → fork skills degrade to inline, not used here

	got, err := st.Execute(context.Background(), `{"skill":"pdf","args":"report.pdf"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.HasPrefix(got, ForkResultPrefix) {
		t.Errorf("inline result must not carry the fork marker: got %q", got)
	}
	if got != "extract the tables" {
		t.Errorf("inline result should be the rendered instructions: got %q", got)
	}
}

func TestSkillToolDescription_NoBudgetCap_AllSkillsSummarized(t *testing.T) {
	// The listing has no budget cap: every visible skill must get a full
	// summary line. ID-only lines carry no trigger signal and are effectively
	// invisible, so degradation is not a fallback we accept.
	reg := NewSkillRegistry()
	for i := 0; i < 50; i++ {
		_ = reg.Register(&Skill{
			ID:          fmt.Sprintf("skill-%02d", i),
			Description: strings.Repeat("很长的描述内容", 30),
		})
	}
	st := NewSkillTool(reg, nil)
	got := st.Description()

	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		if !strings.Contains(line, ": ") {
			t.Errorf("line should carry a full summary, got ID-only %q", line)
		}
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
