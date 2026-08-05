package skill

import (
	"sort"
	"strings"
)

// ─── Skill Listing ──────────────────────────────────────────────────────────
//
// Skills are exposed to the LLM as a budgeted listing aligned with Claude Code:
// overflow degrades to ID-only lines so every ID stays visible, and ordering
// follows invocation counts (desc) so used skills surface first.

// maxSummaryRunes caps a single summary line.
const maxSummaryRunes = 120

// maxTriggerKeywords caps the rendered frontmatter triggers.
const maxTriggerKeywords = 3

// SummarizeSkill renders a single-line trigger-first summary.
// Trigger source priority: Triggers → when_to_use first line → description first line.
func SummarizeSkill(s *Skill) string {
	if s == nil {
		return ""
	}

	var parts []string

	if len(s.Triggers) > 0 {
		kws := s.Triggers
		if len(kws) > maxTriggerKeywords {
			kws = kws[:maxTriggerKeywords]
		}
		parts = append(parts, "当任务提到 "+strings.Join(kws, "、")+" 时使用")
	} else if s.WhenToUse != "" {
		parts = append(parts, firstLine(s.WhenToUse))
	}

	if desc := firstLine(s.Description); desc != "" {
		parts = append(parts, desc)
	}

	summary := strings.Join(parts, " — ")
	if summary == "" {
		return ""
	}
	return truncateRunes(summary, maxSummaryRunes)
}

// BuildSkillListing renders the budgeted listing; overflow degrades to ID-only
// lines. Ordering: invocation count desc, then ID asc.
func BuildSkillListing(skills []*Skill, budgetRunes int, counts map[string]int) string {
	if len(skills) == 0 {
		return ""
	}

	sorted := append([]*Skill(nil), skills...)
	sort.Slice(sorted, func(i, j int) bool {
		ci, cj := counts[sorted[i].ID], counts[sorted[j].ID]
		if ci != cj {
			return ci > cj
		}
		return sorted[i].ID < sorted[j].ID
	})

	var b strings.Builder
	used := 0
	for _, s := range sorted {
		summary := SummarizeSkill(s)
		if summary == "" {
			// ID-only lines don't count against the budget.
			b.WriteString("- " + s.ID + "\n")
			continue
		}
		line := "- " + s.ID + ": " + summary
		lineRunes := len([]rune(line)) + 1 // +1 newline
		if budgetRunes > 0 && used+lineRunes > budgetRunes {
			b.WriteString("- " + s.ID + "\n")
			continue
		}
		b.WriteString(line + "\n")
		used += lineRunes
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// truncateRunes cuts to n runes, so multibyte characters are never split.
func truncateRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}
