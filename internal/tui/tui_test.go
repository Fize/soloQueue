package tui

import (
	"testing"
)

// Test_toolDisplay tests tool argument display extraction
func Test_toolDisplay(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		argsJSON string
		want     string
	}{
		{
			name:     "Bash command",
			toolName: "Bash",
			argsJSON: `{"command": "ls -la"}`,
			want:     "ls -la",
		},
		{
			name:     "Read path",
			toolName: "Read",
			argsJSON: `{"path": "/tmp/test.go"}`,
			want:     "/tmp/test.go",
		},
		{
			name:     "Write path",
			toolName: "Write",
			argsJSON: `{"path": "/home/user/main.go"}`,
			want:     "/home/user/main.go",
		},
		{
			name:     "Edit path",
			toolName: "Edit",
			argsJSON: `{"path": "src/app.go"}`,
			want:     "src/app.go",
		},
		{
			name:     "Glob pattern with dir",
			toolName: "Glob",
			argsJSON: `{"pattern": "*.go", "dir": "/src"}`,
			want:     "*.go  in /src",
		},
		{
			name:     "Grep pattern",
			toolName: "Grep",
			argsJSON: `{"pattern": "func main"}`,
			want:     "func main",
		},
		{
			name:     "WebFetch URL",
			toolName: "WebFetch",
			argsJSON: `{"url": "https://example.com"}`,
			want:     "https://example.com",
		},
		{
			name:     "WebSearch query",
			toolName: "WebSearch",
			argsJSON: `{"query": "golang patterns"}`,
			want:     "golang patterns",
		},
		{
			name:     "delegate_* task",
			toolName: "delegate_frontend",
			argsJSON: `{"task": "Fix the login page styling"}`,
			want:     "Fix the login page styling",
		},
		{
			name:     "MultiWrite single file",
			toolName: "MultiWrite",
			argsJSON: `{"files": [{"path": "a.go"}]}`,
			want:     "a.go",
		},
		{
			name:     "MultiWrite multiple files",
			toolName: "MultiWrite",
			argsJSON: `{"files": [{"path": "a.go"}, {"path": "b.go"}]}`,
			want:     "a.go, b.go",
		},
		{
			name:     "MultiEdit path",
			toolName: "MultiEdit",
			argsJSON: `{"path": "main.go"}`,
			want:     "main.go",
		},
		{
			name:     "Remember content (truncated)",
			toolName: "Remember",
			argsJSON: `{"content": "The user prefers tabs over spaces in all Go files"}`,
			want:     "The user prefers tabs over spaces in all Go files",
		},
		{
			name:     "RecallMemory query",
			toolName: "RecallMemory",
			argsJSON: `{"query": "user coding style"}`,
			want:     "user coding style",
		},
		{
			name:     "fallback path field",
			toolName: "UnknownTool",
			argsJSON: `{"path": "/some/file.txt"}`,
			want:     "/some/file.txt",
		},
		{
			name:     "fallback command field",
			toolName: "UnknownTool",
			argsJSON: `{"command": "make build"}`,
			want:     "make build",
		},
		{
			name:     "empty JSON",
			toolName: "Bash",
			argsJSON: `{}`,
			want:     "",
		},
		{
			name:     "invalid JSON",
			toolName: "Bash",
			argsJSON: `bad json`,
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolDisplay(tt.toolName, tt.argsJSON)
			if got != tt.want {
				t.Errorf("toolDisplay(%q) = %q, want %q", tt.argsJSON, got, tt.want)
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

// Test_addHistory tests history deduplication
func Test_addHistory(t *testing.T) {
	m := &model{}
	m.addHistory("first")
	m.addHistory("first") // consecutive duplicate — skipped
	m.addHistory("second")
	m.addHistory("third")

	if len(m.history) != 3 {
		t.Errorf("addHistory() got %d entries, want 3", len(m.history))
	}
	if m.history[0] != "first" || m.history[1] != "second" || m.history[2] != "third" {
		t.Errorf("addHistory() entries = %v", m.history)
	}

	// Non-consecutive same value is allowed
	m2 := &model{}
	m2.addHistory("a")
	m2.addHistory("b")
	m2.addHistory("a") // non-consecutive — allowed
	if len(m2.history) != 3 {
		t.Errorf("addHistory() non-consecutive got %d entries, want 3", len(m2.history))
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
