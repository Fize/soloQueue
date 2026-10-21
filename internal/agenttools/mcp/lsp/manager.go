package lsp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// Keep unused language processes bounded without changing lazy startup.
const defaultIdleTimeout = 10 * time.Minute

type clientUsage struct {
	active   int
	deadline time.Time
}

type retiringClient struct {
	key    string
	client *Client
	done   chan struct{}
}

type startingClient struct {
	done   chan struct{}
	cancel context.CancelFunc
}

// Manager owns lazy processes and their operation leases.
type Manager struct {
	servers map[string]*Client   // server ID -> client
	defs    map[string]ServerDef // server ID -> definition
	docs    map[string]*docInfo  // file path -> document info

	extToServer map[string]string // extension -> server ID
	rootURI     string
	executor    *tools.Executor

	mu            sync.RWMutex
	log           *logger.Logger
	started       bool
	lifecycleMu   sync.Mutex
	usage         map[*Client]*clientUsage
	retiring      map[string]chan struct{}
	starting      map[string]*startingClient
	idleTimeout   time.Duration
	sweepInterval time.Duration
	now           func() time.Time
	reaperStop    chan struct{}
	reaperDone    chan struct{}
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
	return NewManagerWithExecutor(rootPath, tools.NewExecutor(), log)
}

// NewManagerWithExecutor creates an LSP manager.
func NewManagerWithExecutor(rootPath string, executor *tools.Executor, log *logger.Logger) *Manager {
	if executor == nil {
		executor = tools.NewExecutor()
	}
	return &Manager{
		servers:       make(map[string]*Client),
		defs:          make(map[string]ServerDef),
		docs:          make(map[string]*docInfo),
		extToServer:   make(map[string]string),
		rootURI:       PathToURI(rootPath),
		executor:      executor,
		log:           log,
		usage:         make(map[*Client]*clientUsage),
		retiring:      make(map[string]chan struct{}),
		starting:      make(map[string]*startingClient),
		idleTimeout:   defaultIdleTimeout,
		sweepInterval: time.Minute,
		now:           time.Now,
	}
}

// Start registers definitions without scanning the workspace or launching processes.
// Language servers are started on demand so unused tooling cannot delay startup.
func (m *Manager) Start(_ context.Context, defs []ServerDef) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
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

	m.started = true
	m.reaperStop = make(chan struct{})
	m.reaperDone = make(chan struct{})
	go m.runReaper(m.reaperStop, m.reaperDone)
	if m.log != nil {
		m.log.Info(logger.CatMCP, "LSP manager started",
			"root", m.rootURI, "servers_started", 0)
	}
	return nil
}

// Shutdown stops all LSP clients.
func (m *Manager) Shutdown() {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return
	}
	m.started = false
	close(m.reaperStop)
	reaperDone := m.reaperDone
	var retired []retiringClient
	for key, client := range m.servers {
		retired = append(retired, m.retireLocked(key, client))
	}
	waiting := make([]chan struct{}, 0, len(m.retiring))
	for _, done := range m.retiring {
		waiting = append(waiting, done)
	}
	var initializing []chan struct{}
	for _, start := range m.starting {
		start.cancel()
		initializing = append(initializing, start.done)
	}
	m.mu.Unlock()
	for _, entry := range retired {
		m.stopRetired(entry)
	}
	for _, done := range initializing {
		<-done
	}
	<-reaperDone
	for _, done := range waiting {
		<-done
	}
}

func (m *Manager) runReaper(stop, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.reapIdle()
		}
	}
}

func (m *Manager) reapIdle() {
	m.mu.Lock()
	var retired []retiringClient
	if m.started {
		now := m.now()
		for key, client := range m.servers {
			state := m.usage[client]
			if state.active == 0 && (!client.alive() || !now.Before(state.deadline)) {
				retired = append(retired, m.retireLocked(key, client))
			}
		}
	}
	m.mu.Unlock()
	for _, entry := range retired {
		m.stopRetired(entry)
	}
}

// retireLocked claims a generation before releasing the lock. Acquirers wait
// for its actual process exit; stale releases cannot affect its replacement.
func (m *Manager) retireLocked(key string, client *Client) retiringClient {
	delete(m.servers, key)
	delete(m.usage, client)
	for path, doc := range m.docs {
		if doc.client == client {
			delete(m.docs, path)
		}
	}
	done := make(chan struct{})
	m.retiring[key] = done
	return retiringClient{key: key, client: client, done: done}
}

func (m *Manager) stopRetired(entry retiringClient) {
	entry.client.Stop()
	m.mu.Lock()
	if m.retiring[entry.key] == entry.done {
		delete(m.retiring, entry.key)
		close(entry.done)
	}
	m.mu.Unlock()
}

func (m *Manager) release(client *Client) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if state := m.usage[client]; state != nil {
		state.active--
		if state.active == 0 {
			state.deadline = m.now().Add(m.idleTimeout)
		}
	}
}

// Restart terminates active language servers and registers their definitions
// again; processes are recreated on demand.
func (m *Manager) Restart(ctx context.Context) error {
	m.mu.RLock()
	defs := make([]ServerDef, 0, len(m.defs))
	for _, def := range m.defs {
		defs = append(defs, def)
	}
	m.mu.RUnlock()

	m.Shutdown()
	return m.Start(ctx, defs)
}

// GetTools returns all LSP tool instances for the LLM to use.
func (m *Manager) GetTools() []tools.Tool {
	return LSPTools(m)
}

// clientForFile returns (or starts) the LSP client for a given file.
func (m *Manager) clientForFile(ctx context.Context, filePath string) (*Client, error) {
	client, release, err := m.acquireForFile(ctx, filePath)
	if err == nil {
		release()
	}
	return client, err
}

func (m *Manager) acquireForFile(ctx context.Context, filePath string) (*Client, func(), error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return nil, nil, fmt.Errorf("no file extension for %q", filePath)
	}
	m.mu.RLock()
	serverID, ok := m.extToServer[ext]
	m.mu.RUnlock()
	if !ok {
		return nil, nil, fmt.Errorf("no LSP server registered for extension %q (file: %s)", ext, filePath)
	}
	return m.acquireServer(ctx, serverID, PathToURI(m.resolveRootPath(ctx, filePath)))
}

func (m *Manager) acquireServer(ctx context.Context, serverID, rootURI string) (*Client, func(), error) {
	key := clientKey(serverID, rootURI)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		m.mu.Lock()
		if !m.started {
			m.mu.Unlock()
			return nil, nil, fmt.Errorf("LSP manager is stopped")
		}
		if done := m.retiring[key]; done != nil {
			m.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		if start := m.starting[key]; start != nil {
			m.mu.Unlock()
			select {
			case <-start.done:
				continue
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
		client := m.servers[key]
		if client != nil && !client.alive() {
			entry := m.retireLocked(key, client)
			m.mu.Unlock()
			m.stopRetired(entry)
			continue
		}
		if client == nil {
			def := m.defs[serverID]
			startCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
			start := &startingClient{done: make(chan struct{}), cancel: cancel}
			m.starting[key] = start
			m.mu.Unlock()
			fresh, err := m.startClient(startCtx, serverID, rootURI, def)
			m.mu.Lock()
			if err == nil && (!m.started || startCtx.Err() != nil) {
				err = fmt.Errorf("LSP startup canceled")
			}
			cancel()
			if err != nil {
				m.mu.Unlock()
				if fresh != nil {
					fresh.Stop()
				}
				m.mu.Lock()
				delete(m.starting, key)
				close(start.done)
				m.mu.Unlock()
				return nil, nil, fmt.Errorf("start LSP server %q for %s: %w", serverID, rootURI, err)
			}
			client = fresh
			m.servers[key] = client
			m.usage[client] = &clientUsage{deadline: m.now().Add(m.idleTimeout)}
			delete(m.starting, key)
			close(start.done)
		}
		m.usage[client].active++
		m.mu.Unlock()
		var once sync.Once
		return client, func() { once.Do(func() { m.release(client) }) }, nil
	}
}

// GetAnyClient returns a live client for the current workspace, starting one
// lazily if necessary. Tool calls use acquireAny to hold it until completion.
func (m *Manager) GetAnyClient(ctx context.Context) (*Client, error) {
	client, release, err := m.acquireAny(ctx)
	if err == nil {
		release()
	}
	return client, err
}

func (m *Manager) acquireAny(ctx context.Context) (*Client, func(), error) {
	rootURI := PathToURI(m.resolveRootPath(ctx, ""))
	m.mu.RLock()
	var ids []string
	for key, client := range m.servers {
		if strings.HasSuffix(key, "@"+rootURI) {
			ids = append(ids, client.id)
		}
	}
	if _, ok := m.defs["gopls"]; ok {
		ids = append(ids, "gopls")
	}
	for id := range m.defs {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		client, release, err := m.acquireServer(ctx, id, rootURI)
		if err == nil {
			return client, release, nil
		}
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
	}
	return nil, nil, fmt.Errorf("no active or startable LSP server found for %s", rootURI)
}

// ensureOpen sends didOpen to the LSP server if the document hasn't been opened yet.
func (m *Manager) ensureOpen(client *Client, filePath, uri string) error {
	return m.ensureOpenContext(context.Background(), client, filePath, uri)
}

func (m *Manager) ensureOpenContext(ctx context.Context, client *Client, filePath, uri string) error {
	client.documentsMu.Lock()
	defer client.documentsMu.Unlock()
	m.mu.RLock()
	doc := m.docs[filePath]
	current := m.usage[client] != nil && client.alive()
	m.mu.RUnlock()
	if !current {
		return fmt.Errorf("language server generation is no longer active")
	}
	if doc != nil && doc.client == client {
		return nil
	}
	content, err := client.executor.ReadFile(ctx, filePath, tools.ReadFileOptions{})
	if err != nil {
		return fmt.Errorf("read file %q: %w", filePath, err)
	}
	if err := client.didOpen(ctx, uri, string(content.Data)); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// A crash may have replaced this client while the file was being read/sent.
	if m.usage[client] == nil || !client.alive() {
		return fmt.Errorf("language server generation is no longer active")
	}
	m.docs[filePath] = &docInfo{uri: uri, version: 1, client: client}
	return nil
}

// NotifyFileChanged tells the LSP server that a file has been modified.
// Should be called after Write/Edit operations.
func (m *Manager) NotifyFileChanged(filePath string) error {
	m.mu.Lock()
	doc, docOpened := m.docs[filePath]
	if !docOpened || doc == nil || doc.client == nil {
		m.mu.Unlock()
		return nil
	}
	state := m.usage[doc.client]
	if state == nil || !doc.client.alive() {
		m.mu.Unlock()
		return nil
	}
	state.active++
	m.mu.Unlock()
	defer m.release(doc.client)
	doc.client.documentsMu.Lock()
	defer doc.client.documentsMu.Unlock()

	content, err := doc.client.executor.ReadFile(context.Background(), filePath, tools.ReadFileOptions{})
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.docs[filePath] != doc {
		m.mu.Unlock()
		return nil
	}
	doc.version++
	ver := doc.version
	m.mu.Unlock()

	doc.client.DidChange(doc.uri, string(content.Data), ver)
	return nil
}

// NotifyFileClosed tells the LSP server a file has been closed/deleted.
func (m *Manager) NotifyFileClosed(filePath string) {
	m.mu.Lock()
	doc := m.docs[filePath]
	if doc == nil || doc.client == nil || m.usage[doc.client] == nil || !doc.client.alive() {
		m.mu.Unlock()
		return
	}
	m.usage[doc.client].active++
	m.mu.Unlock()
	defer m.release(doc.client)
	doc.client.documentsMu.Lock()
	defer doc.client.documentsMu.Unlock()
	m.mu.Lock()
	if m.docs[filePath] != doc {
		m.mu.Unlock()
		return
	}
	delete(m.docs, filePath)
	m.mu.Unlock()
	doc.client.DidClose(doc.uri)
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
	for id, client := range m.servers {
		if client.alive() {
			ids = append(ids, id)
		}
	}
	return ids
}

func (m *Manager) startClient(ctx context.Context, serverID, rootURI string, def ServerDef) (*Client, error) {
	rootPath := uriToPath(rootURI)
	command := resolveCommand(def, rootPath)
	if command == "" {
		return nil, fmt.Errorf("LSP server %q: binary %q not found — run: %s", serverID, def.Command, def.InstallHint)
	}
	langID := serverID
	if len(def.Languages) > 0 {
		langID = def.Languages[0]
	}
	client := NewClientWithExecutor(def.ID, langID, rootURI, command, def.Args, rootPath, m.executor, m.log)
	if serverID == "vue" {
		bridge, err := newVueBridge(ctx, client)
		if err != nil {
			return nil, err
		}
		client.vue = bridge
	}
	if err := client.Start(ctx); err != nil {
		client.Stop()
		return nil, err
	}
	return client, nil
}
