package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

// PersonaGenEntry is the LLM output for one persona.
type PersonaGenEntry struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Role            string          `json:"role"`
	Goals           []string        `json:"goals"`
	Traits          json.RawMessage `json:"traits"`
	MBTI            string          `json:"mbti,omitempty"`
	Age             int             `json:"age"`
	Gender          string          `json:"gender"`
	Country         string          `json:"country,omitempty"`
	Profession      string          `json:"profession,omitempty"`
	Bio             string          `json:"bio"`
	Persona         string          `json:"persona"`
	StancePerEntity json.RawMessage `json:"stance_per_entity"`
}

// PersonaGenResult wraps the LLM response.
type PersonaGenResult struct {
	Personas []PersonaGenEntry `json:"personas"`
}

// PersonaGenerator generates personas from seed extraction data.
// It optionally uses the MemoryEngine KG for enriched context.
type PersonaGenerator struct {
	llm          agent.LLMClient
	model        string
	providerID   string
	memoryEngine *memoryengine.Engine // nil = skip KG enhancement
	maxTokens    int                  // 0 = use API default
	log          *logger.Logger
}

// NewPersonaGenerator creates a new PersonaGenerator.
func NewPersonaGenerator(llm agent.LLMClient, model, providerID string, mem *memoryengine.Engine) *PersonaGenerator {
	return &PersonaGenerator{llm: llm, model: model, providerID: providerID, memoryEngine: mem}
}

func (g *PersonaGenerator) SetLogger(log *logger.Logger) { g.log = log }

// SetMaxTokens overrides the default max_tokens for LLM calls.
// If n > 0, it is used directly. If n <= 0 or not called, defaults to 16384
// (matching the compiled default) instead of relying on the LLM API default,
// which may be too low for multilingual persona generation.
func (g *PersonaGenerator) SetMaxTokens(n int) {
	if n > 0 {
		g.maxTokens = n
	}
}

// chatWithJSONRetry calls the LLM and, if JSON parsing fails, retries once with
// a fix instruction. Returns the raw content on success (caller still parses).
func (g *PersonaGenerator) chatWithJSONRetry(ctx context.Context, prompt string, maxTokens int) (string, error) {
	resp, err := g.llm.Chat(ctx, agent.LLMRequest{
		Model:        g.model,
		ProviderID:   g.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    maxTokens,
		ResponseJSON: true,
	})
	if err != nil {
		return "", err
	}
	if resp.FinishReason == llm.FinishLength {
		if g.log != nil {
			g.log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: LLM response truncated (max_tokens)",
				"content_len", len(resp.Content), "usage", resp.Usage)
		}
	}

	// Try parsing; on failure, retry once with a fix instruction
	_, parseErr := parsePersonaGenResult(resp.Content)
	if parseErr == nil {
		return resp.Content, nil
	}

	if g.log != nil {
		g.log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: first parse failed, retrying",
			"err", parseErr.Error())
	}

	retryPrompt := prompt + fmt.Sprintf("\n\n[SYSTEM] Your previous JSON response was invalid: %s\nPlease fix the JSON syntax and output ONLY valid JSON. Common issues to check:\n- Every object key-value pair must be separated by a comma\n- No trailing commas before closing ] or }\n- All strings must be properly quoted with double quotes\n- All brackets and braces must be balanced\n", parseErr.Error())

	retryResp, retryErr := g.llm.Chat(ctx, agent.LLMRequest{
		Model:        g.model,
		ProviderID:   g.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: retryPrompt}},
		MaxTokens:    maxTokens,
		ResponseJSON: true,
	})
	if retryErr != nil {
		return "", fmt.Errorf("retry after parse error: %w (original: %w)", retryErr, parseErr)
	}
	if retryResp.FinishReason == llm.FinishLength {
		if g.log != nil {
			g.log.WarnContext(ctx, logger.CatSimulation, "chatWithJSONRetry: retry response truncated",
				"content_len", len(retryResp.Content))
		}
	}
	return retryResp.Content, nil
}

const defaultPersonaGenMaxTokens = 16384

// personaGenBatchSize is the maximum number of personas to generate per LLM
// call. Generating too many personas in a single call causes two problems:
// 1. Response truncation (finish=length) when output exceeds max_tokens
// 2. JSON structural errors (the LLM makes mistakes in long, complex output)
// Batching keeps each call small enough to avoid both issues.
const personaGenBatchSize = 5

// Generate creates persona definitions from KG entities when available,
// falling back to extraction-based generation when memory engine is nil.
func (g *PersonaGenerator) Generate(ctx context.Context, extraction *SeedExtraction, topic string, count int, language string) ([]Persona, error) {
	if g.memoryEngine != nil {
		return g.generateFromKG(ctx, extraction, topic, count, language)
	}
	return g.generateLegacy(ctx, extraction, topic, count, language)
}

// generateFromKG creates personas directly from knowledge graph entity nodes.
func (g *PersonaGenerator) generateFromKG(ctx context.Context, extraction *SeedExtraction, topic string, count int, language string) ([]Persona, error) {
	if count < 2 {
		count = 2
	}
	if count > 50 {
		count = 50
	}

	if g.log != nil {
		g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: listing KG entities")
	}

	entities, err := g.memoryEngine.Graph().ListEntities(ctx, 100)
	if err != nil {
		return nil, fmt.Errorf("list KG entities: %w", err)
	}

	if g.log != nil {
		g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: entities listed", "total", len(entities))
	}

	selected := g.selectPersonaEntities(entities, extraction, count)
	if len(selected) == 0 {
		return nil, fmt.Errorf("no suitable entities found in knowledge graph for persona generation")
	}
	if len(selected) > 20 {
		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: truncating selected entities", "before", len(selected), "after", 20)
		}
		selected = selected[:20]
	}

	if g.log != nil {
		g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: processing entities", "count", len(selected))
	}

	var personas []Persona
	for i, entity := range selected {
		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: processing entity",
				"index", i+1, "total", len(selected), "name", entity.Name, "type", entity.Type)
		}
		entityCtx := g.buildEntityContext(ctx, entity)
		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: entity context built",
				"name", entity.Name, "ctx_len", len(entityCtx))
		}
		prompt := buildEntityPersonaPrompt(entity, entityCtx, topic, extraction, language)

		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: calling LLM for entity", "name", entity.Name)
		}
		maxTokens := g.maxTokens
		if maxTokens <= 0 {
			maxTokens = defaultPersonaGenMaxTokens
		}
		resp, err := g.llm.Chat(ctx, agent.LLMRequest{
			Model:        g.model,
			ProviderID:   g.providerID,
			Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
			MaxTokens:    maxTokens,
			ResponseJSON: true,
		})
		if err != nil {
			return nil, fmt.Errorf("llm chat for entity %q: %w", entity.Name, err)
		}
		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "generateFromKG: LLM done for entity", "name", entity.Name, "resp_len", len(resp.Content))
		}
		if resp.FinishReason == llm.FinishLength {
			g.log.WarnContext(ctx, logger.CatSimulation, "generateFromKG: LLM response truncated (max_tokens)",
				"entity", entity.Name, "content_len", len(resp.Content), "usage", resp.Usage)
		}

		entry, err := parseSinglePersonaEntry(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("parse persona for entity %q (finish=%s, content_len=%d): %w",
				entity.Name, resp.FinishReason, len(resp.Content), err)
		}
		if entry.ID == "" {
			entry.ID = sanitizeID(entity.Name)
		}
		if entry.Name == "" {
			entry.Name = entity.Name
		}

		p := Persona{
			ID:         entry.ID,
			Name:       entry.Name,
			Role:       entry.Role,
			MBTI:       entry.MBTI,
			Age:        entry.Age,
			Gender:     entry.Gender,
			Country:    entry.Country,
			Profession: entry.Profession,
			Bio:        entry.Bio,
			Persona:    entry.Persona,
			Goals:      entry.Goals,
		}
		if len(p.Goals) == 0 {
			if language == "zh" {
				p.Goals = []string{"Discuss the topic from your perspective"}
			} else {
				p.Goals = []string{"Discuss the topic from your perspective"}
			}
		}
		if p.Traits == nil {
			p.Traits = make(map[string]string)
		}
		for k, v := range parseStringMap(entry.Traits) {
			p.Traits[k] = v
		}
		for entity, stance := range parseStringMap(entry.StancePerEntity) {
			p.Traits["stance:"+entity] = stance
		}
		if strings.Contains(strings.ToLower(entry.Role), "mediator") || strings.Contains(strings.ToLower(entry.Role), "moderator") {
			p.Traits["role_type"] = "mediator"
		}
		if strings.Contains(strings.ToLower(entry.Role), "contrarian") || strings.Contains(strings.ToLower(entry.Role), "skeptic") {
			p.Traits["role_type"] = "contrarian"
		}

		personas = append(personas, p)
	}

	for i := range personas {
		personas[i].SystemPrompt = BuildGenerativeAgentSystemPrompt(language, personas[i], personas, nil, nil, nil, nil, nil, nil, nil)
		// Append topic context for backward compatibility with topic-based simulations
		if topic != "" {
			if language == "zh" {
				personas[i].SystemPrompt += fmt.Sprintf("\nThe current thematic background is: %s\n", topic)
			} else {
				personas[i].SystemPrompt += fmt.Sprintf("\nThe current overarching context is: %s\n", topic)
			}
		}
	}

	if len(personas) < 2 {
		return nil, fmt.Errorf("generated only %d personas, need at least 2", len(personas))
	}

	return personas, nil
}

// generateLegacy is the pre-KG persona generation path, kept for backward
// compatibility when no memory engine is available.
func (g *PersonaGenerator) generateLegacy(ctx context.Context, extraction *SeedExtraction, topic string, count int, language string) ([]Persona, error) {
	isDeduced := extraction != nil && len(extraction.SuggestedAgents) > 0
	if count < 2 {
		count = 2
	}
	if isDeduced {
		count = len(extraction.SuggestedAgents)
		if count > 50 {
			count = 50
		}
	} else {
		if count > 5 {
			count = 5
		}
	}
	if extraction == nil {
		return nil, fmt.Errorf("extraction is nil")
	}

	// Batch generation when count exceeds batch size to prevent LLM response
	// truncation (finish=length) and JSON structural errors in long output.
	if count > personaGenBatchSize {
		return g.generateLegacyBatched(ctx, extraction, topic, count, language)
	}

	kgContext := g.buildKGContext(ctx, extraction)
	prompt := buildPersonaGenPrompt(extraction, topic, count, kgContext, language)

	maxTokens := g.maxTokens
	if maxTokens <= 0 {
		maxTokens = defaultPersonaGenMaxTokens
	}
	content, err := g.chatWithJSONRetry(ctx, prompt, maxTokens)
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	result, err := parsePersonaGenResult(content)
	if err != nil {
		return nil, fmt.Errorf("parse persona gen (content_len=%d): %w",
			len(content), err)
	}

	personas, err := g.buildPersonas(result, extraction, topic, language)
	if err != nil {
		return nil, fmt.Errorf("build personas: %w", err)
	}

	return personas, nil
}

// generateLegacyBatched splits persona generation into multiple LLM calls,
// each producing at most personaGenBatchSize personas. This prevents response
// truncation and JSON structural errors that occur when generating many
// personas in a single call. After all batches complete, a second pass
// rebuilds system prompts so each persona knows about all other personas.
func (g *PersonaGenerator) generateLegacyBatched(ctx context.Context, extraction *SeedExtraction, topic string, count int, language string) ([]Persona, error) {
	isDeduced := len(extraction.SuggestedAgents) > 0

	var allPersonas []Persona

	for i := 0; i < count; i += personaGenBatchSize {
		end := i + personaGenBatchSize
		if end > count {
			end = count
		}
		batchSize := end - i

		// Create a modified extraction with only this batch's suggested agents
		batchExtraction := *extraction
		if isDeduced {
			batchExtraction.SuggestedAgents = extraction.SuggestedAgents[i:end]
		}

		kgContext := g.buildKGContext(ctx, &batchExtraction)
		prompt := buildPersonaGenPrompt(&batchExtraction, topic, batchSize, kgContext, language)

		maxTokens := g.maxTokens
		if maxTokens <= 0 {
			maxTokens = defaultPersonaGenMaxTokens
		}

		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "generateLegacy: batch persona generation",
				"batch", fmt.Sprintf("%d-%d/%d", i+1, end, count), "batch_size", batchSize)
		}

		content, err := g.chatWithJSONRetry(ctx, prompt, maxTokens)
		if err != nil {
			return nil, fmt.Errorf("llm chat batch %d-%d: %w", i+1, end, err)
		}

		result, err := parsePersonaGenResult(content)
		if err != nil {
			return nil, fmt.Errorf("parse persona gen batch %d-%d (content_len=%d): %w",
				i+1, end, len(content), err)
		}

		personas, err := g.buildPersonas(result, &batchExtraction, topic, language)
		if err != nil {
			return nil, fmt.Errorf("build personas batch %d-%d: %w", i+1, end, err)
		}

		allPersonas = append(allPersonas, personas...)
	}

	if len(allPersonas) == 0 {
		return nil, fmt.Errorf("no personas generated from any batch")
	}

	// Second pass: rebuild system prompts with the full persona list so each
	// persona knows about all other personas, not just those in its batch.
	for i := range allPersonas {
		allPersonas[i].SystemPrompt = BuildGenerativeAgentSystemPrompt(
			language, allPersonas[i], allPersonas, nil, nil, nil, nil, nil, nil, nil)
		if topic != "" {
			if language == "zh" {
				allPersonas[i].SystemPrompt += fmt.Sprintf("\nThe current thematic background is: %s\n", topic)
			} else {
				allPersonas[i].SystemPrompt += fmt.Sprintf("\nThe current overarching context is: %s\n", topic)
			}
		}
	}

	return allPersonas, nil
}

// selectPersonaEntities filters KG entities to those suitable as personas.
// Entity types like "person", "organization", "publicfigure" etc. are selected.
// Entities are ordered by mention_count descending, capped at count.
// If suggestedAgents are present, only entities matching those names are selected.
func (g *PersonaGenerator) selectPersonaEntities(entities []memoryengine.GraphNode, extraction *SeedExtraction, count int) []memoryengine.GraphNode {
	if extraction != nil && len(extraction.SuggestedAgents) > 0 {
		// Suggested agent mode: filter entities by name match
		agentNames := make(map[string]bool, len(extraction.SuggestedAgents))
		for _, sa := range extraction.SuggestedAgents {
			agentNames[strings.ToLower(sa.Name)] = true
		}
		var matched []memoryengine.GraphNode
		for _, e := range entities {
			if agentNames[strings.ToLower(e.Name)] {
				matched = append(matched, e)
			}
		}
		if len(matched) > count {
			matched = matched[:count]
		}
		return matched
	}

	// Auto mode: select entities with personas-appropriate types
	personaTypes := map[string]bool{
		"person": true, "publicfigure": true, "expert": true,
		"organization": true, "company": true, "institution": true,
		"mediaoutlet": true, "governmentagency": true, "ngo": true,
		"official": true, "journalist": true, "activist": true,
		"student": true, "alumni": true, "professor": true, "faculty": true,
		"group": true, "community": true,
	}

	var selected []memoryengine.GraphNode
	for _, e := range entities {
		if personaTypes[strings.ToLower(e.Type)] {
			selected = append(selected, e)
		}
	}

	if len(selected) > count {
		selected = selected[:count]
	}
	return selected
}

// buildEntityContext builds rich context for an entity by fetching edges and connected nodes.
func (g *PersonaGenerator) buildEntityContext(ctx context.Context, entity memoryengine.GraphNode) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Entity: %s (type: %s, mention_count: %d, confidence: %.2f)\n", entity.Name, entity.Type, entity.MentionCount, entity.Confidence))

	if g.memoryEngine == nil {
		return b.String()
	}

	outEdges, _ := g.memoryEngine.Graph().GetEdgesFrom(ctx, entity.ID, false)
	if len(outEdges) > 0 {
		b.WriteString("Relationships:\n")
		for _, e := range outEdges {
			b.WriteString(fmt.Sprintf("  → %s (%s) weight=%.1f evidence=%q\n", e.TargetName, e.RelType, e.Weight, truncateStr(e.Evidence, 200)))
		}
	}

	inEdges, _ := g.memoryEngine.Graph().GetEdgesTo(ctx, entity.ID, false)
	if len(inEdges) > 0 {
		b.WriteString("Referenced by:\n")
		for _, e := range inEdges {
			b.WriteString(fmt.Sprintf("  %s → %s (%s) weight=%.1f\n", e.SourceName, e.TargetName, e.RelType, e.Weight))
		}
	}

	nodes, edges, err := g.memoryEngine.Graph().BFS(ctx, entity.Name, 1, 8)
	if err == nil && len(nodes) > 1 {
		if g.log != nil {
			g.log.InfoContext(ctx, logger.CatSimulation, "buildEntityContext: BFS done",
				"name", entity.Name, "nodes", len(nodes), "edges", len(edges))
		}
		b.WriteString("Connected entities:\n")
		connected := 0
		for _, n := range nodes {
			if n.Name != entity.Name {
				if connected >= 15 {
					b.WriteString(fmt.Sprintf("  ... and %d more\n", len(nodes)-connected-1))
					break
				}
				b.WriteString(fmt.Sprintf("  - %s (%s)\n", n.Name, n.Type))
				connected++
			}
		}
	}

	if b.Len() > 5000 {
		result := b.String()[:5000] + "...\n[context truncated at 5000 chars]"
		return result
	}
	return b.String()
}

// buildKGContext enriches the extraction-based prompt with KG data via BFS.
func (g *PersonaGenerator) buildKGContext(ctx context.Context, extraction *SeedExtraction) string {
	if g.memoryEngine == nil || len(extraction.Entities) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Knowledge Graph context:\n")

	maxTraverse := 5
	if len(extraction.Entities) < maxTraverse {
		maxTraverse = len(extraction.Entities)
	}

	for i := 0; i < maxTraverse; i++ {
		entity := extraction.Entities[i]
		nodes, edges, err := g.memoryEngine.Graph().BFS(ctx, entity.Name, 2, 10)
		if err != nil || len(nodes) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("- %q connects to:", entity.Name))
		seen := make(map[string]bool)
		for _, edge := range edges {
			target := edge.TargetName
			if edge.SourceName == entity.Name && !seen[target] {
				seen[target] = true
				b.WriteString(fmt.Sprintf(" %s (%s),", target, edge.RelType))
			}
		}
		b.WriteString("\n")
	}

	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

// buildEntityPersonaPrompt creates a detailed persona generation prompt for a
// single KG entity, modeled after MiroFish's OasisProfileGenerator approach.
func buildEntityPersonaPrompt(entity memoryengine.GraphNode, entityCtx, topic string, extraction *SeedExtraction, language string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generate a detailed persona profile for the following entity to serve as a social simulation agent.\n\n"))
	b.WriteString(fmt.Sprintf("Entity name: %s\n", entity.Name))
	b.WriteString(fmt.Sprintf("Entity type: %s\n", entity.Type))
	b.WriteString(fmt.Sprintf("\nDiscussion topic: %s\n", topic))

	if extraction != nil && len(extraction.KeyTopics) > 0 {
		b.WriteString(fmt.Sprintf("Key topics: %s\n", strings.Join(extraction.KeyTopics, ", ")))
	}
	if extraction != nil && len(extraction.ConflictAreas) > 0 {
		b.WriteString(fmt.Sprintf("Conflict areas: %s\n", strings.Join(extraction.ConflictAreas, ", ")))
	}

	b.WriteString("\nEntity context from knowledge graph:\n")
	b.WriteString(entityCtx)
	b.WriteString("\n")

	b.WriteString("Please generate a JSON object with these fields:\n\n")
	b.WriteString("1. id: unique identifier for this persona (lowercase, underscores)\n")
	b.WriteString("2. name: display name for the agent\n")
	b.WriteString("3. role: the agent's role in the discussion (e.g. advocate, skeptic, mediator, expert, official, journalist, concerned citizen)\n")
	b.WriteString("4. bio: short public bio, 200 characters. Describe who this entity is in the real world based on the context\n")
	b.WriteString("5. persona: detailed persona description, 1500+ characters. Must include:\n")
	b.WriteString("   - Basic info (age, profession, background, location if relevant)\n")
	b.WriteString("   - Personality and cognitive style (based on the MBTI type you assign)\n")
	b.WriteString("   - Stance on the topic and key entities (what they believe, why)\n")
	b.WriteString("   - Behavioral patterns (how they discuss, debate style, emotional triggers)\n")
	b.WriteString("   - Unique traits (memorable characteristics, catchphrases, personal experiences)\n")
	b.WriteString("6. age: integer age appropriate for the entity type\n")
	b.WriteString("7. gender: \"male\", \"female\", or \"other\" (for organizational entities)\n")
	b.WriteString("8. mbti: 4-letter MBTI type that matches the entity's personality (e.g. INTP, ENFJ, ISTJ)\n")
	b.WriteString("9. country: country name\n")
	b.WriteString("10. profession: professional role or occupation\n")
	b.WriteString("11. goals: array of 2-4 goals for this discussion\n")
	b.WriteString("12. traits: key-value map of personality/behavioral trait names to STRING values (e.g. {\"persuasiveness\": \"high\", \"technical_depth\": \"expert\"}). Values MUST be strings, not numbers.\n")
	b.WriteString("13. stance_per_entity: map of entity name → \"pro\", \"con\", or \"neutral\"\n\n")

	b.WriteString("IMPORTANT:\n")
	b.WriteString("- Ground the persona in the entity context from the knowledge graph. Do not invent unrelated details.\n")
	b.WriteString("- For individuals (person, expert, official etc.): create a realistic personal profile.\n")
	b.WriteString("- For organizations/institutions (company, university, agency etc.): create a representative account profile. Use gender \"other\".\n")
	b.WriteString("- The persona field must be a single coherent text, 1500+ characters.\n")
	b.WriteString("- All fields must have values. age must be an integer. gender must be one of: male, female, other.\n")
	if language == "zh" {
		b.WriteString("- IMPORTANT: All generated textual fields (name, role, bio, persona, goals, traits values, etc.) MUST be written in Chinese, because the simulation language is set to Chinese. Maintain IDs in lowercase English/underscores.\n")
	}
	b.WriteString("\n")

	b.WriteString("Output ONLY valid JSON, no markdown fences:\n")
	b.WriteString(`{
  "id": "...",
  "name": "...",
  "role": "...",
  "bio": "...",
  "persona": "...",
  "age": 30,
  "gender": "male",
  "mbti": "INTJ",
  "country": "...",
  "profession": "...",
  "goals": [...],
  "traits": {"trait_name": "value"},
  "stance_per_entity": {...}
}`)

	return b.String()
}

// parseSinglePersonaEntry parses a single PersonaGenEntry from LLM response.
func parseSinglePersonaEntry(content string) (PersonaGenEntry, error) {
	cleaned := cleanJSONResponse(content)

	var entry PersonaGenEntry
	if err := json.Unmarshal([]byte(cleaned), &entry); err != nil {
		return entry, fmt.Errorf("json unmarshal: %w\nraw: %s", err, truncateStr(content, 200))
	}
	return entry, nil
}

// parseStringMap converts json.RawMessage to map[string]string, converting
// numeric/bool/null values to their string representations gracefully.
func parseStringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			out[k] = val
		case float64:
			out[k] = fmt.Sprintf("%v", val)
		case bool:
			out[k] = fmt.Sprintf("%v", val)
		case nil:
			out[k] = ""
		default:
			out[k] = fmt.Sprintf("%v", val)
		}
	}
	return out
}

// sanitizeID converts a name into a valid ID.
func sanitizeID(name string) string {
	s := strings.ToLower(name)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
	return strings.Trim(s, "_")
}

// buildPersonas converts LLM output into simulation Persona structs.
func (g *PersonaGenerator) buildPersonas(result *PersonaGenResult, extraction *SeedExtraction, topic string, language string) ([]Persona, error) {
	if len(result.Personas) == 0 {
		return nil, fmt.Errorf("no personas in LLM result")
	}

	var personas []Persona
	for _, entry := range result.Personas {
		if entry.ID == "" || entry.Name == "" {
			continue
		}

		p := Persona{
			ID:         entry.ID,
			Name:       entry.Name,
			Role:       entry.Role,
			MBTI:       entry.MBTI,
			Age:        entry.Age,
			Gender:     entry.Gender,
			Country:    entry.Country,
			Profession: entry.Profession,
			Bio:        entry.Bio,
			Persona:    entry.Persona,
		}

		// Build traits from stance info
		if p.Traits == nil {
			p.Traits = make(map[string]string)
		}
		for k, v := range parseStringMap(entry.Traits) {
			p.Traits[k] = v
		}
		// Record stance per entity as traits
		for entity, stance := range parseStringMap(entry.StancePerEntity) {
			p.Traits["stance:"+entity] = stance
		}
		// Mark mediator/contrarian role
		if strings.Contains(strings.ToLower(entry.Role), "mediator") || strings.Contains(strings.ToLower(entry.Role), "moderator") {
			p.Traits["role_type"] = "mediator"
		}
		if strings.Contains(strings.ToLower(entry.Role), "contrarian") || strings.Contains(strings.ToLower(entry.Role), "skeptic") {
			p.Traits["role_type"] = "contrarian"
		}

		// Goals — prefer seed-extracted goals over LLM-invented ones
		p.Goals = entry.Goals
		// When in deduction mode, override with goals from the seed extraction,
		// which are character-specific and grounded in the narrative state.
		if extraction != nil {
			for _, sa := range extraction.SuggestedAgents {
				if strings.EqualFold(sa.Name, entry.Name) && len(sa.Goals) > 0 {
					p.Goals = sa.Goals
					break
				}
			}
		}
		if len(p.Goals) == 0 {
			if language == "zh" {
				p.Goals = []string{"Discuss the topic from your perspective"}
			} else {
				p.Goals = []string{"Discuss the topic from your perspective"}
			}
		}
		// Generate system prompt using existing builder
		// We need personas for the prompt builder; since we're building all at once,
		// create a partial list for cross-references
		allPersonas := buildPartialPersonaList(result.Personas)
		p.SystemPrompt = BuildGenerativeAgentSystemPrompt(language, p, allPersonas, nil, nil, nil, nil, nil, nil, nil)
		if topic != "" {
			if language == "zh" {
				p.SystemPrompt += fmt.Sprintf("\nThe current thematic background is: %s\n", topic)
			} else {
				p.SystemPrompt += fmt.Sprintf("\nThe current overarching context is: %s\n", topic)
			}
		}

		personas = append(personas, p)
	}

	if len(personas) == 0 {
		return nil, fmt.Errorf("no valid personas after filtering")
	}

	return personas, nil
}

// buildPartialPersonaList creates Persona stubs from PersonaGenEntry for the system prompt builder.
func buildPartialPersonaList(entries []PersonaGenEntry) []Persona {
	out := make([]Persona, 0, len(entries))
	for _, e := range entries {
		out = append(out, Persona{
			ID:           e.ID,
			Name:         e.Name,
			Role:         e.Role,
			MBTI:         e.MBTI,
			Age:          e.Age,
			Gender:       e.Gender,
			Country:      e.Country,
			Profession:   e.Profession,
			Bio:          e.Bio,
			Persona:      e.Persona,
			SystemPrompt: fmt.Sprintf("Stance on %d topics", len(e.StancePerEntity)),
		})
	}
	return out
}

// --- parse ---

func parsePersonaGenResult(content string) (*PersonaGenResult, error) {
	cleaned := cleanJSONResponse(content)

	var result PersonaGenResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w\nraw: %s", err, truncateStr(content, 200))
	}
	return &result, nil
}

// --- prompt ---

func buildPersonaGenPrompt(extraction *SeedExtraction, topic string, count int, kgContext string, language string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generate %d distinct personas for a multi-agent simulation.\n\n", count))
	b.WriteString("Topic: ")
	b.WriteString(topic)
	b.WriteString("\n\n")

	if len(extraction.KeyTopics) > 0 {
		b.WriteString("Key topics: ")
		b.WriteString(strings.Join(extraction.KeyTopics, ", "))
		b.WriteString("\n")
	}
	if len(extraction.ConflictAreas) > 0 {
		b.WriteString("Conflict areas: ")
		b.WriteString(strings.Join(extraction.ConflictAreas, ", "))
		b.WriteString("\n")
	}

	b.WriteString("\nEntities:\n")
	for _, e := range extraction.Entities {
		relations := ""
		if len(e.Relations) > 0 {
			var rels []string
			for _, r := range e.Relations {
				rels = append(rels, fmt.Sprintf("%s(%s)", r.TargetName, r.RelType))
			}
			relations = " [relations: " + strings.Join(rels, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("- %s (%s, confidence: %.1f)%s\n", e.Name, e.Type, e.Confidence, relations))
	}

	if kgContext != "" {
		b.WriteString("\n")
		b.WriteString(kgContext)
	}

	if len(extraction.SuggestedAgents) > 0 {
		b.WriteString("\nSuggested Agents (Deduction Mode):\n")
		b.WriteString("You MUST generate exactly the following personas based on these characters/participants:\n")
		for _, sa := range extraction.SuggestedAgents {
			b.WriteString(fmt.Sprintf("- Name: %s\n  Role: %s\n  Description: %s\n  Traits: %s\n", sa.Name, sa.Role, sa.Description, strings.Join(sa.Traits, ", ")))
			if len(sa.Goals) > 0 {
				b.WriteString(fmt.Sprintf("  Goals: %s\n", strings.Join(sa.Goals, "; ")))
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\nConstraints:\n")
	if len(extraction.SuggestedAgents) > 0 {
		b.WriteString(fmt.Sprintf("- Output exactly the %d personas listed under 'Suggested Agents' above, matching their names, roles, descriptions, and traits as the base.\n", count))
		b.WriteString("- Use each suggested agent's provided Goals directly as the persona's goals. DO NOT invent new goals based on the topic. The goals above are grounded in the narrative — preserve them.\n")
		b.WriteString("- Map the suggested agent's list of traits into key-value pairs in the 'traits' map (e.g. including key-values for personality traits).\n")
	} else {
		b.WriteString(fmt.Sprintf("- Output exactly %d personas\n", count))
		b.WriteString("- At least one persona must be a contrarian/skeptic\n")
		b.WriteString("- At least one persona must be a mediator/moderator\n")
	}
	b.WriteString("- Each persona gets a unique stance (pro/con/neutral) toward each entity\n")
	b.WriteString("- Personas should cover diverse perspectives\n")
	b.WriteString("- Assign each persona a 4-letter MBTI type (e.g. INTP, ENFJ, ISTJ) that matches their role and traits\n")
	if language == "zh" {
		b.WriteString("- IMPORTANT: All generated textual fields (name, role, bio, persona, goals, traits values, etc.) MUST be written in Chinese, because the simulation language is set to Chinese. Maintain IDs in lowercase English/underscores.\n")
	}
	b.WriteString("\n")

	b.WriteString("Output valid JSON with the following structure:\n")
	b.WriteString(`{
  "personas": [
    {
      "id": "unique-id",
      "name": "Display Name",
      "role": "contrarian | mediator | advocate | neutral",
      "goals": ["goal 1", "goal 2"],
      "traits": {"trait_key": "trait_value"},
      "mbti": "INTP",
      "stance_per_entity": {"entity_name": "pro|con|neutral"}
    }
  ]
}`)

	return b.String()
}
