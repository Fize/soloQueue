package lsp

import (
	"testing"
)

func TestBuiltinServers_NotEmpty(t *testing.T) {
	servers := BuiltinServers()
	if len(servers) == 0 {
		t.Error("BuiltinServers returned empty list")
	}
}

func TestBuiltinServers_HaveRequiredFields(t *testing.T) {
	for _, s := range BuiltinServers() {
		if s.ID == "" {
			t.Error("server has empty ID")
		}
		if s.Command == "" {
			t.Errorf("server %s has empty Command", s.ID)
		}
		if len(s.Extensions) == 0 {
			t.Errorf("server %s has no Extensions", s.ID)
		}
		if len(s.Languages) == 0 {
			t.Errorf("server %s has no Languages", s.ID)
		}
	}
}

func TestBuiltinServers_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, s := range BuiltinServers() {
		if seen[s.ID] {
			t.Errorf("duplicate server ID: %s", s.ID)
		}
		seen[s.ID] = true
	}
}

func TestResolveCommand_WithResolve(t *testing.T) {
	def := ServerDef{
		Resolve: func(workspacePath string) string {
			return "/custom/path/to/server"
		},
	}
	result := resolveCommand(def, "/workspace")
	if result != "/custom/path/to/server" {
		t.Errorf("resolveCommand = %q, want /custom/path/to/server", result)
	}
}

func TestResolveCommand_WithResolveReturnsEmpty(t *testing.T) {
	def := ServerDef{
		Resolve: func(workspacePath string) string {
			return ""
		},
	}
	result := resolveCommand(def, "/workspace")
	if result != "" {
		t.Errorf("resolveCommand = %q, want empty", result)
	}
}

func TestResolveCommand_LookPathFallback(t *testing.T) {
	def := ServerDef{
		Command: "nonexistent-binary-xyz",
	}
	result := resolveCommand(def, "/workspace")
	if result != "" {
		t.Errorf("resolveCommand for nonexistent binary = %q, want empty", result)
	}
}

func TestServerDef_Fields(t *testing.T) {
	def := ServerDef{
		ID:          "test-lsp",
		Command:     "test-server",
		Args:        []string{"--stdio"},
		Languages:   []string{"go"},
		Extensions:  []string{".go"},
		InstallHint: "go install example.com/test@latest",
	}
	if def.ID != "test-lsp" {
		t.Errorf("ID = %q", def.ID)
	}
	if def.Command != "test-server" {
		t.Errorf("Command = %q", def.Command)
	}
	if len(def.Args) != 1 || def.Args[0] != "--stdio" {
		t.Errorf("Args = %v", def.Args)
	}
	if len(def.Languages) != 1 || def.Languages[0] != "go" {
		t.Errorf("Languages = %v", def.Languages)
	}
}
