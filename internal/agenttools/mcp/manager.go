package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

// Manager orchestrates MCP server lifecycle and tool enumeration.
type Manager struct {
	loader       *Loader
	clients      map[string]*Client
	toolMap      map[string][]tools.Tool        // server name -> wrapped tools
	virtualTools map[string]func() []tools.Tool // in-process tool providers
	mu           sync.RWMutex
	log          *logger.Logger
	policies     *PolicyStore
	runtimeMgr   *tools.RuntimeManager
}

// NewManager creates a new Manager.
func NewManager(loader *Loader, log *logger.Logger) *Manager {
	return NewManagerWithPolicy(loader, nil, nil, log)
}

// NewManagerWithPolicy creates the production manager. mcp.json remains the
// server definition source; runtime approval is resolved separately.
func NewManagerWithPolicy(loader *Loader, policies *PolicyStore, runtimeMgr *tools.RuntimeManager, log *logger.Logger) *Manager {
	return &Manager{
		loader:       loader,
		clients:      make(map[string]*Client),
		toolMap:      make(map[string][]tools.Tool),
		virtualTools: make(map[string]func() []tools.Tool),
		log:          log,
		policies:     policies,
		runtimeMgr:   runtimeMgr,
	}
}

// RegisterVirtual registers an in-process tool provider under a virtual server name.
// The getTools function is called each time GetTools is invoked for this server name,
// allowing the provider to return fresh tool instances.
func (m *Manager) RegisterVirtual(name string, getTools func() []tools.Tool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.virtualTools[name] = getTools
	if m.log != nil {
		m.log.Debug(logger.CatMCP, "virtual MCP server registered", "server", name)
	}
}

// GetTools returns wrapped tools.Tool instances for the named server.
// Connects lazily on first call. For virtual servers, calls the registered getter.
// Returns nil if the server is not found or disabled.
func (m *Manager) GetTools(ctx context.Context, serverName string) []tools.Tool {
	return m.GetToolsWithOverride(ctx, serverName, nil)
}

// GetToolsWithOverride returns wrapped tools for the named server, with an optional
// project-level config override. If the override config contains the server, it takes
// precedence over the global config (project-level covers global).
func (m *Manager) GetToolsWithOverride(ctx context.Context, serverName string, overrideCfg *Config) []tools.Tool {
	scope, workDir, projectOverride := m.resolveScope(ctx, serverName, overrideCfg)
	instanceKey := mcpInstanceKey(scope, workDir, serverName)

	// Fast path for virtual providers is global and in-process.
	m.mu.RLock()
	if getter, ok := m.virtualTools[serverName]; ok {
		m.mu.RUnlock()
		tools := getter()
		m.mu.Lock()
		m.toolMap[serverName] = tools
		m.mu.Unlock()
		return tools
	}
	if cached, ok := m.toolMap[instanceKey]; ok {
		m.mu.RUnlock()
		return cached
	}
	m.mu.RUnlock()

	// Slow path: connect to external MCP server.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check: may have been connected while waiting for write lock.
	if cached, ok := m.toolMap[instanceKey]; ok {
		return cached
	}
	if getter, ok := m.virtualTools[serverName]; ok {
		tools := getter()
		m.toolMap[serverName] = tools
		return tools
	}

	// 1. Check override config first (project-level takes precedence)
	var serverCfg *ServerConfig
	if projectOverride {
		for i := range overrideCfg.Servers {
			if overrideCfg.Servers[i].Name == serverName {
				serverCfg = &overrideCfg.Servers[i]
				break
			}
		}
	}

	// 2. Fall back to global config
	if serverCfg == nil {
		cfg := m.loader.Get()
		for i := range cfg.Servers {
			if cfg.Servers[i].Name == serverName {
				serverCfg = &cfg.Servers[i]
				break
			}
		}
	}

	if serverCfg == nil || !serverCfg.Enabled {
		if m.log != nil {
			m.log.Warn(logger.CatMCP, "MCP server not found or disabled",
				"server", serverName,
			)
		}
		m.toolMap[instanceKey] = nil // cache negative result
		return nil
	}

	runtime := tools.ToolRuntime(tools.NewHostRuntime())
	if m.policies != nil {
		policy, err := m.policies.Effective(ctx, scope, *serverCfg)
		if err != nil || policy.State != PolicyApproved {
			if m.log != nil {
				fields := []any{"server", serverName, "scope", scope}
				if err != nil {
					fields = append(fields, "err", err.Error())
				} else {
					fields = append(fields, "state", policy.State, "runtime", policy.Runtime)
				}
				m.log.Warn(logger.CatMCP, "MCP policy is not approved", fields...)
			}
			m.toolMap[instanceKey] = nil
			return nil
		}
		if m.runtimeMgr == nil {
			m.toolMap[instanceKey] = nil
			return nil
		}
		runtime = m.runtimeMgr.ViewForPolicy(
			policy.Runtime,
			"mcp:"+scope+":"+serverName,
			workDir,
			"",
			policy.NetworkEnabled,
		)
	} else if m.runtimeMgr != nil {
		runtime = m.runtimeMgr.ViewOwned("mcp-legacy:"+serverName, workDir, "")
	}

	client := NewClientWithRuntime(*serverCfg, runtime, workDir, m.log)
	if err := client.Connect(ctx); err != nil {
		if m.log != nil {
			m.log.Error(logger.CatMCP, "failed to connect to MCP server",
				"server", serverName, "err", err.Error(),
			)
		}
		m.toolMap[instanceKey] = nil
		return nil
	}

	mcpTools := client.ListTools()
	wrapped := make([]tools.Tool, 0, len(mcpTools))
	for _, mt := range mcpTools {
		wrapped = append(wrapped, NewMCPTool(serverName, mt, client))
	}

	m.clients[instanceKey] = client
	m.toolMap[instanceKey] = wrapped
	return wrapped
}

// Reload re-reads mcp.json and disconnects servers that were removed or changed.
func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reload the config file first.
	if err := m.loader.Load(); err != nil {
		return err
	}
	cfg := m.loader.Get()

	// Definition changes invalidate digest-bound approvals. Tear down all
	// external instances so the next use re-evaluates policy before launch.
	for key, client := range m.clients {
		if err := client.Disconnect(); err != nil && m.log != nil {
			m.log.Warn(logger.CatMCP, "error disconnecting MCP server",
				"instance", key, "err", err.Error(),
			)
		}
		delete(m.clients, key)
	}
	m.toolMap = make(map[string][]tools.Tool)

	if m.log != nil {
		m.log.Info(logger.CatMCP, "MCP config reloaded", "servers", len(cfg.Servers))
	}

	return nil
}

func (m *Manager) resolveScope(ctx context.Context, serverName string, overrideCfg *Config) (scope, workDir string, projectOverride bool) {
	workDir = iface.WorkDirFromContext(ctx)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	workDir = filepath.Clean(workDir)
	if overrideCfg != nil {
		for i := range overrideCfg.Servers {
			if overrideCfg.Servers[i].Name == serverName {
				return "project:" + workDir, workDir, true
			}
		}
	}
	return "global", workDir, false
}

// StopInstance revokes the live grant by terminating the matching process and
// clearing cached wrappers. Policy persistence is handled by PolicyStore.
func (m *Manager) StopInstance(scope, serverName string) {
	normalizedScope := NormalizePolicyScope(scope)
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, client := range m.clients {
		keyScope, _, keyServer, ok := parseMCPInstanceKey(key)
		if !ok || keyScope != normalizedScope || keyServer != serverName {
			continue
		}
		_ = client.Disconnect()
		delete(m.clients, key)
		delete(m.toolMap, key)
	}
	for key := range m.toolMap {
		keyScope, _, keyServer, ok := parseMCPInstanceKey(key)
		if ok && keyScope == normalizedScope && keyServer == serverName {
			delete(m.toolMap, key)
		}
	}
}

func mcpInstanceKey(scope, workDir, serverName string) string {
	return NormalizePolicyScope(scope) + "\x00" + filepath.Clean(workDir) + "\x00" + serverName
}

func parseMCPInstanceKey(key string) (scope, workDir, serverName string, ok bool) {
	parts := strings.SplitN(key, "\x00", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func (m *Manager) PolicyStore() *PolicyStore { return m.policies }

func (m *Manager) HostExceptionCount() int {
	if m.policies != nil {
		if count, err := m.policies.CountApprovedHost(context.Background()); err == nil {
			return count
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, client := range m.clients {
		if client.RuntimeType() == tools.RuntimeHost {
			count++
		}
	}
	return count
}

// Shutdown disconnects all MCP clients.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		if err := client.Disconnect(); err != nil && m.log != nil {
			m.log.Warn(logger.CatMCP, "error disconnecting MCP server during shutdown",
				"server", name, "err", err.Error(),
			)
		}
	}
	m.clients = make(map[string]*Client)
	m.toolMap = make(map[string][]tools.Tool)
	m.virtualTools = make(map[string]func() []tools.Tool)
}

// VirtualServerNames returns the names of all registered virtual servers.
func (m *Manager) VirtualServerNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.virtualTools))
	for name := range m.virtualTools {
		names = append(names, name)
	}
	return names
}

// Loader returns the underlying config loader.
func (m *Manager) Loader() *Loader {
	return m.loader
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
