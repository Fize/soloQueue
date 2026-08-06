package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func newReflectionTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(t.TempDir(), logger.WithFile(false), logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	return l
}

// validPersonaState builds a state.md body that passes validatePersonaState,
// with set_at set to the RFC3339 form of now.
func validPersonaState(now time.Time) string {
	return "## Personality Drift (slow, long-term)\n" +
		"- familiarity: 0.5\n" +
		"- humor: 0.4\n" +
		"- patience: 0.6\n" +
		"- topic_affinity: [\"coding\", \"music\"]\n" +
		"- tone: warm and direct\n" +
		"- because: " + now.Format("2006-01-02") + " user asked two follow-up questions in a row\n" +
		"## Emotional State (transient)\n" +
		"- mood: calm\n" +
		"- set_at: " + now.Format(time.RFC3339) + "\n"
}

func TestBuildPersonaReflectionPrompt(t *testing.T) {
	now := time.Date(2026, 8, 6, 21, 30, 0, 0, time.Local)
	prompt := BuildPersonaReflectionPrompt("Solo", "User: hello\nAssistant: hi", "user was friendly", now)

	// Name appears and is used as the subject.
	if !strings.Contains(prompt, "Solo") {
		t.Error("prompt should contain the assistant name")
	}

	// Drift clamp ±0.1.
	if !strings.Contains(prompt, "±0.1") {
		t.Error("prompt should contain the ±0.1 drift clamp")
	}

	// Evidence rule.
	if !strings.Contains(prompt, "- because:") {
		t.Error("prompt should contain the evidence rule '- because:'")
	}
	if !strings.Contains(prompt, "observable user signal") {
		t.Error("prompt should contain the observable-signal evidence rule")
	}

	// Mood reset.
	if !strings.Contains(prompt, "- mood: calm") {
		t.Error("prompt should contain the mood reset line '- mood: calm'")
	}

	// Fixed tone set entries.
	for _, tone := range []string{"warm and direct", "dry wit", "playful", "calm and supportive", "more concise"} {
		if !strings.Contains(prompt, tone) {
			t.Errorf("prompt should contain tone set entry %q", tone)
		}
	}

	// Both section headers.
	if !strings.Contains(prompt, "## Personality Drift") {
		t.Error("prompt should contain '## Personality Drift' header")
	}
	if !strings.Contains(prompt, "## Emotional State") {
		t.Error("prompt should contain '## Emotional State' header")
	}

	// Inputs are embedded.
	if !strings.Contains(prompt, "User: hello") {
		t.Error("prompt should contain the raw conversation")
	}
	if !strings.Contains(prompt, "user was friendly") {
		t.Error("prompt should contain the daily memory")
	}
	if !strings.Contains(prompt, now.Format(time.RFC3339)) {
		t.Error("prompt should contain the set_at timestamp")
	}

	// Defaults for empty inputs.
	def := BuildPersonaReflectionPrompt("", "", "", now)
	if !strings.Contains(def, "assistant") {
		t.Error("empty name should fall back to 'assistant'")
	}
	if !strings.Contains(def, "(no conversation today)") {
		t.Error("empty conversation should fall back to placeholder")
	}
	if !strings.Contains(def, "(none available)") {
		t.Error("empty daily memory should fall back to placeholder")
	}
}

func TestUpdatePersonaState_Success(t *testing.T) {
	now := time.Now()
	log := newReflectionTestLogger(t)
	llm := &agenttest.FakeLLM{Responses: []string{validPersonaState(now)}}
	statePath := filepath.Join(t.TempDir(), "persona", "roles", "state.md")

	err := UpdatePersonaState(context.Background(), log, llm, statePath, "Solo", "User: hi", "user was chatty", now, "test-provider", "test-model")
	if err != nil {
		t.Fatalf("UpdatePersonaState: %v", err)
	}

	// FakeLLM called exactly once.
	if calls := llm.CallCount(); calls != 1 {
		t.Errorf("expected exactly 1 LLM call, got %d", calls)
	}

	// File written with the expected sections.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "## Personality Drift") {
		t.Error("written state.md should contain '## Personality Drift'")
	}
	if !strings.Contains(content, "## Emotional State") {
		t.Error("written state.md should contain '## Emotional State'")
	}
	if !strings.Contains(content, "- familiarity: 0.5") {
		t.Error("written state.md should contain familiarity field")
	}
	if !strings.Contains(content, "- tone: warm and direct") {
		t.Error("written state.md should contain tone field")
	}

	// set_at in the file is today.
	if !strings.Contains(content, "set_at:") {
		t.Fatal("written state.md should contain set_at")
	}
	if setAt := parsePersonaSetAt(content); setAt.IsZero() {
		t.Fatal("written state.md set_at should parse")
	} else {
		ey, em, ed := setAt.Date()
		ny, nm, nd := now.Date()
		if ey != ny || em != nm || ed != nd {
			t.Errorf("set_at should be today, got %s", setAt.Format(time.RFC3339))
		}
	}

	// Day gate: second call on the same day must not hit the LLM again.
	err = UpdatePersonaState(context.Background(), log, llm, statePath, "Solo", "User: hi", "user was chatty", now, "test-provider", "test-model")
	if err != nil {
		t.Fatalf("second UpdatePersonaState (day gate): %v", err)
	}
	if calls := llm.CallCount(); calls != 1 {
		t.Errorf("day gate should skip LLM call: expected 1 total call, got %d", calls)
	}
}

func TestUpdatePersonaState_ParseFailure(t *testing.T) {
	now := time.Now()
	log := newReflectionTestLogger(t)
	llm := &agenttest.FakeLLM{Responses: []string{"this is not a state.md at all"}}
	statePath := filepath.Join(t.TempDir(), "persona", "roles", "state.md")

	// Pre-existing state.md must be preserved.
	oldContent := "## Personality Drift (slow, long-term)\n- familiarity: 0.9\n- tone: dry wit\n## Emotional State (transient)\n- mood: calm\n- set_at: " + now.AddDate(0, 0, -1).Format(time.RFC3339) + "\n"
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(statePath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := UpdatePersonaState(context.Background(), log, llm, statePath, "Solo", "User: hi", "", now, "test-provider", "test-model")
	if err == nil {
		t.Fatal("expected error for unparseable LLM output, got nil")
	}
	if !strings.Contains(err.Error(), "parse failed") {
		t.Errorf("error should mention parse failure, got: %v", err)
	}

	// Old file preserved unchanged.
	data, rerr := os.ReadFile(statePath)
	if rerr != nil {
		t.Fatalf("ReadFile: %v", rerr)
	}
	if string(data) != oldContent {
		t.Errorf("pre-existing state.md should be preserved unchanged, got:\n%s", string(data))
	}
}

func TestUpdatePersonaState_MissingFile(t *testing.T) {
	now := time.Now()
	log := newReflectionTestLogger(t)
	llm := &agenttest.FakeLLM{Responses: []string{validPersonaState(now)}}
	statePath := filepath.Join(t.TempDir(), "persona", "roles", "state.md")

	// No state.md exists → initialization path creates it.
	err := UpdatePersonaState(context.Background(), log, llm, statePath, "Solo", "User: hi", "", now, "test-provider", "test-model")
	if err != nil {
		t.Fatalf("UpdatePersonaState: %v", err)
	}

	data, rerr := os.ReadFile(statePath)
	if rerr != nil {
		t.Fatalf("ReadFile: %v", rerr)
	}
	content := string(data)
	if !strings.Contains(content, "## Personality Drift") || !strings.Contains(content, "## Emotional State") {
		t.Errorf("initialized state.md should contain both sections, got: %s", content)
	}
	if calls := llm.CallCount(); calls != 1 {
		t.Errorf("expected exactly 1 LLM call for init, got %d", calls)
	}
}

func TestUpdatePersonaState_DayGate(t *testing.T) {
	now := time.Now()
	log := newReflectionTestLogger(t)
	llm := &agenttest.FakeLLM{Responses: []string{"should never be consumed"}}
	statePath := filepath.Join(t.TempDir(), "persona", "roles", "state.md")

	// Pre-create state.md with today's set_at.
	content := "## Personality Drift (slow, long-term)\n- familiarity: 0.3\n- tone: playful\n## Emotional State (transient)\n- mood: calm\n- set_at: " + now.Format(time.RFC3339) + "\n"
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(statePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := UpdatePersonaState(context.Background(), log, llm, statePath, "Solo", "User: hi", "", now, "test-provider", "test-model")
	if err != nil {
		t.Fatalf("UpdatePersonaState (day gate): %v", err)
	}
	if calls := llm.CallCount(); calls != 0 {
		t.Errorf("day gate should skip the LLM entirely: expected 0 calls, got %d", calls)
	}

	// File untouched.
	data, rerr := os.ReadFile(statePath)
	if rerr != nil {
		t.Fatalf("ReadFile: %v", rerr)
	}
	if string(data) != content {
		t.Error("day-gated state.md should not be rewritten")
	}
}

func TestUpdatePersonaState_NilLLM(t *testing.T) {
	log := newReflectionTestLogger(t)
	statePath := filepath.Join(t.TempDir(), "persona", "roles", "state.md")
	err := UpdatePersonaState(context.Background(), log, nil, statePath, "Solo", "User: hi", "", time.Now(), "test-provider", "test-model")
	if err == nil {
		t.Fatal("expected error for nil llm client, got nil")
	}
	if !strings.Contains(err.Error(), "nil llm client") {
		t.Errorf("error should mention nil llm client, got: %v", err)
	}
}

func TestUpdatePersonaState_LLMError(t *testing.T) {
	now := time.Now()
	log := newReflectionTestLogger(t)
	llm := &agenttest.FakeLLM{Err: os.ErrClosed}
	statePath := filepath.Join(t.TempDir(), "persona", "roles", "state.md")

	err := UpdatePersonaState(context.Background(), log, llm, statePath, "Solo", "User: hi", "", now, "test-provider", "test-model")
	if err == nil {
		t.Fatal("expected error when LLM call fails, got nil")
	}
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Error("state.md should not be created when the LLM call fails")
	}
}
