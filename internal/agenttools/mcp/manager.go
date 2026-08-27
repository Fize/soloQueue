package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// Manager orchestrates MCP server lifecycle and tool enumeration.
type Manager struct {
	loader       *Loader
	clients      map[string]*Client
	toolMap      map[string][]tools.Tool        // server name -> wrapped tools
	virtualTools map[string]func() []tools.Tool // in-process tool providers
	mu           sync.RWMutex
	log          *logger.Logger
	executor     *tools.Executor
}

// NewManager creates a new Manager.
func NewManager(loader *Loader, log *logger.Logger) *Manager {
	return NewManagerWithExecutor(loader, nil, log)
}

// NewManagerWithExecutor creates a manager using the shared process launcher.
func NewManagerWithExecutor(loader *Loader, executor *tools.Executor, log *logger.Logger) *Manager {
	return &Manager{
		loader:       loader,
		clients:      make(map[string]*Client),
		toolMap:      make(map[string][]tools.Tool),
		virtualTools: make(map[string]func() []tools.Tool),
		log:          log,
		executor:     executor,
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
	workDir := m.resolveWorkDir(ctx)
	serverCfg, configSource := m.effectiveServerConfig(serverName, overrideCfg)
	instanceKey := mcpInstanceKey(workDir, serverName, configSource, serverCfg)

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

	if serverCfg == nil || !serverCfg.Enabled {
		if m.log != nil {
			m.log.Warn(logger.CatMCP, "MCP server not found or disabled",
				"server", serverName,
			)
		}
		m.toolMap[instanceKey] = nil // cache negative result
		return nil
	}

	executor := m.executor
	if executor == nil {
		executor = tools.NewExecutor()
	}
	client := NewClientWithExecutor(*serverCfg, executor, workDir, m.log)
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

	// Tear down external instances so the next use applies the reloaded config.
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

func (m *Manager) resolveWorkDir(ctx context.Context) string {
	workDir := iface.WorkDirFromContext(ctx)
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return filepath.Clean(workDir)
}

func (m *Manager) effectiveServerConfig(serverName string, overrideCfg *Config) (*ServerConfig, string) {
	if overrideCfg != nil {
		for i := range overrideCfg.Servers {
			if overrideCfg.Servers[i].Name == serverName {
				return &overrideCfg.Servers[i], "project"
			}
		}
	}
	cfg := m.loader.Get()
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == serverName {
			return &cfg.Servers[i], "global"
		}
	}
	return nil, "global"
}

func mcpInstanceKey(workDir, serverName, configSource string, cfg *ServerConfig) string {
	definition, _ := json.Marshal(cfg)
	digest := sha256.Sum256(definition)
	return filepath.Clean(workDir) + "\x00" + serverName + "\x00" + configSource + "\x00" + hex.EncodeToString(digest[:])
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
