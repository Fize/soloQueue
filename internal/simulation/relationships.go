package simulation

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// RelationshipManager tracks agent-to-agent relationships within the simulation.
// Unlike the external keyword-matching RelationGraph, this models how each agent
// internally perceives other agents (familiarity, affinity, tags).
type RelationshipManager struct {
	relationships map[string]*AgentRelationship // key: "subjectID:targetID"
	mu            sync.RWMutex
}

// NewRelationshipManager creates a relationship manager.
func NewRelationshipManager() *RelationshipManager {
	return &RelationshipManager{
		relationships: make(map[string]*AgentRelationship),
	}
}

// relationshipKey creates a deterministic key.
func relationshipKey(subject, target string) string {
	return subject + ":" + target
}

// Get returns the subject agent's perception of the target agent.
func (rm *RelationshipManager) Get(subjectID, targetID string) *AgentRelationship {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.relationships[relationshipKey(subjectID, targetID)]
}

// Set updates or creates a relationship from the subject's perspective.
func (rm *RelationshipManager) Set(subjectID, targetID string, familiarity, affinity float64, tags []string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	key := relationshipKey(subjectID, targetID)
	rel, exists := rm.relationships[key]
	if exists {
		// Smooth update: average familiarity, blend affinity
		rel.Familiarity = (rel.Familiarity + familiarity) / 2.0
		rel.Affinity = (rel.Affinity + affinity) / 2.0
		rel.Tags = mergeTags(rel.Tags, tags)
		rel.LastUpdated = time.Now()
	} else {
		rm.relationships[key] = &AgentRelationship{
			SubjectID:   subjectID,
			TargetID:    targetID,
			Familiarity: familiarity,
			Affinity:    affinity,
			Tags:        tags,
			LastUpdated: time.Now(),
		}
	}
}

// RemoveSubject removes all relationships where the given agent is either subject or target.
func (rm *RelationshipManager) RemoveSubject(agentID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for key, rel := range rm.relationships {
		if rel.SubjectID == agentID || rel.TargetID == agentID {
			delete(rm.relationships, key)
		}
	}
}

// BoostFamiliarity increases familiarity by a small delta on each interaction.
func (rm *RelationshipManager) BoostFamiliarity(subjectID, targetID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	key := relationshipKey(subjectID, targetID)
	rel, exists := rm.relationships[key]
	if exists {
		rel.Familiarity = min(1.0, rel.Familiarity+0.05)
		rel.LastUpdated = time.Now()
	} else {
		rm.relationships[key] = &AgentRelationship{
			SubjectID:   subjectID,
			TargetID:    targetID,
			Familiarity: 0.05,
			Affinity:    0,
			LastUpdated: time.Now(),
		}
	}
}

// UpdateAffinity adjusts affinity based on interaction sentiment.
func (rm *RelationshipManager) UpdateAffinity(subjectID, targetID string, delta float64) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	key := relationshipKey(subjectID, targetID)
	rel, exists := rm.relationships[key]
	if exists {
		rel.Affinity = clamp(rel.Affinity+delta, -1.0, 1.0)
		rel.LastUpdated = time.Now()
	} else {
		rm.relationships[key] = &AgentRelationship{
			SubjectID:   subjectID,
			TargetID:    targetID,
			Familiarity: 0.05,
			Affinity:    clamp(delta, -1.0, 1.0),
			LastUpdated: time.Now(),
		}
	}
}

// ParseRelationshipUpdate extracts relationship changes from an agent's response.
// Looks for directives like:
//   [RELATION target: familiarity=0.8, affinity=+0.2, tags=reliable,friendly]
func ParseRelationshipUpdate(agentID, content string) []struct {
	TargetID    string
	Familiarity float64
	Affinity    float64
	Tags        []string
} {
	var updates []struct {
		TargetID    string
		Familiarity float64
		Affinity    float64
		Tags        []string
	}

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[RELATION ") || !strings.HasSuffix(trimmed, "]") {
			continue
		}
		inner := strings.TrimPrefix(trimmed, "[RELATION ")
		inner = strings.TrimSuffix(inner, "]")

		parts := strings.SplitN(inner, ":", 2)
		if len(parts) != 2 {
			continue
		}
		target := strings.TrimSpace(parts[0])
		params := strings.TrimSpace(parts[1])

		update := struct {
			TargetID    string
			Familiarity float64
			Affinity    float64
			Tags        []string
		}{TargetID: target}

		for _, param := range strings.Split(params, ",") {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "familiarity=") {
				fmt.Sscanf(strings.TrimPrefix(param, "familiarity="), "%f", &update.Familiarity)
			} else if strings.HasPrefix(param, "affinity=") {
				fmt.Sscanf(strings.TrimPrefix(param, "affinity="), "%f", &update.Affinity)
			} else if strings.HasPrefix(param, "tags=") {
				tagStr := strings.TrimPrefix(param, "tags=")
				update.Tags = strings.Split(tagStr, "|")
			}
		}
		updates = append(updates, update)
	}

	return updates
}

// FormatForPrompt generates a relationship summary for prompt injection.
func (rm *RelationshipManager) FormatForPrompt(agentID string, personaNameByID map[string]string) string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var b strings.Builder
	b.WriteString("## My Relationships\n\n")
	hasRelations := false

	for key, rel := range rm.relationships {
		if !strings.HasPrefix(key, agentID+":") {
			continue
		}
		hasRelations = true
		name := rel.TargetID
		if displayName, ok := personaNameByID[rel.TargetID]; ok {
			name = displayName
		}

		b.WriteString(fmt.Sprintf("- **%s**: familiarity=%.2f, affinity=%.2f", name, rel.Familiarity, rel.Affinity))
		if len(rel.Tags) > 0 {
			b.WriteString(fmt.Sprintf(", tags=%s", strings.Join(rel.Tags, ", ")))
		}
		b.WriteString("\n")
	}

	if !hasRelations {
		b.WriteString("(You haven't formed relationships with anyone yet.)\n")
	}

	return b.String()
}

func mergeTags(existing, new []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, t := range existing {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	for _, t := range new {
		if !seen[t] {
			seen[t] = true
			result = append(result, t)
		}
	}
	return result
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
