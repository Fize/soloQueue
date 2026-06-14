package simulation

import (
	"context"
	"strings"
	"testing"
	"time"

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

// TestChunkText_LargeParagraphNoInfiniteLoop is a regression test for an
// infinite loop (and resulting OOM/SIGKILL) that occurred when a paragraph
// larger than maxChunkSize followed a smaller paragraph. The old chunker never
// advanced past the oversized paragraph, growing the chunks slice without bound.
func TestChunkText_LargeParagraphNoInfiniteLoop(t *testing.T) {
	small := "Intro paragraph."
	large := strings.TrimSpace(strings.Repeat("word ", 1000)) // ~5000 chars, well over maxChunkSize
	text := small + "\n\n" + large + "\n\nClosing paragraph."

	done := make(chan []string, 1)
	go func() {
		done <- chunkText(text, 1500, 1)
	}()

	select {
	case chunks := <-done:
		if len(chunks) == 0 {
			t.Fatalf("expected at least one chunk, got 0")
		}
		// The oversized paragraph must be present intact in some chunk.
		found := false
		for _, c := range chunks {
			if strings.Contains(c, large) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("oversized paragraph was dropped or split across chunks")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("chunkText did not terminate (infinite loop regression)")
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

func TestParseExtraction_WorldStateString(t *testing.T) {
	// LLM sometimes returns world_state as a string instead of an object
	raw := `{"entities":[],"world_state":"The year is 2025, location is Beijing.","key_topics":[],"conflict_areas":[]}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction should not error on string world_state: %v", err)
	}
	if ext.WorldState == nil {
		t.Fatal("expected non-nil WorldState")
	}
	if ext.WorldState["description"] != "The year is 2025, location is Beijing." {
		t.Errorf("expected description key, got: %v", ext.WorldState)
	}
}

func TestParseExtraction_WorldStateObject(t *testing.T) {
	// Normal object form should still work
	raw := `{"entities":[],"world_state":{"era":"2025","location":"Beijing"},"key_topics":[],"conflict_areas":[]}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction should work on object world_state: %v", err)
	}
	if ext.WorldState["era"] != "2025" {
		t.Errorf("expected era=2025, got: %v", ext.WorldState["era"])
	}
	if ext.WorldState["location"] != "Beijing" {
		t.Errorf("expected location=Beijing, got: %v", ext.WorldState["location"])
	}
}

func TestParseExtraction_WorldStateNull(t *testing.T) {
	raw := `{"entities":[],"world_state":null,"key_topics":[],"conflict_areas":[]}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction should handle null world_state: %v", err)
	}
	if ext.WorldState == nil || len(ext.WorldState) != 0 {
		t.Errorf("expected empty WorldState for null, got: %v", ext.WorldState)
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
