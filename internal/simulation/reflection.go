package simulation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ReflectionEngine periodically generates high-level abstractions from an agent's
// memory stream, inspired by the Generative Agents paper.
type ReflectionEngine struct {
	llm           agent.LLMClient
	model         string
	providerID    string
	log           *logger.Logger
	intervalTicks int // how many ticks between reflection cycles
	maxTokens     int // 0 = use default (1024)
}

// NewReflectionEngine creates a reflection engine.
func NewReflectionEngine(llm agent.LLMClient, model, providerID string, intervalTicks int) *ReflectionEngine {
	if intervalTicks <= 0 {
		intervalTicks = 50
	}
	return &ReflectionEngine{
		llm:           llm,
		model:         model,
		providerID:    providerID,
		intervalTicks: intervalTicks,
	}
}

// SetMaxTokens overrides max_tokens for LLM calls.
func (re *ReflectionEngine) SetMaxTokens(n int) {
	if n > 0 {
		re.maxTokens = n
	}
}

// SetLogger sets the logger.
func (re *ReflectionEngine) SetLogger(log *logger.Logger) {
	re.log = log
}

// ShouldReflect returns true if the agent should run a reflection cycle based on the
// number of ticks since their last reflection.
func (re *ReflectionEngine) ShouldReflect(ticksSinceLast int) bool {
	return ticksSinceLast >= re.intervalTicks
}

// Reflect generates high-level reflections from an agent's recent memories.
// Returns the reflection text and any extracted entities for KG indexing.
func (re *ReflectionEngine) Reflect(
	ctx context.Context,
	persona *Persona,
	memory *AgentMemory,
	clock *SimClock,
) (*ReflectionRecord, error) {
	records := memory.Records()
	if len(records) == 0 {
		return nil, nil
	}

	// Sample the most recent records (last 30, or all if fewer)
	start := 0
	if len(records) > 30 {
		start = len(records) - 30
	}
	recent := records[start:]

	prompt := buildReflectionPrompt(persona, recent, clock.Now())

	if re.log != nil {
		re.log.InfoContext(ctx, logger.CatSimulation, "reflection: generating for agent",
			"agent_id", persona.ID, "memory_count", len(recent))
	}

	mt := re.maxTokens
	if mt <= 0 {
		mt = 16384
	}
	reflectionTokens := mt / 8
	if reflectionTokens < 1024 {
		reflectionTokens = 1024
	}
	resp, err := re.llm.Chat(ctx, agent.LLMRequest{
		Model:      re.model,
		ProviderID: re.providerID,
		Messages:   []agent.LLMMessage{{Role: "user", Content: prompt}},
		MaxTokens:  reflectionTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("reflection LLM: %w", err)
	}

	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return nil, nil
	}

	record := &ReflectionRecord{
		AgentID:     persona.ID,
		Content:     content,
		GeneratedAt: clock.Now(),
		Importance:  computeReflectionImportance(recent),
	}

	// Track which memory rounds inspired this reflection
	for _, r := range recent {
		record.Sources = append(record.Sources, r.Round)
	}

	return record, nil
}

func buildReflectionPrompt(persona *Persona, recentRecords []MemoryRecord, now time.Time) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("You are %s. You are reflecting on your recent experiences.\n\n", persona.Name))
	b.WriteString(fmt.Sprintf("Current time: %s.\n\n", now.Format("15:04")))

	b.WriteString("Below are your recent observations and actions. Based on these, generate 3-5 high-level reflections about your situation, relationships, and goals.\n\n")

	b.WriteString("Recent experiences:\n")
	for _, r := range recentRecords {
		summary := r.Content
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		b.WriteString(fmt.Sprintf("- [%s] %s: %s\n", r.RecordType, r.Role, summary))
	}
	b.WriteString("\n")

	b.WriteString("Generate reflections that answer these questions:\n")
	b.WriteString("1. What patterns do you notice in your recent experiences?\n")
	b.WriteString("2. How are your relationships with other people evolving?\n")
	b.WriteString("3. What should you prioritize next based on what has happened?\n")
	b.WriteString("4. Are your goals still aligned with your current situation?\n\n")

	b.WriteString("Write your reflections in first person, as if thinking to yourself. Be insightful and honest.\n")
	b.WriteString("Each reflection should be 1-2 sentences. Output ONLY the reflections, one per line, no numbering.\n")

	return b.String()
}

// computeReflectionImportance scores a reflection based on the importance of its
// constituent memories, following the Generative Agents paper's approach of
// weighting reflections by the significance of the memories that inspired them.
func computeReflectionImportance(recentRecords []MemoryRecord) float64 {
	if len(recentRecords) == 0 {
		return 5.0
	}

	var total float64
	var count int
	for _, r := range recentRecords {
		if r.Importance > 0 {
			total += r.Importance
			count++
		}
	}

	if count == 0 {
		// All memories have default importance; use median default
		return 5.0
	}

	avg := total / float64(count)

	// Reflections are generally more important than single memories,
	// so we apply a slight boost (1.2x) capped at 10.0.
	boosted := avg * 1.2
	if boosted > 10.0 {
		boosted = 10.0
	}
	return boosted
}
