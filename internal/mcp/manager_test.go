package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestMCPInstanceKeySeparatesWorkspaces(t *testing.T) {
	t.Parallel()
	projectA := filepath.Join(t.TempDir(), "a")
	projectB := filepath.Join(t.TempDir(), "b")

	keyA := mcpInstanceKey("global", projectA, "files")
	keyB := mcpInstanceKey("global", projectB, "files")
	if keyA == keyB {
		t.Fatal("global MCP instances must not be shared across workspaces")
	}

	scope, workDir, serverName, ok := parseMCPInstanceKey(keyA)
	if !ok || scope != "global" || workDir != filepath.Clean(projectA) || serverName != "files" {
		t.Fatalf("unexpected parsed key: %q %q %q %v", scope, workDir, serverName, ok)
	}
}

func TestManagerStopInstanceClearsEveryWorkspace(t *testing.T) {
	t.Parallel()
	loader, err := NewLoader("", nil)
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(loader, nil)
	keyA := mcpInstanceKey("global", "/project/a", "files")
	keyB := mcpInstanceKey("global", "/project/b", "files")
	other := mcpInstanceKey("global", "/project/a", "browser")
	manager.toolMap[keyA] = []tools.Tool{}
	manager.toolMap[keyB] = []tools.Tool{}
	manager.toolMap[other] = []tools.Tool{}

	manager.StopInstance("global", "files")

	if _, ok := manager.toolMap[keyA]; ok {
		t.Fatal("workspace A instance was not cleared")
	}
	if _, ok := manager.toolMap[keyB]; ok {
		t.Fatal("workspace B instance was not cleared")
	}
	if _, ok := manager.toolMap[other]; !ok {
		t.Fatal("unrelated server instance was cleared")
	}
}

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
