package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// DefaultTimeout is the default timeout for LSP requests.
const DefaultTimeout = 30 * time.Second

type documentDiagnostics struct {
	version int
	items   []Diagnostic
	ready   chan struct{}
}

// Client manages a single LSP server connection.
type Client struct {
	id        string
	command   string
	args      []string
	workDir   string
	executor  *tools.Executor
	process   tools.Process
	stdin     io.WriteCloser
	transport *transport
	log       *logger.Logger

	mu          sync.Mutex
	initialized bool
	serverCaps  ServerCapabilities
	reqID       atomic.Int64
	pending     map[int64]chan *Response // request ID -> response channel
	diagnostics map[string]*documentDiagnostics
	shutdown    chan struct{}

	languageID            string
	rootURI               string
	done                  chan struct{}
	processDone           chan struct{}
	stopOnce              sync.Once
	documentsMu           sync.Mutex
	initializationOptions any
	vue                   *vueBridge
}

// NewClient creates a new LSP client. Command and args are the LSP server binary.
func NewClient(id, languageID, rootURI, command string, args []string, log *logger.Logger) *Client {
	return NewClientWithExecutor(id, languageID, rootURI, command, args, uriToPath(rootURI), tools.NewExecutor(), log)
}

// NewClientWithExecutor creates an LSP client using the shared process launcher.
func NewClientWithExecutor(
	id, languageID, rootURI, command string,
	args []string,
	workDir string,
	executor *tools.Executor,
	log *logger.Logger,
) *Client {
	if executor == nil {
		executor = tools.NewExecutor()
	}
	return &Client{
		id:          id,
		languageID:  languageID,
		rootURI:     rootURI,
		command:     command,
		args:        args,
		workDir:     workDir,
		executor:    executor,
		pending:     make(map[int64]chan *Response),
		shutdown:    make(chan struct{}),
		done:        make(chan struct{}),
		processDone: make(chan struct{}),
		log:         log,
	}
}

// Start launches the LSP subprocess and sends initialize request.
func (c *Client) Start(ctx context.Context) error {
	// A lazily started server outlives the tool call that first needs it.
	// Initialization still uses ctx; Stop owns process cleanup on failure/shutdown.
	process, err := c.executor.StartProcess(context.WithoutCancel(ctx), tools.ProcessSpec{
		Command:          c.command,
		Args:             c.args,
		WorkingDirectory: c.workDir,
	})
	if err != nil {
		return fmt.Errorf("start %s server %q: %w", c.id, c.command, err)
	}
	// Publish the process before starting the reader goroutines. Keep this
	// critical section narrow: initialize calls call, which also uses c.mu.
	c.mu.Lock()
	c.process = process
	c.stdin = process.Stdin()
	c.transport = newTransport(process.Stdout(), process.Stdin())
	c.mu.Unlock()

	go c.readLoop()
	go c.drainStderr(process.Stderr())
	go func() {
		_ = process.Wait()
		close(c.processDone)
	}()

	if err := c.initialize(ctx); err != nil {
		c.Stop()
		return fmt.Errorf("initialize %q: %w", c.id, err)
	}

	if c.log != nil {
		c.log.Info(logger.CatMCP, "LSP server started",
			"server", c.id, "command", c.command, "root", c.rootURI)
	}
	return nil
}

// Stop sends shutdown+exit and waits for the process to terminate.
func (c *Client) Stop() {
	c.stopOnce.Do(c.stop)
}

func (c *Client) alive() bool {
	select {
	case <-c.done:
		return false
	case <-c.processDone:
		return false
	case <-c.shutdown:
		return false
	default:
		return c.vue == nil || c.vue.typescript.alive()
	}
}

func (c *Client) stop() {
	if c.vue != nil {
		c.vue.cancel()
		defer c.vue.close()
	}
	c.mu.Lock()
	initialized := c.initialized
	process := c.process
	c.mu.Unlock()
	wasAlive := c.alive()
	close(c.shutdown)
	if process == nil {
		return
	}
	if !initialized || !wasAlive {
		_ = process.Kill()
	} else {
		// A blocked protocol write must not prevent the forced-stop deadline.
		go func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = c.sendRequest(shutdownCtx, c.reqID.Add(1), "shutdown", nil)
			c.sendNotification("exit", nil)
		}()
		select {
		case <-c.processDone:
		case <-time.After(5 * time.Second):
			_ = process.Kill()
		}
	}
	// Reader EOF alone does not prove that the subprocess has been reaped.
	if c.processDone != nil {
		<-c.processDone
		<-c.done
	}

	if c.log != nil {
		c.log.Info(logger.CatMCP, "LSP server stopped", "server", c.id)
	}
}

func (c *Client) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProcessID:             os.Getpid(),
		RootURI:               c.rootURI,
		RootPath:              uriToPath(c.rootURI),
		WorkspaceFolders:      []WorkspaceFolder{{URI: c.rootURI, Name: filepath.Base(c.workDir)}},
		InitializationOptions: c.initializationOptions,
		Capabilities: ClientCapabilities{
			TextDocument: &TextDocumentClientCapabilities{
				Synchronization: &SynchronizationCapability{
					DynamicRegistration: false,
					DidSave:             false,
				},
				Hover:      &BoolCapability{DynamicRegistration: false},
				Definition: &BoolCapability{DynamicRegistration: false},
				References: &BoolCapability{DynamicRegistration: false},
				DocumentSymbol: &struct {
					DynamicRegistration               bool `json:"dynamicRegistration,omitempty"`
					HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
				}{HierarchicalDocumentSymbolSupport: true},
				Implementation:     &BoolCapability{DynamicRegistration: false},
				CallHierarchy:      &BoolCapability{DynamicRegistration: false},
				Diagnostic:         &BoolCapability{DynamicRegistration: false},
				PublishDiagnostics: &struct{}{},
			},
			Workspace: &WorkspaceClientCapabilities{
				Symbol: &BoolCapability{DynamicRegistration: false},
			},
		},
	}

	var result InitializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return err
	}

	c.serverCaps = result.Capabilities
	if c.log != nil && result.ServerInfo != nil {
		c.log.Debug(logger.CatMCP, "LSP server initialized",
			"server", c.id, "name", result.ServerInfo.Name, "version", result.ServerInfo.Version)
	}

	c.sendNotification("initialized", map[string]any{})
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

// readLoop reads JSON-RPC responses from the transport and dispatches them.
func (c *Client) readLoop() {
	defer func() {
		if r := recover(); r != nil {
			if c.log != nil {
				c.log.Error(logger.CatMCP, "LSP readLoop panic recovered",
					"server", c.id, "panic", fmt.Sprintf("%v", r))
			}
		}
		close(c.done)
	}()

	for {
		select {
		case <-c.shutdown:
			return
		default:
		}

		raw, err := c.transport.readMessage()
		if err != nil {
			if c.log != nil {
				c.log.Debug(logger.CatMCP, "LSP read error", "server", c.id, "err", err.Error())
			}
			return
		}

		var resp Response

		if err := json.Unmarshal(raw, &resp); err != nil {
			if c.log != nil {
				c.log.Warn(logger.CatMCP, "LSP bad response", "server", c.id, "body", string(raw))
			}
			continue
		}

		if resp.ID == nil {
			var notification Notification
			if err := json.Unmarshal(raw, &notification); err != nil {
				continue
			}
			if c.vue != nil && notification.Method == "tsserver/request" {
				c.vue.dispatch(notification.Params)
			}
			if notification.Method == "textDocument/publishDiagnostics" {
				var params struct {
					URI         string       `json:"uri"`
					Version     *int         `json:"version"`
					Diagnostics []Diagnostic `json:"diagnostics"`
				}
				if err := json.Unmarshal(notification.Params, &params); err == nil {
					c.mu.Lock()
					if state := c.diagnostics[params.URI]; state != nil && (params.Version == nil || *params.Version >= state.version) {
						state.items = params.Diagnostics
						select {
						case <-state.ready:
						default:
							close(state.ready)
						}
					}
					c.mu.Unlock()
				}
			}
			continue
		}

		var reqID int64
		switch v := resp.ID.(type) {
		case float64:
			reqID = int64(v)
		case json.Number:
			reqID, _ = v.Int64()
		default:
			if c.log != nil {
				c.log.Warn(logger.CatMCP, "LSP unexpected response ID type", "server", c.id, "id", fmt.Sprintf("%v", resp.ID))
			}
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[reqID]
		if ok {
			delete(c.pending, reqID)
		}
		c.mu.Unlock()

		if ok {
			select {
			case ch <- &resp:
			default:
			}
		}
	}
}

func (c *Client) drainStderr(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && c.log != nil {
			c.log.Debug(logger.CatMCP, "LSP stderr",
				"server", c.id, "output", string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// call sends a JSON-RPC request and waits for the response.
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	if !c.alive() {
		return fmt.Errorf("language server stopped")
	}
	id := c.reqID.Add(1)
	ch := make(chan *Response, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(ctx, func() error { return c.transport.sendRequest(id, method, params) }); err != nil {
		return fmt.Errorf("send %q: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.shutdown:
		return fmt.Errorf("client shutting down")
	case <-c.done:
		return fmt.Errorf("language server disconnected")
	case <-c.processDone:
		return fmt.Errorf("language server exited")
	case <-c.dependencyDone():
		return fmt.Errorf("Vue TypeScript service exited")
	case resp, ok := <-ch:
		if !ok || resp == nil {
			return fmt.Errorf("no response for %q", method)
		}
		if resp.Error != nil {
			return fmt.Errorf("LSP error %s: %s (code %d)", method, resp.Error.Message, resp.Error.Code)
		}
		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("unmarshal %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (c *Client) sendNotification(method string, params any) {
	_ = c.notify(context.Background(), method, params)
}

// A canceled partial frame cannot safely be reused. Killing a process stuck on
// stdin also releases its writer goroutine and lets the next call reload it.
func (c *Client) write(ctx context.Context, write func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() { result <- write() }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		c.mu.Lock()
		process := c.process
		c.mu.Unlock()
		if process != nil {
			_ = process.Kill()
		}
		return ctx.Err()
	}
}

func (c *Client) notify(ctx context.Context, method string, params any) error {

	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	return c.write(ctx, func() error { return c.transport.sendNotification(method, params) })
}

func (c *Client) sendRequest(ctx context.Context, id int64, method string, params any) error {
	return c.transport.sendRequest(id, method, params)
}

// ── LSP Methods ───────────────────────────────────────────────────────────────

// DidOpen notifies the LSP server that a document is open.
func (c *Client) DidOpen(uri, text string) {
	_ = c.didOpen(context.Background(), uri, text)
}

func (c *Client) didOpen(ctx context.Context, uri, text string) error {
	if c.vue != nil {
		if err := c.vue.typescript.didOpen(ctx, uri, text); err != nil {
			return err
		}
	}
	languageID := c.languageID
	if c.id == "typescript" {
		switch strings.ToLower(filepath.Ext(uriToPath(uri))) {
		case ".js", ".mjs", ".cjs":
			languageID = "javascript"
		case ".jsx":
			languageID = "javascriptreact"
		case ".tsx":
			languageID = "typescriptreact"
		case ".ts":
			languageID = "typescript"
		}
	}
	if c.id == "clangd" {
		switch strings.ToLower(filepath.Ext(uriToPath(uri))) {
		case ".cpp", ".hpp", ".cc", ".cxx", ".hxx", ".c++", ".h++":
			languageID = "cpp"
		case ".c", ".h":
			languageID = "c"
		}
	}
	c.resetDiagnostics(uri, 1)
	return c.notify(ctx, "textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: languageID,
			Version:    1,
			Text:       text,
		},
	})
}

// DidChange notifies the LSP server that a document changed.
func (c *Client) DidChange(uri, text string, version int) {
	if c.vue != nil {
		c.vue.typescript.DidChange(uri, text, version)
	}
	c.resetDiagnostics(uri, version)
	c.sendNotification("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{
			URI:     uri,
			Version: version,
		},
		ContentChanges: []TextDocumentContentChangeEvent{
			{Text: text},
		},
	})
}

// DidClose notifies the LSP server that a document was closed.
func (c *Client) DidClose(uri string) {
	if c.vue != nil {
		c.vue.typescript.DidClose(uri)
	}
	c.mu.Lock()
	if state := c.diagnostics[uri]; state != nil {
		select {
		case <-state.ready:
		default:
			close(state.ready)
		}
	}
	delete(c.diagnostics, uri)
	c.mu.Unlock()
	c.sendNotification("textDocument/didClose", DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// GotoDefinition returns the definition location(s) of a symbol.
func (c *Client) GotoDefinition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	if c.vue != nil {
		locations, err := c.vue.typescript.GotoDefinition(ctx, uri, pos)
		if err != nil || len(locations) > 0 {
			return locations, err
		}
	}
	params := DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	var result any
	if err := c.call(ctx, "textDocument/definition", params, &result); err != nil {
		return nil, err
	}
	return decodeLocations(result)
}

// FindReferences returns all reference locations of a symbol.
func (c *Client) FindReferences(ctx context.Context, uri string, pos Position, includeDecl bool) ([]Location, error) {
	if c.vue != nil {
		return c.vue.typescript.FindReferences(ctx, uri, pos, includeDecl)
	}
	params := ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
		Context:      ReferenceContext{IncludeDeclaration: includeDecl},
	}
	var result any
	if err := c.call(ctx, "textDocument/references", params, &result); err != nil {
		return nil, err
	}
	return decodeLocations(result)
}

// Hover returns type and documentation info at a position.
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (*Hover, error) {
	if c.vue != nil {
		hover, err := c.vue.typescript.Hover(ctx, uri, pos)
		if err != nil || hover != nil {
			return hover, err
		}
	}
	params := HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	var result Hover
	if err := c.call(ctx, "textDocument/hover", params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DocumentSymbols returns all symbols in a document.
func (c *Client) DocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	params := DocumentSymbolParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}
	var result any
	if err := c.call(ctx, "textDocument/documentSymbol", params, &result); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(result)
	var symbols []DocumentSymbol
	if err := json.Unmarshal(data, &symbols); err != nil {
		var info []SymbolInformation
		if err := json.Unmarshal(data, &info); err != nil {
			return nil, fmt.Errorf("unmarshal documentSymbols: %w", err)
		}
		symbols = symbolInfoToDocumentSymbols(info)
	}
	return symbols, nil
}

// WorkspaceSymbols searches for symbols across the entire workspace.
func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]SymbolInformation, error) {
	if c.vue != nil {
		return c.vue.typescript.WorkspaceSymbols(ctx, query)
	}
	params := WorkspaceSymbolParams{Query: query}
	var result any
	if err := c.call(ctx, "workspace/symbol", params, &result); err != nil {
		return nil, err
	}
	data, _ := json.Marshal(result)
	var symbols []SymbolInformation
	if err := json.Unmarshal(data, &symbols); err != nil {
		return nil, fmt.Errorf("unmarshal workspaceSymbols: %w", err)
	}
	return symbols, nil
}

// FindImplementations returns implementations of an interface/abstract method.
func (c *Client) FindImplementations(ctx context.Context, uri string, pos Position) ([]Location, error) {
	if c.vue != nil {
		return c.vue.typescript.FindImplementations(ctx, uri, pos)
	}
	params := ImplementationParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	var result any
	if err := c.call(ctx, "textDocument/implementation", params, &result); err != nil {
		return nil, err
	}
	return decodeLocations(result)
}

// Diagnostics returns diagnostics (errors, warnings) for a document.
func (c *Client) Diagnostics(ctx context.Context, uri string) ([]Diagnostic, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	if c.vue != nil {
		own, err := c.documentDiagnosticSnapshot(ctx, uri)
		if err != nil {
			return nil, err
		}
		typescript, err := c.vue.typescript.Diagnostics(ctx, uri)
		if err != nil {
			return nil, err
		}
		return append(own, typescript...), nil
	}
	if (c.id == "typescript" || c.id == "vue-typescript") && c.supportsTSRequests() {
		return c.typescriptDiagnostics(ctx, uri)
	}
	return c.documentDiagnosticSnapshot(ctx, uri)
}

func (c *Client) documentDiagnosticSnapshot(ctx context.Context, uri string) ([]Diagnostic, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	// Servers such as typescript-language-server publish diagnostics instead of
	// implementing pull diagnostics. Wait for the current document's first push.
	if c.serverCaps.DiagnosticProvider == nil || c.serverCaps.DiagnosticProvider == false {
		for {
			c.mu.Lock()
			state := c.diagnostics[uri]
			c.mu.Unlock()
			if state == nil {
				return nil, fmt.Errorf("document %q is not open", uri)
			}
			select {
			case <-state.ready:
				c.mu.Lock()
				if c.diagnostics[uri] != state {
					c.mu.Unlock()
					continue
				}
				items := append([]Diagnostic(nil), state.items...)
				c.mu.Unlock()
				return items, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-c.done:
				return nil, fmt.Errorf("language server stopped before publishing diagnostics")
			case <-c.processDone:
				return nil, fmt.Errorf("language server exited before publishing diagnostics")
			case <-c.dependencyDone():
				return nil, fmt.Errorf("Vue TypeScript service exited before publishing diagnostics")
			case <-c.shutdown:
				return nil, fmt.Errorf("client shutting down")
			}
		}
	}
	params := DocumentDiagnosticParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	var report any
	err := c.call(ctx, "textDocument/diagnostic", params, &report)
	if err != nil {
		return nil, err
	}

	data, _ := json.Marshal(report)
	var dr DiagnosticReport
	if err := json.Unmarshal(data, &dr); err != nil {
		return nil, fmt.Errorf("unmarshal diagnostics: %w", err)
	}
	return dr.Items, nil
}

func (c *Client) resetDiagnostics(uri string, version int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.diagnostics == nil {
		c.diagnostics = make(map[string]*documentDiagnostics)
	}
	if previous := c.diagnostics[uri]; previous != nil {
		select {
		case <-previous.ready:
		default:
			close(previous.ready)
		}
	}
	c.diagnostics[uri] = &documentDiagnostics{version: version, ready: make(chan struct{})}
}

// CallHierarchyIncoming returns incoming calls (who calls this symbol).
func (c *Client) CallHierarchyIncoming(ctx context.Context, uri string, pos Position) ([]CallHierarchyIncomingCall, error) {
	if c.vue != nil {
		return c.vue.typescript.CallHierarchyIncoming(ctx, uri, pos)
	}
	// Step 1: prepare call hierarchy
	prepareParams := CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	var items []CallHierarchyItem
	if err := c.call(ctx, "textDocument/prepareCallHierarchy", prepareParams, &items); err != nil {
		return nil, fmt.Errorf("prepareCallHierarchy: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	// Step 2: incoming calls
	inParams := CallHierarchyIncomingCallsParams{Item: items[0]}
	var calls []CallHierarchyIncomingCall
	if err := c.call(ctx, "callHierarchy/incomingCalls", inParams, &calls); err != nil {
		return nil, fmt.Errorf("incomingCalls: %w", err)
	}
	return calls, nil
}

// CallHierarchyOutgoing returns outgoing calls (what this symbol calls).
func (c *Client) CallHierarchyOutgoing(ctx context.Context, uri string, pos Position) ([]CallHierarchyOutgoingCall, error) {
	if c.vue != nil {
		return c.vue.typescript.CallHierarchyOutgoing(ctx, uri, pos)
	}
	prepareParams := CallHierarchyPrepareParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
	}
	var items []CallHierarchyItem
	if err := c.call(ctx, "textDocument/prepareCallHierarchy", prepareParams, &items); err != nil {
		return nil, fmt.Errorf("prepareCallHierarchy: %w", err)
	}
	if len(items) == 0 {
		return nil, nil
	}

	outParams := CallHierarchyOutgoingCallsParams{Item: items[0]}
	var calls []CallHierarchyOutgoingCall
	if err := c.call(ctx, "callHierarchy/outgoingCalls", outParams, &calls); err != nil {
		return nil, fmt.Errorf("outgoingCalls: %w", err)
	}
	return calls, nil
}

// Rename requests the server to rename the symbol at the given position and returns the workspace edit.
func (c *Client) Rename(ctx context.Context, uri string, pos Position, newName string) (*WorkspaceEdit, error) {
	if c.vue != nil {
		return c.vue.typescript.Rename(ctx, uri, pos, newName)
	}
	params := RenameParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     pos,
		NewName:      newName,
	}
	var edit WorkspaceEdit
	if err := c.call(ctx, "textDocument/rename", params, &edit); err != nil {
		return nil, fmt.Errorf("rename: %w", err)
	}
	return &edit, nil
}

// Formatting requests the server to format the whole document and returns the text edits.
func (c *Client) Formatting(ctx context.Context, uri string, tabSize int, insertSpaces bool) ([]TextEdit, error) {
	params := DocumentFormattingParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Options: FormattingOptions{
			TabSize:      tabSize,
			InsertSpaces: insertSpaces,
		},
	}
	var edits []TextEdit
	if err := c.call(ctx, "textDocument/formatting", params, &edits); err != nil {
		return nil, fmt.Errorf("formatting: %w", err)
	}
	return edits, nil
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func decodeLocations(result any) ([]Location, error) {
	if result == nil {
		return nil, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	// Try array first
	var locations []Location
	if err := json.Unmarshal(data, &locations); err == nil {
		return locations, nil
	}
	// Try single location
	var loc Location
	if err := json.Unmarshal(data, &loc); err == nil {
		return []Location{loc}, nil
	}
	return nil, fmt.Errorf("unable to decode locations from: %s", string(data))
}

func symbolInfoToDocumentSymbols(info []SymbolInformation) []DocumentSymbol {
	symbols := make([]DocumentSymbol, len(info))
	for i, si := range info {
		symbols[i] = DocumentSymbol{
			Name:           si.Name,
			Kind:           si.Kind,
			Range:          si.Location.Range,
			SelectionRange: si.Location.Range,
		}
	}
	return symbols
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	path := u.Path
	if u.Host != "" {
		path = "//" + u.Host + path
	}
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

// PathToURI converts a filesystem path to an escaped file URI.
func PathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if runtime.GOOS == "windows" && !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	if strings.HasPrefix(slash, "//") {
		parts := strings.SplitN(strings.TrimPrefix(slash, "//"), "/", 2)
		if len(parts) == 2 {
			return (&url.URL{Scheme: "file", Host: parts[0], Path: "/" + parts[1]}).String()
		}
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

// Vue 3 delegates TypeScript analysis to a plugin-hosting companion. Owning it
// with the Vue client keeps both processes under one idle/operation lease and
// avoids changing the configuration of an existing plain JS/TS service.
type vueBridge struct {
	owner      *Client
	typescript *Client
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func newVueBridge(ctx context.Context, owner *Client) (*vueBridge, error) {
	command := resolveTypescript(owner.workDir)
	if command == "" {
		return nil, fmt.Errorf("Vue 3 requires typescript-language-server")
	}
	serverPackage, err := nodePackage(owner.workDir, owner.command, "@vue/language-server")
	if err != nil {
		return nil, err
	}
	// Resolve the package itself, not a pnpm .bin shell wrapper, before
	// looking for dependencies private to the language-server installation.
	packageFile, err := filepath.EvalSymlinks(filepath.Join(serverPackage, "package.json"))
	if err != nil {
		return nil, err
	}
	plugin, err := nodePackage(filepath.Dir(packageFile), packageFile, "@vue/typescript-plugin")
	if err != nil {
		return nil, err
	}
	sdk := ""
	for _, arg := range owner.args {
		if strings.HasPrefix(arg, "--tsdk=") {
			sdk = strings.TrimPrefix(arg, "--tsdk=")
			break
		}
	}
	if sdk == "" {
		typescript, err := nodePackage(owner.workDir, packageFile, "typescript")
		if err != nil {
			return nil, err
		}
		sdk = filepath.Join(typescript, "lib")
		owner.args = append(append([]string(nil), owner.args...), "--tsdk="+sdk)
	} else if !filepath.IsAbs(sdk) {
		sdk = filepath.Join(owner.workDir, sdk)
	}
	companion := NewClientWithExecutor("vue-typescript", "vue", owner.rootURI, command, []string{"--stdio"}, owner.workDir, owner.executor, owner.log)
	companion.initializationOptions = map[string]any{
		"tsserver":                          map[string]any{"path": filepath.Join(sdk, "tsserver.js")},
		"plugins":                           []any{map[string]any{"name": "@vue/typescript-plugin", "location": plugin, "languages": []string{"vue"}}},
		"disableAutomaticTypingAcquisition": true,
	}
	if err := companion.Start(ctx); err != nil {
		companion.Stop()
		return nil, err
	}
	if !companion.supportsTSRequests() {
		companion.Stop()
		return nil, fmt.Errorf("Vue 3 requires a typescript-language-server supporting typescript.tsserverRequest")
	}
	bridgeCtx, cancel := context.WithCancel(context.Background())
	return &vueBridge{owner: owner, typescript: companion, ctx: bridgeCtx, cancel: cancel}, nil
}

func (c *Client) dependencyDone() <-chan struct{} {
	if c.vue == nil {
		return nil
	}
	return c.vue.typescript.done
}

// Search project dependencies first, then the server installation's ancestors.
// This also reaches pnpm's package-local dependency symlinks without a scan.
func nodePackage(workDir, serverCommand, name string) (string, error) {
	resolved, err := filepath.EvalSymlinks(serverCommand)
	if err != nil {
		return "", err
	}
	for _, base := range []string{workDir, filepath.Dir(resolved)} {
		for {
			candidate := filepath.Join(base, "node_modules", filepath.FromSlash(name))
			if _, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil {
				return candidate, nil
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}
	return "", fmt.Errorf("Vue 3 requires Node package %q in the project or language-server installation", name)
}

func (b *vueBridge) dispatch(params json.RawMessage) {
	var args []json.RawMessage
	if err := json.Unmarshal(params, &args); err != nil {
		return
	}
	nested := len(args) == 1
	if nested {
		var unpacked []json.RawMessage
		if err := json.Unmarshal(args[0], &unpacked); err != nil {
			return
		}
		args = unpacked
	}
	if len(args) != 3 {
		return
	}
	var command string
	if err := json.Unmarshal(args[1], &command); err != nil {
		return
	}
	if b.owner.log != nil {
		b.owner.log.Debug(logger.CatMCP, "Vue TypeScript request", "command", command)
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ctx, cancel := context.WithTimeout(b.ctx, DefaultTimeout)
		defer cancel()
		var response struct {
			Body json.RawMessage `json:"body"`
		}
		err := b.typescript.call(ctx, "workspace/executeCommand", map[string]any{
			"command":   "typescript.tsserverRequest",
			"arguments": []any{command, args[2], map[string]any{"isAsync": true, "lowPriority": true}},
		}, &response)
		if err != nil && b.owner.log != nil {
			b.owner.log.Warn(logger.CatMCP, "Vue TypeScript bridge failed", "command", command, "err", err.Error())
		}
		if b.owner.log != nil {
			b.owner.log.Debug(logger.CatMCP, "Vue TypeScript response", "command", command, "body_bytes", len(response.Body))
		}
		// Vue must receive a response even on failure, otherwise its pending
		// request remains unresolved. Keep the response shape of Vue 3's wire API.
		var reply any = []any{args[0], response.Body}
		if nested {
			reply = []any{reply}
		}
		_ = b.owner.notify(b.ctx, "tsserver/response", reply)
	}()
}

func (b *vueBridge) close() {
	// The owner's read loop has ended, so no dispatcher can add new work.
	b.wg.Wait()
	b.typescript.Stop()
}
