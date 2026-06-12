package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
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
}

// NewPersonaGenerator creates a new PersonaGenerator.
func NewPersonaGenerator(llm agent.LLMClient, model, providerID string, mem *memoryengine.Engine) *PersonaGenerator {
	return &PersonaGenerator{llm: llm, model: model, providerID: providerID, memoryEngine: mem}
}

// Generate creates persona definitions from KG entities when available,
// falling back to extraction-based generation when memory engine is nil.
func (g *PersonaGenerator) Generate(ctx context.Context, extraction *SeedExtraction, topic string, count int) ([]Persona, error) {
	if g.memoryEngine != nil {
		return g.generateFromKG(ctx, extraction, topic, count)
	}
	return g.generateLegacy(ctx, extraction, topic, count)
}

// generateFromKG creates personas directly from knowledge graph entity nodes.
//
// Each entity node becomes a persona candidate grounded in actual data rather
// than LLM imagination. The LLM enriches each entity with a detailed character
// profile (bio, persona, age, gender, MBTI, profession, country).
func (g *PersonaGenerator) generateFromKG(ctx context.Context, extraction *SeedExtraction, topic string, count int) ([]Persona, error) {
	if count < 2 {
		count = 2
	}
	if count > 50 {
		count = 50
	}

	entities, err := g.memoryEngine.Graph().ListEntities(ctx, 100)
	if err != nil {
		return nil, fmt.Errorf("list KG entities: %w", err)
	}

	selected := g.selectPersonaEntities(entities, extraction, count)
	if len(selected) == 0 {
		return nil, fmt.Errorf("no suitable entities found in knowledge graph for persona generation")
	}

	var personas []Persona
	for _, entity := range selected {
		entityCtx := g.buildEntityContext(ctx, entity)
		prompt := buildEntityPersonaPrompt(entity, entityCtx, topic, extraction)

		resp, err := g.llm.Chat(ctx, agent.LLMRequest{
			Model:        g.model,
			ProviderID:   g.providerID,
			Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
			MaxTokens:    2048,
			ResponseJSON: true,
		})
		if err != nil {
			return nil, fmt.Errorf("llm chat for entity %q: %w", entity.Name, err)
		}

		entry, err := parseSinglePersonaEntry(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("parse persona for entity %q: %w", entity.Name, err)
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
			p.Goals = []string{"Discuss the topic from your perspective"}
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
		personas[i].SystemPrompt = BuildSimulationSystemPrompt(personas[i], topic, personas)
	}

	if len(personas) < 2 {
		return nil, fmt.Errorf("generated only %d personas, need at least 2", len(personas))
	}

	return personas, nil
}

// generateLegacy is the pre-KG persona generation path, kept for backward
// compatibility when no memory engine is available.
func (g *PersonaGenerator) generateLegacy(ctx context.Context, extraction *SeedExtraction, topic string, count int) ([]Persona, error) {
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

	kgContext := g.buildKGContext(ctx, extraction)
	prompt := buildPersonaGenPrompt(extraction, topic, count, kgContext)

	resp, err := g.llm.Chat(ctx, agent.LLMRequest{
		Model:        g.model,
		ProviderID:   g.providerID,
		Messages:     []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:    3072,
		ResponseJSON: true,
	})
	if err != nil {
		return nil, fmt.Errorf("llm chat: %w", err)
	}

	result, err := parsePersonaGenResult(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse persona gen: %w", err)
	}

	personas, err := g.buildPersonas(result, extraction, topic)
	if err != nil {
		return nil, fmt.Errorf("build personas: %w", err)
	}

	return personas, nil
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

	nodes, _, err := g.memoryEngine.Graph().BFS(ctx, entity.Name, 1, 8)
	if err == nil && len(nodes) > 1 {
		b.WriteString("Connected entities:\n")
		for _, n := range nodes {
			if n.Name != entity.Name {
				b.WriteString(fmt.Sprintf("  - %s (%s)\n", n.Name, n.Type))
			}
		}
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
func buildEntityPersonaPrompt(entity memoryengine.GraphNode, entityCtx, topic string, extraction *SeedExtraction) string {
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
	b.WriteString("- All fields must have values. age must be an integer. gender must be one of: male, female, other.\n\n")

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
func (g *PersonaGenerator) buildPersonas(result *PersonaGenResult, extraction *SeedExtraction, topic string) ([]Persona, error) {
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

		// Goals
		p.Goals = entry.Goals
		if len(p.Goals) == 0 {
			p.Goals = []string{"Discuss the topic from your perspective"}
		}

		// Generate system prompt using existing builder
		// We need personas for the prompt builder; since we're building all at once,
		// create a partial list for cross-references
		allPersonas := buildPartialPersonaList(result.Personas)
		p.SystemPrompt = BuildSimulationSystemPrompt(p, topic, allPersonas)

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

func buildPersonaGenPrompt(extraction *SeedExtraction, topic string, count int, kgContext string) string {
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
			b.WriteString(fmt.Sprintf("- Name: %s\n  Role: %s\n  Description: %s\n  Traits: %s\n\n", sa.Name, sa.Role, sa.Description, strings.Join(sa.Traits, ", ")))
		}
	}

	b.WriteString("\nConstraints:\n")
	if len(extraction.SuggestedAgents) > 0 {
		b.WriteString(fmt.Sprintf("- Output exactly the %d personas listed under 'Suggested Agents' above, matching their names, roles, descriptions, and traits as the base. You should map their list of traits into key-value pairs in the 'traits' map (e.g. including key-values for personality traits) and complete their goals.\n", count))
	} else {
		b.WriteString(fmt.Sprintf("- Output exactly %d personas\n", count))
		b.WriteString("- At least one persona must be a contrarian/skeptic\n")
		b.WriteString("- At least one persona must be a mediator/moderator\n")
	}
	b.WriteString("- Each persona gets a unique stance (pro/con/neutral) toward each entity\n")
	b.WriteString("- Personas should cover diverse perspectives\n")
	b.WriteString("- Assign each persona a 4-letter MBTI type (e.g. INTP, ENFJ, ISTJ) that matches their role and traits\n\n")

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
