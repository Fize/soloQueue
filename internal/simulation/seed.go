package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// SeedExtraction holds the structured output from LLM seed analysis.
type SuggestedAgent struct {
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Description string   `json:"description"`
	Traits      []string `json:"traits"`
}

// SeedExtraction holds the structured output from LLM seed analysis.
type SeedExtraction struct {
	Entities        []memoryengine.EntityExtraction `json:"entities"`
	WorldState      map[string]any                  `json:"world_state"`
	KeyTopics       []string                        `json:"key_topics"`
	ConflictAreas   []string                        `json:"conflict_areas"`
	SuggestedAgents []SuggestedAgent                `json:"suggested_agents,omitempty"`
	LifecycleEvents []SeedLifecycleEvent            `json:"lifecycle_events,omitempty"`
}

// SeedExtractor extracts entities, world state, and topics from seed text.
// It optionally writes extracted entities into the MemoryEngine KG.
type SeedExtractor struct {
	llm          agent.LLMClient
	model        string
	providerID   string
	memoryEngine *memoryengine.Engine // nil = skip KG writes
	log          *logger.Logger
}

// NewSeedExtractor creates a new SeedExtractor.
func NewSeedExtractor(llm agent.LLMClient, model, providerID string, mem *memoryengine.Engine) *SeedExtractor {
	return &SeedExtractor{llm: llm, model: model, providerID: providerID, memoryEngine: mem}
}

func (s *SeedExtractor) SetLogger(log *logger.Logger) { s.log = log }

// Extract parses seed text and returns structured extraction.
func (s *SeedExtractor) Extract(ctx context.Context, seedText string) (*SeedExtraction, error) {
	if strings.TrimSpace(seedText) == "" {
		return nil, fmt.Errorf("seed text is empty")
	}

	chunks := chunkText(seedText, 1500, 1)
	if len(chunks) == 0 {
		chunks = []string{seedText}
	}
	if s.log != nil {
		s.log.InfoContext(ctx, logger.CatSimulation, "extraction: text chunked", "chunks", len(chunks))
	}

	var merged *SeedExtraction
	for _, chunk := range chunks {
		ext, err := s.extractChunk(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("extract chunk: %w", err)
		}
		merged = mergeExtractions(merged, ext)
	}

	return merged, nil
}

// extractChunk calls LLM on a single chunk and optionally writes to KG.
func (s *SeedExtractor) extractChunk(ctx context.Context, chunk string) (*SeedExtraction, error) {
	if s.log != nil {
		s.log.DebugContext(ctx, logger.CatSimulation, "extractChunk: calling LLM", "chunk_len", len(chunk))
	}

	prompt := buildExtractionPrompt(chunk)

	resp, err := s.llm.Chat(ctx, agent.LLMRequest{
		Model:        s.model,
		ProviderID:   s.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    2048,
		ResponseJSON: true,
	})
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	if s.log != nil {
		s.log.DebugContext(ctx, logger.CatSimulation, "extractChunk: LLM response received", "content_len", len(resp.Content))
	}

	ext, err := parseExtraction(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse extraction: %w", err)
	}

	if s.log != nil {
		s.log.InfoContext(ctx, logger.CatSimulation, "extractChunk: parsed OK",
			"entities", len(ext.Entities), "topics", len(ext.KeyTopics))
	}

	// Optionally write to MemoryEngine KG
	if s.memoryEngine != nil && len(ext.Entities) > 0 {
		if s.log != nil {
			s.log.DebugContext(ctx, logger.CatSimulation, "extractChunk: saving to KG", "entities", len(ext.Entities))
		}
		_, _, err := s.memoryEngine.SaveWithEntities(ctx, chunk, time.Now().Format(time.RFC3339), "simulation_seed", "", ext.Entities)
		if err != nil {
			return nil, fmt.Errorf("save to memory engine: %w", err)
		}
	}

	return ext, nil
}

// --- chunking ---

// chunkText splits text into overlapping chunks for multi-pass extraction.
//
// It iterates over non-empty paragraphs, consuming exactly one paragraph per
// loop iteration so termination is always guaranteed. When appending the next
// paragraph to the current chunk would exceed maxChunkSize, the current chunk is
// flushed and a new chunk is seeded with the last overlapLines paragraphs before
// the current one is appended. A single paragraph larger than maxChunkSize is
// emitted on its own rather than splitting mid-paragraph.
func chunkText(text string, maxChunkSize int, overlapLines int) []string {
	paragraphs := strings.Split(text, "\n\n")
	var chunks []string

	var current strings.Builder
	var currentParas []string // paragraphs currently buffered in `current` (for overlap)

	flush := func() {
		if current.Len() > 0 {
			chunks = append(chunks, current.String())
			current.Reset()
		}
	}

	for _, raw := range paragraphs {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}

		// If the current chunk is non-empty and adding this paragraph would
		// exceed the limit, flush it and seed a fresh chunk with the overlap.
		if current.Len() > 0 && current.Len()+len(p)+2 > maxChunkSize {
			flush()

			start := len(currentParas) - overlapLines
			if start < 0 {
				start = 0
			}
			overlap := append([]string(nil), currentParas[start:]...)
			currentParas = currentParas[:0]
			for _, ov := range overlap {
				current.WriteString(ov)
				current.WriteString("\n\n")
				currentParas = append(currentParas, ov)
			}
		}

		current.WriteString(p)
		current.WriteString("\n\n")
		currentParas = append(currentParas, p)
	}

	flush()

	return chunks
}

// --- merging ---

func mergeExtractions(a, b *SeedExtraction) *SeedExtraction {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}

	// Merge entities (dedup by name)
	seen := make(map[string]bool)
	var mergedEntities []memoryengine.EntityExtraction
	for _, e := range append(a.Entities, b.Entities...) {
		if !seen[e.Name] {
			seen[e.Name] = true
			mergedEntities = append(mergedEntities, e)
		}
	}
	a.Entities = mergedEntities

	// Merge WorldState (b overwrites a on key conflict)
	for k, v := range b.WorldState {
		a.WorldState[k] = v
	}

	// Merge KeyTopics (union, avoid dupes against existing a.KeyTopics)
	topicSeen := make(map[string]bool, len(a.KeyTopics))
	for _, t := range a.KeyTopics {
		topicSeen[t] = true
	}
	for _, t := range b.KeyTopics {
		if !topicSeen[t] {
			topicSeen[t] = true
			a.KeyTopics = append(a.KeyTopics, t)
		}
	}

	// Merge ConflictAreas (union)
	conflictSeen := make(map[string]bool, len(a.ConflictAreas))
	for _, c := range a.ConflictAreas {
		conflictSeen[c] = true
	}
	for _, c := range b.ConflictAreas {
		if !conflictSeen[c] {
			conflictSeen[c] = true
			a.ConflictAreas = append(a.ConflictAreas, c)
		}
	}

	// Merge SuggestedAgents (dedup by name)
	agentSeen := make(map[string]bool)
	var mergedAgents []SuggestedAgent
	for _, ag := range append(a.SuggestedAgents, b.SuggestedAgents...) {
		if !agentSeen[ag.Name] {
			agentSeen[ag.Name] = true
			mergedAgents = append(mergedAgents, ag)
		}
	}
	a.SuggestedAgents = mergedAgents

	return a
}

// --- parsing ---

func parseExtraction(content string) (*SeedExtraction, error) {
	cleaned := cleanJSONResponse(content)

	// Validate field types before full unmarshal — catch common LLM mistakes early
	if typeErr := validateExtractionJSON(cleaned); typeErr != nil {
		return nil, typeErr
	}

	var ext SeedExtraction
	if err := json.Unmarshal([]byte(cleaned), &ext); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w\nraw: %s", err, truncateStr(content, 200))
	}

	if ext.WorldState == nil {
		ext.WorldState = make(map[string]any)
	}

	return &ext, nil
}

// validateExtractionJSON checks for common LLM JSON mistakes like returning
// a string where an object is required. Returns a descriptive error for the user.
func validateExtractionJSON(raw string) error {
	var partial struct {
		WorldState json.RawMessage `json:"world_state"`
	}
	if err := json.Unmarshal([]byte(raw), &partial); err != nil {
		// Can't even parse as JSON — let the full unmarshal handle the error
		return nil
	}

	if len(partial.WorldState) == 0 || string(partial.WorldState) == "null" {
		return nil
	}

	// Check if world_state is a string instead of an object
	if partial.WorldState[0] == '"' {
		var s string
		json.Unmarshal(partial.WorldState, &s)
		return fmt.Errorf("world_state must be a JSON object {}, got a string: %q. The LLM returned a malformed response. Please retry or simplify the seed text.", truncateStr(s, 100))
	}

	// Check if world_state is an array instead of an object
	if partial.WorldState[0] == '[' {
		return fmt.Errorf("world_state must be a JSON object {}, got an array. The LLM returned a malformed response. Please retry or simplify the seed text.")
	}

	return nil
}

// --- prompts ---

func buildExtractionPrompt(text string) string {
	var b strings.Builder
	b.WriteString("Analyze the following text and extract structured information.\n\n")
	b.WriteString("Output valid JSON with these fields:\n")
	b.WriteString("- `entities`: array of {name, type, confidence, relations[{target_name, rel_type, weight}]}\n")
	b.WriteString("  Type must be one of: technology, person, concept, organization, product\n")
	b.WriteString("  rel_type must be one of: mention, agree, rebuttal, propose\n")
	b.WriteString("- `world_state`: MUST be a JSON object (NOT a string). Flat key-value pairs, e.g. {\"era\": \"2025\", \"location\": \"Beijing\"}. If no world state to extract, return {}.\n")
	b.WriteString("- `key_topics`: array of main topic strings (max 3), e.g. [\"AI regulation\", \"innovation vs safety\"]\n")
	b.WriteString("- `conflict_areas`: array of debated or controversial aspects (max 3), e.g. [\"regulatory approach\", \"timeline\"]\n")
	b.WriteString("- `suggested_agents`: array of objects representing specific individuals or characters in the text. Each object MUST have: `name` (string), `role` (string, e.g. advocate, skeptic, mediator), `description` (brief summary), `traits` (array of strings). If no specific characters exist, return [].\n")
	b.WriteString("- `lifecycle_events`: array of scheduled events. Each object MUST have: `type` (\"agent_spawn\"|\"agent_death\"|\"simulation_end\"), `agent_name` (string), `agent_role` (string, for spawn), `trigger` (\"sim_time\"|\"wall_time\"|\"condition\"), `trigger_value` (string), `reason` (string). If no explicit timing/conditions in text, return [].\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- All fields MUST use the exact JSON types specified. Do NOT use strings where objects are required, or vice versa.\n")
	b.WriteString("- Only extract entities that are debatable: concepts, technologies, organizations, or people that agents could take different stances on.\n")
	b.WriteString("- `world_state` MUST be a JSON object {} with key-value pairs. Use {} if nothing to extract. NEVER use a string.\n")
	b.WriteString("- Keep key_topics specific enough to generate focused discussion.\n")
	b.WriteString("- If the text features actual characters (e.g. characters in a novel, meeting attendees, or historical figures in a debate), you MUST extract them into `suggested_agents` so they can be simulated directly.\n")
	b.WriteString("- For lifecycle_events: only extract events explicitly stated in the text. Use sim_time triggers for relative durations (\"3h\"), absolute clock times (\"14:30\"), wall_time triggers for real-time constraints (\"300s\"), and condition triggers for world-state-dependent events (\"consensus_reached\").\n\n")
	b.WriteString("Text:\n")
	b.WriteString(text)

	return b.String()
}
