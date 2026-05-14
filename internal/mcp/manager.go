package mcp

import (
	"context"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Manager orchestrates MCP server lifecycle and tool enumeration.
type Manager struct {
	loader       *Loader
	clients      map[string]*Client
	toolMap      map[string][]tools.Tool         // server name -> wrapped tools
	virtualTools map[string]func() []tools.Tool // in-process tool providers
	mu           sync.RWMutex
	log          *logger.Logger
}

// NewManager creates a new Manager.
func NewManager(loader *Loader, log *logger.Logger) *Manager {
	return &Manager{
		loader:       loader,
		clients:      make(map[string]*Client),
		toolMap:      make(map[string][]tools.Tool),
		virtualTools: make(map[string]func() []tools.Tool),
		log:          log,
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
	// Fast path: already cached in toolMap.
	m.mu.RLock()
	if tools, ok := m.toolMap[serverName]; ok {
		m.mu.RUnlock()
		return tools
	}
	// Check virtual servers (in-process, no connection needed).
	if getter, ok := m.virtualTools[serverName]; ok {
		m.mu.RUnlock()
		tools := getter()
		m.mu.Lock()
		m.toolMap[serverName] = tools
		m.mu.Unlock()
		return tools
	}
	m.mu.RUnlock()

	// Slow path: connect to external MCP server.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check: may have been connected while waiting for write lock.
	if tools, ok := m.toolMap[serverName]; ok {
		return tools
	}
	if getter, ok := m.virtualTools[serverName]; ok {
		tools := getter()
		m.toolMap[serverName] = tools
		return tools
	}

	cfg := m.loader.Get()
	var serverCfg *ServerConfig
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == serverName {
			serverCfg = &cfg.Servers[i]
			break
		}
	}
	if serverCfg == nil || !serverCfg.Enabled {
		if m.log != nil {
			m.log.Warn(logger.CatMCP, "MCP server not found or disabled",
				"server", serverName,
			)
		}
		m.toolMap[serverName] = nil // cache negative result
		return nil
	}

	client := NewClient(*serverCfg, m.log)
	if err := client.Connect(ctx); err != nil {
		if m.log != nil {
			m.log.Error(logger.CatMCP, "failed to connect to MCP server",
				"server", serverName, "err", err.Error(),
			)
		}
		m.toolMap[serverName] = nil
		return nil
	}

	mcpTools := client.ListTools()
	wrapped := make([]tools.Tool, 0, len(mcpTools))
	for _, mt := range mcpTools {
		wrapped = append(wrapped, NewMCPTool(serverName, mt, client))
	}

	m.clients[serverName] = client
	m.toolMap[serverName] = wrapped
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
	currentNames := make(map[string]bool)
	for _, s := range cfg.Servers {
		currentNames[s.Name] = true
	}

	// Disconnect removed servers.
	for name, client := range m.clients {
		if !currentNames[name] {
			if err := client.Disconnect(); err != nil && m.log != nil {
				m.log.Warn(logger.CatMCP, "error disconnecting MCP server",
					"server", name, "err", err.Error(),
				)
			}
			delete(m.clients, name)
			delete(m.toolMap, name)
		}
	}

	// Disconnect changed servers so they reconnect with new config.
	for _, s := range cfg.Servers {
		if !s.Enabled {
			if client, ok := m.clients[s.Name]; ok {
				_ = client.Disconnect()
				delete(m.clients, s.Name)
				delete(m.toolMap, s.Name)
			}
			continue
		}
		client, exists := m.clients[s.Name]
		if exists {
			// Check if config changed by comparing key fields.
			if client.cfg.Command != s.Command ||
				!stringSlicesEqual(client.cfg.Args, s.Args) ||
				!stringMapsEqual(client.cfg.Env, s.Env) {
				_ = client.Disconnect()
				delete(m.clients, s.Name)
				delete(m.toolMap, s.Name)
			}
		}
	}

	if m.log != nil {
		m.log.Info(logger.CatMCP, "MCP config reloaded", "servers", len(cfg.Servers))
	}

	return nil
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
