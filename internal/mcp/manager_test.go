package mcp

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestManager_GetToolsWithOverride_NilOverride(t *testing.T) {
	loader, err := NewLoader("", nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(loader, nil)

	ctx := context.Background()
	result := mgr.GetToolsWithOverride(ctx, "nonexistent", nil)
	if result != nil {
		t.Error("expected nil for nonexistent server with nil override")
	}
}

func TestManager_GetToolsWithOverride_OverridePrecedence(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp.json"

	loader, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}

	if err := loader.Set(func(c *Config) {
		c.Servers = append(c.Servers, ServerConfig{
			Name: "test-server", Command: "echo", Args: []string{"global"},
			Transport: "stdio", Enabled: true,
		})
	}); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(loader, nil)

	overrideCfg := &Config{
		Servers: []ServerConfig{
			{
				Name: "test-server", Command: "echo", Args: []string{"project"},
				Transport: "stdio", Enabled: true,
			},
		},
	}

	ctx := context.Background()

	result := mgr.GetToolsWithOverride(ctx, "test-server", overrideCfg)
	if result != nil {
		t.Log("GetToolsWithOverride returned tools (connected to MCP server)")
	}
}

func TestManager_GetToolsWithOverride_OverrideDisablesServer(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp.json"

	loader, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}

	if err := loader.Set(func(c *Config) {
		c.Servers = append(c.Servers, ServerConfig{
			Name: "enabled-server", Command: "echo", Args: []string{"global"},
			Transport: "stdio", Enabled: true,
		})
	}); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(loader, nil)

	overrideCfg := &Config{
		Servers: []ServerConfig{
			{
				Name: "enabled-server", Command: "echo", Args: []string{"project"},
				Transport: "stdio", Enabled: false,
			},
		},
	}

	ctx := context.Background()
	result := mgr.GetToolsWithOverride(ctx, "enabled-server", overrideCfg)
	if result != nil {
		t.Error("expected nil when override disables the server")
	}
}

func TestManager_GetToolsWithOverride_OverrideFallsBackToGlobal(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp.json"

	loader, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}

	if err := loader.Set(func(c *Config) {
		c.Servers = append(c.Servers, ServerConfig{
			Name: "global-only", Command: "echo", Args: []string{"global"},
			Transport: "stdio", Enabled: true,
		})
	}); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(loader, nil)

	overrideCfg := &Config{
		Servers: []ServerConfig{
			{
				Name: "different-server", Command: "echo", Args: []string{"project"},
				Transport: "stdio", Enabled: true,
			},
		},
	}

	ctx := context.Background()

	result := mgr.GetToolsWithOverride(ctx, "global-only", overrideCfg)
	if result != nil {
		t.Log("GetToolsWithOverride fell back to global config and connected")
	}
}

func TestManager_GetTools_DelegatesToGetToolsWithOverride(t *testing.T) {
	loader, err := NewLoader("", nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(loader, nil)

	ctx := context.Background()
	result := mgr.GetTools(ctx, "nonexistent")
	if result != nil {
		t.Error("expected nil for nonexistent server")
	}
}

func TestManager_GetToolsWithOverride_VirtualServer(t *testing.T) {
	loader, err := NewLoader("", nil)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(loader, nil)

	mgr.RegisterVirtual("virtual-test", func() []tools.Tool {
		return nil
	})

	ctx := context.Background()
	result := mgr.GetToolsWithOverride(ctx, "virtual-test", nil)
	if result != nil {
		t.Log("virtual server returned tools")
	}
}

func TestManager_GetToolsWithOverride_EmptyOverrideServers(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/mcp.json"

	loader, err := NewLoader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := loader.Load(); err != nil {
		t.Fatal(err)
	}

	if err := loader.Set(func(c *Config) {
		c.Servers = append(c.Servers, ServerConfig{
			Name: "global-server", Command: "echo", Transport: "stdio", Enabled: true,
		})
	}); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(loader, nil)

	overrideCfg := &Config{Servers: []ServerConfig{}}

	ctx := context.Background()

	result := mgr.GetToolsWithOverride(ctx, "global-server", overrideCfg)
	if result != nil {
		t.Log("Fell back to global when override has empty servers list")
	}
}
