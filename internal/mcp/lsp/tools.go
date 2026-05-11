package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// LSPTool implements tools.Tool for an LSP capability.
type LSPTool struct {
	name        string
	description string
	params      json.RawMessage
	manager     *Manager
	action      string // one of: goto_definition, find_references, hover, etc.
}

func (t *LSPTool) Name() string               { return t.name }
func (t *LSPTool) Description() string        { return t.description }
func (t *LSPTool) Parameters() json.RawMessage { return t.params }

// Execute routes the tool call to the correct LSP server and calls the LSP method.
func (t *LSPTool) Execute(ctx context.Context, args string) (string, error) {
	var in struct {
		File      string `json:"file"`
		Line      int    `json:"line"`
		Character int    `json:"character"`
		Query     string `json:"query"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	client, err := t.manager.clientForFile(in.File)
	if err != nil {
		return "", err
	}

	uri := PathToURI(in.File)
	pos := Position{
		Line:      in.Line - 1,
		Character: in.Character - 1,
	}

	if err := t.manager.ensureOpen(client, in.File, uri); err != nil {
		return "", fmt.Errorf("ensureOpen: %w", err)
	}

	switch t.action {
	case "goto_definition":
		return t.doGotoDefinition(ctx, client, uri, pos)
	case "find_references":
		return t.doFindReferences(ctx, client, uri, pos)
	case "hover":
		return t.doHover(ctx, client, uri, pos)
	case "document_symbols":
		return t.doDocumentSymbols(ctx, client, uri)
	case "workspace_symbols":
		return t.doWorkspaceSymbols(ctx, client, in.Query)
	case "find_implementations":
		return t.doFindImplementations(ctx, client, uri, pos)
	case "diagnostics":
		return t.doDiagnostics(ctx, client, uri)
	case "call_hierarchy_incoming":
		return t.doCallHierarchyIncoming(ctx, client, uri, pos)
	case "call_hierarchy_outgoing":
		return t.doCallHierarchyOutgoing(ctx, client, uri, pos)
	default:
		return "", fmt.Errorf("unknown LSP action: %s", t.action)
	}
}

func (t *LSPTool) doGotoDefinition(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	locs, err := c.GotoDefinition(ctx, uri, pos)
	if err != nil {
		return formatError("goto_definition", err), nil
	}
	return formatLocations(locs, "definitions found"), nil
}

func (t *LSPTool) doFindReferences(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	locs, err := c.FindReferences(ctx, uri, pos, true)
	if err != nil {
		return formatError("find_references", err), nil
	}
	return formatLocations(locs, "references found"), nil
}

func (t *LSPTool) doHover(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	hover, err := c.Hover(ctx, uri, pos)
	if err != nil {
		return formatError("hover", err), nil
	}
	if hover == nil {
		return `{"result": null, "message": "no hover info available"}`, nil
	}
	data, _ := json.Marshal(hover)
	return string(data), nil
}

func (t *LSPTool) doDocumentSymbols(ctx context.Context, c *Client, uri string) (string, error) {
	symbols, err := c.DocumentSymbols(ctx, uri)
	if err != nil {
		return formatError("document_symbols", err), nil
	}
	return formatSymbols(symbols), nil
}

func (t *LSPTool) doWorkspaceSymbols(ctx context.Context, c *Client, query string) (string, error) {
	if query == "" {
		return `{"error": "query is required"}`, nil
	}
	symbols, err := c.WorkspaceSymbols(ctx, query)
	if err != nil {
		return formatError("workspace_symbols", err), nil
	}
	return formatSymbolInfos(symbols), nil
}

func (t *LSPTool) doFindImplementations(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	locs, err := c.FindImplementations(ctx, uri, pos)
	if err != nil {
		return formatError("find_implementations", err), nil
	}
	return formatLocations(locs, "implementations found"), nil
}

func (t *LSPTool) doDiagnostics(ctx context.Context, c *Client, uri string) (string, error) {
	diags, err := c.Diagnostics(ctx, uri)
	if err != nil {
		return formatError("diagnostics", err), nil
	}
	if diags == nil {
		return `{"diagnostics": [], "count": 0}`, nil
	}
	data, _ := json.Marshal(map[string]any{
		"diagnostics": diags,
		"count":       len(diags),
	})
	return string(data), nil
}

func (t *LSPTool) doCallHierarchyIncoming(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	calls, err := c.CallHierarchyIncoming(ctx, uri, pos)
	if err != nil {
		return formatError("call_hierarchy_incoming", err), nil
	}
	if calls == nil {
		return `{"calls": [], "count": 0}`, nil
	}
	data, _ := json.Marshal(map[string]any{
		"calls": calls,
		"count": len(calls),
	})
	return string(data), nil
}

func (t *LSPTool) doCallHierarchyOutgoing(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	calls, err := c.CallHierarchyOutgoing(ctx, uri, pos)
	if err != nil {
		return formatError("call_hierarchy_outgoing", err), nil
	}
	if calls == nil {
		return `{"calls": [], "count": 0}`, nil
	}
	data, _ := json.Marshal(map[string]any{
		"calls": calls,
		"count": len(calls),
	})
	return string(data), nil
}

// ── Formatting helpers ─────────────────────────────────────────────────────────

func formatLocations(locs []Location, label string) string {
	if locs == nil {
		locs = []Location{}
	}
	data, _ := json.Marshal(map[string]any{
		"locations": locs,
		"count":     len(locs),
		"message":   fmt.Sprintf("%d %s", len(locs), label),
	})
	return string(data)
}

func formatSymbols(symbols []DocumentSymbol) string {
	if symbols == nil {
		symbols = []DocumentSymbol{}
	}
	var flat []DocumentSymbol
	flattenSymbols(symbols, &flat)
	data, _ := json.Marshal(map[string]any{
		"symbols": flat,
		"count":   len(flat),
	})
	return string(data)
}

func formatSymbolInfos(symbols []SymbolInformation) string {
	if symbols == nil {
		symbols = []SymbolInformation{}
	}
	data, _ := json.Marshal(map[string]any{
		"symbols": symbols,
		"count":   len(symbols),
	})
	return string(data)
}

func flattenSymbols(symbols []DocumentSymbol, dst *[]DocumentSymbol) {
	for i := range symbols {
		*dst = append(*dst, symbols[i])
		if len(symbols[i].Children) > 0 {
			flattenSymbols(symbols[i].Children, dst)
		}
	}
}

func formatError(action string, err error) string {
	data, _ := json.Marshal(map[string]string{
		"action": action,
		"error":  err.Error(),
	})
	return string(data)
}

// ── Tool constructors ─────────────────────────────────────────────────────────

func newGotoDefinitionTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__goto_definition",
		description: "Jump to the definition of a symbol at a given file, line, and character position.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "goto_definition",
	}
}

func newFindReferencesTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__find_references",
		description: "Find all references to a symbol at a given file, line, and character position.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "find_references",
	}
}

func newHoverTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__hover",
		description: "Get type information, documentation, and signature at a given file, line, and character position.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "hover",
	}
}

func newDocumentSymbolsTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__document_symbols",
		description: "List all symbols (functions, classes, variables, etc.) in a given file.",
		params:      fileOnlyParamsSchema(),
		manager:     mgr,
		action:      "document_symbols",
	}
}

func newWorkspaceSymbolsTool(mgr *Manager) tools.Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Symbol name or substring to search for across the workspace"
			}
		},
		"required": ["query"]
	}`)
	return &LSPTool{
		name:        "lsp__workspace_symbols",
		description: "Search for symbols by name across the entire workspace. Returns matching symbols with their locations.",
		params:      schema,
		manager:     mgr,
		action:      "workspace_symbols",
	}
}

func newFindImplementationsTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__find_implementations",
		description: "Find all implementations of an interface or abstract method at a given file, line, and character position.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "find_implementations",
	}
}

func newDiagnosticsTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__diagnostics",
		description: "Get diagnostics (errors, warnings, hints) for a given file.",
		params:      fileOnlyParamsSchema(),
		manager:     mgr,
		action:      "diagnostics",
	}
}

func newCallHierarchyIncomingTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__call_hierarchy_incoming",
		description: "Find all incoming calls — who calls the function/method at a given file, line, and character position.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "call_hierarchy_incoming",
	}
}

func newCallHierarchyOutgoingTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__call_hierarchy_outgoing",
		description: "Find all outgoing calls — what functions/methods are called by the symbol at a given file, line, and character position.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "call_hierarchy_outgoing",
	}
}

func positionParamsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file": {"type": "string", "description": "Absolute path to the source file"},
			"line": {"type": "integer", "description": "1-indexed line number"},
			"character": {"type": "integer", "description": "1-indexed character offset on the line"}
		},
		"required": ["file", "line", "character"]
	}`)
}

func fileOnlyParamsSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"file": {"type": "string", "description": "Absolute path to the source file"}
		},
		"required": ["file"]
	}`)
}

// LSPTools returns all LSP tools bound to the given manager.
func LSPTools(mgr *Manager) []tools.Tool {
	return []tools.Tool{
		newGotoDefinitionTool(mgr),
		newFindReferencesTool(mgr),
		newHoverTool(mgr),
		newDocumentSymbolsTool(mgr),
		newWorkspaceSymbolsTool(mgr),
		newFindImplementationsTool(mgr),
		newDiagnosticsTool(mgr),
		newCallHierarchyIncomingTool(mgr),
		newCallHierarchyOutgoingTool(mgr),
	}
}

// ToolActionNames returns the tool action names, used for display.
func ToolActionNames() []string {
	return []string{
		"goto_definition",
		"find_references",
		"hover",
		"document_symbols",
		"workspace_symbols",
		"find_implementations",
		"diagnostics",
		"call_hierarchy_incoming",
		"call_hierarchy_outgoing",
	}
}

// toolNames returns full tool names, used for display.
func toolNames() []string {
	return []string{
		"lsp__goto_definition",
		"lsp__find_references",
		"lsp__hover",
		"lsp__document_symbols",
		"lsp__workspace_symbols",
		"lsp__find_implementations",
		"lsp__diagnostics",
		"lsp__call_hierarchy_incoming",
		"lsp__call_hierarchy_outgoing",
	}
}

// toolNames is used by manager for display.
func ToolNames() string {
	return strings.Join(toolNames(), ", ")
}
