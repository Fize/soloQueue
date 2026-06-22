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
	Entities             []memoryengine.EntityExtraction `json:"entities"`
	WorldState           map[string]any                  `json:"world_state"`
	KeyTopics            []string                        `json:"key_topics"`
	ConflictAreas        []string                        `json:"conflict_areas"`
	SuggestedAgents      []SuggestedAgent                `json:"suggested_agents,omitempty"`
	LifecycleEvents      []SeedLifecycleEvent            `json:"lifecycle_events,omitempty"`
	InitialRelationships []InitialRelationship           `json:"initial_relationships,omitempty"`
}

// SeedExtractor extracts entities, world state, and topics from seed text.
// It optionally writes extracted entities into the MemoryEngine KG.
type SeedExtractor struct {
	llm          agent.LLMClient
	model        string
	providerID   string
	memoryEngine *memoryengine.Engine // nil = skip KG writes
	maxTokens    int                  // 0 = use sensible default per phase
	log          *logger.Logger
}

// NewSeedExtractor creates a new SeedExtractor.
func NewSeedExtractor(llm agent.LLMClient, model, providerID string, mem *memoryengine.Engine) *SeedExtractor {
	return &SeedExtractor{llm: llm, model: model, providerID: providerID, memoryEngine: mem}
}

func (s *SeedExtractor) SetLogger(log *logger.Logger) { s.log = log }

// SetMaxTokens overrides the default max_tokens for LLM calls.
// If 0 or not called, uses a per-phase sensible default.
func (s *SeedExtractor) SetMaxTokens(n int) {
	if n > 0 {
		s.maxTokens = n
	}
}

// chatWithJSONRetry calls the LLM and, if JSON parsing fails, retries once with
// a fix instruction.
func (s *SeedExtractor) chatWithJSONRetry(ctx context.Context, prompt string, maxTokens int) (string, error) {
	resp, err := s.llm.Chat(ctx, agent.LLMRequest{
		Model:        s.model,
		ProviderID:   s.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    maxTokens,
		ResponseJSON: true,
	})
	if err != nil {
		return "", err
	}

	// Try parsing; on failure, retry once
	_, parseErr := parseExtraction(resp.Content)
	if parseErr == nil {
		return resp.Content, nil
	}

	if s.log != nil {
		s.log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: first parse failed, retrying",
			"err", parseErr.Error())
	}

	retryPrompt := prompt + fmt.Sprintf("\n\n[SYSTEM] Your previous JSON response was invalid: %s\nPlease fix the JSON syntax and output ONLY valid JSON. Check for missing commas, unbalanced brackets, and unescaped characters.\n", parseErr.Error())

	retryResp, retryErr := s.llm.Chat(ctx, agent.LLMRequest{
		Model:        s.model,
		ProviderID:   s.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: retryPrompt}},
		MaxTokens:    maxTokens,
		ResponseJSON: true,
	})
	if retryErr != nil {
		return "", fmt.Errorf("retry after parse error: %w (original: %w)", retryErr, parseErr)
	}
	return retryResp.Content, nil
}

const defaultSeedMaxTokens = 16384

// Extract parses seed text and returns structured extraction.
// simulatedHours is the total simulation duration; passed to Phase 2 so the LLM
// can map narrative timeline events to simulation clock offsets.
func (s *SeedExtractor) Extract(ctx context.Context, seedText string, simulatedHours int) (*SeedExtraction, error) {
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

	// Phase 1: Per-chunk basic extraction (entities, world_state, key_topics, conflict_areas)
	var merged *SeedExtraction
	for _, chunk := range chunks {
		ext, err := s.extractChunk(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("extract chunk: %w", err)
		}
		merged = mergeExtractions(merged, ext)
	}

	// Phase 2: Character extraction from merged context (suggested_agents, lifecycle_events, initial_relationships)
	// Uses full token budget to avoid truncation of the last fields.
	if merged != nil && len(merged.Entities) > 0 {
		charExt, err := s.extractCharacters(ctx, seedText, merged, simulatedHours)
		if err != nil {
			// Non-fatal: continue without character data
			if s.log != nil {
				s.log.WarnContext(ctx, logger.CatSimulation, "extractCharacters failed, continuing without character data", "err", err.Error())
			}
		} else if charExt != nil {
			merged = mergeExtractions(merged, charExt)
		}
	}

	return merged, nil
}

// extractChunk calls LLM on a single chunk for basic extraction (Phase 1).
// Extracts entities, world_state, key_topics, and conflict_areas only.
func (s *SeedExtractor) extractChunk(ctx context.Context, chunk string) (*SeedExtraction, error) {
	if s.log != nil {
		s.log.DebugContext(ctx, logger.CatSimulation, "extractChunk: calling LLM", "chunk_len", len(chunk))
	}

	prompt := buildBasicExtractionPrompt(chunk)

	mt := s.maxTokens
	if mt <= 0 {
		mt = defaultSeedMaxTokens
	}
	// Phase 1 uses a fraction of the budget; output is small
	phase1Tokens := mt / 8
	if phase1Tokens < 1024 {
		phase1Tokens = 1024
	}

	content, err := s.chatWithJSONRetry(ctx, prompt, phase1Tokens)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	if s.log != nil {
		s.log.DebugContext(ctx, logger.CatSimulation, "extractChunk: LLM response received", "content_len", len(content))
	}

	ext, err := parseBasicExtraction(content)
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

// extractCharacters calls LLM with merged entity context to extract character-level data (Phase 2).
// Extracts suggested_agents, lifecycle_events, and initial_relationships.
// Uses the full token budget to avoid truncation.
func (s *SeedExtractor) extractCharacters(ctx context.Context, seedText string, merged *SeedExtraction, simulatedHours int) (*SeedExtraction, error) {
	if s.log != nil {
		s.log.InfoContext(ctx, logger.CatSimulation, "extractCharacters: calling LLM with merged context",
			"entities", len(merged.Entities), "topics", len(merged.KeyTopics))
	}

	prompt := buildCharacterExtractionPrompt(seedText, merged, simulatedHours)

	mt := s.maxTokens
	if mt <= 0 {
		mt = defaultSeedMaxTokens
	}
	// Phase 2 uses the majority of the budget for character + relationship data
	phase2Tokens := mt * 3 / 4
	if phase2Tokens < 4096 {
		phase2Tokens = 4096
	}

	content, err := s.chatWithJSONRetry(ctx, prompt, phase2Tokens)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	if s.log != nil {
		s.log.DebugContext(ctx, logger.CatSimulation, "extractCharacters: LLM response received", "content_len", len(content))
	}

	ext, err := parseCharacterExtraction(content)
	if err != nil {
		return nil, fmt.Errorf("parse character extraction: %w", err)
	}

	if s.log != nil {
		s.log.InfoContext(ctx, logger.CatSimulation, "extractCharacters: parsed OK",
			"agents", len(ext.SuggestedAgents), "lifecycle", len(ext.LifecycleEvents), "relationships", len(ext.InitialRelationships))
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

	// Merge InitialRelationships (dedup by subject+target)
	relSeen := make(map[string]bool)
	var mergedRels []InitialRelationship
	for _, rel := range append(a.InitialRelationships, b.InitialRelationships...) {
		key := rel.SubjectName + "->" + rel.TargetName + ":" + string(rel.Kind)
		if !relSeen[key] {
			relSeen[key] = true
			mergedRels = append(mergedRels, rel)
		}
	}
	a.InitialRelationships = mergedRels

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

// parseBasicExtraction parses Phase 1 output (entities, world_state, key_topics, conflict_areas).
func parseBasicExtraction(content string) (*SeedExtraction, error) {
	return parseExtraction(content)
}

// parseCharacterExtraction parses Phase 2 output (suggested_agents, lifecycle_events, initial_relationships).
func parseCharacterExtraction(content string) (*SeedExtraction, error) {
	return parseExtraction(content)
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

// buildBasicExtractionPrompt creates the Phase 1 prompt for entities + world state + topics + conflicts.
func buildBasicExtractionPrompt(text string) string {
	var b strings.Builder
	b.WriteString("Analyze the following text and extract structured information.\n\n")
	b.WriteString("Output valid JSON with these fields:\n")
	b.WriteString("- `entities`: array of {name, type, confidence, relations[{target_name, rel_type, weight}]}\n")
	b.WriteString("  Type must be one of: person, location, faction, concept, organization, technology, product\n")
	b.WriteString("  Include all locations (cities, mountains, valleys, realms, sects) as type \"location\".\n")
	b.WriteString("  Include all factions, sects, alliances as type \"faction\".\n")
	b.WriteString("  rel_type must be one of: mention, agree, rebuttal, propose\n")
	b.WriteString("- `world_state`: MUST be a JSON object (NOT a string). Flat key-value pairs describing the world setting, e.g. {\"era\": \"玄黄纪元\", \"location\": \"永宁城\"}. If no world state, return {}.\n")
	b.WriteString("- `key_topics`: array of main topic strings (max 3)\n")
	b.WriteString("- `conflict_areas`: array of debated or controversial aspects (max 3)\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Extract ALL entities that are meaningful to the world, not just debatable ones.\n")
	b.WriteString("- `world_state` MUST be a JSON object {} with key-value pairs. NEVER use a string.\n")
	b.WriteString("- Keep key_topics specific enough to capture the story premise.\n\n")
	b.WriteString("Text:\n")
	b.WriteString(text)

	return b.String()
}

// buildCharacterExtractionPrompt creates the Phase 2 prompt for characters + relationships.
// It receives the merged basic extraction as context so the LLM knows all entities.
func buildCharacterExtractionPrompt(seedText string, merged *SeedExtraction, simulatedHours int) string {
	var b strings.Builder
	b.WriteString("You previously extracted the following entities and world context from a story seed.\n\n")

	b.WriteString("## Extracted Entities\n")
	for _, e := range merged.Entities {
		relations := ""
		if len(e.Relations) > 0 {
			var rels []string
			for _, r := range e.Relations {
				rels = append(rels, fmt.Sprintf("%s(%s)", r.TargetName, r.RelType))
			}
			relations = " [→ " + strings.Join(rels, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("- %s (type: %s, confidence: %.1f)%s\n", e.Name, e.Type, e.Confidence, relations))
	}

	b.WriteString("\n## World State\n")
	for k, v := range merged.WorldState {
		b.WriteString(fmt.Sprintf("- %s: %v\n", k, v))
	}

	b.WriteString("\n## Key Topics\n")
	for _, t := range merged.KeyTopics {
		b.WriteString(fmt.Sprintf("- %s\n", t))
	}

	// Tell the LLM the total simulation duration so it can map narrative
	// timeline events ("卷二登场" etc.) to simulation clock offsets.
	if simulatedHours > 0 {
		b.WriteString(fmt.Sprintf("\n## Simulation Duration\n"))
		b.WriteString(fmt.Sprintf("The total simulation will run for **%d hours** (approximately %.1f days).\n", simulatedHours, float64(simulatedHours)/24.0))
		b.WriteString("When extracting lifecycle_events, map narrative timeline markers to sim_time triggers:\n")
		b.WriteString("- Events at the very start of the story → trigger \"0h\" or omit (start as agent)\n")
		b.WriteString(fmt.Sprintf("- Events in the first quarter of the narrative → trigger \"%dh\"\n", simulatedHours/4))
		b.WriteString(fmt.Sprintf("- Events at the midpoint → trigger \"%dh\"\n", simulatedHours/2))
		b.WriteString(fmt.Sprintf("- Events in the final quarter → trigger \"%dh\"\n", simulatedHours*3/4))
		b.WriteString("- Use sim_time trigger values like \"1h\", \"24h\", \"48h\" etc.\n")
		b.WriteString("- For conditional events (e.g. \"when two characters meet\"), use trigger \"condition\" with a descriptive value.\n\n")
	}

	// Include a compressed version of the seed text for context
	compressed := seedText
	if len(seedText) > 3000 {
		compressed = seedText[:3000] + "\n...[truncated]..."
	}
	b.WriteString("\n## Original Seed Text (for reference)\n")
	b.WriteString(compressed)
	b.WriteString("\n\n")

	b.WriteString("Based on the above, extract character-level information. Output valid JSON with ONLY these fields:\n\n")
	b.WriteString("- `suggested_agents`: array of objects representing specific characters. Each MUST have: `name`, `role`, `description`, `traits` (array of strings). Extract ALL named characters.\n")
	b.WriteString("- `lifecycle_events`: array of scheduled events. Each MUST have: `type` (\"agent_spawn\"|\"agent_death\"|\"simulation_end\"), `agent_name`, `agent_role` (for spawn), `trigger` (\"sim_time\"|\"wall_time\"|\"condition\"), `trigger_value`, `reason`. Only extract if explicitly stated in text.\n")
	b.WriteString("- `initial_relationships`: array of social relationships between characters. Each MUST have: `subject_name`, `target_name`, `kind` (one of: parent, child, sibling, spouse, friend, rival, colleague, mentor, mentee, neighbor), and optionally `familiarity` (0.0-1.0) and `affinity` (-1.0 to 1.0).\n")
	b.WriteString("  Directional kinds (parent, child, mentor, mentee): subject_name IS the parent/mentor, target_name IS the child/mentee.\n")
	b.WriteString("  Extract from text clues: \"父子\" → kind=parent (subject=father), \"兄弟\" → kind=sibling, \"师徒\" → kind=mentor (subject=master), \"仇敌\" → kind=rival, \"夫妻\" → kind=spouse.\n\n")
	b.WriteString("IMPORTANT:\n")
	b.WriteString("- Use the EXACT character names from the entities above. Do not invent new names.\n")
	b.WriteString("- Extract ALL relationships explicitly stated or implied in the seed text.\n")
	b.WriteString("- For directional relationships, set subject_name correctly (e.g. for \"杨烨是杨凡的父亲\", use subject_name=\"杨烨\", target_name=\"杨凡\", kind=\"parent\").\n\n")
	b.WriteString("Output ONLY valid JSON, no markdown fences.")

	return b.String()
}
