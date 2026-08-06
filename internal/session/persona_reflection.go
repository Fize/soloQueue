package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// personaToneSet is the fixed set of allowed tone values in state.md.
var personaToneSet = []string{"warm and direct", "dry wit", "playful", "calm and supportive", "more concise"}

// BuildPersonaReflectionPrompt builds the nightly reflection prompt that asks
// the LLM to produce the next state.md. Inputs are the raw conversation of the
// day and the archivist's daily memory; the output format is strict so the
// result can be parsed and written back atomically.
func BuildPersonaReflectionPrompt(name string, rawConversation string, dailyMemory string, now time.Time) string {
	if name == "" {
		name = "assistant"
	}
	if rawConversation == "" {
		rawConversation = "(no conversation today)"
	}
	if dailyMemory == "" {
		dailyMemory = "(none available)"
	}
	return fmt.Sprintf(`You are updating %[1]s's relationship-layer personality state (state.md) based on the evidence below.

Inputs:
- RAW CONVERSATION (today's turns):
%[2]s

- DAILY MEMORY (today's archivist summary):
%[3]s

Rules:
1. Only adjust the whitelisted fields: familiarity (0.0-1.0), humor (0.0-1.0),
   patience (0.0-1.0), topic_affinity (list of topics), and tone (one of: %[4]s).
2. Slow, long-term drift only: each numeric field may change by at most ±0.1
   from its previous value. Never jump values in a single day.
3. Evidence rule: every changed field MUST be backed by "- because: <date> <observable user signal>",
   citing an observable signal from the conversation (wording, punctuation, message length,
   repetition, emoji, user actions) and the date it occurred. Never cite inferences
   about the user's personality.
4. Identity lock: never contradict soul.md. %[1]s's core identity is fixed; only
   closeness, humor, patience, topic affinity, and tone may drift.
5. Mood reset: the emotional state is transient and resets nightly. Always write "- mood: calm".
6. set_at must be: %[5]s

Output ONLY the complete state.md, exactly in this format:
## Personality Drift (slow, long-term)
- familiarity: <0.0-1.0>
- humor: <0.0-1.0>
- patience: <0.0-1.0>
- topic_affinity: ["<topic>", ...]
- tone: <one of fixed set>
- because: <date> <observable user signal>
## Emotional State (transient)
- mood: calm
- set_at: <now RFC3339>
`, name, rawConversation, dailyMemory, strings.Join(personaToneSet, ", "), now.Format(time.RFC3339))
}

// UpdatePersonaState runs the nightly reflection for the main agent:
// reads the current state.md, day-gates on its set_at value, makes one Chat
// call, validates the strict output format, and atomically replaces the file.
// Any failure keeps the previous state.md intact.
func UpdatePersonaState(ctx context.Context, log *logger.Logger, llm agent.LLMClient, statePath, name, rawConversation, dailyMemory string, now time.Time, providerID, modelID string) error {
	if llm == nil {
		return fmt.Errorf("persona reflection: nil llm client")
	}
	if name == "" {
		name = "assistant"
	}

	existing := ""
	content, err := os.ReadFile(statePath)
	switch {
	case err == nil:
		existing = string(content)
		if setAt := parsePersonaSetAt(existing); !setAt.IsZero() {
			ey, em, ed := setAt.Date()
			ny, nm, nd := now.Date()
			if ey == ny && em == nm && ed == nd {
				log.Debug(logger.CatApp, "persona reflection: skipped (already updated today)")
				return nil
			}
		}
	case os.IsNotExist(err):
		log.Info(logger.CatApp, "persona reflection: initializing state.md from defaults")
	default:
		return fmt.Errorf("persona reflection: read state.md: %w", err)
	}

	systemMsg := fmt.Sprintf("You are the nightly reflection process for %s.\nRules:\n- Update only the whitelisted fields: familiarity, humor, patience, topic_affinity, tone.\n- Drift at most ±0.1 per day; every change needs an observable-evidence reason.\n- Never contradict soul.md.\n- Reset the emotional state: mood is always \"calm\".\n- Output only the complete state.md in the exact requested format.", name)

	resp, err := llm.Chat(ctx, agent.LLMRequest{
		Messages: []agent.LLMMessage{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: BuildPersonaReflectionPrompt(name, rawConversation, dailyMemory, now)},
		},
		ProviderID:  providerID,
		Model:       modelID,
		MaxTokens:   1024,
		Temperature: 0.0,
	})
	if err != nil {
		log.Error(logger.CatApp, "persona reflection: llm call failed", "err", err.Error())
		return fmt.Errorf("persona reflection: llm call: %w", err)
	}

	out := strings.TrimSpace(resp.Content)
	if err := validatePersonaState(out); err != nil {
		log.Error(logger.CatApp, "persona reflection: parse failed, keeping previous state.md", "err", err.Error())
		return fmt.Errorf("persona reflection: parse failed: %w", err)
	}

	if err := writePersonaStateAtomically(statePath, out); err != nil {
		log.Error(logger.CatApp, "persona reflection: write failed, keeping previous state.md", "err", err.Error())
		return fmt.Errorf("persona reflection: write: %w", err)
	}

	if existing == "" {
		log.Info(logger.CatApp, "persona reflection: state.md initialized")
	} else {
		log.Info(logger.CatApp, "persona reflection: state.md updated", "changed", summarizePersonaChanges(existing, out))
	}
	return nil
}

// parsePersonaSetAt extracts the set_at timestamp from state.md content.
// Returns zero time if absent or unparseable. Accepts both the documented
// bullet form ("- set_at: ...") and a bare "set_at: ..." line.
func parsePersonaSetAt(content string) time.Time {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		trimmed = strings.TrimPrefix(trimmed, "- ")
		if strings.HasPrefix(trimmed, "set_at:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "set_at:"))
			t, err := time.Parse(time.RFC3339, val)
			if err != nil {
				return time.Time{}
			}
			return t
		}
	}
	return time.Time{}
}

// validatePersonaState checks that the LLM output matches the strict state.md
// format well enough to be written back.
func validatePersonaState(out string) error {
	if !strings.Contains(out, "## Personality Drift") {
		return fmt.Errorf("missing \"## Personality Drift\" section")
	}
	if !strings.Contains(out, "## Emotional State") {
		return fmt.Errorf("missing \"## Emotional State\" section")
	}
	if !strings.Contains(out, "set_at:") {
		return fmt.Errorf("missing \"set_at:\" field")
	}
	return nil
}

// writePersonaStateAtomically writes content to statePath via a temp file in
// the same directory followed by os.Rename, so a crash mid-write never
// corrupts the previous state.md.
func writePersonaStateAtomically(statePath, content string) error {
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".state.md-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, statePath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// summarizePersonaChanges produces a short log summary of the fields whose
// values changed between the previous and the new state.md.
func summarizePersonaChanges(oldContent, newContent string) string {
	var changed []string
	for _, field := range []string{"familiarity", "humor", "patience", "tone"} {
		oldVal := personaFieldValue(oldContent, field)
		newVal := personaFieldValue(newContent, field)
		if oldVal != newVal {
			changed = append(changed, field+": "+oldVal+" -> "+newVal)
		}
	}
	if len(changed) == 0 {
		return "no field changes"
	}
	return strings.Join(changed, ", ")
}

// personaFieldValue returns the first "- field: value" line in state.md content.
func personaFieldValue(content, field string) string {
	prefix := "- " + field + ":"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return ""
}
