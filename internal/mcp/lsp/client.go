package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// DefaultTimeout is the default timeout for LSP requests.
const DefaultTimeout = 30 * time.Second

// Client manages a single LSP server connection.
type Client struct {
	id        string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	transport *transport
	log       *logger.Logger

	mu          sync.Mutex
	initialized bool
	serverCaps  ServerCapabilities
	reqID       atomic.Int64
	pending     map[int64]chan *Response // request ID -> response channel
	shutdown    chan struct{}

	languageID string
	rootURI    string
	done       chan struct{}
}

// NewClient creates a new LSP client. Command and args are the LSP server binary.
func NewClient(id, languageID, rootURI, command string, args []string, log *logger.Logger) *Client {
	return &Client{
		id:         id,
		languageID: languageID,
		rootURI:    rootURI,
		cmd:        exec.Command(command, args...),
		pending:    make(map[int64]chan *Response),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		log:        log,
	}
}

// Start launches the LSP server process and initializes the connection.
func (c *Client) Start(ctx context.Context) error {
	stdin, err := c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create stdin pipe for %q: %w", c.id, err)
	}
	stdout, err := c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe for %q: %w", c.id, err)
	}
	stderr, err := c.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe for %q: %w", c.id, err)
	}

	c.stdin = stdin
	c.transport = newTransport(stdout, stdin)

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("start %s server %q: %w", c.id, c.cmd.Path, err)
	}

	go c.readLoop()
	go c.drainStderr(stderr)

	if err := c.initialize(ctx); err != nil {
		c.Stop()
		return fmt.Errorf("initialize %q: %w", c.id, err)
	}

	if c.log != nil {
		c.log.Info(logger.CatMCP, "LSP server started",
			"server", c.id, "command", c.cmd.Path, "root", c.rootURI)
	}
	return nil
}

// Stop sends shutdown+exit and waits for the process to terminate.
func (c *Client) Stop() {
	c.mu.Lock()
	if !c.initialized {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	close(c.shutdown)

	// Best-effort shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.sendRequest(shutdownCtx, c.reqID.Add(1), "shutdown", nil)
	c.sendNotification("exit", nil)

	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		c.cmd.Process.Kill()
	}

	if c.log != nil {
		c.log.Info(logger.CatMCP, "LSP server stopped", "server", c.id)
	}
}

func (c *Client) initialize(ctx context.Context) error {
	params := InitializeParams{
		ProcessID: os.Getpid(),
		RootURI:   c.rootURI,
		RootPath:  uriToPath(c.rootURI),
		Capabilities: ClientCapabilities{
			TextDocument: &TextDocumentClientCapabilities{
				Synchronization: &SynchronizationCapability{
					DynamicRegistration: false,
					DidSave:             false,
				},
				Hover:          &BoolCapability{DynamicRegistration: false},
				Definition:     &BoolCapability{DynamicRegistration: false},
				References:     &BoolCapability{DynamicRegistration: false},
				DocumentSymbol: &struct {
					DynamicRegistration             bool `json:"dynamicRegistration,omitempty"`
					HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
				}{HierarchicalDocumentSymbolSupport: true},
				Implementation: &BoolCapability{DynamicRegistration: false},
				CallHierarchy:  &BoolCapability{DynamicRegistration: false},
				Diagnostic:     &BoolCapability{DynamicRegistration: false},
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

		// Skip responses without ID (notifications from server)
		if resp.ID == nil {
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

	if err := c.transport.sendRequest(id, method, params); err != nil {
		return fmt.Errorf("send %q: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.shutdown:
		return fmt.Errorf("client shutting down")
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
	_ = c.transport.sendNotification(method, params)
}

func (c *Client) sendRequest(ctx context.Context, id int64, method string, params any) error {
	return c.transport.sendRequest(id, method, params)
}

// ── LSP Methods ───────────────────────────────────────────────────────────────

// DidOpen notifies the LSP server that a document is open.
func (c *Client) DidOpen(uri, text string) {
	c.sendNotification("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:        uri,
			LanguageID: c.languageID,
			Version:    1,
			Text:       text,
		},
	})
}

// DidChange notifies the LSP server that a document changed.
func (c *Client) DidChange(uri, text string, version int) {
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
	c.sendNotification("textDocument/didClose", DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
}

// GotoDefinition returns the definition location(s) of a symbol.
func (c *Client) GotoDefinition(ctx context.Context, uri string, pos Position) ([]Location, error) {
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
	params := DocumentDiagnosticParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}

	// Try textDocument/diagnostic first (newer LSP 3.17+)
	var report any
	err := c.call(ctx, "textDocument/diagnostic", params, &report)
	if err != nil {
		// Fall back: some servers use pull diagnostics via textDocument/diagnostic
		// but older servers might not support it. Return empty.
		if c.log != nil {
			c.log.Debug(logger.CatMCP, "LSP diagnostic not supported, returning empty",
				"server", c.id, "err", err.Error())
		}
		return nil, nil
	}

	data, _ := json.Marshal(report)
	var dr DiagnosticReport
	if err := json.Unmarshal(data, &dr); err != nil {
		return nil, fmt.Errorf("unmarshal diagnostics: %w", err)
	}
	return dr.Items, nil
}

// CallHierarchyIncoming returns incoming calls (who calls this symbol).
func (c *Client) CallHierarchyIncoming(ctx context.Context, uri string, pos Position) ([]CallHierarchyIncomingCall, error) {
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
	if len(uri) > 7 && uri[:7] == "file://" {
		return uri[7:]
	}
	return uri
}

// PathToURI converts a filesystem path to a file:// URI.
func PathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "file://" + path
	}
	return "file://" + abs
}
