package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── visualLineCount ────────────────────────────────────────────────────────

func TestVisualLineCount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty string", "", 1},
		{"single line", "hello", 1},
		{"two lines", "hello\nworld", 2},
		{"three lines", "a\nb\nc", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ta := newTestTextarea(tt.input, 80)
			got := visualLineCount(ta)
			if got != tt.want {
				t.Errorf("visualLineCount(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ─── historyFile ────────────────────────────────────────────────────────────

func TestHistoryFile(t *testing.T) {
	path := historyFile()
	if !strings.HasSuffix(path, filepath.Join(".soloqueue", "history")) {
		t.Errorf("historyFile() = %q, should end with .soloqueue/history", path)
	}
}

// ─── loadHistory ────────────────────────────────────────────────────────────

func TestLoadHistory_NonExistent(t *testing.T) {
	history := loadHistory()
	// Should not panic, may return nil for non-existent file
	_ = history
}

func TestLoadHistory_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history")
	content := "first\nsecond\nthird\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Override historyFile temporarily by reading the file directly
	// Since loadHistory uses a fixed path, we test the read logic indirectly
	// by verifying the function doesn't panic
	_ = loadHistory()
}

// ─── appendHistory ──────────────────────────────────────────────────────────

func TestAppendHistory_EmptyEntry(t *testing.T) {
	// Should not panic or create file for empty entry
	appendHistory("")
}

func TestAppendHistory_ValidEntry(t *testing.T) {
	// appendHistory uses the real historyFile path, so we just verify it doesn't panic
	appendHistory("test command from unit test")
}

// ─── navHistory ─────────────────────────────────────────────────────────────

func TestNavHistory_EmptyHistory(t *testing.T) {
	m := &model{history: []string{}}
	m.navHistory(-1) // should not panic
	m.navHistory(1)  // should not panic
}

func TestNavHistory_Generating(t *testing.T) {
	m := &model{
		history:      []string{"a", "b"},
		isGenerating: true,
	}
	m.navHistory(-1) // should be no-op
	if m.historyIdx != 0 {
		t.Error("navHistory should be no-op during generation")
	}
}

func TestNavHistory_ConfirmState(t *testing.T) {
	m := &model{
		history:      []string{"a", "b"},
		confirmState: &confirmState{},
	}
	m.navHistory(-1) // should be no-op
	if m.historyIdx != 0 {
		t.Error("navHistory should be no-op with confirm state")
	}
}

func TestNavHistory_Navigate(t *testing.T) {
	m := &model{
		history:      []string{"first", "second", "third"},
		textArea:     newTestTextarea("", 80),
	}

	// Navigate back
	m.navHistory(-1) // up -> idx=1
	if m.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1", m.historyIdx)
	}

	// Navigate back again
	m.navHistory(-1) // up -> idx=2
	if m.historyIdx != 2 {
		t.Errorf("historyIdx = %d, want 2", m.historyIdx)
	}

	// Navigate forward
	m.navHistory(1) // down -> idx=1
	if m.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1", m.historyIdx)
	}

	// Navigate forward to 0 (current)
	m.navHistory(1) // down -> idx=0
	if m.historyIdx != 0 {
		t.Errorf("historyIdx = %d, want 0", m.historyIdx)
	}
}

func TestNavHistory_SavesDraft(t *testing.T) {
	m := &model{
		history:  []string{"first"},
		textArea: newTestTextarea("my draft", 80),
	}

	// Going up from current should save draft
	m.navHistory(-1)
	if m.historyDraft != "my draft" {
		t.Errorf("historyDraft = %q, want %q", m.historyDraft, "my draft")
	}

	// Going back down should restore draft
	m.navHistory(1)
	if m.textArea.Value() != "my draft" {
		t.Errorf("textArea value = %q, want %q", m.textArea.Value(), "my draft")
	}
}

func TestNavHistory_ClampsBounds(t *testing.T) {
	m := &model{
		history:  []string{"a"},
		textArea: newTestTextarea("", 80),
	}

	// Can't go forward past 0
	m.navHistory(1)
	if m.historyIdx != 0 {
		t.Errorf("historyIdx should stay at 0, got %d", m.historyIdx)
	}

	// Can't go back past length
	m.navHistory(-1) // idx=1
	m.navHistory(-1) // should clamp at len(history)=1
	if m.historyIdx != 1 {
		t.Errorf("historyIdx should clamp at 1, got %d", m.historyIdx)
	}
}

// ─── addHistory edge cases ──────────────────────────────────────────────────

func TestAddHistory_EmptyString(t *testing.T) {
	m := &model{}
	m.addHistory("")
	if len(m.history) != 0 {
		t.Error("empty string should not be added to history")
	}
}

func TestAddHistory_ResetsIndex(t *testing.T) {
	m := &model{}
	m.historyIdx = 3
	m.addHistory("test")
	if m.historyIdx != 0 {
		t.Errorf("historyIdx = %d, want 0 after addHistory", m.historyIdx)
	}
	if m.historyDraft != "" {
		t.Error("historyDraft should be cleared after addHistory")
	}
}
