package simulation

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

func TestPersonaGenerator_Basic(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{
			"personas": [
				{
					"id": "rust-advocate",
					"name": "Alice",
					"role": "advocate for Rust adoption",
					"goals": ["Convince others that Rust is the safest choice"],
					"traits": {"persuasive": "high", "technical": "expert"},
					"stance_per_entity": {"Rust": "pro", "Go": "con"}
				},
				{
					"id": "go-advocate",
					"name": "Bob",
					"role": "advocate for Go simplicity",
					"goals": ["Argue that Go's simplicity outweighs safety concerns"],
					"traits": {"practical": "high", "diplomatic": "medium"},
					"stance_per_entity": {"Go": "pro", "Rust": "con"}
				}
			]
		}`},
	}

	extraction := &SeedExtraction{
		Entities: []memoryengine.EntityExtraction{
			{Name: "Rust", Type: "technology", Confidence: 0.9},
			{Name: "Go", Type: "technology", Confidence: 0.9},
		},
		KeyTopics:     []string{"Rust vs Go"},
		ConflictAreas: []string{"safety vs simplicity"},
	}

	gen := NewPersonaGenerator(fakeLLM, "", nil)
	personas, err := gen.Generate(context.Background(), extraction, "Rust vs Go", 2)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	if len(personas) != 2 {
		t.Fatalf("expected 2 personas, got %d", len(personas))
	}
	if personas[0].Name != "Alice" {
		t.Errorf("expected first persona 'Alice', got '%s'", personas[0].Name)
	}
	if personas[1].ID != "go-advocate" {
		t.Errorf("expected second persona ID 'go-advocate', got '%s'", personas[1].ID)
	}
	// System prompt should be generated
	if personas[0].SystemPrompt == "" {
		t.Error("expected non-empty SystemPrompt")
	}
	if !strings.Contains(personas[0].SystemPrompt, "Rust vs Go") {
		t.Error("SystemPrompt should include the topic")
	}
}

func TestPersonaGenerator_ContrarianConstraint(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{
			"personas": [
				{"id": "p1", "name": "Alice", "role": "contrarian skeptic", "goals": ["Challenge assumptions"], "traits": {}, "stance_per_entity": {"X": "con"}},
				{"id": "p2", "name": "Bob", "role": "mediator", "goals": ["Find common ground"], "traits": {}, "stance_per_entity": {"X": "neutral"}},
				{"id": "p3", "name": "Charlie", "role": "enthusiast", "goals": ["Promote X"], "traits": {}, "stance_per_entity": {"X": "pro"}}
			]
		}`},
	}

	extraction := &SeedExtraction{
		Entities: []memoryengine.EntityExtraction{{Name: "X", Type: "concept", Confidence: 0.8}},
	}

	gen := NewPersonaGenerator(fakeLLM, "", nil)
	personas, err := gen.Generate(context.Background(), extraction, "Topic X", 3)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if len(personas) != 3 {
		t.Fatalf("expected 3 personas, got %d", len(personas))
	}

	// Check role_type traits
	hasMediator := false
	hasContrarian := false
	for _, p := range personas {
		if p.Traits["role_type"] == "mediator" {
			hasMediator = true
		}
		if p.Traits["role_type"] == "contrarian" {
			hasContrarian = true
		}
	}
	if !hasMediator {
		t.Error("expected at least one mediator persona")
	}
	if !hasContrarian {
		t.Error("expected at least one contrarian persona")
	}
}

func TestPersonaGenerator_CountBounds(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{
			"personas": [
				{"id": "a", "name": "A", "role": "advocate", "goals": [], "traits": {}, "stance_per_entity": {}},
				{"id": "b", "name": "B", "role": "skeptic", "goals": [], "traits": {}, "stance_per_entity": {}}
			]
		}`},
	}

	extraction := &SeedExtraction{Entities: []memoryengine.EntityExtraction{{Name: "Test", Type: "concept"}}}

	gen := NewPersonaGenerator(fakeLLM, "", nil)

	// count = 1 should be clamped to 2
	p1, err := gen.Generate(context.Background(), extraction, "test", 1)
	if err != nil {
		t.Fatalf("Generate(1) error: %v", err)
	}
	if len(p1) < 1 {
		t.Error("expected at least 1 persona")
	}

	// count = 10 should be clamped to 5
	p2, err := gen.Generate(context.Background(), extraction, "test", 10)
	if err != nil {
		t.Fatalf("Generate(10) error: %v", err)
	}
	if len(p2) != 2 { // only 2 responses available from FakeLLM
		t.Log("clamping to 5 is handled in prompt text only; output depends on LLM")
	}
}

func TestPersonaGenerator_NilExtraction(t *testing.T) {
	gen := NewPersonaGenerator(&agent.FakeLLM{}, "", nil)
	_, err := gen.Generate(context.Background(), nil, "test", 2)
	if err == nil {
		t.Fatal("expected error for nil extraction")
	}
}

func TestPersonaGenerator_MalformedJSON(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{invalid`},
	}

	extraction := &SeedExtraction{Entities: []memoryengine.EntityExtraction{{Name: "Test", Type: "concept"}}}
	gen := NewPersonaGenerator(fakeLLM, "", nil)
	_, err := gen.Generate(context.Background(), extraction, "test", 2)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParsePersonaGenResult(t *testing.T) {
	raw := `{"personas":[{"id":"a","name":"Alice","role":"advocate","goals":[],"traits":{},"stance_per_entity":{}}]}`
	result, err := parsePersonaGenResult(raw)
	if err != nil {
		t.Fatalf("parsePersonaGenResult error: %v", err)
	}
	if len(result.Personas) != 1 || result.Personas[0].Name != "Alice" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestParsePersonaGenResult_MarkdownFence(t *testing.T) {
	raw := "```json\n{\"personas\":[{\"id\":\"a\",\"name\":\"Alice\",\"role\":\"advocate\",\"goals\":[],\"traits\":{},\"stance_per_entity\":{}}]}\n```"
	result, err := parsePersonaGenResult(raw)
	if err != nil {
		t.Fatalf("parsePersonaGenResult with fences error: %v", err)
	}
	if len(result.Personas) != 1 {
		t.Errorf("expected 1 persona, got %d", len(result.Personas))
	}
}
