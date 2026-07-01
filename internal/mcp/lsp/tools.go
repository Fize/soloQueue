package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// LSPTool implements tools.Tool for an LSP capability.
type LSPTool struct {
	name        string
	description string
	params      json.RawMessage
	manager     *Manager
	action      string // one of: goto_definition, find_references, hover, etc.
	log         *logger.Logger
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
		NewName   string `json:"newName"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "", fmt.Errorf("parse args: %w", err)
	}

	var client *Client
	var err error
	var uri string
	var pos Position

	if in.File != "" {
		client, err = t.manager.clientForFile(in.File)
		if err != nil {
			t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "file", in.File, "err", err.Error())
			return "", err
		}
		uri = PathToURI(in.File)
		pos = Position{
			Line:      in.Line - 1,
			Character: in.Character - 1,
		}
		if err := t.manager.ensureOpen(client, in.File, uri); err != nil {
			t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "file", in.File, "err", err.Error())
			return "", fmt.Errorf("ensureOpen: %w", err)
		}
	} else {
		client, err = t.manager.GetAnyClient()
		if err != nil {
			t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
			return "", err
		}
	}

	t.log.Debug(logger.CatMCP, "lsp tool called",
		"tool", t.action,
		"file", in.File,
		"line", in.Line,
		"character", in.Character,
	)

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
	case "rename_symbol":
		return t.doRenameSymbol(ctx, client, uri, pos, in.NewName)
	case "document_outline":
		return t.doDocumentOutline(ctx, client, uri)
	case "get_code_item":
		return t.doGetCodeItem(ctx, client, uri, in.Query)
	case "goto_definition_by_name":
		return t.doGotoDefinitionByName(ctx, client, in.Query)
	default:
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", fmt.Sprintf("unknown LSP action: %s", t.action))
		return "", fmt.Errorf("unknown LSP action: %s", t.action)
	}
}

func (t *LSPTool) doGotoDefinition(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	locs, err := c.GotoDefinition(ctx, uri, pos)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("goto_definition", err), nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(locs))
	return formatLocations(locs, "definitions found"), nil
}

func (t *LSPTool) doFindReferences(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	locs, err := c.FindReferences(ctx, uri, pos, true)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("find_references", err), nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(locs))
	return formatLocations(locs, "references found"), nil
}

func (t *LSPTool) doHover(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	hover, err := c.Hover(ctx, uri, pos)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("hover", err), nil
	}
	if hover == nil {
		t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", 0)
		return `{"result": null, "message": "no hover info available"}`, nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", 1)
	data, _ := json.Marshal(hover)
	return string(data), nil
}

func (t *LSPTool) doDocumentSymbols(ctx context.Context, c *Client, uri string) (string, error) {
	symbols, err := c.DocumentSymbols(ctx, uri)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("document_symbols", err), nil
	}
	var flat []DocumentSymbol
	flattenSymbols(symbols, &flat)
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(flat))
	return formatSymbols(symbols), nil
}

func (t *LSPTool) doWorkspaceSymbols(ctx context.Context, c *Client, query string) (string, error) {
	if query == "" {
		return `{"error": "query is required"}`, nil
	}
	symbols, err := c.WorkspaceSymbols(ctx, query)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("workspace_symbols", err), nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(symbols))
	return formatSymbolInfos(symbols), nil
}

func (t *LSPTool) doFindImplementations(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	locs, err := c.FindImplementations(ctx, uri, pos)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("find_implementations", err), nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(locs))
	return formatLocations(locs, "implementations found"), nil
}

func (t *LSPTool) doDiagnostics(ctx context.Context, c *Client, uri string) (string, error) {
	diags, err := c.Diagnostics(ctx, uri)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("diagnostics", err), nil
	}
	if diags == nil {
		t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", 0)
		return `{"diagnostics": [], "count": 0}`, nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(diags))
	data, _ := json.Marshal(map[string]any{
		"diagnostics": diags,
		"count":       len(diags),
	})
	return string(data), nil
}

func (t *LSPTool) doCallHierarchyIncoming(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	calls, err := c.CallHierarchyIncoming(ctx, uri, pos)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("call_hierarchy_incoming", err), nil
	}
	if calls == nil {
		t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", 0)
		return `{"calls": [], "count": 0}`, nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(calls))
	data, _ := json.Marshal(map[string]any{
		"calls": calls,
		"count": len(calls),
	})
	return string(data), nil
}

func (t *LSPTool) doCallHierarchyOutgoing(ctx context.Context, c *Client, uri string, pos Position) (string, error) {
	calls, err := c.CallHierarchyOutgoing(ctx, uri, pos)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("call_hierarchy_outgoing", err), nil
	}
	if calls == nil {
		t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", 0)
		return `{"calls": [], "count": 0}`, nil
	}
	t.log.Debug(logger.CatMCP, "lsp tool completed", "tool", t.action, "count", len(calls))
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
		description: "Jump to the definition of a symbol at a given file, line, and character position. Use this when you need to find where a function, type, struct, interface, or variable is declared — much more precise than Grep + Read for locating definitions because LSP resolves the exact AST node. After finding the definition location, call lsp__get_code_item to retrieve the full implementation code. Input uses 1-indexed line/character (first line = line 1).",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "goto_definition",
		log:         mgr.log,
	}
}

func newFindReferencesTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__find_references",
		description: "Find all references to a symbol (function, variable, type, method) across the workspace. Use this before refactoring to understand the full impact of a change — it finds every usage location that LSP can resolve semantically, which is more complete than Grep text search. Input uses 1-indexed line/character. Combine with lsp__rename_symbol for safe global renaming.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "find_references",
		log:         mgr.log,
	}
}

func newHoverTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__hover",
		description: "Get type information, documentation, signature, and doc comments for a symbol at a given position. Use this when you need to understand what a symbol represents, its type signature, or its documentation without navigating away from the current context. Faster than Read + searching for the definition when you just need a quick lookup. Input uses 1-indexed line/character.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "hover",
		log:         mgr.log,
	}
}

func newDocumentSymbolsTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__document_symbols",
		description: "List all symbols (functions, classes, structs, interfaces, methods, variables, constants, enums) in a given file, with their kinds and line ranges. Use this to get a quick structural overview of a file before diving into specific symbols. More accurate than Grep for finding all definitions in a file because LSP understands the language's symbol hierarchy. For a formatted tree view, use lsp__document_outline instead.",
		params:      fileOnlyParamsSchema(),
		manager:     mgr,
		action:      "document_symbols",
		log:         mgr.log,
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
		description: "Search for symbols by name across the entire workspace. Use this when you know a symbol's name (or partial name) but not which file it's in. Returns matching symbols with their file paths, line numbers, and symbol kinds. The best first step when exploring an unfamiliar codebase: search for a class/function name and use the results to navigate directly to relevant files. The query supports fuzzy matching — try partial names if exact match returns nothing.",
		params:      schema,
		manager:     mgr,
		action:      "workspace_symbols",
		log:         mgr.log,
	}
}

func newFindImplementationsTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__find_implementations",
		description: "Find all concrete implementations of an interface or abstract method. Use this to discover all types that implement a given interface, or all method overrides in subclasses. Essential for understanding polymorphism and the type hierarchy in object-oriented code. Input uses 1-indexed line/character. Position the cursor on the interface/abstract method definition, not on a concrete implementation.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "find_implementations",
		log:         mgr.log,
	}
}

func newDiagnosticsTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__diagnostics",
		description: "Get all diagnostics (compiler errors, warnings, hints, suggestions) for a given file from the LSP server. Use this after editing a file to check for compilation errors, type mismatches, or lint warnings before running the build. More immediate than running a full compile because LSP reports issues in real-time from the language server.",
		params:      fileOnlyParamsSchema(),
		manager:     mgr,
		action:      "diagnostics",
		log:         mgr.log,
	}
}

func newCallHierarchyIncomingTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__call_hierarchy_incoming",
		description: "Find all incoming calls to a function or method — who calls it and from where. Use this before refactoring a function to understand the full call graph and ensure you don't miss callers. Also use this to trace how data flows into a function. Input uses 1-indexed line/character. Follow up with lsp__goto_definition on caller locations to navigate the call chain.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "call_hierarchy_incoming",
		log:         mgr.log,
	}
}

func newCallHierarchyOutgoingTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__call_hierarchy_outgoing",
		description: "Find all functions and methods called by a given symbol — what it depends on. Use this to understand a function's dependencies, trace execution flow, or analyze the coupling of a code unit. Input uses 1-indexed line/character. Follow up with lsp__goto_definition on called symbols to navigate deeper into the call chain.",
		params:      positionParamsSchema(),
		manager:     mgr,
		action:      "call_hierarchy_outgoing",
		log:         mgr.log,
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
		newRenameSymbolTool(mgr),
		newDocumentOutlineTool(mgr),
		newGetCodeItemTool(mgr),
		newGotoDefinitionByNameTool(mgr),
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
		"rename_symbol",
		"document_outline",
		"get_code_item",
		"goto_definition_by_name",
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
		"lsp__rename_symbol",
		"lsp__document_outline",
		"lsp__get_code_item",
		"lsp__goto_definition_by_name",
	}
}

// toolNames is used by manager for display.
func ToolNames() string {
	return strings.Join(toolNames(), ", ")
}

// ── New Tool Constructors ───────────────────────────────────────────────────

func newRenameSymbolTool(mgr *Manager) tools.Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"file": {"type": "string", "description": "Absolute path to the source file containing the symbol"},
			"line": {"type": "integer", "description": "1-indexed line number where the symbol is located"},
			"character": {"type": "integer", "description": "1-indexed character offset on the line"},
			"newName": {"type": "string", "description": "The new name of the symbol"}
		},
		"required": ["file", "line", "character", "newName"]
	}`)
	return &LSPTool{
		name:        "lsp__rename_symbol",
		description: "Rename a symbol globally across all files in the workspace using LSP semantics. This performs a safe, complete rename — it updates the declaration, all references, imports, and usages in every file. Use this INSTEAD of search-and-replace (Grep + Edit) for symbol renaming because LSP understands the language grammar and won't accidentally rename unrelated text that happens to match the name. ⚠️ This modifies files directly — it writes changes to disk. Verify with lsp__find_references first to understand the impact scope.",
		params:      schema,
		manager:     mgr,
		action:      "rename_symbol",
		log:         mgr.log,
	}
}

func newDocumentOutlineTool(mgr *Manager) tools.Tool {
	return &LSPTool{
		name:        "lsp__document_outline",
		description: "Generate a clean Markdown hierarchy of all symbols (classes, methods, functions, structs, interfaces) in a file, with nesting depth indicated by indentation and line numbers shown for each symbol. Use this as the FIRST step when exploring an unfamiliar file — it provides a structured overview much faster than reading the entire file. After identifying a symbol of interest, use lsp__get_code_item to retrieve its implementation, or lsp__goto_definition_by_name to search across the workspace.",
		params:      fileOnlyParamsSchema(),
		manager:     mgr,
		action:      "document_outline",
		log:         mgr.log,
	}
}

func newGetCodeItemTool(mgr *Manager) tools.Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"file": {"type": "string", "description": "Absolute path to the source file"},
			"query": {"type": "string", "description": "The name of the function, class, struct, or method to retrieve code for"}
		},
		"required": ["file", "query"]
	}`)
	return &LSPTool{
		name:        "lsp__get_code_item",
		description: "Retrieve the exact source code of a named symbol (function, struct, class, method, interface) from a file. Use this when you need to see a symbol's full implementation without reading the entire file — more precise than Read + manually scrolling to the right line. First use lsp__document_outline or lsp__document_symbols to discover available symbol names, then call this to get the code. Returns only the lines within the symbol's AST range.",
		params:      schema,
		manager:     mgr,
		action:      "get_code_item",
		log:         mgr.log,
	}
}

func newGotoDefinitionByNameTool(mgr *Manager) tools.Tool {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "The name of the class, struct, or function definition to locate"}
		},
		"required": ["query"]
	}`)
	return &LSPTool{
		name:        "lsp__goto_definition_by_name",
		description: "Find a symbol definition by name across the entire workspace and return a preview of its source code with surrounding context. THE tool to use when you know a symbol's name but not its file location — it searches the workspace via LSP, resolves to the actual definition (not just a declaration), and returns a readable preview. More powerful than lsp__workspace_symbols because it follows through to the definition and shows code context. Supports fuzzy matching.",
		params:      schema,
		manager:     mgr,
		action:      "goto_definition_by_name",
		log:         mgr.log,
	}
}

// ── New Tool Execution Helpers ──────────────────────────────────────────────

func comparePositions(p1, p2 Position) int {
	if p1.Line != p2.Line {
		return p1.Line - p2.Line
	}
	return p1.Character - p2.Character
}

func applyTextEdits(content string, edits []TextEdit) (string, error) {
	lines := strings.Split(content, "\n")

	// Sort edits in descending order of start position so line changes do not shift earlier offsets
	sort.Slice(edits, func(i, j int) bool {
		return comparePositions(edits[i].Range.Start, edits[j].Range.Start) > 0
	})

	for _, edit := range edits {
		startLine := edit.Range.Start.Line
		startChar := edit.Range.Start.Character
		endLine := edit.Range.End.Line
		endChar := edit.Range.End.Character

		if startLine < 0 || startLine >= len(lines) || endLine < 0 || endLine >= len(lines) {
			return "", fmt.Errorf("edit range out of bounds: line %d (start) / %d (end) not in [0, %d)", startLine, endLine, len(lines))
		}

		startRunes := []rune(lines[startLine])
		endRunes := []rune(lines[endLine])
		if startChar < 0 || startChar > len(startRunes) || endChar < 0 || endChar > len(endRunes) {
			return "", fmt.Errorf("edit char offset out of bounds: start %d in [0, %d], end %d in [0, %d]", startChar, len(startRunes), endChar, len(endRunes))
		}

		prefix := string(startRunes[:startChar])
		suffix := string(endRunes[endChar:])

		replacementLines := strings.Split(edit.NewText, "\n")
		replacementLines[0] = prefix + replacementLines[0]
		replacementLines[len(replacementLines)-1] = replacementLines[len(replacementLines)-1] + suffix

		newLines := make([]string, 0, len(lines)-(endLine-startLine)+len(replacementLines)-1)
		newLines = append(newLines, lines[:startLine]...)
		newLines = append(newLines, replacementLines...)
		newLines = append(newLines, lines[endLine+1:]...)
		lines = newLines
	}

	return strings.Join(lines, "\n"), nil
}

func (t *LSPTool) doRenameSymbol(ctx context.Context, c *Client, uri string, pos Position, newName string) (string, error) {
	if newName == "" {
		return `{"error": "newName is required"}`, nil
	}
	edit, err := c.Rename(ctx, uri, pos, newName)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("rename_symbol", err), nil
	}
	if edit == nil || len(edit.Changes) == 0 {
		return `{"changes_applied": 0, "message": "no changes returned by LSP server"}`, nil
	}

	type fileResult struct {
		File  string `json:"file"`
		Edits int    `json:"edits"`
	}
	var results []fileResult

	for fileURI, fileEdits := range edit.Changes {
		filePath := uriToPath(fileURI)
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			t.log.Error(logger.CatMCP, "failed to read file for rename", "file", filePath, "err", err.Error())
			return formatError("rename_symbol", fmt.Errorf("read %s: %w", filePath, err)), nil
		}

		newContent, err := applyTextEdits(string(contentBytes), fileEdits)
		if err != nil {
			t.log.Error(logger.CatMCP, "failed to apply edits for rename", "file", filePath, "err", err.Error())
			return formatError("rename_symbol", fmt.Errorf("apply edits to %s: %w", filePath, err)), nil
		}

		if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
			t.log.Error(logger.CatMCP, "failed to save file after rename", "file", filePath, "err", err.Error())
			return formatError("rename_symbol", fmt.Errorf("write %s: %w", filePath, err)), nil
		}

		// Notify LSP server of the modification
		c.DidChange(fileURI, newContent, 0)

		results = append(results, fileResult{
			File:  filePath,
			Edits: len(fileEdits),
		})
	}

	data, _ := json.Marshal(map[string]any{
		"changes_applied": len(results),
		"modified_files":  results,
	})
	return string(data), nil
}

func formatSymbolsOutline(symbols []DocumentSymbol, depth int) string {
	var sb strings.Builder
	indent := strings.Repeat("  ", depth)
	for _, sym := range symbols {
		kindStr := symbolKindToString(sym.Kind)
		sb.WriteString(fmt.Sprintf("%s- %s %s [Line %d]\n", indent, kindStr, sym.Name, sym.Range.Start.Line+1))
		if len(sym.Children) > 0 {
			sb.WriteString(formatSymbolsOutline(sym.Children, depth+1))
		}
	}
	return sb.String()
}

func symbolKindToString(k SymbolKind) string {
	switch k {
	case SymbolKindFile:
		return "file"
	case SymbolKindModule:
		return "module"
	case SymbolKindNamespace:
		return "namespace"
	case SymbolKindPackage:
		return "package"
	case SymbolKindClass:
		return "class"
	case SymbolKindMethod:
		return "method"
	case SymbolKindProperty:
		return "property"
	case SymbolKindField:
		return "field"
	case SymbolKindConstructor:
		return "constructor"
	case SymbolKindEnum:
		return "enum"
	case SymbolKindInterface:
		return "interface"
	case SymbolKindFunction:
		return "function"
	case SymbolKindVariable:
		return "variable"
	case SymbolKindConstant:
		return "constant"
	case SymbolKindStruct:
		return "struct"
	default:
		return "symbol"
	}
}

func (t *LSPTool) doDocumentOutline(ctx context.Context, c *Client, uri string) (string, error) {
	symbols, err := c.DocumentSymbols(ctx, uri)
	if err != nil {
		t.log.Warn(logger.CatMCP, "lsp tool error", "tool", t.action, "err", err.Error())
		return formatError("document_outline", err), nil
	}
	if len(symbols) == 0 {
		return "No symbols found in file.", nil
	}
	return formatSymbolsOutline(symbols, 0), nil
}

func (t *LSPTool) doGetCodeItem(ctx context.Context, c *Client, uri string, name string) (string, error) {
	if name == "" {
		return `{"error": "query (symbol name) is required"}`, nil
	}
	symbols, err := c.DocumentSymbols(ctx, uri)
	if err != nil {
		return formatError("get_code_item", err), nil
	}
	var flat []DocumentSymbol
	flattenSymbols(symbols, &flat)

	var target *DocumentSymbol
	for i := range flat {
		if strings.EqualFold(flat[i].Name, name) {
			target = &flat[i]
			break
		}
	}

	if target == nil {
		return fmt.Sprintf("Symbol %q not found in file.", name), nil
	}

	filePath := uriToPath(uri)
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		return formatError("get_code_item", err), nil
	}

	lines := strings.Split(string(contentBytes), "\n")
	startLine := target.Range.Start.Line
	endLine := target.Range.End.Line
	if startLine < 0 || endLine >= len(lines) || startLine > endLine {
		return "Invalid symbol range returned by LSP server.", nil
	}

	selected := lines[startLine : endLine+1]
	return strings.Join(selected, "\n"), nil
}

func (t *LSPTool) doGotoDefinitionByName(ctx context.Context, c *Client, query string) (string, error) {
	if query == "" {
		return `{"error": "query (symbol name) is required"}`, nil
	}
	symbols, err := c.WorkspaceSymbols(ctx, query)
	if err != nil {
		return formatError("goto_definition_by_name", err), nil
	}

	var bestSymbols []SymbolInformation
	for _, s := range symbols {
		if strings.EqualFold(s.Name, query) || strings.Contains(strings.ToLower(s.Name), strings.ToLower(query)) {
			bestSymbols = append(bestSymbols, s)
		}
	}
	if len(bestSymbols) == 0 {
		bestSymbols = symbols // Fall back to all returned symbols if no close matches
	}

	if len(bestSymbols) == 0 {
		return fmt.Sprintf("No symbols found matching %q in workspace.", query), nil
	}

	type matchPreview struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		File    string `json:"file"`
		Line    int    `json:"line"`
		Preview string `json:"preview"`
	}
	var previews []matchPreview

	// Show previews for at most the top 5 matches to keep token usage small
	limit := len(bestSymbols)
	if limit > 5 {
		limit = 5
	}

	for i := 0; i < limit; i++ {
		sym := bestSymbols[i]

		// Follow the definition just in case the symbol location points to declaration/reference
		defURI := sym.Location.URI
		defPos := sym.Location.Range.Start

		// Attempt goto definition
		locs, defErr := c.GotoDefinition(ctx, sym.Location.URI, sym.Location.Range.Start)
		if defErr == nil && len(locs) > 0 {
			defURI = locs[0].URI
			defPos = locs[0].Range.Start
		}

		defPath := uriToPath(defURI)
		contentBytes, err := os.ReadFile(defPath)
		var previewText string
		var previewLine int
		if err == nil {
			lines := strings.Split(string(contentBytes), "\n")
			targetLine := defPos.Line
			previewLine = targetLine + 1
			start := targetLine - 5
			if start < 0 {
				start = 0
			}
			end := targetLine + 15
			if end >= len(lines) {
				end = len(lines) - 1
			}
			var snippet []string
			for lineIdx := start; lineIdx <= end; lineIdx++ {
				prefix := "   "
				if lineIdx == targetLine {
					prefix = "-> "
				}
				snippet = append(snippet, fmt.Sprintf("%s%d: %s", prefix, lineIdx+1, lines[lineIdx]))
			}
			previewText = strings.Join(snippet, "\n")
		} else {
			previewText = fmt.Sprintf("Error reading file preview: %v", err)
			previewLine = defPos.Line + 1
		}

		previews = append(previews, matchPreview{
			Name:    sym.Name,
			Kind:    symbolKindToString(sym.Kind),
			File:    defPath,
			Line:    previewLine,
			Preview: previewText,
		})
	}

	data, _ := json.Marshal(map[string]any{
		"matches": previews,
		"count":   len(symbols),
	})
	return string(data), nil
}
