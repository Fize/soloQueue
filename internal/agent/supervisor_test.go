package agent

import (
	"context"
	"testing"
	"time"
)

func TestSupervisor_New(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	if sv == nil {
		t.Fatal("NewSupervisor returned nil")
	}
	if sv.ChildCount() != 0 {
		t.Errorf("initial child count = %d, want 0", sv.ChildCount())
	}
	if len(sv.Children()) != 0 {
		t.Error("initial children should be empty")
	}
}

func TestSupervisor_SpawnChild_NilFactory(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	_, err := sv.SpawnChild(context.Background(), AgentTemplate{ID: "child1"})
	if err == nil {
		t.Error("expected error when factory is nil")
	}
}

func TestSupervisor_ReapChild_NotFound(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	err := sv.ReapChild("nonexistent", time.Second)
	if err == nil {
		t.Error("expected error when child not found")
	}
}

func TestSupervisor_ReapAll_Empty(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	errs := sv.ReapAll(time.Second)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d", len(errs))
	}
}

func TestSupervisor_SpawnFnFor(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	tmpl := AgentTemplate{ID: "test-child", Name: "Test Child"}
	spawnFn := sv.SpawnFnFor(tmpl)

	if spawnFn == nil {
		t.Fatal("SpawnFnFor returned nil")
	}

	// SpawnFn with nil factory should return error
	_, err := spawnFn(context.Background(), "test task")
	if err == nil {
		t.Error("expected error when factory is nil")
	}
}

func TestSupervisor_SpawnFnForID_NotFound(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	templates := []AgentTemplate{
		{ID: "existing", Name: "Existing"},
	}

	// Existing template
	spawnFn := sv.SpawnFnForID("existing", templates)
	if spawnFn == nil {
		t.Fatal("SpawnFnForID returned nil for existing template")
	}

	// Non-existing template
	spawnFnNotFound := sv.SpawnFnForID("missing", templates)
	if spawnFnNotFound == nil {
		t.Fatal("SpawnFnForID returned nil for missing template")
	}

	_, err := spawnFnNotFound(context.Background(), "task")
	if err == nil {
		t.Error("expected error for missing template")
	}
}

func TestSupervisor_Agent(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2-agent", Name: "DevLead"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	got := sv.Agent()
	if got != a {
		t.Errorf("Agent() returned different pointer: got %p, want %p", got, a)
	}
	if got.Def.ID != "l2-agent" {
		t.Errorf("Agent().Def.ID = %q, want %q", got.Def.ID, "l2-agent")
	}
}
