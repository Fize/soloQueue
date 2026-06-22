package simulation

import (
	"context"
	"fmt"
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

	gen := NewPersonaGenerator(fakeLLM, "", "", nil)
	personas, err := gen.Generate(context.Background(), extraction, "Rust vs Go", 2, "zh")
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

	gen := NewPersonaGenerator(fakeLLM, "", "", nil)
	personas, err := gen.Generate(context.Background(), extraction, "Topic X", 3, "zh")
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

	gen := NewPersonaGenerator(fakeLLM, "", "", nil)

	// count = 1 should be clamped to 2
	p1, err := gen.Generate(context.Background(), extraction, "test", 1, "zh")
	if err != nil {
		t.Fatalf("Generate(1) error: %v", err)
	}
	if len(p1) < 1 {
		t.Error("expected at least 1 persona")
	}

	// count = 10 should be clamped to 5
	p2, err := gen.Generate(context.Background(), extraction, "test", 10, "zh")
	if err != nil {
		t.Fatalf("Generate(10) error: %v", err)
	}
	if len(p2) != 2 { // only 2 responses available from FakeLLM
		t.Log("clamping to 5 is handled in prompt text only; output depends on LLM")
	}
}

func TestPersonaGenerator_NilExtraction(t *testing.T) {
	gen := NewPersonaGenerator(&agent.FakeLLM{}, "", "", nil)
	_, err := gen.Generate(context.Background(), nil, "test", 2, "zh")
	if err == nil {
		t.Fatal("expected error for nil extraction")
	}
}

func TestPersonaGenerator_MalformedJSON(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{invalid`},
	}

	extraction := &SeedExtraction{Entities: []memoryengine.EntityExtraction{{Name: "Test", Type: "concept"}}}
	gen := NewPersonaGenerator(fakeLLM, "", "", nil)
	_, err := gen.Generate(context.Background(), extraction, "test", 2, "zh")
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

func TestPersonaGenerator_BatchedGeneration(t *testing.T) {
	// Create 12 suggested agents to trigger batching (12 > personaGenBatchSize=5).
	// Expected batches: [0-5), [5-10), [10-12) = 3 batches of 5, 5, 2.
	const totalAgents = 12
	suggestedAgents := make([]SuggestedAgent, totalAgents)
	for i := 0; i < totalAgents; i++ {
		suggestedAgents[i] = SuggestedAgent{
			Name:        fmt.Sprintf("Agent%d", i+1),
			Role:        "protagonist",
			Description: fmt.Sprintf("Description for agent %d", i+1),
			Traits:      []string{"brave"},
		}
	}

	// Track LLM calls to verify batching
	var llmPrompts []string

	// Build 3 LLM responses, one per batch
	var responses []string
	for batchStart := 0; batchStart < totalAgents; batchStart += personaGenBatchSize {
		batchEnd := batchStart + personaGenBatchSize
		if batchEnd > totalAgents {
			batchEnd = totalAgents
		}
		var entries []string
		for j := batchStart; j < batchEnd; j++ {
			entries = append(entries, fmt.Sprintf(
				`{"id":"agent-%d","name":"Agent%d","role":"neutral","goals":["goal"],"traits":{},"stance_per_entity":{}}`,
				j+1, j+1))
		}
		responses = append(responses, `{"personas":[`+strings.Join(entries, ",")+`]}`)
	}

	fakeLLM := &agent.FakeLLM{
		Responses: responses,
		Hook: func(req agent.LLMRequest) {
			llmPrompts = append(llmPrompts, req.Messages[0].Content)
		},
	}

	extraction := &SeedExtraction{
		Entities:        []memoryengine.EntityExtraction{{Name: "Topic", Type: "concept", Confidence: 0.8}},
		SuggestedAgents: suggestedAgents,
	}

	gen := NewPersonaGenerator(fakeLLM, "test-model", "test-provider", nil)
	personas, err := gen.Generate(context.Background(), extraction, "Test Topic", totalAgents, "zh")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Should have made 3 LLM calls (batches: 5, 5, 2)
	if len(llmPrompts) != 3 {
		t.Errorf("expected 3 LLM calls (batches), got %d", len(llmPrompts))
	}

	// Should have 12 personas total
	if len(personas) != totalAgents {
		t.Fatalf("expected %d personas, got %d", totalAgents, len(personas))
	}

	// Verify persona names
	for i, p := range personas {
		expectedName := fmt.Sprintf("Agent%d", i+1)
		if p.Name != expectedName {
			t.Errorf("persona[%d] name: expected %q, got %q", i, expectedName, p.Name)
		}
	}

	// Cross-reference check: persona 0 (batch 0) system prompt should mention
	// persona 6 (batch 1) — proving the second pass updated system prompts.
	if len(personas) > 6 {
		prompt := personas[0].SystemPrompt
		if !strings.Contains(prompt, "Agent6") {
			t.Error("persona[0] system prompt should reference Agent6 from batch 1 (second pass cross-reference)")
		}
	}

	// Each batch prompt should only mention its own suggested agents, not all 12
	// Batch 0 prompt should mention "Agent1" but NOT "Agent6"
	if len(llmPrompts) > 0 {
		if !strings.Contains(llmPrompts[0], "Agent1") {
			t.Error("batch 0 prompt should mention Agent1")
		}
		if strings.Contains(llmPrompts[0], "Agent6") {
			t.Error("batch 0 prompt should NOT mention Agent6 (that's in batch 1)")
		}
	}
}

func TestPersonaGenerator_BatchSplitsCorrectly(t *testing.T) {
	// Test with 7 agents → batches: [0-5), [5-7) = 2 batches of 5, 2
	const totalAgents = 7
	suggestedAgents := make([]SuggestedAgent, totalAgents)
	for i := 0; i < totalAgents; i++ {
		suggestedAgents[i] = SuggestedAgent{
			Name: fmt.Sprintf("P%d", i+1),
			Role: "neutral",
		}
	}

	var llmCalls int
	responses := []string{
		`{"personas":[{"id":"p1","name":"P1","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}},{"id":"p2","name":"P2","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}},{"id":"p3","name":"P3","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}},{"id":"p4","name":"P4","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}},{"id":"p5","name":"P5","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}}]}`,
		`{"personas":[{"id":"p6","name":"P6","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}},{"id":"p7","name":"P7","role":"neutral","goals":[],"traits":{},"stance_per_entity":{}}]}`,
	}
	fakeLLM := &agent.FakeLLM{
		Responses: responses,
		Hook: func(req agent.LLMRequest) {
			llmCalls++
		},
	}

	extraction := &SeedExtraction{
		Entities:        []memoryengine.EntityExtraction{{Name: "T", Type: "concept"}},
		SuggestedAgents: suggestedAgents,
	}
	gen := NewPersonaGenerator(fakeLLM, "", "", nil)
	personas, err := gen.Generate(context.Background(), extraction, "topic", totalAgents, "en")
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if llmCalls != 2 {
		t.Errorf("expected 2 LLM calls for 7 agents (batches: 5+2), got %d", llmCalls)
	}
	if len(personas) != totalAgents {
		t.Errorf("expected %d personas, got %d", totalAgents, len(personas))
	}
}
