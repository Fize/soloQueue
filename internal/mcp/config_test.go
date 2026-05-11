package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Servers == nil {
		t.Error("DefaultConfig().Servers is nil")
	}
}

func TestLoader_LoadCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	l, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := l.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := l.Get()
	if cfg.Servers == nil {
		t.Error("Servers should not be nil after Load")
	}

	// Verify the file was created on disk.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatal(err)
	}
}

func TestLoader_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	input := Config{Servers: []ServerConfig{
		{Name: "test", Command: "echo", Args: []string{"hello"}, Transport: "stdio", Enabled: true},
	}}
	data, _ := json.MarshalIndent(input, "", "  ")
	os.WriteFile(path, data, 0o644)

	l, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Load(); err != nil {
		t.Fatal(err)
	}

	cfg := l.Get()
	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(cfg.Servers))
	}
	s := cfg.Servers[0]
	if s.Name != "test" || s.Command != "echo" {
		t.Errorf("unexpected server config: %+v", s)
	}
}

func TestLoader_Set(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	l, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Load()

	if err := l.Set(func(c *Config) {
		c.Servers = append(c.Servers, ServerConfig{Name: "added", Command: "cmd", Transport: "stdio", Enabled: true})
	}); err != nil {
		t.Fatal(err)
	}

	cfg := l.Get()
	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "added" {
		t.Errorf("unexpected config after Set: %+v", cfg)
	}
}

func TestConfig_UnmarshalMCPServers(t *testing.T) {
	data := []byte(`{
  "mcpServers": {
    "playwright": {
      "command": "npx",
      "args": ["@playwright/mcp@latest"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {"GITHUB_TOKEN": "abc123"},
      "enabled": false
    }
  }
}`)

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(cfg.Servers))
	}

	s0 := cfg.Servers[0]
	if s0.Name != "playwright" || s0.Command != "npx" || s0.Enabled != true {
		t.Errorf("unexpected server 0: %+v", s0)
	}
	if s0.Transport != "stdio" {
		t.Errorf("expected default transport stdio, got %q", s0.Transport)
	}

	s1 := cfg.Servers[1]
	if s1.Name != "github" || s1.Enabled != false {
		t.Errorf("unexpected server 1: %+v", s1)
	}
	if s1.Env["GITHUB_TOKEN"] != "abc123" {
		t.Errorf("unexpected env: %v", s1.Env)
	}
}

func TestConfig_UnmarshalServers(t *testing.T) {
	data := []byte(`{
  "servers": [
    {"name": "legacy", "command": "echo", "args": ["hi"], "transport": "sse", "enabled": false}
  ]
}`)

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}

	if len(cfg.Servers) != 1 || cfg.Servers[0].Name != "legacy" || cfg.Servers[0].Transport != "sse" {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestConfig_MarshalMCPServers(t *testing.T) {
	cfg := Config{
		Servers: []ServerConfig{
			{Name: "test", Command: "echo", Args: []string{"hello"}, Transport: "stdio", Enabled: true},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	// Parse back to verify mcpServers key.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	mcpServers, ok := raw["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level 'mcpServers' key, got %s", data)
	}

	srv, ok := mcpServers["test"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'test' key in mcpServers, got %v", mcpServers)
	}
	if srv["command"] != "echo" {
		t.Errorf("expected command 'echo', got %v", srv["command"])
	}

	// Verify round-trip via unmarshal.
	var parsed Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Servers) != 1 || parsed.Servers[0].Name != "test" {
		t.Errorf("round-trip failed: %+v", parsed)
	}
}
