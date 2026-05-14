package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Manager manages multiple LSP client instances, routing tool calls
// to the correct server based on file extension.
type Manager struct {
	servers map[string]*Client  // server ID -> client
	defs    map[string]ServerDef // server ID -> definition
	docs    map[string]*docInfo  // file path -> document info

	extToServer map[string]string // extension -> server ID
	rootURI     string

	mu        sync.RWMutex
	log       *logger.Logger
	started   bool
}

type docInfo struct {
	uri     string
	version int
}

// NewManager creates a new LSP manager.
func NewManager(rootPath string, log *logger.Logger) *Manager {
	return &Manager{
		servers:     make(map[string]*Client),
		defs:        make(map[string]ServerDef),
		docs:        make(map[string]*docInfo),
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
				if err := m.startClient(ctx, serverID); err != nil {
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
func (m *Manager) clientForFile(filePath string) (*Client, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return nil, fmt.Errorf("no file extension for %q", filePath)
	}

	m.mu.RLock()
	serverID, ok := m.extToServer[ext]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no LSP server registered for extension %q (file: %s)", ext, filePath)
	}

	m.mu.RLock()
	client, exists := m.servers[serverID]
	m.mu.RUnlock()

	if exists && client != nil {
		return client, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check
	if client, ok := m.servers[serverID]; ok && client != nil {
		return client, nil
	}

	if err := m.startClient(context.Background(), serverID); err != nil {
		return nil, fmt.Errorf("start LSP server %q: %w", serverID, err)
	}
	return m.servers[serverID], nil
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
	m.docs[filePath] = &docInfo{uri: uri, version: 1}
	return nil
}

// NotifyFileChanged tells the LSP server that a file has been modified.
// Should be called after Write/Edit operations.
func (m *Manager) NotifyFileChanged(filePath string) error {
	ext := strings.ToLower(filepath.Ext(filePath))
	m.mu.RLock()
	serverID, ok := m.extToServer[ext]
	if !ok {
		m.mu.RUnlock()
		return nil // no LSP server, nothing to notify
	}
	client, exists := m.servers[serverID]
	_, docOpened := m.docs[filePath]
	m.mu.RUnlock()

	if !exists || client == nil || !docOpened {
		return nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.docs[filePath].version++
	ver := m.docs[filePath].version
	m.mu.Unlock()

	uri := PathToURI(filePath)
	client.DidChange(uri, string(content), ver)
	return nil
}

// NotifyFileClosed tells the LSP server a file has been closed/deleted.
func (m *Manager) NotifyFileClosed(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.docs[filePath]; ok {
		ext := strings.ToLower(filepath.Ext(filePath))
		if serverID, ok := m.extToServer[ext]; ok {
			if client, ok := m.servers[serverID]; ok && client != nil {
				client.DidClose(PathToURI(filePath))
			}
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

func (m *Manager) startClient(ctx context.Context, serverID string) error {
	def, ok := m.defs[serverID]
	if !ok {
		return fmt.Errorf("unknown LSP server: %q", serverID)
	}

	langID := serverID
	if len(def.Languages) > 0 {
		langID = def.Languages[0]
	}

	rootPath := uriToPath(m.rootURI)
	client := NewClient(def.ID, langID, m.rootURI, def.Command, def.Args, m.log)
	if err := client.Start(ctx); err != nil {
		return err
	}
	m.servers[serverID] = client
	if m.log != nil {
		m.log.Debug(logger.CatMCP, "LSP server connected",
			"server", serverID, "command", def.Command, "root", rootPath)
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
