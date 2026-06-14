package simulation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

func TestReflectionEngine_ShouldReflect(t *testing.T) {
	re := NewReflectionEngine(nil, "model", "provider", 50)

	// Should reflect when >= intervalTicks
	if !re.ShouldReflect(50) {
		t.Error("should reflect at 50 (equal to interval)")
	}
	if !re.ShouldReflect(60) {
		t.Error("should reflect at 60 (above interval)")
	}
	// Should not reflect when < intervalTicks
	if re.ShouldReflect(0) {
		t.Error("should not reflect at 0")
	}
	if re.ShouldReflect(49) {
		t.Error("should not reflect at 49 (below interval)")
	}
}

func TestReflectionEngine_DefaultInterval(t *testing.T) {
	re := NewReflectionEngine(nil, "model", "provider", 0)
	if re.intervalTicks != 50 {
		t.Errorf("expected default interval 50, got %d", re.intervalTicks)
	}
}

func TestReflectionEngine_ReflectEmptyMemory(t *testing.T) {
	fakeLLM := &agent.FakeLLM{Responses: []string{"I've been thinking..."}}
	re := NewReflectionEngine(fakeLLM, "model", "provider", 50)

	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)

	mem := NewAgentMemory("alice")
	persona := &Persona{ID: "alice", Name: "Alice", Role: "Developer"}

	record, err := re.Reflect(context.Background(), persona, mem, clock)
	if err != nil {
		t.Fatalf("Reflect: %v", err)
	}
	if record != nil {
		t.Error("expected nil record for empty memory")
	}
}

func TestReflectionEngine_Reflect(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{
			"I notice patterns of collaboration emerging.\nMy relationship with Bob is improving.\nI should focus on my goals.",
		},
	}
	re := NewReflectionEngine(fakeLLM, "model", "provider", 50)

	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)

	mem := NewAgentMemory("alice")
	mem.Record(MemoryRecord{
		Round:      1,
		Role:       "observation",
		Content:    "Bob entered the cafe.",
		RecordType: "agent_enter",
		Importance: 5.0,
	})
	mem.Record(MemoryRecord{
		Round:      2,
		Role:       "assistant",
		Content:    "I had a conversation with Bob about our goals.",
		RecordType: "action",
		Importance: 7.0,
	})

	persona := &Persona{ID: "alice", Name: "Alice", Role: "Developer"}

	record, err := re.Reflect(context.Background(), persona, mem, clock)
	if err != nil {
		t.Fatalf("Reflect: %v", err)
	}
	if record == nil {
		t.Fatal("expected non-nil record")
	}

	if record.AgentID != "alice" {
		t.Errorf("expected agent alice, got %s", record.AgentID)
	}
	if record.Importance <= 0 {
		t.Errorf("expected positive importance, got %f", record.Importance)
	}
	if len(record.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(record.Sources))
	}
}

func TestReflectionEngine_ReflectLLMError(t *testing.T) {
	fakeLLM := &agent.FakeLLM{Responses: []string{}, Err: fmt.Errorf("LLM unavailable")}
	re := NewReflectionEngine(fakeLLM, "model", "provider", 50)

	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)

	mem := NewAgentMemory("alice")
	mem.Record(MemoryRecord{Round: 1, Role: "observation", Content: "test"})

	persona := &Persona{ID: "alice", Name: "Alice"}

	_, err := re.Reflect(context.Background(), persona, mem, clock)
	if err == nil {
		t.Error("expected error from LLM")
	}
}

func TestBuildReflectionPrompt(t *testing.T) {
	persona := &Persona{ID: "alice", Name: "Alice", Role: "Developer"}
	records := []MemoryRecord{
		{Round: 1, Role: "observation", Content: "Bob entered the room.", RecordType: "agent_enter"},
		{Round: 2, Role: "assistant", Content: "I greeted Bob.", RecordType: "action"},
	}

	prompt := buildReflectionPrompt(persona, records, time.Now())

	if !strings.Contains(prompt, "Alice") {
		t.Error("prompt should contain persona name")
	}
	if !strings.Contains(prompt, "patterns") {
		t.Error("prompt should ask about patterns")
	}
	if !strings.Contains(prompt, "Bob entered") {
		t.Error("prompt should contain memory content")
	}
}

func TestComputeReflectionImportance(t *testing.T) {
	// Empty records
	if computeReflectionImportance(nil) != 5.0 {
		t.Error("expected 5.0 for nil records")
	}
	if computeReflectionImportance([]MemoryRecord{}) != 5.0 {
		t.Error("expected 5.0 for empty records")
	}

	// Records with no importance (all zero)
	records := []MemoryRecord{
		{Importance: 0},
		{Importance: 0},
	}
	if computeReflectionImportance(records) != 5.0 {
		t.Error("expected 5.0 (default) for zero-importance records")
	}

	// Normal case
	records = []MemoryRecord{
		{Importance: 5.0},
		{Importance: 7.0},
		{Importance: 6.0},
	}
	// avg = 6.0, boosted = 7.2
	imp := computeReflectionImportance(records)
	if imp < 7.0 || imp > 7.5 {
		t.Errorf("expected ~7.2, got %f", imp)
	}

	// Cap at 10.0
	records = []MemoryRecord{
		{Importance: 10.0},
		{Importance: 10.0},
	}
	imp = computeReflectionImportance(records)
	if imp > 10.0 {
		t.Errorf("expected capped at 10.0, got %f", imp)
	}
}
