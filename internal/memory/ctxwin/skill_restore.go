package ctxwin

import (
	"encoding/json"
	"strings"
)

// ─── Skill Content Restoration ───────────────────────────────────────────────
//
// Compaction replaces history with a summary, dropping rendered skill
// instructions (oversized tool messages are filtered first). Without
// restoration the model would forget it is executing under a skill's workflow.
// RestoreRecentSkillContent re-attaches the most recent invocation of each
// distinct skill, aligned with Claude Code's post-compaction re-attach.

// Default budgets aligned with Claude Code's re-attach behavior.
const (
	DefaultSkillRestorePerSkillTokens = 5000
	DefaultSkillRestoreTotalTokens    = 25000
)

// alreadyLoadedNotePrefix marks deduplicated invocations (see dedupeSkillResult
// in the agent package); these carry no instructions and are skipped.
const alreadyLoadedNotePrefix = "Skill already loaded"

// RestoreRecentSkillContent extracts the most recent rendering of each
// distinct invoked skill. Output is most-recent-first; entries are truncated
// to perSkillTokens and dropped once the total exceeds totalTokens.
// Skill identity comes from the matching assistant tool_calls arguments,
// falling back to the content itself when unresolvable. Returns "" if none.
func RestoreRecentSkillContent(msgs []Message, tokenizer *Tokenizer, perSkillTokens, totalTokens int) string {
	if tokenizer == nil || len(msgs) == 0 || perSkillTokens <= 0 || totalTokens <= 0 {
		return ""
	}

	// Map tool call IDs → skill ID from assistant messages.
	callToSkill := make(map[string]string)
	for _, m := range msgs {
		if m.Role != RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "Skill" {
				continue
			}
			var args struct {
				Skill string `json:"skill"`
			}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil && args.Skill != "" {
				callToSkill[tc.ID] = args.Skill
			}
		}
	}

	// Collect Skill tool results, newest first.
	type skillEntry struct {
		skillID string
		content string
	}
	var entries []skillEntry
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != RoleTool || m.Name != "Skill" {
			continue
		}
		if strings.HasPrefix(m.Content, alreadyLoadedNotePrefix) {
			continue
		}
		skillID := callToSkill[m.ToolCallID]
		if skillID == "" {
			skillID = m.Content // unresolvable: keep distinct renderings apart
		}
		entries = append(entries, skillEntry{skillID: skillID, content: m.Content})
	}

	// Keep only the most recent entry per skill.
	seen := make(map[string]bool, len(entries))
	var latest []skillEntry
	for _, e := range entries {
		if seen[e.skillID] {
			continue
		}
		seen[e.skillID] = true
		latest = append(latest, e)
	}

	// Assemble under budgets; entries are already most-recent-first.
	var parts []string
	used := 0
	for _, e := range latest {
		content := truncateToTokens(e.content, perSkillTokens, tokenizer)
		if content == "" {
			continue
		}
		n := tokenizer.Count(content)
		if used+n > totalTokens {
			continue // drop this older entry to respect the combined budget
		}
		parts = append(parts, content)
		used += n
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// truncateToTokens cuts to at most n tokens, appending an ellipsis when cut.
func truncateToTokens(text string, n int, tokenizer *Tokenizer) string {
	if tokenizer.Count(text) <= n {
		return text
	}
	// Binary search the longest rune prefix within the token budget.
	runes := []rune(text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if tokenizer.Count(string(runes[:mid])) <= n {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return ""
	}
	return string(runes[:lo]) + "…"
}
