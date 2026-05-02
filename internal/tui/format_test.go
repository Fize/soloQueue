package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ─── formatDuration ─────────────────────────────────────────────────────────

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"1 second", 1 * time.Second, "1s"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"1 minute", 1 * time.Minute, "1m 0s"},
		{"90 seconds", 90 * time.Second, "1m 30s"},
		{"1 hour", 1 * time.Hour, "1h 0m"},
		{"1 hour 23 minutes", 83 * time.Minute, "1h 23m"},
		{"rounds to second", 1500 * time.Millisecond, "2s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.d)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

// ─── formatTokenCount ───────────────────────────────────────────────────────

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"zero", 0, "0"},
		{"small", 999, "999"},
		{"exactly 1k", 1000, "1.0k"},
		{"1.5k", 1500, "1.5k"},
		{"8.2k", 8200, "8.2k"},
		{"large", 1000000, "1000.0k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTokenCount(tt.n)
			if got != tt.want {
				t.Errorf("formatTokenCount(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// ─── formatToolBlock ────────────────────────────────────────────────────────

func TestFormatToolBlock(t *testing.T) {
	tests := []struct {
		name string
		tb   toolBlock
		want []string
		dont []string
	}{
		{
			name: "running with path",
			tb:   toolBlock{name: "file_read", args: `{"path":"/tmp/test.go"}`, done: false},
			want: []string{"⚙", "/tmp/test.go"},
		},
		{
			name: "running with command",
			tb:   toolBlock{name: "shell_exec", args: `{"command":"go test"}`, done: false},
			want: []string{"⚙", "go test"},
		},
		{
			name: "running with file",
			tb:   toolBlock{name: "file_write", args: `{"file":"main.go"}`, done: false},
			want: []string{"⚙", "main.go"},
		},
		{
			name: "running no display arg",
			tb:   toolBlock{name: "list_files", args: `{}`, done: false},
			want: []string{"⚙"},
		},
		{
			name: "done success with lines",
			tb:   toolBlock{name: "file_read", args: `{"path":"a.go"}`, done: true, lineCount: 24, duration: 120 * time.Millisecond},
			want: []string{"✓", "24 行", "120ms"},
		},
		{
			name: "done success no lines",
			tb:   toolBlock{name: "grep", args: `{"path":"."}`, done: true, duration: 80 * time.Millisecond},
			want: []string{"✓", ".", "80ms"},
		},
		{
			name: "done success no duration",
			tb:   toolBlock{name: "file_read", args: `{"path":"x.go"}`, done: true, lineCount: 5},
			want: []string{"✓", "5 行"},
			dont: []string{"·"},
		},
		{
			name: "done with error",
			tb:   toolBlock{name: "shell_exec", args: `{"command":"rm -rf /"}`, done: true, err: fmt.Errorf("permission denied: you are not root")},
			want: []string{"✗", "permission denied"},
		},
		{
			name: "done success no args",
			tb:   toolBlock{name: "list_files", args: `{}`, done: true},
			want: []string{"✓"},
		},
		{
			name: "invalid args JSON",
			tb:   toolBlock{name: "tool", args: `bad json`, done: false},
			want: []string{"⚙", "[parse error]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatToolBlock(tt.tb)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("formatToolBlock() = %q, want to contain %q", got, w)
				}
			}
			for _, d := range tt.dont {
				if strings.Contains(got, d) {
					t.Errorf("formatToolBlock() = %q, should NOT contain %q", got, d)
				}
			}
		})
	}
}

// ─── truncate edge cases ────────────────────────────────────────────────────

func TestTruncate_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"exact length", "hello", 5, "hello"},
		{"one over", "hello!", 5, "hello…"},
		{"empty string", "", 5, ""},
		{"max zero", "hello", 0, "…"},
		{"multibyte rune", "你好世界", 2, "你好…"},
		{"mixed newline and carriage", "a\nb\rc", 10, "a↵bc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
