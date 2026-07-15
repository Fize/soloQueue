package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Manager manages multiple LSP client instances, routing tool calls
// to the correct server based on file extension.
type Manager struct {
	servers     map[string]*Client  // server ID -> client
	defs        map[string]ServerDef // server ID -> definition
	docs        map[string]*docInfo  // file path -> document info
	unavailable map[string]string    // server ID -> install hint (binary not found)

	extToServer map[string]string // extension -> server ID
	rootURI     string

	mu      sync.RWMutex
	log     *logger.Logger
	started bool
}

type docInfo struct {
	uri     string
	version int
	client  *Client
}

func clientKey(serverID, rootURI string) string {
	return serverID + "@" + rootURI
}

func (m *Manager) resolveRootPath(ctx context.Context, filePath string) string {
	if workDir := iface.WorkDirFromContext(ctx); workDir != "" {
		return workDir
	}
	if filePath != "" {
		return filepath.Dir(filePath)
	}
	return uriToPath(m.rootURI)
}

// NewManager creates a new LSP manager.
func NewManager(rootPath string, log *logger.Logger) *Manager {
	return &Manager{
		servers:     make(map[string]*Client),
		defs:        make(map[string]ServerDef),
		docs:        make(map[string]*docInfo),
		unavailable: make(map[string]string),
		extToServer: make(map[string]string),
		rootURI:     PathToURI(rootPath),
		log:         log,
	}
}

// Start launches all configured LSP servers that match files in the workspace.
// If no workspace files match, servers are started lazily on first tool call.
func (m *Manager) Start(ctx context.Context, defs []ServerDef) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	for _, def := range defs {
		m.defs[def.ID] = def
		for _, ext := range def.Extensions {
			if _, exists := m.extToServer[ext]; !exists {
				m.extToServer[ext] = def.ID
			}
		}
	}

	// Auto-start servers whose extensions match files in the workspace.
	exts := m.scanWorkspaceExtensions()
	started := make(map[string]bool)
	for ext := range exts {
		if serverID, ok := m.extToServer[ext]; ok {
			if !started[serverID] {
				if err := m.startClient(ctx, serverID, m.rootURI); err != nil {
					if m.log != nil {
						m.log.Warn(logger.CatMCP, "LSP server start failed, will start lazily",
							"server", serverID, "err", err.Error())
					}
				} else {
					started[serverID] = true
				}
			}
		}
	}

	m.started = true
	if m.log != nil {
		var serverNames []string
		for id := range started {
			serverNames = append(serverNames, id)
		}
		m.log.Info(logger.CatMCP, "LSP manager started",
			"root", m.rootURI, "servers_started", len(started), "servers", strings.Join(serverNames, ", "))
	}
	return nil
}

// Shutdown stops all LSP clients.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, client := range m.servers {
		client.Stop()
		if m.log != nil {
			m.log.Debug(logger.CatMCP, "LSP client stopped during shutdown", "server", id)
		}
	}
	m.servers = make(map[string]*Client)
	m.started = false
}

// GetTools returns all LSP tool instances for the LLM to use.
func (m *Manager) GetTools() []tools.Tool {
	return LSPTools(m)
}

// clientForFile returns (or starts) the LSP client for a given file.
func (m *Manager) clientForFile(ctx context.Context, filePath string) (*Client, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return nil, fmt.Errorf("no file extension for %q", filePath)
	}

	m.mu.RLock()
	serverID, ok := m.extToServer[ext]
	installHint, isUnavailable := m.unavailable[serverID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no LSP server registered for extension %q (file: %s)", ext, filePath)
	}

	// If binary was not found during startup, skip lazy-start and give actionable hint.
	if isUnavailable {
		msg := fmt.Sprintf("LSP server %q binary not found", serverID)
		if installHint != "" {
			msg += fmt.Sprintf("; to enable it run: %s", installHint)
		}
		return nil, fmt.Errorf("%s", msg)
	}

	rootPath := m.resolveRootPath(ctx, filePath)
	rootURI := PathToURI(rootPath)
	key := clientKey(serverID, rootURI)

	m.mu.RLock()
	client, exists := m.servers[key]
	m.mu.RUnlock()

	if exists && client != nil {
		return client, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock.
	if client, ok := m.servers[key]; ok && client != nil {
		return client, nil
	}

	if err := m.startClient(ctx, serverID, rootURI); err != nil {
		return nil, fmt.Errorf("start LSP server %q for %s: %w", serverID, rootURI, err)
	}
	return m.servers[key], nil
}

// GetAnyClient returns any currently active client, or attempts to start a default server.
func (m *Manager) GetAnyClient(ctx context.Context) (*Client, error) {
	rootPath := m.resolveRootPath(ctx, "")
	rootURI := PathToURI(rootPath)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Try active servers for this rootURI first
	for key, client := range m.servers {
		if strings.HasSuffix(key, "@"+rootURI) && client != nil {
			return client, nil
		}
	}

	// Try starting gopls by default
	if _, ok := m.defs["gopls"]; ok {
		if err := m.startClient(ctx, "gopls", rootURI); err == nil {
			return m.servers[clientKey("gopls", rootURI)], nil
		}
	}

	// Try starting any defined server
	for id := range m.defs {
		if err := m.startClient(ctx, id, rootURI); err == nil {
			return m.servers[clientKey(id, rootURI)], nil
		}
	}

	return nil, fmt.Errorf("no active or startable LSP server found for %s", rootURI)
}


// ensureOpen sends didOpen to the LSP server if the document hasn't been opened yet.
func (m *Manager) ensureOpen(client *Client, filePath, uri string) error {
	m.mu.RLock()
	_, opened := m.docs[filePath]
	m.mu.RUnlock()

	if opened {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %q: %w", filePath, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.docs[filePath]; ok {
		return nil
	}

	client.DidOpen(uri, string(content))
	m.docs[filePath] = &docInfo{uri: uri, version: 1, client: client}
	return nil
}

// NotifyFileChanged tells the LSP server that a file has been modified.
// Should be called after Write/Edit operations.
func (m *Manager) NotifyFileChanged(filePath string) error {
	m.mu.RLock()
	doc, docOpened := m.docs[filePath]
	m.mu.RUnlock()

	if !docOpened || doc == nil || doc.client == nil {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	m.mu.Lock()
	doc.version++
	ver := doc.version
	m.mu.Unlock()

	doc.client.DidChange(doc.uri, string(content), ver)
	return nil
}

// NotifyFileClosed tells the LSP server a file has been closed/deleted.
func (m *Manager) NotifyFileClosed(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if doc, ok := m.docs[filePath]; ok {
		if doc.client != nil {
			doc.client.DidClose(doc.uri)
		}
		delete(m.docs, filePath)
	}
}

// ServerIDs returns the list of configured server IDs.
func (m *Manager) ServerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id := range m.defs {
		ids = append(ids, id)
	}
	return ids
}

// RunningServerIDs returns the list of currently running server IDs.
func (m *Manager) RunningServerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id := range m.servers {
		ids = append(ids, id)
	}
	return ids
}

func (m *Manager) startClient(ctx context.Context, serverID string, rootURI string) error {
	def, ok := m.defs[serverID]
	if !ok {
		return fmt.Errorf("unknown LSP server: %q", serverID)
	}

	rootPath := uriToPath(rootURI)

	// Resolve the actual binary path. A custom Resolve func handles venv / node_modules.
	command := resolveCommand(def, rootPath)
	if command == "" {
		// Mark unavailable so clientForFile doesn't retry on every call.
		m.unavailable[serverID] = def.InstallHint
		if def.InstallHint != "" {
			return fmt.Errorf("LSP server %q: binary %q not found — run: %s", serverID, def.Command, def.InstallHint)
		}
		return fmt.Errorf("LSP server %q: binary %q not found in PATH", serverID, def.Command)
	}

	langID := serverID
	if len(def.Languages) > 0 {
		langID = def.Languages[0]
	}

	client := NewClient(def.ID, langID, rootURI, command, def.Args, m.log)
	if err := client.Start(ctx); err != nil {
		return err
	}
	key := clientKey(serverID, rootURI)
	m.servers[key] = client
	if m.log != nil {
		m.log.Debug(logger.CatMCP, "LSP server connected",
			"server", serverID, "command", command, "root", rootPath)
	}
	return nil
}

// scanWorkspaceExtensions scans the workspace root for file extensions.
func (m *Manager) scanWorkspaceExtensions() map[string]bool {
	rootPath := uriToPath(m.rootURI)
	exts := make(map[string]bool)

	// Only scan top-level and one level deep to avoid huge directories.
	depth := 2
	filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(rootPath, path)
			if strings.Count(rel, string(os.PathSeparator)) >= depth {
				return filepath.SkipDir
			}
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") && base != "." {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			exts[ext] = true
		}
		return nil
	})
	return exts
}
