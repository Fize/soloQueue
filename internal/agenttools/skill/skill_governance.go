package skill

import (
	"context"
	"time"
)

// ─── Skill Governance Report ────────────────────────────────────────────────
//
// Data behind skill-hygiene decisions: never-invoked skills (weak trigger
// signals or removal candidates) and description-quality issues that suppress
// activation.

// GovernanceWarning describes one description-quality issue for a skill.
type GovernanceWarning struct {
	SkillID string
	Issue   string
}

// GovernanceReport is the outcome of a skill-hygiene inspection.
type GovernanceReport struct {
	// NeverInvoked lists model-invocable skills with no recorded invocations in
	// the stats window. Nil stats ⇒ all qualify.
	NeverInvoked []string
	// QualityWarnings lists skills whose metadata weakens model activation.
	QualityWarnings []GovernanceWarning
}

// BuildGovernanceReport inspects the registry against invocation telemetry.
// stats may be nil; since is the lookback window for counts.
func BuildGovernanceReport(reg *SkillRegistry, stats InvocationStats, since time.Time) (*GovernanceReport, error) {
	rep := &GovernanceReport{}

	counts := map[string]int{}
	if stats != nil {
		var err error
		counts, err = stats.Counts(context.Background(), since)
		if err != nil {
			return nil, err
		}
	}

	for _, s := range reg.Skills() {
		if s.DisableModelInvocation {
			continue
		}
		if counts[s.ID] == 0 {
			rep.NeverInvoked = append(rep.NeverInvoked, s.ID)
		}
		if w := qualityWarning(s); w != "" {
			rep.QualityWarnings = append(rep.QualityWarnings, GovernanceWarning{SkillID: s.ID, Issue: w})
		}
	}
	return rep, nil
}

// qualityWarning returns a description-quality issue for the skill, or "".
func qualityWarning(s *Skill) string {
	switch {
	case len(s.Triggers) == 0 && s.WhenToUse == "":
		if s.Description == "" {
			return "no description, no triggers, no when_to_use — nothing to match against"
		}
		return "no triggers and no when_to_use — model must match the bare description"
	case s.Description == "":
		return "empty description — only trigger text is available"
	case len(s.WhenToUse) > maxSkillDescriptionChars:
		return "when_to_use exceeds 1536 chars — truncated in listing, tail trigger phrases lost"
	}
	return ""
}
