package agent

import "strings"

// TargetKind identifies the delegation boundary represented by a target.
type TargetKind string

const (
	TargetWorker TargetKind = "worker"
	TargetLeader TargetKind = "leader"
)

// TargetRef is the canonical, prompt-visible identity of a delegatable target.
// ID is used in tool arguments; DisplayName is presentation-only.
type TargetRef struct {
	ID          string
	DisplayName string
	Kind        TargetKind
	Group       string
	Description string
}

// CanonicalTargetID returns the stable target ID used by registry, resolver and
// prompt generation. Older agent files may omit id, so names remain a safe
// compatibility fallback until their persisted IDs are normalized.
func CanonicalTargetID(t AgentTemplate) string {
	if id := strings.TrimSpace(t.ID); id != "" {
		return strings.ToLower(id)
	}
	return strings.ToLower(strings.TrimSpace(t.Name))
}

func targetRef(t AgentTemplate, kind TargetKind) TargetRef {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		name = CanonicalTargetID(t)
	}
	return TargetRef{
		ID:          CanonicalTargetID(t),
		DisplayName: name,
		Kind:        kind,
		Group:       t.Group,
		Description: t.Description,
	}
}

// ResolveTarget resolves a raw target against a visible catalog. Canonical IDs
// are preferred; display names remain accepted for compatibility with older
// prompts, but all generated prompts use IDs.
func ResolveTarget(raw string, refs []TargetRef) (TargetRef, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return TargetRef{}, false
	}
	for _, ref := range refs {
		if raw == strings.ToLower(ref.ID) {
			return ref, true
		}
	}
	for _, ref := range refs {
		if raw == strings.ToLower(strings.TrimSpace(ref.DisplayName)) {
			return ref, true
		}
	}
	return TargetRef{}, false
}

// VisibleLeaderTargets returns leaders from other Teams visible to a leader.
func VisibleLeaderTargets(templates map[string]AgentTemplate, self AgentTemplate) []TargetRef {
	refs := make([]TargetRef, 0)
	for _, t := range templates {
		if !t.IsLeader || strings.EqualFold(CanonicalTargetID(t), CanonicalTargetID(self)) {
			continue
		}
		if self.Group != "" && strings.EqualFold(t.Group, self.Group) {
			continue
		}
		refs = append(refs, targetRef(t, TargetLeader))
	}
	return refs
}

// VisibleWorkerTargets returns same-team and project-level workers visible to
// a leader. Project workers override global workers with the same canonical ID.
func VisibleWorkerTargets(templates map[string]AgentTemplate, self AgentTemplate, projectAgents []AgentTemplate) []TargetRef {
	merged := make(map[string]AgentTemplate)
	for _, t := range templates {
		if t.IsLeader || strings.EqualFold(CanonicalTargetID(t), CanonicalTargetID(self)) || t.Group == "" || !strings.EqualFold(t.Group, self.Group) {
			continue
		}
		merged[CanonicalTargetID(t)] = t
	}
	for _, t := range projectAgents {
		if t.IsLeader || strings.EqualFold(CanonicalTargetID(t), CanonicalTargetID(self)) {
			continue
		}
		merged[CanonicalTargetID(t)] = t
	}
	refs := make([]TargetRef, 0, len(merged))
	for _, t := range merged {
		refs = append(refs, targetRef(t, TargetWorker))
	}
	return refs
}
