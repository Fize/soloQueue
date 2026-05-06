package tui

import (
	"strings"
	"testing"
)

func TestComposerLineCountCapsPastedContent(t *testing.T) {
	value := strings.Repeat("line\n", 50)
	got := composerLineCountForValue(value, 80, 8)
	if got != 8 {
		t.Errorf("composerLineCountForValue pasted 50 lines = %d, want cap 8", got)
	}
}

func TestComposerLineCountWrapsLongLine(t *testing.T) {
	got := composerLineCountForValue(strings.Repeat("x", 25), 10, 8)
	if got != 3 {
		t.Errorf("wrapped line count = %d, want 3", got)
	}
}

func TestIsSlashCommandInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"single slash", "/help", true},
		{"trimmed slash", "  /help  ", true},
		{"normal prompt", "hello", false},
		{"multiline slash paste", "/path/to/file\nsecond line", false},
		{"multiline markdown", "/title\n```go\nfmt.Println()\n```", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSlashCommandInput(tt.in); got != tt.want {
				t.Errorf("isSlashCommandInput(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
