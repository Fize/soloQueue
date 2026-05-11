package lsp

import "encoding/json"

// ── JSON-RPC 2.0 ──────────────────────────────────────────────────────────────

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// ── LSP Base Types ────────────────────────────────────────────────────────────

type URI = string

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   URI   `json:"uri"`
	Range Range `json:"range"`
}

type TextDocumentIdentifier struct {
	URI URI `json:"uri"`
}

type TextDocumentItem struct {
	URI        URI    `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     URI `json:"uri"`
	Version int `json:"version"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// ── SymbolKind ────────────────────────────────────────────────────────────────

type SymbolKind int

const (
	SymbolKindFile          SymbolKind = 1
	SymbolKindModule        SymbolKind = 2
	SymbolKindNamespace     SymbolKind = 3
	SymbolKindPackage       SymbolKind = 4
	SymbolKindClass         SymbolKind = 5
	SymbolKindMethod        SymbolKind = 6
	SymbolKindProperty      SymbolKind = 7
	SymbolKindField         SymbolKind = 8
	SymbolKindConstructor   SymbolKind = 9
	SymbolKindEnum          SymbolKind = 10
	SymbolKindInterface     SymbolKind = 11
	SymbolKindFunction      SymbolKind = 12
	SymbolKindVariable      SymbolKind = 13
	SymbolKindConstant      SymbolKind = 14
	SymbolKindString        SymbolKind = 15
	SymbolKindNumber        SymbolKind = 16
	SymbolKindBoolean       SymbolKind = 17
	SymbolKindArray         SymbolKind = 18
	SymbolKindObject        SymbolKind = 19
	SymbolKindKey           SymbolKind = 20
	SymbolKindNull          SymbolKind = 21
	SymbolKindEnumMember    SymbolKind = 22
	SymbolKindStruct        SymbolKind = 23
	SymbolKindEvent         SymbolKind = 24
	SymbolKindOperator      SymbolKind = 25
	SymbolKindTypeParameter SymbolKind = 26
)

func (k SymbolKind) String() string {
	switch k {
	case SymbolKindFile:
		return "File"
	case SymbolKindModule:
		return "Module"
	case SymbolKindNamespace:
		return "Namespace"
	case SymbolKindPackage:
		return "Package"
	case SymbolKindClass:
		return "Class"
	case SymbolKindProperty:
		return "Property"
	case SymbolKindMethod:
		return "Method"
	case SymbolKindField:
		return "Field"
	case SymbolKindConstructor:
		return "Constructor"
	case SymbolKindEnum:
		return "Enum"
	case SymbolKindInterface:
		return "Interface"
	case SymbolKindFunction:
		return "Function"
	case SymbolKindVariable:
		return "Variable"
	case SymbolKindConstant:
		return "Constant"
	case SymbolKindString:
		return "String"
	case SymbolKindNumber:
		return "Number"
	case SymbolKindBoolean:
		return "Boolean"
	case SymbolKindArray:
		return "Array"
	case SymbolKindObject:
		return "Object"
	case SymbolKindStruct:
		return "Struct"
	case SymbolKindTypeParameter:
		return "TypeParameter"
	default:
		return "Unknown"
	}
}

// ── Hover ─────────────────────────────────────────────────────────────────────

type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"` // "markdown" or "plaintext"
	Value string `json:"value"`
}

// ── DocumentSymbol ─────────────────────────────────────────────────────────────

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// ── SymbolInformation (workspace/symbol) ──────────────────────────────────────

type SymbolInformation struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	Location Location   `json:"location"`
	// ContainerName is the name of the symbol containing this symbol.
	ContainerName string `json:"containerName,omitempty"`
}

// ── Diagnostic ─────────────────────────────────────────────────────────────────

type DiagnosticSeverity int

const (
	SeverityError       DiagnosticSeverity = 1
	SeverityWarning     DiagnosticSeverity = 2
	SeverityInformation DiagnosticSeverity = 3
	SeverityHint        DiagnosticSeverity = 4
)

func (s DiagnosticSeverity) String() string {
	switch s {
	case SeverityError:
		return "Error"
	case SeverityWarning:
		return "Warning"
	case SeverityInformation:
		return "Info"
	case SeverityHint:
		return "Hint"
	default:
		return "Unknown"
	}
}

type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     any                `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

type DiagnosticReport struct {
	Items []Diagnostic `json:"items"`
}

// ── CallHierarchy ─────────────────────────────────────────────────────────────

type CallHierarchyItem struct {
	Name           string     `json:"name"`
	Kind           SymbolKind `json:"kind"`
	URI            URI        `json:"uri"`
	Range          Range      `json:"range"`
	SelectionRange Range      `json:"selectionRange"`
	Detail         string     `json:"detail,omitempty"`
}

type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

// ── Initialize ─────────────────────────────────────────────────────────────────

type InitializeParams struct {
	ProcessID             int                `json:"processId,omitempty"`
	RootURI               URI                `json:"rootUri,omitempty"`
	RootPath              string             `json:"rootPath,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
	InitializationOptions any                `json:"initializationOptions,omitempty"`
}

type WorkspaceFolder struct {
	URI  URI    `json:"uri"`
	Name string `json:"name"`
}

type ClientCapabilities struct {
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
	Workspace    *WorkspaceClientCapabilities    `json:"workspace,omitempty"`
}

type TextDocumentClientCapabilities struct {
	Synchronization *SynchronizationCapability `json:"synchronization,omitempty"`
	Hover           *BoolCapability            `json:"hover,omitempty"`
	Definition      *BoolCapability            `json:"definition,omitempty"`
	References      *BoolCapability            `json:"references,omitempty"`
	DocumentSymbol  *struct {
		DynamicRegistration             bool `json:"dynamicRegistration,omitempty"`
		HierarchicalDocumentSymbolSupport bool `json:"hierarchicalDocumentSymbolSupport,omitempty"`
	} `json:"documentSymbol,omitempty"`
	Implementation *BoolCapability `json:"implementation,omitempty"`
	CallHierarchy  *BoolCapability `json:"callHierarchy,omitempty"`
	Diagnostic     *BoolCapability `json:"diagnostic,omitempty"`
}

type SynchronizationCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
	DidSave             bool `json:"didSave,omitempty"`
	WillSave            bool `json:"willSave,omitempty"`
	WillSaveWaitUntil   bool `json:"willSaveWaitUntil,omitempty"`
}

type BoolCapability struct {
	DynamicRegistration bool `json:"dynamicRegistration,omitempty"`
}

type WorkspaceClientCapabilities struct {
	Symbol *BoolCapability `json:"symbol,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

type ServerCapabilities struct {
	TextDocumentSync   *TextDocumentSyncOptions `json:"textDocumentSync,omitempty"`
	HoverProvider      any                      `json:"hoverProvider,omitempty"`
	DefinitionProvider any                      `json:"definitionProvider,omitempty"`
	ReferencesProvider any                      `json:"referencesProvider,omitempty"`
	DocumentSymbolProvider any                  `json:"documentSymbolProvider,omitempty"`
	WorkspaceSymbolProvider any                 `json:"workspaceSymbolProvider,omitempty"`
	ImplementationProvider any                  `json:"implementationProvider,omitempty"`
	CallHierarchyProvider any                   `json:"callHierarchyProvider,omitempty"`
	DiagnosticProvider   any                    `json:"diagnosticProvider,omitempty"`
}

type TextDocumentSyncOptions struct {
	OpenClose bool `json:"openClose,omitempty"`
	Change    int  `json:"change,omitempty"` // 0=None, 1=Full, 2=Incremental
	Save      any  `json:"save,omitempty"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ── Parameter Types ───────────────────────────────────────────────────────────

type DefinitionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext        `json:"context"`
}

type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

type ImplementationParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type CallHierarchyPrepareParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type DocumentDiagnosticParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}
