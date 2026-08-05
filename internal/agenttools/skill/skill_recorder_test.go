package skill

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockStats struct {
	events []InvocationEvent
	counts map[string]int
	err    error
}

func (m *mockStats) Record(_ context.Context, e InvocationEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, e)
	return nil
}

func (m *mockStats) Counts(_ context.Context, _ time.Time) (map[string]int, error) {
	return m.counts, nil
}

func newRecorderTool(t *testing.T, reg *SkillRegistry, stats *mockStats) *SkillTool {
	t.Helper()
	st := NewSkillTool(reg, nil,
		WithInvocationStats(stats),
		WithAgentID("test-agent"),
	)
	return st
}

func TestSkillToolExecute_RecordsOK(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "docx", Description: "d", Instructions: "do $ARGUMENTS"})
	stats := &mockStats{}
	st := newRecorderTool(t, reg, stats)

	out, err := st.Execute(context.Background(), `{"skill":"docx","args":"build report"}`)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "do build report") {
		t.Errorf("unexpected output: %q", out)
	}
	if len(stats.events) != 1 {
		t.Fatalf("expected 1 recorded event, got %d", len(stats.events))
	}
	e := stats.events[0]
	if e.SkillID != "docx" || e.Result != InvocationOK || e.AgentID != "test-agent" || e.Args != "build report" {
		t.Errorf("unexpected event: %+v", e)
	}
	if e.Duration <= 0 {
		t.Errorf("duration should be positive: %v", e.Duration)
	}
}

func TestSkillToolExecute_RecordsNotFound(t *testing.T) {
	reg := NewSkillRegistry()
	stats := &mockStats{}
	st := newRecorderTool(t, reg, stats)

	out, _ := st.Execute(context.Background(), `{"skill":"ghost"}`)
	if !strings.Contains(out, "not found") {
		t.Errorf("unexpected output: %q", out)
	}
	if len(stats.events) != 1 || stats.events[0].Result != InvocationNotFound {
		t.Errorf("expected not_found event: %+v", stats.events)
	}
}

func TestSkillToolExecute_RecordsErrorOnBadArgs(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "docx", Description: "d", Instructions: "do it"})
	stats := &mockStats{}
	st := newRecorderTool(t, reg, stats)

	st.Execute(context.Background(), `not-json`)
	if len(stats.events) != 1 || stats.events[0].Result != InvocationError {
		t.Errorf("expected error event: %+v", stats.events)
	}
}

func TestSkillToolExecute_RecordFailureDoesNotBlock(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "docx", Description: "d", Instructions: "do it"})
	stats := &mockStats{err: errors.New("db down")}
	st := newRecorderTool(t, reg, stats)

	out, err := st.Execute(context.Background(), `{"skill":"docx"}`)
	if err != nil {
		t.Fatalf("execute should succeed despite record failure: %v", err)
	}
	if !strings.Contains(out, "do it") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestSkillToolDescription_OrdersByInvocationCounts(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "cold", Description: "cold desc"})
	_ = reg.Register(&Skill{ID: "hot", Description: "hot desc"})
	stats := &mockStats{counts: map[string]int{"hot": 42, "cold": 1}}
	st := newRecorderTool(t, reg, stats)

	got := st.Description()
	if strings.Index(got, "hot") > strings.Index(got, "cold") {
		t.Errorf("most-invoked skill should be listed first: %q", got)
	}
}

func TestSkillToolDescription_NoStatsFallsBackToIDOrder(t *testing.T) {
	reg := NewSkillRegistry()
	_ = reg.Register(&Skill{ID: "zulu", Description: "z"})
	_ = reg.Register(&Skill{ID: "alpha", Description: "a"})
	st := NewSkillTool(reg, nil) // no stats

	got := st.Description()
	if strings.Index(got, "alpha") > strings.Index(got, "zulu") {
		t.Errorf("without stats, ID order expected: %q", got)
	}
}
