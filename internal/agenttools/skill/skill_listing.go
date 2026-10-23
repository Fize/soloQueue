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

// maxSummaryRunes caps each description, including the truncation marker,
// at Claude Code's 1,536-character listing limit.
const maxSummaryRunes = 1536

// SummarizeSkill renders the complete description up to the character limit.
// Internal line breaks are preserved; legacy trigger fields are not included.
func SummarizeSkill(s *Skill) string {
	if s == nil {
		return ""
	}

	return truncateRunes(strings.TrimSpace(s.Description), maxSummaryRunes)
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

// truncateRunes caps text at n runes including the ellipsis, without splitting
// multibyte characters.
func truncateRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n-1]) + "…"
}
