package simulation

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

func TestSeedExtractor_BasicExtraction(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{
			"entities": [
				{"name": "Rust", "type": "technology", "confidence": 0.9, "relations": [{"target_name": "Go", "rel_type": "rebuttal", "weight": 0.8}]},
				{"name": "Go", "type": "technology", "confidence": 0.9},
				{"name": "Memory Safety", "type": "concept", "confidence": 0.7}
			],
			"world_state": {"language": "systems programming", "era": "2025"},
			"key_topics": ["Rust vs Go for systems programming"],
			"conflict_areas": ["memory safety vs simplicity"]
		}`},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	ext, err := extractor.Extract(context.Background(), "Rust focuses on memory safety while Go prioritizes simplicity and fast compilation.")
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}

	if len(ext.Entities) != 3 {
		t.Errorf("expected 3 entities, got %d", len(ext.Entities))
	}
	if ext.Entities[0].Name != "Rust" {
		t.Errorf("expected first entity 'Rust', got '%s'", ext.Entities[0].Name)
	}
	if ext.Entities[0].Type != "technology" {
		t.Errorf("expected type 'technology', got '%s'", ext.Entities[0].Type)
	}
	if v := ext.WorldState["language"]; v != "systems programming" {
		t.Errorf("expected world_state.language='systems programming', got '%v'", v)
	}
	if len(ext.KeyTopics) != 1 || ext.KeyTopics[0] != "Rust vs Go for systems programming" {
		t.Errorf("unexpected key_topics: %v", ext.KeyTopics)
	}
}

func TestSeedExtractor_EmptyText(t *testing.T) {
	extractor := NewSeedExtractor(&agent.FakeLLM{}, "", "", nil)
	_, err := extractor.Extract(context.Background(), "  ")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSeedExtractor_MalformedJSON(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{invalid json`},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	_, err := extractor.Extract(context.Background(), "some text")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSeedExtractor_MarkdownCodeFence(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{"```json\n{\"entities\": [{\"name\": \"Test\", \"type\": \"concept\", \"confidence\": 1.0}], \"world_state\": {}, \"key_topics\": [\"test\"], \"conflict_areas\": []}\n```"},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	ext, err := extractor.Extract(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if len(ext.Entities) != 1 || ext.Entities[0].Name != "Test" {
		t.Errorf("unexpected entities: %+v", ext.Entities)
	}
}

func TestSeedExtractor_Chunking(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("Paragraph about Go and Rust performance characteristics.\n\n")
	}
	longText := b.String()

	callCount := 0
	fakeLLM := &agent.FakeLLM{
		Responses: []string{
			`{"entities":[{"name":"Go","type":"technology","confidence":0.9}],"world_state":{},"key_topics":["programming"],"conflict_areas":[]}`,
			`{"entities":[{"name":"Rust","type":"technology","confidence":0.9}],"world_state":{},"key_topics":["programming"],"conflict_areas":[]}`,
		},
		Hook: func(req agent.LLMRequest) {
			callCount++
		},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	ext, err := extractor.Extract(context.Background(), longText)
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
	if callCount < 2 {
		t.Errorf("expected >=2 LLM calls for long text, got %d", callCount)
	}
	if len(ext.Entities) < 1 {
		t.Errorf("expected at least 1 entity from merged chunks, got %d", len(ext.Entities))
	}
}

func TestSeedExtractor_WithMemoryEngine(t *testing.T) {
	t.Skip("requires real DB; tested via integration test")

	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{
			"entities": [{"name": "Rust", "type": "technology", "confidence": 0.9}],
			"world_state": {},
			"key_topics": ["Rust"],
			"conflict_areas": []
		}`},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	_, err := extractor.Extract(context.Background(), "Rust text")
	if err != nil {
		t.Fatalf("Extract() error: %v", err)
	}
}

func TestChunkText_Small(t *testing.T) {
	text := "Short text."
	chunks := chunkText(text, 1500, 1)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunkText_LargeWithOverlap(t *testing.T) {
	var paragraphs []string
	for i := 0; i < 10; i++ {
		paragraphs = append(paragraphs, strings.Repeat("word ", 40)+"\n")
	}
	text := strings.Join(paragraphs, "\n\n")

	chunks := chunkText(text, 500, 1)

	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}

	for i := 1; i < len(chunks); i++ {
		if len(chunks[i]) < 20 {
			t.Errorf("chunk %d is too short: %d chars", i, len(chunks[i]))
		}
	}
}

func TestParseExtraction(t *testing.T) {
	raw := `{"entities":[{"name":"Go","type":"technology","confidence":0.8}],"world_state":{"version":"1.22"},"key_topics":["Go programming"],"conflict_areas":[]}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction error: %v", err)
	}
	if ext.Entities[0].Name != "Go" {
		t.Errorf("expected 'Go', got '%s'", ext.Entities[0].Name)
	}
	if ext.WorldState["version"] != "1.22" {
		t.Errorf("expected version '1.22', got '%v'", ext.WorldState["version"])
	}
}

func TestParseExtraction_EmptyJSON(t *testing.T) {
	raw := `{}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction error: %v", err)
	}
	if ext.WorldState == nil {
		t.Error("expected non-nil WorldState")
	}
}

func TestMergeExtractions(t *testing.T) {
	a := &SeedExtraction{
		Entities:      []memoryengine.EntityExtraction{{Name: "Go", Type: "technology", Confidence: 0.9}},
		WorldState:    map[string]any{"version": "1.22"},
		KeyTopics:     []string{"Go"},
		ConflictAreas: []string{"simplicity"},
	}
	b := &SeedExtraction{
		Entities:      []memoryengine.EntityExtraction{{Name: "Rust", Type: "technology", Confidence: 0.9}, {Name: "Go", Type: "technology", Confidence: 0.9}},
		WorldState:    map[string]any{"rust_version": "2024"},
		KeyTopics:     []string{"Rust", "memory safety"},
		ConflictAreas: []string{"performance"},
	}

	merged := mergeExtractions(a, b)

	if len(merged.Entities) != 2 {
		t.Errorf("expected 2 deduped entities, got %d", len(merged.Entities))
	}
	if _, ok := merged.WorldState["version"]; !ok {
		t.Error("missing 'version' key")
	}
	if _, ok := merged.WorldState["rust_version"]; !ok {
		t.Error("missing 'rust_version' key")
	}
	if len(merged.KeyTopics) != 3 {
		t.Errorf("expected 2 key_topics, got %d: %v", len(merged.KeyTopics), merged.KeyTopics)
	}
	if len(merged.ConflictAreas) != 2 {
		t.Errorf("expected 2 conflict_areas, got %d", len(merged.ConflictAreas))
	}
}

func TestMergeExtractions_NilFirst(t *testing.T) {
	b := &SeedExtraction{
		Entities:      []memoryengine.EntityExtraction{{Name: "Go", Type: "technology", Confidence: 0.9}},
		WorldState:    map[string]any{"version": "1.22"},
		KeyTopics:     []string{"Go"},
		ConflictAreas: []string{},
	}
	merged := mergeExtractions(nil, b)
	if len(merged.Entities) != 1 || merged.Entities[0].Name != "Go" {
		t.Error("merge with nil first should return second")
	}
}
