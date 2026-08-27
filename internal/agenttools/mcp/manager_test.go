package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
)

func TestMCPInstanceKeySeparatesWorkspaces(t *testing.T) {
	t.Parallel()
	projectA := filepath.Join(t.TempDir(), "a")
	projectB := filepath.Join(t.TempDir(), "b")
	cfg := &ServerConfig{Name: "files", Command: "files", Enabled: true}

	keyA := mcpInstanceKey(projectA, "files", "global", cfg)
	keyB := mcpInstanceKey(projectB, "files", "global", cfg)
	if keyA == keyB {
		t.Fatal("global MCP instances must not be shared across workspaces")
	}
}

type cachedTestTool struct{ name string }

func (t cachedTestTool) Name() string                                  { return t.name }
func (cachedTestTool) Description() string                             { return "cached test tool" }
func (cachedTestTool) Parameters() json.RawMessage                     { return nil }
func (cachedTestTool) Execute(context.Context, string) (string, error) { return "", nil }

func TestManager_GetToolsWithOverride_GlobalThenDisabledProjectDoNotShareCache(t *testing.T) {
	loader, err := NewLoader("", nil)
	if err != nil {
		t.Fatal(err)
	}
	global := ServerConfig{
		Name: "same", Command: "global-command", Args: []string{"global"},
		Env: map[string]string{"SOURCE": "global"}, Transport: "stdio", Enabled: true,
	}
	loader.current = Config{Servers: []ServerConfig{global}}
	mgr := NewManager(loader, nil)
	workDir := t.TempDir()
	ctx := iface.ContextWithWorkDir(context.Background(), workDir)
	globalTools := []tools.Tool{cachedTestTool{name: "global"}}
	mgr.toolMap[mcpInstanceKey(workDir, global.Name, "global", &global)] = globalTools

	if got := mgr.GetToolsWithOverride(ctx, global.Name, nil); len(got) != 1 || got[0].Name() != "global" {
		t.Fatalf("global lookup = %v, want cached global tool", got)
	}
	project := &Config{Servers: []ServerConfig{{
		Name: "same", Command: "project-command", Args: []string{"project"},
		Env: map[string]string{"SOURCE": "project"}, Transport: "stdio", Enabled: false,
	}}}
	if got := mgr.GetToolsWithOverride(ctx, global.Name, project); got != nil {
		t.Fatalf("disabled project override reused global cache: %v", got)
	}
}

func TestManager_GetToolsWithOverride_DisabledProjectThenGlobalDoNotShareCache(t *testing.T) {
	loader, err := NewLoader("", nil)
	if err != nil {
		t.Fatal(err)
	}
	global := ServerConfig{
		Name: "same", Command: "global-command", Args: []string{"global"},
		Env: map[string]string{"SOURCE": "global"}, Transport: "stdio", Enabled: true,
	}
	loader.current = Config{Servers: []ServerConfig{global}}
	mgr := NewManager(loader, nil)
	workDir := t.TempDir()
	ctx := iface.ContextWithWorkDir(context.Background(), workDir)
	globalTools := []tools.Tool{cachedTestTool{name: "global"}}
	mgr.toolMap[mcpInstanceKey(workDir, global.Name, "global", &global)] = globalTools
	project := &Config{Servers: []ServerConfig{{
		Name: "same", Command: "project-command", Args: []string{"project"},
		Env: map[string]string{"SOURCE": "project"}, Transport: "stdio", Enabled: false,
	}}}

	if got := mgr.GetToolsWithOverride(ctx, global.Name, project); got != nil {
		t.Fatalf("disabled project lookup = %v, want nil", got)
	}
	if got := mgr.GetToolsWithOverride(ctx, global.Name, nil); len(got) != 1 || got[0].Name() != "global" {
		t.Fatalf("global lookup was suppressed by project cache: %v", got)
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
