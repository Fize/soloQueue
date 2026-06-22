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
	ext, err := extractor.Extract(context.Background(), "Rust focuses on memory safety while Go prioritizes simplicity and fast compilation.", 48)
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
	_, err := extractor.Extract(context.Background(), "  ", 48)
	if err == nil {
		t.Fatal("expected error for empty text")
	}
}

func TestSeedExtractor_MalformedJSON(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{`{invalid json`},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	_, err := extractor.Extract(context.Background(), "some text", 48)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSeedExtractor_MarkdownCodeFence(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{"```json\n{\"entities\": [{\"name\": \"Test\", \"type\": \"concept\", \"confidence\": 1.0}], \"world_state\": {}, \"key_topics\": [\"test\"], \"conflict_areas\": []}\n```"},
	}

	extractor := NewSeedExtractor(fakeLLM, "", "", nil)
	ext, err := extractor.Extract(context.Background(), "test text", 48)
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
	ext, err := extractor.Extract(context.Background(), longText, 48)
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
	_, err := extractor.Extract(context.Background(), "Rust text", 48)
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
	// LLM returning world_state as a string should produce a clear error,
	// not silently convert it to something meaningless.
	raw := `{"entities":[],"world_state":"The year is 2025, location is Beijing.","key_topics":[],"conflict_areas":[]}`
	_, err := parseExtraction(raw)
	if err == nil {
		t.Fatal("expected error for string world_state")
	}
	if !strings.Contains(err.Error(), "must be a JSON object") {
		t.Errorf("error should explain the problem, got: %v", err)
	}
}

func TestParseExtraction_WorldStateArray(t *testing.T) {
	raw := `{"entities":[],"world_state":["item1","item2"],"key_topics":[],"conflict_areas":[]}`
	_, err := parseExtraction(raw)
	if err == nil {
		t.Fatal("expected error for array world_state")
	}
	if !strings.Contains(err.Error(), "must be a JSON object") {
		t.Errorf("error should explain the problem, got: %v", err)
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

func TestParseExtraction_WithInitialRelationships(t *testing.T) {
	raw := `{
		"entities": [{"name": "5G", "type": "technology", "confidence": 0.9}],
		"world_state": {},
		"key_topics": ["5G debate"],
		"conflict_areas": [],
		"initial_relationships": [
			{"subject_name": "陈镇长", "target_name": "张店主", "kind": "friend", "familiarity": 0.9, "affinity": 0.8},
			{"subject_name": "陈镇长", "target_name": "王医生", "kind": "neighbor", "familiarity": 0.2}
		]
	}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction error: %v", err)
	}
	if len(ext.InitialRelationships) != 2 {
		t.Fatalf("expected 2 initial_relationships, got %d", len(ext.InitialRelationships))
	}
	r1 := ext.InitialRelationships[0]
	if r1.SubjectName != "陈镇长" || r1.TargetName != "张店主" || r1.Kind != RelationFriend {
		t.Errorf("first relationship: expected 陈镇长→张店主 friend, got %+v", r1)
	}
	if r1.Familiarity != 0.9 {
		t.Errorf("expected familiarity 0.9, got %f", r1.Familiarity)
	}
	r2 := ext.InitialRelationships[1]
	if r2.SubjectName != "陈镇长" || r2.TargetName != "王医生" || r2.Kind != RelationNeighbor {
		t.Errorf("second relationship: expected 陈镇长→王医生 neighbor, got %+v", r2)
	}
}

func TestParseExtraction_InitialRelationshipsEmpty(t *testing.T) {
	raw := `{"entities":[],"world_state":{},"key_topics":["test"],"conflict_areas":[],"initial_relationships":[]}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction error: %v", err)
	}
	if len(ext.InitialRelationships) != 0 {
		t.Errorf("expected empty initial_relationships, got %d", len(ext.InitialRelationships))
	}
}

func TestParseExtraction_InitialRelationshipsOmitted(t *testing.T) {
	// When LLM omits initial_relationships entirely, it should still parse OK
	raw := `{"entities":[],"world_state":{},"key_topics":["test"],"conflict_areas":[]}`
	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction error: %v", err)
	}
	if ext.InitialRelationships == nil {
		// It's OK to be nil, not an error
	}
}

func TestMergeExtractions_InitialRelationships(t *testing.T) {
	a := &SeedExtraction{
		Entities:    []memoryengine.EntityExtraction{{Name: "Go", Type: "technology", Confidence: 0.9}},
		WorldState:  map[string]any{"version": "1.22"},
		KeyTopics:   []string{"Go"},
		InitialRelationships: []InitialRelationship{
			{SubjectName: "Alice", TargetName: "Bob", Kind: RelationFriend, Familiarity: 0.9},
		},
	}
	b := &SeedExtraction{
		Entities:    []memoryengine.EntityExtraction{{Name: "Rust", Type: "technology", Confidence: 0.9}},
		WorldState:  map[string]any{"rust_version": "2024"},
		KeyTopics:   []string{"Rust"},
		InitialRelationships: []InitialRelationship{
			{SubjectName: "Charlie", TargetName: "Dave", Kind: RelationColleague, Familiarity: 0.7},
		},
	}

	merged := mergeExtractions(a, b)

	if len(merged.InitialRelationships) != 2 {
		t.Fatalf("expected 2 merged relationships, got %d: %+v", len(merged.InitialRelationships), merged.InitialRelationships)
	}
	if merged.InitialRelationships[0].Kind != RelationFriend {
		t.Errorf("first relationship should be friend, got %q", merged.InitialRelationships[0].Kind)
	}
	if merged.InitialRelationships[1].Kind != RelationColleague {
		t.Errorf("second relationship should be colleague, got %q", merged.InitialRelationships[1].Kind)
	}
}

func TestMergeExtractions_InitialRelationshipsDedup(t *testing.T) {
	a := &SeedExtraction{
		InitialRelationships: []InitialRelationship{
			{SubjectName: "Alice", TargetName: "Bob", Kind: RelationFriend, Familiarity: 0.9},
		},
	}
	b := &SeedExtraction{
		InitialRelationships: []InitialRelationship{
			// Same subject+target+kind, should be deduped
			{SubjectName: "Alice", TargetName: "Bob", Kind: RelationFriend, Familiarity: 0.8},
		},
	}

	merged := mergeExtractions(a, b)
	if len(merged.InitialRelationships) != 1 {
		t.Errorf("expected 1 deduped relationship, got %d", len(merged.InitialRelationships))
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

func TestParseExtraction_RawNewlineInString(t *testing.T) {
	// Reproduces the real-world bug: LLM generates JSON with raw newline
	// characters inside string values (e.g. in "description" or "reason" fields).
	// The JSON spec requires control chars to be escaped as \n, but some LLMs
	// emit literal newlines. Go's json.Unmarshal rejects these with
	// "invalid character '\n' in string literal".
	//
	// The raw content below contains literal \n bytes (not escaped) inside
	// the "description" field of suggested_agents, matching the actual
	// error from the logs.
	raw := "{\n  \"entities\": [\n    {\"name\": \"杨凡\", \"type\": \"person\", \"confidence\": 1.0}\n  ],\n  \"suggested_agents\": [\n    {\"name\": \"杨凡\", \"role\": \"protagonist\", \"description\": \"杨凡是一个\n年轻的剑客\n他追求力量\", \"traits\": [\"勇敢\", \"执着\"]}\n  ],\n  \"key_topics\": [\"修炼\"],\n  \"conflict_areas\": [],\n  \"world_state\": {}\n}"

	ext, err := parseExtraction(raw)
	if err != nil {
		t.Fatalf("parseExtraction should handle raw newlines in strings, got: %v", err)
	}
	if len(ext.Entities) != 1 || ext.Entities[0].Name != "杨凡" {
		t.Errorf("expected 1 entity '杨凡', got %+v", ext.Entities)
	}
	if len(ext.SuggestedAgents) != 1 {
		t.Fatalf("expected 1 suggested agent, got %d", len(ext.SuggestedAgents))
	}
	// The description should contain the text with newlines (now properly escaped)
	if !strings.Contains(ext.SuggestedAgents[0].Description, "年轻的剑客") {
		t.Errorf("description should contain '年轻的剑客', got: %q", ext.SuggestedAgents[0].Description)
	}
}

func TestEscapeControlCharsInStrings(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no strings",
			input: `[1, 2, 3]`,
			want:  `[1, 2, 3]`,
		},
		{
			name:  "normal string",
			input: `"hello world"`,
			want:  `"hello world"`,
		},
		{
			name:  "raw newline in string",
			input: "\"line1\nline2\"",
			want:  `"line1\nline2"`,
		},
		{
			name:  "raw tab in string",
			input: "\"col1\tcol2\"",
			want:  `"col1\tcol2"`,
		},
		{
			name:  "raw carriage return in string",
			input: "\"line1\rline2\"",
			want:  `"line1\rline2"`,
		},
		{
			name:  "escaped newline stays as-is",
			input: `"line1\nline2"`,
			want:  `"line1\nline2"`,
		},
		{
			name:  "escaped quote stays as-is",
			input: `"he said \"hello\""`,
			want:  `"he said \"hello\""`,
		},
		{
			name:  "backslash at end of string",
			input: `"path\\"`,
			want:  `"path\\"`,
		},
		{
			name:  "newlines outside strings (whitespace) untouched",
			input: "{\n  \"key\": \"value\"\n}",
			want:  "{\n  \"key\": \"value\"\n}",
		},
		{
			name:  "multiple strings with mixed control chars",
			input: "{\"a\": \"x\ny\", \"b\": \"z\tw\"}",
			want:  `{"a": "x\ny", "b": "z\tw"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeControlCharsInStrings(tt.input)
			if got != tt.want {
				t.Errorf("escapeControlCharsInStrings(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
