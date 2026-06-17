# SoloQueue Architecture

## Overview

SoloQueue uses a hierarchical architecture design. The core systems include Agent, LLM, Tool, Skill, and Config subsystems.

### System Architecture & Subsystem Relationships

```
                        ┌─────────────────────────────────────────────────────────────┐
                        │                        Web UI (React)                       │
                        └──────────────────────────────┬──────────────────────────────┘
                                                       │ REST / WebSockets
                                                       ▼
                        ┌─────────────────────────────────────────────────────────────┐
                        │                   Server (internal/server)                  │
                        └──────────────────────────────┬──────────────────────────────┘
                                                       │
                                                       ▼
                        ┌─────────────────────────────────────────────────────────────┐
                        │                  Session (internal/session)                 │
                        └──────┬───────────────────────┬───────────────────────┬──────┘
                               │                       │                       │
                               ▼                       ▼                       ▼
      ┌────────────────────────┴──┐        ┌───────────┴───────────┐      ┌────┴──────────────────────┐
      └───────────────────────────┘        └───────────┬───────────┘      └────────────┬──────────────┘
                                                       │                               │
                                                       ├───────────────────────────────┤ SQLite WAL
                                                       │                               ▼
                                                       │                  ┌───────────────────────────┐
                                                       │                  │ SQLite DB (sqlitedb)      │
                                                       │                  └───────────────────────────┘
                                                       ▼
                                   ┌───────────────────┴───────────────────┐
                                   │       Tool / Skill Capability Layer   │
                                   │  (internal/tools, internal/skill)     │
                                   └───────────────────┬───────────────────┘
                                                       │
                                                       ▼
                                   ┌───────────────────┴───────────────────┐
                                   │         LLM (internal/llm)            │
                                   └───────────────────┬───────────────────┘
                                                       │
                                                       ▼
                                   ┌───────────────────┴───────────────────┐
                                   │     Config (internal/config)          │
                                   └───────────────────────────────────────┘
```

### Subsystems Index

| Subsystem          | Path                                        | Description                                                               | Documentation                    |
| ------------------ | ------------------------------------------- | ------------------------------------------------------------------------- | -------------------------------- |
| **Agent System**   | `internal/agent/`                           | Actor-model agent runtime, tool loops, async delegation, supervisor       | Below                            |
| **LLM System**     | `internal/llm/`                             | Provider-agnostic protocol layer + DeepSeek HTTP/SSE transport            | Below                            |
| **Tool System**    | `internal/tools/`                           | Executable primitive layer (file, shell, HTTP, search, LSP)               | Below                            |
| **Skill System**   | `internal/skill/`                           | Reusable task recipes (inline/fork) & remote store manager                | [skill_store.md](skill_store.md) |
| **Config System**  | `internal/config/`                          | Layered TOML config, hot-reload, type-safe access                         | [config.md](config.md)           |
| **Task Routing**   | `internal/router/`                          | Intelligent task classification and model routing (L0-L3)                 | [routing.md](routing.md)         |
| **Context Window** | `internal/ctxwin/`                          | Token count calibration, middle-out JSON truncation, FIFO sliding         | [ctxwin.md](ctxwin.md)           |
| **Memory System**  | `internal/memory/` `internal/memoryengine/` | Short-term daily summaries & long-term BM25 + KG + optional vector engine | [memory.md](memory.md)           |
| **QQ Bot Client**  | `internal/qqbot/`                           | WebSocket connection loop, active/passive reply queue, media upload       | [qqbot.md](qqbot.md)             |
| **MCP & LSP**      | `internal/mcp/`                             | Model Context Protocol servers loading, LSP JSON-RPC tool binding         | [mcp.md](mcp.md)                 |

---

## Agent System

**Location**: `internal/agent/`

The agent package is the execution core of SoloQueue. It implements a long-lived actor model that processes tasks sequentially via mailboxes and emits typed event streams.

### Core Concepts

- **Definition** (`types.go`): Immutable agent specifications (ID, Name, ModelID, thinking rules, etc.).
- **Agent** (`agent.go`): Mixes immutable config with runtime execution state (mailbox, active turns, priority mailbox).
- **Lifecycle**: `NewAgent -> Start -> Ask/Submit -> Stop` (restart allowed after stop).

### Key Features

1. **Actor Model**: Tasks are serialized. One job executes at a time per agent, eliminating concurrent state mutation races.
2. **Streaming-First**: The agent stream emits fine-grained events (`ContentDeltaEvent`, `ToolCallDeltaEvent`, etc.) natively, which are consumed by client WebSockets.
3. **Confirmation Pipeline**: Prompts requiring authorization block and emit `ToolNeedsConfirmEvent` to the UI, resuming execution when `Agent.Confirm` is called.

### Multi-Agent Async Delegation & Supervision

SoloQueue orchestrates complex workloads by letting higher-level agents delegate sub-tasks to child agents.

```
       [ L1 Core Agent ]
               │
      Delegate Tool (Async)
               │
               ▼
┌──────────────────────────────┐
│  Supervisor (internal/agent) │
├──────────────────────────────┤
│  Manages L2 Child Lifecycles │
└──────────────┬───────────────┘
               │
               ├─ Spawns L2 Agent A ──➔ Synchronous Execution
               │
               └─ Spawns L2 Agent B ──➔ Asynchronous (Mailbox Continuation)
```

#### 1. Synchronous vs Asynchronous Delegation

The system supports two delegation modes via the `DelegateTool` (`internal/tools/delegate.go`):

- **Synchronous (L2 ➔ L3)**: The parent agent blocks waiting for the child agent to complete. This is used for deeply nested, linear sub-tasks.
- **Asynchronous (L1 ➔ L2)**: The L1 agent yields execution immediately after spawning the L2 agent. The parent is freed to process other mailbox messages or user interactions.

#### 2. Continuation-Passing over Mailbox

To support async delegation without blocking OS threads, SoloQueue uses a **Continuation-Passing** pattern:

- When L1 calls `DelegateTool` asynchronously, it records the delegation status in `internal/agent/async_turn.go` and returns an `AsyncAction` payload to the tool loop.
- The L1 agent's tool loop yields execution and returns to `Idle`.
- The child L2 agent executes its job in a separate actor thread.
- When the L2 child completes or yields, it submits a completion job back to the L1 parent's mailbox.
- The L1 parent wakes up, correlates the result with the saved async state, restores the tool execution context, and continues the conversation.

#### 3. Supervisor Lifecycle Management (`supervisor.go`)

Child agents are managed by a `Supervisor` instance:

- **Registry**: Tracks active child agent IDs spawned during the current session.
- **Orphan Prevention**: If a session is cancelled or the parent L1 agent stops, the supervisor captures the event and cascades termination commands to all running L2/L3 child agents, ensuring no orphan processes or dangling LLM API connections remain.

### File Layout

```
internal/agent/
├── agent.go           # Core Agent struct and Ask/Submit APIs
├── lifecycle.go       # Start/Stop lifecycle
├── run.go             # Mailbox run loops (FIFO and priority)
├── stream.go          # LLM tool loop, tool execution
├── ask.go             # Public APIs (Ask, AskStream, AskWithHistory)
├── async_turn.go     # Async delegation state and resumption
├── confirm.go         # Confirmation handling
├── factory.go         # Template-driven agent creation
├── registry.go        # Agent registry and supervision
├── supervisor.go      # L2 child agent lifecycle management
└── llm.go            # LLMClient contract definition
```

### Architectural Strengths

- Strong actor semantics simplify concurrency reasoning
- Streaming-first design avoids duplicate implementations
- Async delegation is continuation-based, not thread-blocking
- Factory centralizes multi-system assembly

---

## LLM System

**Location**: `internal/llm/` (protocol) + `internal/llm/deepseek/` (provider)

### Layer 1: Provider-Agnostic Types (`internal/llm/types.go`)

Defines universal protocol objects:

- Tool-calling types: `ToolCall`, `ToolDef`, `FunctionCall`
- Streaming event model: `Event` (EventDelta/EventDone/EventError)
- Usage accounting: `Usage`, `FinishReason`
- Error envelope: `APIError`

### Layer 2: Agent-Facing Contract (`internal/agent/llm.go`)

`LLMClient` interface:

```go
type LLMClient interface {
    ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error)
}
```

### DeepSeek Provider (`internal/llm/deepseek/`)

- **Streaming-first**: `Chat` is implemented by consuming `ChatStream`
- **Retry**: `doWithRetry()` with configurable backoff
- **SSE Parsing**: Minimal `sseReader` (skips comments, recognizes `[DONE]`)
- **Chunk Conversion**: `chunkToEvents()` normalizes provider chunks to `llm.Event`

### File Layout

```
internal/llm/
├── types.go           # Shared protocol types
├── retry.go           # Generic retry helper
└── deepseek/
    ├── client.go      # HTTP/SSE transport
    ├── wire.go        # Wire request/response structs
    └── sse.go        # SSE parser
```

---

## Tool System

**Location**: `internal/tools/`

Tools are the executable primitive layer. Every tool maps directly to one LLM function-calling entry.

### Core Contracts

1. **Tool** interface:
   - `Name()`, `Description()`, `Parameters()` (JSON Schema), `Execute(ctx, args)`

2. **Confirmable** interface (optional):
   - `NeedsConfirm()`, `ConfirmPrompt()`, `OnConfirm(choice, userInput)`

3. **AsyncTool** interface (optional):
   - `ExecuteAsync(ctx, args)` returns `AsyncAction`

### Built-in Tools

- File tools: `Read`, `Grep`, `Glob`, `Write`, `Edit`, `Replace`
- Network tools: `WebFetch`, `WebSearch`
- Command tool: `Bash` (shell execution)
- Delegation tool: `Delegate` (L1->L2, L2->L3)

### Delegation Tool

`DelegateTool` supports two modes:

- **Synchronous**: L2 -> L3 delegation (blocks until complete)
- **Asynchronous**: L1 -> L2 delegation (returns `AsyncAction` for agent framework)

### File Layout

```
internal/tools/
├── tool.go            # Core interfaces (Tool, Confirmable, AsyncTool)
├── registry.go        # Tool registry (name -> Tool mapping)
├── config.go          # Shared config for all tools
├── delegate.go        # Delegation tool (sync + async)
├── http_fetch.go      # WebFetch tool
├── web_search.go      # WebSearch tool
├── shell_exec.go      # Bash tool
├── file_read.go       # Read tool
├── grep.go            # Grep tool
├── glob.go            # Glob tool
└── write_file.go      # Write tool
```

---

## Skill System

**Location**: `internal/skill/`

Skills add a second abstraction layer above raw tools. A tool is a low-level executable primitive. A skill is a reusable task recipe with instructions, optional preprocessing, and an execution mode.

### Core Concepts

- **Skill**: Immutable definition (ID, Description, Instructions, AllowedTools, Context)
- **SkillRegistry**: Concurrent-safe `id -> *Skill` map
- **SkillTool**: Adapter that exposes skills to LLM through one function call

### Execution Modes

1. **Inline**: Return preprocessed instruction to parent agent (default)
2. **Fork**: Execute in a child agent (isolated execution)

### Preprocessing Pipeline

`PreprocessContent()` applies three transformations:

1. `$ARGUMENTS` substitution
2. Shell expansion for `` `!`command` ``
3. File reference expansion for `@path`

### Skill Loading

Skills are loaded from Markdown files (`SKILL.md`) with YAML frontmatter:

- `name`, `description`, `allowed-tools`, `context`, `agent`

Loading supports layered scopes: `plugin -> user -> project` (override precedence)

### File Layout

```
internal/skill/
├── skill.go           # Core types and registry
├── skill_tool.go      # LLM-facing adapter
├── fork.go            # Fork execution mode
├── skill_md.go       # Markdown loading and frontmatter parsing
└── preprocess.go      # Instruction preprocessing pipeline
```

---

## Config System

**Location**: `internal/config/`

The config system is the runtime control plane. It owns the global settings schema, layered file loading, merge semantics, and hot-reload watching.

### Core Features

- **Layered Loading**: `defaults -> settings.toml -> settings.local.toml`
- **Hot Reload**: `fsnotify`-based file watching with 200ms debounce
- **Type-Safe**: Generic `Loader[T]` with concurrent-safe snapshot access
- **Atomic Saves**: Temp-file-plus-rename pattern

### Schema Sections

- `Session`: Session management settings
- `Log`: Logging configuration
- `Tools`: Tool limits and policies
- `Providers`: LLM provider credentials and settings
- `Models`: Model catalog with context windows and settings
- `DefaultModels`: Role-based model mappings (`expert`, `superior`, `universal`, `fast`)

### Merge Semantics

- Object fields: merge recursively
- Arrays: replace wholesale
- Omitted fields: preserve previous values

### File Layout

```
internal/config/
├── schema.go          # Settings struct definition
├── defaults.go        # Hardcoded default values
├── loader.go          # Generic layered loader with watch
├── merge_toml.go     # TOML merge semantics
├── service.go         # GlobalService facade
└── tools_convert.go   # Runtime config conversion helpers
```

---

## Session & Routing

### Session Management (`internal/session/`)

- Manages conversation lifetime and ordering
- Owns `ContextWindow` (tiktoken-based, dual-waterline compaction)
- `inFlight` CAS lock ensures only one concurrent Ask per session

### Task Routing (`internal/router/`)

Classifies user input into 4 levels (L0-L3) based on complexity:

| Level | Name         | Model                      | Use Case             |
| ----- | ------------ | -------------------------- | -------------------- |
| L0    | Conversation | deepseek-v4-flash          | Q&A, explanation     |
| L1    | Simple       | deepseek-v4-flash-thinking | Single file changes  |
| L2    | Medium       | deepseek-v4-pro            | Multi-file features  |
| L3    | Complex      | deepseek-v4-pro-max        | Architecture changes |

**Classification strategy**:

1. **Fast Track**: Pattern-based rules (zero latency)
2. **LLM Fallback**: Semantic understanding (4s timeout)

**Hybrid Sticky Logic**: Session remembers current task level to handle short follow-ups correctly.

---

## Key Design Decisions

### 1. Streaming-First Design

Both Agent and LLM systems prioritize streaming APIs. Blocking APIs are wrappers over event accumulation. This avoids divergence between sync and stream paths.

### 2. Event-Based Architecture

Typed event streams (`llm.Event` -> `agent.AgentEvent`) provide clear contract boundaries between:

- Agent and Session/Server layers
- Parent and child agents during delegation
- Agent and Tools package during confirm forwarding

### 3. Actor Model for Concurrency

Each agent processes one job at a time (mailbox semantics). This simplifies concurrency reasoning and avoids shared mutable state issues.

### 4. Separation of Concerns

- `llm` owns retry policy mechanics
- Provider package owns request execution and error classification
- `tools` own capability semantics
- `agent` owns orchestration semantics
- `session` owns conversation ordering and context mutation

### 5. Markdown-Defined Agents and Skills

Agent templates and skills are defined in Markdown with YAML frontmatter. This makes them:

- Easy to author and version control
- Supports user/project overrides through directory layering
- Separates behavior definition from code

---

## Files to Read First

**Agent System:**

- `internal/agent/agent.go`
- `internal/agent/stream.go`
- `internal/agent/async_turn.go`

**LLM System:**

- `internal/llm/types.go`
- `internal/llm/deepseek/client.go`
- `internal/llm/deepseek/wire.go`

**Tool System:**

- `internal/tools/tool.go`
- `internal/tools/delegate.go`

**Skill System:**

- `internal/skill/skill.go`
- `internal/skill/skill_tool.go`

**Config System:**

- `internal/config/schema.go`
- `internal/config/loader.go`
- `internal/config/service.go`
