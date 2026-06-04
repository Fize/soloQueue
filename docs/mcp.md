# MCP & LSP Subsystem

**Location**: `internal/mcp/` (MCP client and manager), `internal/mcp/lsp/` (Language Server Protocol client and tools)

SoloQueue integrates Model Context Protocol (MCP) servers and standard Language Server Protocol (LSP) engines to extend the agent's capabilities. This allows agents to interact with external tools and query code symbols (like definitions, references, and diagnostics) with IDE-level precision.

---

## Model Context Protocol (MCP) Integration

MCP provides an open standard for exposing tools and resources to LLMs. SoloQueue implements a client-side manager in `internal/mcp/manager.go`.

### Loading Configurations
- **Configuration File**: MCP servers are defined in `~/.soloqueue/mcp.json`.
- **Hot-Reload**: The system monitors `mcp.json` using `fsnotify`. When changed, it reloads the config, restarts modified server processes, and updates active tool mappings without restarting the SoloQueue serve process.

### Client & Tool Adaptation
1. **Server Processes**: The manager spawns configured MCP servers as subprocesses (communicating via stdin/stdout JSON-RPC).
2. **Tool Retrieval**: Upon startup, the manager queries the server for its list of available tools.
3. **Registry Integration**: Each MCP tool is wrapped inside a SoloQueue adapter (`mcp/tool.go`) that implements the `tools.Tool` interface. The tool parameters are dynamically parsed from JSON Schema, making them natively callable by the L1/L2 agent loops.

---

## Language Server Protocol (LSP) Subsystem

The LSP subsystem (`internal/mcp/lsp/`) enables agents to perform code navigation, syntax checking, and static analysis directly within their workspace, bypassing slow text grep operations.

```
 ┌──────────────┐          JSON-RPC (stdio)         ┌──────────────┐
 │ L1/L2 Agent  │ <───────────────────────────────> │  LSP Server  │
 └──────┬───────┘                                   │ (e.g. gopls) │
        │                                           └──────────────┘
        ├─ Hover tool ───➔ getHover()
        ├─ Definition ───➔ getDefinition()
        ├─ References ───➔ getReferences()
        └─ Diagnostics ──➔ publishDiagnostics()
```

### 1. LSP Client & Transport (`client.go` & `transport.go`)
- **JSON-RPC Protocol**: Implements the LSP 3.x specification over stdio.
- **Initialization**: Automatically detects the workspace language, spawns the corresponding language server (e.g., `gopls` for Go, `typescript-language-server` for TS), and executes the `initialize` handshake.
- **Document Synchronization**: Sends `textDocument/didOpen` and `textDocument/didChange` notifications to sync memory buffers with the LSP server.

### 2. LSP-Backed LLM Tools (`tools.go`)
The client wraps language server capabilities into standard LLM function-calling tools:

| Tool Name | LSP Method | Description |
|-----------|------------|-------------|
| **`lsp_hover`** | `textDocument/hover` | Retrieves documentation, type signatures, and docstrings under the virtual cursor. |
| **`lsp_definition`** | `textDocument/definition` | Finds the exact source code location (file and line range) where a function, struct, or variable is defined. |
| **`lsp_references`** | `textDocument/references` | Locates all usages of a code symbol across the entire workspace. |
| **`lsp_symbols`** | `textDocument/documentSymbol` | Lists all functions, classes, and variables declared within a specific file. |
| **`lsp_diagnostics`** | `textDocument/publishDiagnostics` | Captures compiler/syntax errors and warnings in real-time, helping agents self-correct errors during the coding loop. |
| **`lsp_format`** | `textDocument/formatting` | Automatically formats source code according to style rules (e.g., `go fmt`, `prettier`). |

### 3. Verification & Safety
- **Process Supervision**: Spawns language servers with controlled timeouts. If an LSP server hangs, it is automatically terminated and restarted.
- **Path Sanitization**: Restricts all LSP operations to the active project workspace to prevent agents from querying sensitive system files.
