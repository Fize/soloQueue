package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func newStartedTestAgent(t *testing.T, def Definition, llm LLMClient) *Agent {
	t.Helper()
	a := NewAgent(def, llm, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	waitForState(t, a, time.Second, StateIdle)
	return a
}

// ─── RegistryInspectQuery ───────────────────────────────────────────────

func TestRegistryInspectQuery_InspectByID(t *testing.T) {
	reg := NewRegistry(nil)
	a := newStartedTestAgent(t, Definition{ID: "dev", Name: "Dev"}, &FakeLLM{})
	_ = reg.Register(a)

	fn := RegistryInspectQuery(reg)
	output, err := fn(context.Background(), a.InstanceID, "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if output.QueryType != "single" {
		t.Errorf("QueryType = %q, want single", output.QueryType)
	}
	if output.Single == nil {
		t.Fatal("Single should not be nil")
	}
	if output.Single.InstanceID != a.InstanceID {
		t.Errorf("InstanceID = %q, want %q", output.Single.InstanceID, a.InstanceID)
	}
	if output.Single.TemplateID != "dev" {
		t.Errorf("TemplateID = %q, want dev", output.Single.TemplateID)
	}
	if output.Summary.Total != 1 {
		t.Errorf("Summary.Total = %d, want 1", output.Summary.Total)
	}
}

func TestRegistryInspectQuery_InspectByID_NotFound(t *testing.T) {
	reg := NewRegistry(nil)
	fn := RegistryInspectQuery(reg)
	_, err := fn(context.Background(), "nonexistent", "")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestRegistryInspectQuery_InspectAll(t *testing.T) {
	reg := NewRegistry(nil)
	a1 := newStartedTestAgent(t, Definition{ID: "dev", Name: "Dev"}, &FakeLLM{})
	a2 := newStartedTestAgent(t, Definition{ID: "ops", Name: "Ops"}, &FakeLLM{})
	_ = reg.Register(a1)
	_ = reg.Register(a2)

	fn := RegistryInspectQuery(reg)
	output, err := fn(context.Background(), "", "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if output.QueryType != "all" {
		t.Errorf("QueryType = %q, want all", output.QueryType)
	}
	if len(output.Teams) != 2 {
		t.Errorf("Teams count = %d, want 2", len(output.Teams))
	}
	if output.Summary.Total != 2 {
		t.Errorf("Summary.Total = %d, want 2", output.Summary.Total)
	}
}

func TestRegistryInspectQuery_InspectByTemplate_Exact(t *testing.T) {
	reg := NewRegistry(nil)
	a := newStartedTestAgent(t, Definition{ID: "dev", Name: "Dev"}, &FakeLLM{})
	_ = reg.Register(a)

	fn := RegistryInspectQuery(reg)
	output, err := fn(context.Background(), "", "dev")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if output.QueryType != "template" {
		t.Errorf("QueryType = %q, want template", output.QueryType)
	}
	if len(output.Teams) != 1 {
		t.Errorf("Teams count = %d, want 1", len(output.Teams))
	}
}

func TestRegistryInspectQuery_InspectByTemplate_Fuzzy(t *testing.T) {
	reg := NewRegistry(nil)
	a := newStartedTestAgent(t, Definition{ID: "dev-leader", Name: "Development Leader"}, &FakeLLM{})
	_ = reg.Register(a)

	fn := RegistryInspectQuery(reg)
	output, err := fn(context.Background(), "", "dev")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(output.Teams) != 1 {
		t.Errorf("Teams count = %d, want 1 (fuzzy match)", len(output.Teams))
	}
}

func TestRegistryInspectQuery_Empty(t *testing.T) {
	reg := NewRegistry(nil)
	fn := RegistryInspectQuery(reg)
	output, err := fn(context.Background(), "", "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if output.QueryType != "all" {
		t.Errorf("QueryType = %q, want all", output.QueryType)
	}
	if output.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0", output.Summary.Total)
	}
}

func TestRegistryInspectQuery_RunningCount(t *testing.T) {
	reg := NewRegistry(nil)
	blockedTool := newBlockingTool()
	defer close(blockedTool.ch)

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "b1",
			Function: llm.FunctionCall{Name: "block", Arguments: `{}`},
		}}},
		Responses: []string{"done"},
	}
	a := NewAgent(Definition{ID: "runner", Name: "Runner", ModelID: "m"}, fake, nil, WithTools(blockedTool))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	_ = reg.Register(a)

	// Start a task to put agent in processing state.
	go a.Ask(context.Background(), "run")
	waitForState(t, a, time.Second, StateProcessing)

	fn := RegistryInspectQuery(reg)
	output, err := fn(context.Background(), "", "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if output.Summary.Running != 1 {
		t.Errorf("Summary.Running = %d, want 1", output.Summary.Running)
	}
}

// ─── SupervisorInspectQuery ─────────────────────────────────────────────

func TestSupervisorInspectQuery_NoChildren(t *testing.T) {
	sv := &Supervisor{children: make(map[string][]*childSlot)}
	fn := SupervisorInspectQuery(sv)
	output, err := fn(context.Background(), "", "")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if output.Summary.Total != 0 {
		t.Errorf("Summary.Total = %d, want 0", output.Summary.Total)
	}
}

// ─── Conversion helpers ─────────────────────────────────────────────────

func TestAgentToStatus(t *testing.T) {
	a := newStartedTestAgent(t, Definition{ID: "dev", Name: "Developer"}, &FakeLLM{})
	a.RecordError(errors.New("test error"))

	s := agentToStatus(a)
	if s.InstanceID != a.InstanceID {
		t.Errorf("InstanceID = %q", s.InstanceID)
	}
	if s.TemplateID != "dev" {
		t.Errorf("TemplateID = %q, want dev", s.TemplateID)
	}
	if s.TemplateName != "Developer" {
		t.Errorf("TemplateName = %q, want Developer", s.TemplateName)
	}
	if s.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", s.ErrorCount)
	}
}

func TestMatchAgentTemplate(t *testing.T) {
	a := NewAgent(Definition{ID: "dev-leader", Name: "Development Leader"}, &FakeLLM{}, nil)
	if !matchAgentTemplate(a, "dev") {
		t.Error("matchAgentTemplate should match fuzzy 'dev'")
	}
	if !matchAgentTemplate(a, "dev-leader") {
		t.Error("matchAgentTemplate should match exact ID")
	}
	if !matchAgentTemplate(a, "development") {
		t.Error("matchAgentTemplate should match fuzzy name")
	}
	if matchAgentTemplate(a, "ops") {
		t.Error("matchAgentTemplate should not match 'ops'")
	}
}
