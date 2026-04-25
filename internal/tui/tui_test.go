package tui

import (
	"testing"
)

// Test_parseToolArgs tests JSON argument parsing
func Test_parseToolArgs(t *testing.T) {
	tests := []struct {
		name        string
		argsJSON    string
		wantPath    string
		wantCommand string
		wantFile    string
	}{
		{
			name:     "parse path arg",
			argsJSON: `{"path": "/home/user/test.go", "other": "value"}`,
			wantPath: "/home/user/test.go",
		},
		{
			name:        "parse command arg",
			argsJSON:    `{"command": "ls -la", "timeout": 30}`,
			wantCommand: "ls -la",
		},
		{
			name:     "parse file arg",
			argsJSON: `{"file": "README.md"}`,
			wantFile: "README.md",
		},
		{
			name:     "empty JSON",
			argsJSON: `{}`,
		},
		{
			name:     "invalid JSON",
			argsJSON: `invalid json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseToolArgs(tt.argsJSON)
			if got.Path != tt.wantPath {
				t.Errorf("parseToolArgs() got Path = %v, want %v", got.Path, tt.wantPath)
			}
			if got.Command != tt.wantCommand {
				t.Errorf("parseToolArgs() got Command = %v, want %v", got.Command, tt.wantCommand)
			}
			if got.File != tt.wantFile {
				t.Errorf("parseToolArgs() got File = %v, want %v", got.File, tt.wantFile)
			}
		})
	}
}

// Test_truncate tests string truncation
func Test_truncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{
			name: "short string not truncated",
			s:    "hello",
			max:  10,
			want: "hello",
		},
		{
			name: "long string truncated",
			s:    "hello world",
			max:  5,
			want: "hello…",
		},
		{
			name: "contains newline",
			s:    "hello\nworld",
			max:  20,
			want: "hello↵world",
		},
		{
			name: "contains carriage return",
			s:    "hello\rworld",
			max:  20,
			want: "helloworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("truncate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test_Styled tests ANSI style composition
func Test_Styled(t *testing.T) {
	// No attrs → plain text
	if got := Styled("hello"); got != "hello" {
		t.Errorf("Styled with no attrs = %q, want %q", got, "hello")
	}

	// With attrs → wrapped with ANSI codes and RESET
	got := Styled("hello", Fg(10))
	if len(got) < len("hello") {
		t.Errorf("Styled with Fg(10) too short: %q", got)
	}
	if got[:len(got)-len("hello")] == "" {
		t.Errorf("Styled should have ANSI prefix")
	}
}

// Test_addHistory tests history deduplication (only consecutive duplicates skipped)
func Test_addHistory(t *testing.T) {
	app := &App{}

	app.addHistory("first")
	app.addHistory("first") // consecutive duplicate — skipped
	app.addHistory("second")
	app.addHistory("third")

	if len(app.history) != 3 {
		t.Errorf("addHistory() got %d entries, want 3", len(app.history))
	}
	if app.history[0] != "first" || app.history[1] != "second" || app.history[2] != "third" {
		t.Errorf("addHistory() entries = %v", app.history)
	}

	// Non-consecutive same value is allowed
	app2 := &App{}
	app2.addHistory("a")
	app2.addHistory("b")
	app2.addHistory("a") // non-consecutive — allowed
	if len(app2.history) != 3 {
		t.Errorf("addHistory() non-consecutive got %d entries, want 3", len(app2.history))
	}
}

// Test_Config tests that Config zero values are usable
func Test_Config(t *testing.T) {
	cfg := Config{
		Version: "0.1.0",
	}
	if cfg.Version != "0.1.0" {
		t.Errorf("Config.Version = %v, want 0.1.0", cfg.Version)
	}
}
