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
			name: "Bash running",
			tb:   toolBlock{name: "Bash", args: `{"command":"go test ./..."}`, done: false},
			want: []string{"⚙", "go test ./..."},
		},
		{
			name: "Read running with path",
			tb:   toolBlock{name: "Read", args: `{"path":"/tmp/test.go"}`, done: false},
			want: []string{"⚙", "/tmp/test.go"},
		},
		{
			name: "Write running with path",
			tb:   toolBlock{name: "Write", args: `{"path":"src/main.go"}`, done: false},
			want: []string{"⚙", "src/main.go"},
		},
		{
			name: "Edit running with path",
			tb:   toolBlock{name: "Edit", args: `{"path":"app.go"}`, done: false},
			want: []string{"⚙", "app.go"},
		},
		{
			name: "Glob running with pattern",
			tb:   toolBlock{name: "Glob", args: `{"pattern":"*.go","dir":"/src"}`, done: false},
			want: []string{"⚙", "*.go"},
		},
		{
			name: "Grep running with pattern",
			tb:   toolBlock{name: "Grep", args: `{"pattern":"func main"}`, done: false},
			want: []string{"⚙", "func main"},
		},
		{
			name: "WebFetch running with URL",
			tb:   toolBlock{name: "WebFetch", args: `{"url":"https://api.example.com/data"}`, done: false},
			want: []string{"⚙", "https://api.example.com/data"},
		},
		{
			name: "WebSearch running with query",
			tb:   toolBlock{name: "WebSearch", args: `{"query":"Go generics"}`, done: false},
			want: []string{"⚙", "Go generics"},
		},
		{
			name: "delegate_* running with task",
			tb:   toolBlock{name: "delegate_frontend", args: `{"task":"Fix login page"}`, done: false},
			want: []string{"⚙", "Fix login page"},
		},
		{
			name: "running no display arg",
			tb:   toolBlock{name: "Bash", args: `{}`, done: false},
			want: []string{"⚙"},
			dont: []string{"⚙ "}, // no trailing space
		},
		{
			name: "done success with lines and duration",
			tb:   toolBlock{name: "Read", args: `{"path":"a.go"}`, done: true, lineCount: 24, duration: 120 * time.Millisecond},
			want: []string{"✓", "a.go", "24行", "120ms"},
		},
		{
			name: "done success with file path and duration",
			tb:   toolBlock{name: "Read", args: `{"path":"main.go"}`, done: true, duration: 80 * time.Millisecond},
			want: []string{"✓", "main.go", "80ms"},
		},
		{
			name: "done success with command (Bash), no lines",
			tb:   toolBlock{name: "Bash", args: `{"command":"make build"}`, done: true, duration: 1 * time.Second},
			want: []string{"✓", "make build", "1s"},
		},
		{
			name: "done success no duration",
			tb:   toolBlock{name: "Read", args: `{"path":"x.go"}`, done: true, lineCount: 5},
			want: []string{"✓", "x.go", "5行"},
		},
		{
			name: "done success - Bash with lines",
			tb:   toolBlock{name: "Bash", args: `{"command":"ls"}`, done: true, lineCount: 3, duration: 50 * time.Millisecond},
			want: []string{"✓", "ls", "3行", "50ms"},
		},
		{
			name: "done with error - Bash permission denied",
			tb:   toolBlock{name: "Bash", args: `{"command":"rm -rf /"}`, done: true, err: fmt.Errorf("permission denied: you are not root")},
			want: []string{"✗", "rm -rf /", "permission denied"},
		},
		{
			name: "done with error - Read file not found",
			tb:   toolBlock{name: "Read", args: `{"path":"missing.go"}`, done: true, err: fmt.Errorf("no such file")},
			want: []string{"✗", "missing.go", "no such file"},
		},
		{
			name: "done with error - no args",
			tb:   toolBlock{name: "Bash", args: `{}`, done: true, err: fmt.Errorf("something went wrong")},
			want: []string{"✗", "something went wrong"},
			dont: []string{"—"}, // no display arg → no separator
		},
		{
			name: "done success no args",
			tb:   toolBlock{name: "Bash", args: `{}`, done: true},
			want: []string{"✓"},
		},
		{
			name: "done success - WebFetch with lines",
			tb:   toolBlock{name: "WebFetch", args: `{"url":"https://go.dev"}`, done: true, lineCount: 120, duration: 850 * time.Millisecond},
			want: []string{"✓", "https://go.dev", "120行", "850ms"},
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
