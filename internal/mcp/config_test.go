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
