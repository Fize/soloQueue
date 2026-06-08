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
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Role            string            `json:"role"`
	Goals           []string          `json:"goals"`
	Traits          map[string]string `json:"traits"`
	StancePerEntity map[string]string `json:"stance_per_entity"`
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
	memoryEngine *memoryengine.Engine // nil = skip KG enhancement
}

// NewPersonaGenerator creates a new PersonaGenerator.
func NewPersonaGenerator(llm agent.LLMClient, model string, mem *memoryengine.Engine) *PersonaGenerator {
	return &PersonaGenerator{llm: llm, model: model, memoryEngine: mem}
}

// Generate creates N persona definitions from the extraction data.
func (g *PersonaGenerator) Generate(ctx context.Context, extraction *SeedExtraction, topic string, count int) ([]Persona, error) {
	if count < 2 {
		count = 2
	}
	if count > 5 {
		count = 5
	}
	if extraction == nil {
		return nil, fmt.Errorf("extraction is nil")
	}

	// Build enriched context from KG
	kgContext := g.buildKGContext(ctx, extraction)

	prompt := buildPersonaGenPrompt(extraction, topic, count, kgContext)

	resp, err := g.llm.Chat(ctx, agent.LLMRequest{
		Model:        g.model,
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

// buildKGContext enriches the generation prompt with KG data if available.
func (g *PersonaGenerator) buildKGContext(ctx context.Context, extraction *SeedExtraction) string {
	if g.memoryEngine == nil || len(extraction.Entities) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Knowledge Graph context:\n")

	// For top entities, BFS traverse to find connections
	maxTraverse := 5
	if len(extraction.Entities) < maxTraverse {
		maxTraverse = len(extraction.Entities)
	}

	for i := 0; i < maxTraverse; i++ {
		entity := extraction.Entities[i]
		// RecallEntity wraps Search; use BFS from the graph for richer traversal
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
			ID:   entry.ID,
			Name: entry.Name,
			Role: entry.Role,
		}

		// Build traits from stance info
		if p.Traits == nil {
			p.Traits = make(map[string]string)
		}
		for k, v := range entry.Traits {
			p.Traits[k] = v
		}
		// Record stance per entity as traits
		for entity, stance := range entry.StancePerEntity {
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
			SystemPrompt: fmt.Sprintf("Stance on %d topics", len(e.StancePerEntity)),
		})
	}
	return out
}

// --- parse ---

func parsePersonaGenResult(content string) (*PersonaGenResult, error) {
	cleaned := strings.TrimSpace(content)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

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

	b.WriteString("\nConstraints:\n")
	b.WriteString(fmt.Sprintf("- Output exactly %d personas\n", count))
	b.WriteString("- At least one persona must be a contrarian/skeptic\n")
	b.WriteString("- At least one persona must be a mediator/moderator\n")
	b.WriteString("- Each persona gets a unique stance (pro/con/neutral) toward each entity\n")
	b.WriteString("- Personas should cover diverse perspectives\n\n")

	b.WriteString("Output valid JSON with the following structure:\n")
	b.WriteString(`{
  "personas": [
    {
      "id": "unique-id",
      "name": "Display Name",
      "role": "contrarian | mediator | advocate | neutral",
      "goals": ["goal 1", "goal 2"],
      "traits": {"trait_key": "trait_value"},
      "stance_per_entity": {"entity_name": "pro|con|neutral"}
    }
  ]
}`)

	return b.String()
}
