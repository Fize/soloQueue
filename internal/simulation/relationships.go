package simulation

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// RelationshipManager tracks agent-to-agent relationships within the simulation.
// Unlike the external keyword-matching RelationGraph, this models how each agent
// internally perceives other agents (familiarity, affinity, tags, relationship kind).
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
// Kept for backward compatibility; sets Kind to empty (treated as stranger).
func (rm *RelationshipManager) Set(subjectID, targetID string, familiarity, affinity float64, tags []string) {
	rm.SetWithKind(subjectID, targetID, "", familiarity, affinity, tags)
}

// SetWithKind updates or creates a relationship with a specific kind.
func (rm *RelationshipManager) SetWithKind(subjectID, targetID string, kind RelationKind, familiarity, affinity float64, tags []string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	key := relationshipKey(subjectID, targetID)

	// Ensure kind is in tags
	kindStr := string(kind)
	hasKindTag := false
	for _, t := range tags {
		if t == kindStr {
			hasKindTag = true
			break
		}
	}
	if kindStr != "" && !hasKindTag {
		tags = append([]string{kindStr}, tags...)
	}

	rel, exists := rm.relationships[key]
	if exists {
		if kindStr != "" {
			rel.Kind = kind
		}
		rel.Familiarity = (rel.Familiarity + familiarity) / 2.0
		rel.Affinity = (rel.Affinity + affinity) / 2.0
		rel.Tags = mergeTags(rel.Tags, tags)
		rel.LastUpdated = time.Now()
	} else {
		rm.relationships[key] = &AgentRelationship{
			SubjectID:   subjectID,
			TargetID:    targetID,
			Kind:        kind,
			Familiarity: familiarity,
			Affinity:    affinity,
			Tags:        tags,
			LastUpdated: time.Now(),
		}
	}
}

// SetKind updates only the relationship kind without changing familiarity/affinity.
func (rm *RelationshipManager) SetKind(subjectID, targetID string, kind RelationKind) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	key := relationshipKey(subjectID, targetID)
	rel, exists := rm.relationships[key]
	if exists {
		rel.Kind = kind
		rel.LastUpdated = time.Now()
	} else {
		rm.relationships[key] = &AgentRelationship{
			SubjectID:   subjectID,
			TargetID:    targetID,
			Kind:        kind,
			Familiarity: 0.5,
			Affinity:    0,
			LastUpdated: time.Now(),
		}
	}
}

// KindOf returns the relationship kind from subject to target.
// Returns RelationStranger if no relationship exists.
func (rm *RelationshipManager) KindOf(subjectID, targetID string) RelationKind {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	rel, ok := rm.relationships[relationshipKey(subjectID, targetID)]
	if !ok || rel.Kind == "" {
		return RelationStranger
	}
	return rel.Kind
}

// BulkInit initializes multiple relationships from seed extraction data.
// Matches subject/target names to persona IDs for lookup.
func (rm *RelationshipManager) BulkInit(rels []InitialRelationship, personaNameByID map[string]string) error {
	// Build name → ID reverse index
	nameToID := make(map[string]string, len(personaNameByID))
	for id, name := range personaNameByID {
		nameToID[name] = id
	}

	// Also build lowercase index for fuzzy matching
	lowerNameToID := make(map[string]string, len(personaNameByID))
	for _, name := range personaNameByID {
		lowerNameToID[strings.ToLower(name)] = nameToID[name]
	}

	resolveName := func(name string) string {
		if id, ok := nameToID[name]; ok {
			return id
		}
		// Try case-insensitive match
		if id, ok := lowerNameToID[strings.ToLower(name)]; ok {
			return id
		}
		// Try trimming
		if id, ok := nameToID[strings.TrimSpace(name)]; ok {
			return id
		}
		return ""
	}

	for _, rel := range rels {
		subjectID := resolveName(rel.SubjectName)
		if subjectID == "" {
			continue
		}
		targetID := resolveName(rel.TargetName)
		if targetID == "" {
			continue
		}
		if subjectID == targetID {
			continue
		}

		familiarity := rel.Familiarity
		if familiarity <= 0 {
			familiarity = 0.5
		}

		tags := []string{string(rel.Kind)}

		if rel.Kind.IsDirectional() {
			rm.SetWithKind(subjectID, targetID, rel.Kind, familiarity, rel.Affinity, tags)
			// Inverse relationship
			invKind := rel.Kind.InverseKind()
			invFamiliarity := familiarity * 0.9 // slightly less from the other side
			rm.SetWithKind(targetID, subjectID, invKind, invFamiliarity, rel.Affinity*0.8, []string{string(invKind)})
		} else {
			rm.SetWithKind(subjectID, targetID, rel.Kind, familiarity, rel.Affinity, tags)
			rm.SetWithKind(targetID, subjectID, rel.Kind, familiarity, rel.Affinity, tags)
		}
	}
	return nil
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

// AllRelationships returns all relationships as DTOs for API exposure.
// Requires a nameByID lookup to populate subject/target names.
func (rm *RelationshipManager) AllRelationships(nameByID map[string]string) []RelationshipDTO {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	dtos := make([]RelationshipDTO, 0, len(rm.relationships))
	for _, rel := range rm.relationships {
		dto := RelationshipDTO{
			SubjectID:   rel.SubjectID,
			SubjectName: nameByID[rel.SubjectID],
			TargetID:    rel.TargetID,
			TargetName:  nameByID[rel.TargetID],
			Kind:        string(rel.Kind),
			Familiarity: rel.Familiarity,
			Affinity:    rel.Affinity,
			Tags:        rel.Tags,
		}
		// Default kind to stranger if empty
		if dto.Kind == "" {
			dto.Kind = string(RelationStranger)
		}
		dtos = append(dtos, dto)
	}
	return dtos
}

// ParseRelationshipUpdate extracts relationship changes from an agent's response.
// Looks for directives like:
//   [RELATION target: kind=friend, familiarity=0.8, affinity=+0.2, tags=reliable,friendly]
func ParseRelationshipUpdate(agentID, content string) []struct {
	TargetID    string
	Kind        RelationKind
	Familiarity float64
	Affinity    float64
	Tags        []string
} {
	var updates []struct {
		TargetID    string
		Kind        RelationKind
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
			Kind        RelationKind
			Familiarity float64
			Affinity    float64
			Tags        []string
		}{TargetID: target}

		for _, param := range strings.Split(params, ",") {
			param = strings.TrimSpace(param)
			if strings.HasPrefix(param, "kind=") {
				kindStr := strings.TrimPrefix(param, "kind=")
				update.Kind = RelationKind(kindStr)
			} else if strings.HasPrefix(param, "familiarity=") {
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

// FormatForPrompt generates a relationship summary for prompt injection,
// grouped by relationship kind for more natural reading.
func (rm *RelationshipManager) FormatForPrompt(agentID string, personaNameByID map[string]string) string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Group by kind
	type groupedRel struct {
		rel  *AgentRelationship
		name string
	}
	groups := make(map[RelationKind][]groupedRel)
	var noKind []groupedRel

	for key, rel := range rm.relationships {
		if !strings.HasPrefix(key, agentID+":") {
			continue
		}
		name := rel.TargetID
		if displayName, ok := personaNameByID[rel.TargetID]; ok {
			name = displayName
		}
		gr := groupedRel{rel: rel, name: name}

		if rel.Kind == "" || rel.Kind == RelationStranger {
			noKind = append(noKind, gr)
		} else {
			groups[rel.Kind] = append(groups[rel.Kind], gr)
		}
	}

	if len(groups) == 0 && len(noKind) == 0 {
		return "## My Relationships\n(You haven't formed relationships with anyone yet.)\n"
	}

	var b strings.Builder
	b.WriteString("## My Relationships\n\n")

	// Family section
	var familyRels []groupedRel
	for _, kind := range []RelationKind{RelationParent, RelationChild, RelationSibling, RelationSpouse} {
		familyRels = append(familyRels, groups[kind]...)
	}
	if len(familyRels) > 0 {
		b.WriteString("### Family\n")
		for _, gr := range familyRels {
			rel := gr.rel
			desc := describeRelationship(rel)
			b.WriteString(fmt.Sprintf("- %s: %s\n", gr.name, desc))
		}
		b.WriteString("\n")
	}

	// Friends
	if friends := groups[RelationFriend]; len(friends) > 0 {
		b.WriteString("### Friends\n")
		for _, gr := range friends {
			rel := gr.rel
			desc := describeRelationship(rel)
			b.WriteString(fmt.Sprintf("- %s: %s\n", gr.name, desc))
		}
		b.WriteString("\n")
	}

	// Rivals
	if rivals := groups[RelationRival]; len(rivals) > 0 {
		b.WriteString("### Rivals\n")
		for _, gr := range rivals {
			rel := gr.rel
			desc := describeRelationship(rel)
			b.WriteString(fmt.Sprintf("- %s: %s\n", gr.name, desc))
		}
		b.WriteString("\n")
	}

	// Professional (colleagues, mentor, mentee)
	var profRels []groupedRel
	for _, kind := range []RelationKind{RelationColleague, RelationMentor, RelationMentee} {
		profRels = append(profRels, groups[kind]...)
	}
	if len(profRels) > 0 {
		b.WriteString("### Professional\n")
		for _, gr := range profRels {
			rel := gr.rel
			desc := describeRelationship(rel)
			b.WriteString(fmt.Sprintf("- %s: %s\n", gr.name, desc))
		}
		b.WriteString("\n")
	}

	// Neighbors
	if neighbors := groups[RelationNeighbor]; len(neighbors) > 0 {
		b.WriteString("### Neighbors\n")
		for _, gr := range neighbors {
			rel := gr.rel
			desc := describeRelationship(rel)
			b.WriteString(fmt.Sprintf("- %s: %s\n", gr.name, desc))
		}
		b.WriteString("\n")
	}

	// Acquaintances (no kind / stranger)
	if len(noKind) > 0 {
		b.WriteString("### Acquaintances\n")
		for _, gr := range noKind {
			rel := gr.rel
			b.WriteString(fmt.Sprintf("- %s", gr.name))
			if rel.Familiarity > 0 {
				b.WriteString(fmt.Sprintf(" (familiarity: %.0f%%)", rel.Familiarity*100))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// describeRelationship creates a human-readable description of a relationship.
func describeRelationship(rel *AgentRelationship) string {
	var parts []string

	if rel.Familiarity >= 0.8 {
		parts = append(parts, "close")
	} else if rel.Familiarity >= 0.5 {
		parts = append(parts, "familiar")
	} else if rel.Familiarity <= 0.2 {
		parts = append(parts, "distant")
	}

	if rel.Affinity >= 0.5 {
		parts = append(parts, "warm")
	} else if rel.Affinity <= -0.3 {
		parts = append(parts, "cold")
	}

	// Add kind-specific descriptors
	switch rel.Kind {
	case RelationSibling:
		if len(parts) == 0 {
			parts = append(parts, "sibling")
		}
	case RelationSpouse:
		if len(parts) == 0 {
			parts = append(parts, "spouse")
		}
	}

	// Add custom tags (excluding the kind tag)
	var customTags []string
	kindStr := string(rel.Kind)
	for _, t := range rel.Tags {
		if t != kindStr {
			customTags = append(customTags, t)
		}
	}
	if len(customTags) > 0 {
		parts = append(parts, strings.Join(customTags, ", "))
	}

	if len(parts) == 0 {
		return "acquaintance"
	}
	return strings.Join(parts, ", ")
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
