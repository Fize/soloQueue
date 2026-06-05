# SoloQueue Project Analysis

> Generated on: 2026-06-05
> Analyzed by: Claude (AI Analysis)

## 1. Project Overview

### 1.1 Basic Information
- **Project Name**: SoloQueue (SoloQueue)
- **Project Type**: Web Application + CLI Tool
- **Version**: 0.1.0
- **Primary Language**: Go (backend), TypeScript (frontend)
- **Repository**: https://github.com/xiaobaitu/soloqueue
- **Description**: AI multi-agent collaboration platform with hierarchical task routing, built with Go and React, designed for DeepSeek LLM integration

### 1.2 Project Status
- **Current Phase**: Active Development
- **Team Size**: 1 (individual developer)
- **Active Branches**: main

## 2. Technology Stack

### 2.1 Backend
| Category | Technology | Version | Notes |
|----------|-----------|---------|-------|
| Language | Go | 1.25.8 | Modern Go with generics |
| HTTP Router | chi/v5 | 5.2.5 | Lightweight, idiomatic Go router |
| CLI Framework | Cobra | 1.10.2 | Subcommand-based (serve, version) |
| Database | SQLite (modernc) | 1.50.0 | Pure Go, no CGO required, WAL mode |
| LLM Provider | DeepSeek (custom SSE client) | - | Streaming-first HTTP/SSE transport |
| Token Counting | tiktoken-go | 0.1.8 | Context window calibration |
| Configuration | go-toml/v2 | 2.3.0 | Layered TOML with hot-reload |
| WebSocket | Gorilla WebSocket | 1.5.3 | Real-time state broadcasting |
| File Watching | fsnotify | 1.9.0 | Hot-reload config/filesystem |
| MCP Protocol | mcp-go (mark3labs) | 0.52.0 | Model Context Protocol server/client |
| YAML | go-yaml.v3 | 3.0.1 | Agent template frontmatter parsing |
| HTML Parsing | goquery | 1.12.0 | WebFetch tool |
| Globbing | doublestar/v4 | 4.10.0 | Glob tool |
| Sync | golang.org/x/sync | 0.20.0 | errgroup for parallel tool execution |

### 2.2 Frontend
| Category | Technology | Version | Notes |
|----------|-----------|---------|-------|
| Language | TypeScript | ~5.8.3 | Strict mode |
| Framework | React | 19.1.0 | Latest stable |
| Build Tool | Vite | 7.x | Fast HMR |
| CSS | TailwindCSS | v4 | Utility-first |
| State Management | Zustand | 5.0.13 | Lightweight store |
| Routing | react-router-dom | 7.15.0 | Client-side SPA routing |
| Drag & Drop | @dnd-kit/core + sortable | 6.3.1 / 10.0.0 | Task reordering |
| UI Primitives | @base-ui/react | 1.4.1 | Accessible component primitives |
| Markdown | react-markdown + remark-gfm | 10.1.0 / 4.0.1 | Content rendering with GFM |
| Syntax Highlighting | react-syntax-highlighter | 16.1.1 | Code block display |
| Icons | lucide-react | 1.14.0 | Icon library |
| Package Manager | pnpm | 11.1.2 | Fast, disk-efficient |
| Testing | Vitest + Playwright | 3.2.4 / 1.60.0 | Unit + E2E |
| Linting | ESLint + Prettier | 9.x / 3.x | Code quality |

### 2.3 Infrastructure & DevOps
| Category | Technology | Notes |
|----------|-----------|-------|
| Build | Make + Go build | `make build` orchestrates web+go build |
| Static Embedding | Go //go:embed | Web dist embedded in binary |
| Proxy | Custom Go reverse proxy | Subdomain/path-based routing to external services |
| Scheduled Tasks | Custom cron (SQLite-backed) | Cron expressions + timers |
| RTK Integration | rtk CLI (optional) | 60-90% token savings on dev operations |

## 3. Project Architecture

### 3.1 Directory Structure
```
soloQueue/
├── cmd/soloqueue/              # Cobra entrypoint (main.go + cli/)
│   ├── main.go                 # Root command assembly
│   ├── cli/commands.go         # serve & version subcommands
│   └── cli/qqbot.go           # QQ Bot WebSocket integration startup
├── internal/                   # All application code (no external API)
│   ├── agent/                  # Actor-model agent runtime
│   │   ├── agent.go           # Core Agent struct (mailbox, lifecycle, runtime)
│   │   ├── lifecycle.go       # Start/Stop lifecycle management
│   │   ├── run.go             # Mailbox run loops (FIFO + priority)
│   │   ├── stream.go          # LLM tool loop: call → execute → repeat
│   │   ├── ask.go             # Public API (Ask, AskStream, AskWithHistory)
│   │   ├── async_turn.go     # Async delegation state & continuation
│   │   ├── confirm.go         # Tool confirmation pipeline
│   │   ├── factory.go         # Template-driven agent creation
│   │   ├── registry.go        # Agent registry with supervision
│   │   ├── supervisor.go      # L2 child agent lifecycle management
│   │   └── llm.go            # LLMClient contract + FakeLLM for testing
│   ├── compactor/             # LLM-based context compression engine
│   ├── config/                 # Layered TOML config, hot-reload, type-safe access
│   │   ├── schema.go          # Settings struct hierarchy
│   │   ├── defaults.go        # Compiled-in defaults
│   │   ├── loader.go          # Generic layered loader with fsnotify
│   │   ├── merge_toml.go     # TOML recursive merge semantics
│   │   ├── service.go         # GlobalService facade
│   │   └── tools_convert.go   # Runtime config conversion helpers
│   ├── cron/                   # Scheduled task system (cron + timers, SQLite-backed)
│   ├── ctxwin/                 # Context window: tiktoken counts, dual-waterline compaction
│   ├── embedding/              # Embedding service interface
│   ├── iface/                  # Shared interfaces (breaks agent↔tools cycle)
│   ├── llm/                    # Provider-agnostic LLM protocol types
│   │   ├── types.go           # Event, ToolCall, Usage, APIError
│   │   ├── retry.go           # Exponential backoff retry helper
│   │   └── deepseek/          # DeepSeek HTTP/SSE transport
│   │       ├── client.go      # HTTP client with streaming
│   │       ├── wire.go        # Wire request/response structs
│   │       └── sse.go        # Minimal SSE parser
│   ├── logger/                 # Structured logging (file + console, categorized)
│   ├── mcp/                    # MCP server manager + config + LSP integration
│   │   └── lsp/               # Built-in LSP MCP server manager
│   ├── memory/                 # Short-term memory (daily summaries, Markdown files)
│   ├── permanent/              # Embedding-based permanent memory (>7 day migration)
│   ├── prompt/                 # Prompt assembly, templates, team management, parser
│   ├── proxy/                  # Go native reverse proxy manager (nginx-style)
│   ├── qqbot/                  # QQ official bot WebSocket integration
│   ├── rotating/               # Rotating file writer
│   ├── router/                 # L0-L3 task classification & model routing
│   ├── runtime/                # Shared dependency container (Stack, Build once)
│   │   ├── stack.go           # Stack struct: holds all shared dependencies
│   │   └── build.go           # Build(): wires all subsystems together
│   ├── server/                 # REST + WebSocket HTTP router (chi/v5)
│   │   ├── server.go          # Mux: route registration, middleware, SPA fallback
│   │   ├── hub.go             # WebSocket Hub for real-time state broadcast
│   │   ├── session_handlers.go # Session API handlers (ask, cancel, clear)
│   │   ├── agent_handlers.go  # Agent CRUD + profile/config handlers
│   │   ├── team_handlers.go   # Team CRUD handlers
│   │   ├── config_handlers.go # Config endpoints (providers, models, tools)
│   │   ├── skill_handlers.go  # Skill import/install/store endpoints
│   │   ├── file_handlers.go   # File read/list/content endpoints
│   │   ├── cron_handlers.go   # Scheduled task CRUD
│   │   ├── mcp_handlers.go    # MCP config endpoints
│   │   ├── proxy_handlers.go  # Reverse proxy management
│   │   ├── project_handlers.go # Project CRUD
│   │   └── auth.go            # HTTP token-based authentication
│   ├── session/                # Session manager (single active, inFlight CAS lock)
│   ├── skill/                  # Claude Code-compatible skill system
│   │   ├── skill.go           # Skill type + registry
│   │   ├── skill_tool.go      # LLM-facing adapter (inline/fork modes)
│   │   ├── fork.go            # Fork execution (child agent spawn)
│   │   ├── skill_md.go        # SKILL.md loading + frontmatter parsing
│   │   └── preprocess.go      # $ARGUMENTS / `!`command / @file expansion
│   ├── sqlitedb/               # Shared SQLite wrapper (WAL mode, reused connection)
│   ├── team/                    # Team group reload
│   ├── teamstore/               # DB-backed team/agent store (SQLite)
│   ├── timeline/                # Append-only JSONL event sourcing (session replay)
│   ├── todo/                    # Plan/task store (SQLite-backed, BFS dep check)
│   ├── tools/                   # Tool implementations + sandbox execution backend
│   │   ├── tool.go            # Tool, Confirmable, AsyncTool interfaces
│   │   ├── registry.go        # Name→Tool concurrent-safe registry
│   │   ├── config.go          # Shared tool configuration
│   │   ├── delegate.go        # Delegation tool (sync L2→L3, async L1→L2)
│   │   ├── shell_exec.go      # Bash tool (RTK-integrated)
│   │   ├── file_read.go       # Read tool
│   │   ├── write_file.go      # Write/Edit/Replace tools
│   │   ├── grep.go            # Grep tool
│   │   ├── glob.go            # Glob tool
│   │   ├── http_fetch.go      # WebFetch tool
│   │   └── web_search.go      # WebSearch tool
│   └── vectorstore/            # SQLite-backed vector store (permanent memory)
├── web/                        # React 19 + TypeScript + Vite frontend
│   ├── src/
│   │   ├── App.tsx            # Root: auth gate → router with sidebar+main layout
│   │   ├── main.tsx           # Entry: fetch interceptor + theme detection
│   │   ├── index.css          # Tailwind v4 CSS
│   │   ├── components/        # UI components
│   │   │   ├── ui/            # shadcn-style primitives
│   │   │   ├── settings/      # Settings tabs (Config, Profile, Skills, MCP, Teams, Projects, Proxies)
│   │   │   ├── AgentListPage.tsx      # Agent list + runtime overview
│   │   │   ├── AgentDetailPage.tsx    # Agent chat + file browser
│   │   │   ├── AgentStreamView.tsx    # Real-time streaming display
│   │   │   ├── Sidebar.tsx            # Desktop navigation sidebar
│   │   │   ├── MobileNav.tsx          # Mobile bottom navigation
│   │   │   ├── FilesPage.tsx          # File system browser
│   │   │   ├── CronPage.tsx           # Scheduled task management
│   │   │   ├── SettingsLayout.tsx      # Settings wrapper layout
│   │   │   ├── FileContentView.tsx    # Code/file viewer
│   │   │   ├── FilePreview.tsx        # File preview component
│   │   │   └── IframePageView.tsx     # Embedded iframe rendering
│   │   ├── stores/            # Zustand stores
│   │   │   ├── agentStore.ts          # Agent state
│   │   │   ├── runtimeStore.ts        # Runtime metrics
│   │   │   ├── authStore.ts           # Authentication state
│   │   │   ├── planStore.ts           # Plan/task state
│   │   │   ├── toolsAndSkillsStore.ts # Tools & skills state
│   │   │   └── mcpConfigStore.ts      # MCP configuration state
│   │   ├── hooks/             # Custom React hooks
│   │   │   ├── useAgents.ts           # Agent list hook
│   │   │   ├── useAgentConfig.ts      # Agent config CRUD hook
│   │   │   ├── useAgentProfile.ts     # Agent profile hook
│   │   │   ├── useAgentStream.ts      # Streaming response hook
│   │   │   ├── useRuntime.ts          # Runtime metrics hook
│   │   │   ├── usePlans.ts            # Plan CRUD hook
│   │   │   ├── useTeams.ts            # Team list hook
│   │   │   ├── useToolsAndSkills.ts   # Tools & skills hook
│   │   │   ├── useMCPConfig.ts        # MCP config hook
│   │   │   └── useMediaQuery.ts       # Responsive breakpoint hook
│   │   ├── lib/               # Shared utilities (websocket, API client)
│   │   └── types/             # TypeScript type definitions
│   ├── package.json           # Dependencies & scripts
│   └── vite.config.ts         # Vite configuration
├── skills/                    # Pre-installed skills (Markdown files)
│   ├── claude-code/
│   ├── frontend-design/
│   ├── mcp-builder/
│   ├── skill-creator/
│   ├── webapp-testing/
│   └── ... (20+ skills)
├── docs/                      # Project documentation
│   ├── architecture.md        # Architecture overview & subsystem index
│   ├── design.md              # Open Design System template/guide
│   ├── config.md              # Configuration documentation
│   ├── ctxwin.md              # Context window documentation
│   ├── mcp.md                 # MCP protocol documentation
│   ├── memory.md              # Memory system documentation
│   ├── qqbot.md               # QQ Bot documentation
│   ├── routing.md             # Task routing documentation
│   ├── skill_store.md         # Skill store documentation
│   ├── timeline.md            # Timeline documentation
│   ├── todo.md                # Todo/plan documentation
│   └── roles/                 # Agent role definitions (Markdown)
├── open-design/               # Design system infrastructure
├── soloqueue-cozy-office/     # Office-themed game assets (sprites, furniture)
├── AGENTS.md                  # Developer quick-start guide for AI agents
├── Makefile                   # Build orchestration
├── go.mod / go.sum            # Go module definition
└── README.md                  # Project README
```

### 3.2 Core Modules & Responsibilities

#### Module 1: Agent System (`internal/agent/`)
- **Responsibility**: Actor-model agent runtime — each agent is a long-lived goroutine with a FIFO (+ priority for L1) mailbox. Processes user asks sequentially via LLM tool-calling loops. Supports async delegation (L1→L2) with continuation-passing, streaming event emission, confirmation pipelines, and circuit breakers.
- **Key Files**: `agent.go`, `stream.go`, `async_turn.go`, `supervisor.go`, `factory.go`, `registry.go`
- **Dependencies**: llm, tools, skill, ctxwin, runtime(Stack)

#### Module 2: LLM System (`internal/llm/` + `internal/llm/deepseek/`)
- **Responsibility**: Provider-agnostic LLM protocol layer. Defines shared types (ToolCall, Event, Usage, APIError) and a streaming-first contract. The DeepSeek sub-package implements HTTP/SSE transport with retry logic, SSE parsing, and chunk-to-event normalization.
- **Key Files**: `types.go`, `deepseek/client.go`, `deepseek/sse.go`, `retry.go`
- **Dependencies**: None (zero external dependencies except stdlib)

#### Module 3: Tool System (`internal/tools/`)
- **Responsibility**: Minimal executable primitives. Each tool maps 1:1 to an LLM function-calling entry. Supports confirmation (Confirmable), async execution (AsyncTool), and platform-specific execution (exec_unix.go/exec_windows.go). Includes Bash, Read, Write/Edit, Grep, Glob, WebFetch, WebSearch, Delegate (multi-agent delegation).
- **Key Files**: `tool.go`, `registry.go`, `delegate.go`, `shell_exec.go`, `config.go`
- **Dependencies**: iface (for Locatable interface)

#### Module 4: Skill System (`internal/skill/`)
- **Responsibility**: Higher-level abstraction above tools. Skills are reusable task recipes (inline or fork execution). Supports SKILL.md file loading with YAML frontmatter, preprocessing ($ARGUMENTS, shell expansion, @file references), and layered scopes (plugin → user → project).
- **Key Files**: `skill.go`, `skill_tool.go`, `fork.go`, `skill_md.go`, `preprocess.go`
- **Dependencies**: tools

#### Module 5: Config System (`internal/config/`)
- **Responsibility**: Global runtime control plane. Layered TOML loading (defaults → settings.toml → settings.local.toml), fsnotify-based hot-reload with 200ms debounce, type-safe generic Loader[T], and atomic saves via temp-file-plus-rename.
- **Key Files**: `schema.go`, `loader.go`, `service.go`, `defaults.go`, `merge_toml.go`
- **Dependencies**: fsnotify, go-toml

#### Module 6: Context Window (`internal/ctxwin/`)
- **Responsibility**: Token count calibration via tiktoken, dual-waterline compaction (soft/hard limits), FIFO sliding window, middle-out JSON truncation for tool outputs, and LLM-based compactor integration.
- **Key Files**: `ctxwin.go`
- **Dependencies**: llm, tiktoken-go

#### Module 7: Memory System (`internal/memory/` + `internal/permanent/`)
- **Responsibility**: Short-term memory via daily summaries (Markdown files, auto-loaded into system prompt), long-term permanent memory via embedding-based vector store (entries >7 days old get LLM-summarized and migrated).
- **Key Files**: `memory/memory.go`, `permanent/manager.go`
- **Dependencies**: embedding, vectorstore, sqlitedb

#### Module 8: Task Router (`internal/router/`)
- **Responsibility**: Intelligent task classification (L0-L3) with hybrid pattern matching + LLM semantic understanding. Routes to appropriate model tier: L0 (conversation/flash), L1 (simple/thinking), L2 (medium/pro), L3 (complex/pro-max). Supports sticky session logic.
- **Key Files**: `router.go`, classifier files
- **Dependencies**: config, ctxwin, llm

#### Module 9: Server & Session (`internal/server/` + `internal/session/`)
- **Responsibility**: REST + WebSocket API via chi router. Session management with single active session, inFlight CAS lock (atomic.Int32), context window binding, and idle reaper. WebSocket Hub broadcasts real-time state changes to all connected clients.
- **Key Files**: `server/server.go`, `server/hub.go`, `server/session_handlers.go`, `session/session.go`
- **Dependencies**: agent, ctxwin, config, timeline, memory

#### Module 10: Runtime Container (`internal/runtime/`)
- **Responsibility**: Single shared dependency container (Stack) initialized once by Build(). Wires all subsystems together: LLM client, prompt system, agent registry + factory, L2 supervisors, MCP manager, memory manager, task router, SQLite DB, permanent memory scheduler.
- **Key Files**: `stack.go`, `build.go`
- **Dependencies**: All other internal packages

#### Module 11: MCP Integration (`internal/mcp/` + `internal/mcp/lsp/`)
- **Responsibility**: Model Context Protocol server manager. Supports external MCP servers (mcp.json config), virtual (in-process) MCP servers, and built-in LSP MCP server. Lazy connection, tool enumeration, and graceful shutdown.
- **Key Files**: `mcp/manager.go`, `mcp/loader.go`, `mcp/lsp/manager.go`
- **Dependencies**: tools, logger, mcp-go

#### Module 12: Frontend (Web UI)
- **Responsibility**: React 19 SPA with TailwindCSS v4, Zustand state management, real-time WebSocket updates, responsive design (mobile bottom nav + desktop sidebar), agent chat interface with streaming, file browser, settings management (config, skills, MCP, teams, projects, proxies), cron task management.
- **Key Files**: `App.tsx`, `AgentDetailPage.tsx`, `AgentListPage.tsx`, `FilesPage.tsx`, `SettingsLayout.tsx`
- **Dependencies**: React 19, Vite 7, TailwindCSS v4, Zustand, react-router-dom v7

### 3.3 Data Flow

```
User Input (Web UI / QQ Bot / CLI)
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│  Server (chi router)                                        │
│  POST /api/session/ask → Session Manager                    │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  Task Router (L0-L3 classification)                         │
│  Pattern-based fast track + LLM semantic fallback           │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  L1 Core Agent (actor model, priority mailbox)              │
│  System Prompt → LLM Request → Stream Loop                  │
│    ├─ Tool Call → Execute (sync)                            │
│    ├─ Tool Call → Delegate (async L1→L2)                    │
│    └─ Content Delta → WebSocket Stream → UI                 │
└──────────────────────────┬──────────────────────────────────┘
                           │ (async delegation)
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  L2 Supervisor → Spawns L2 Agent → Mailbox Execution        │
│    └─ L2 Agent may delegate (sync L2→L3)                    │
│       └─ L3 Agent executes sub-task                         │
│  Result sent back to L1 via mailbox continuation            │
└─────────────────────────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  Context Window (tiktoken tokens, dual-waterline compaction) │
│  History appended to Timeline (JSONL event sourcing)        │
│  Memory extracted → Short-term (daily) + Permanent (>7 days)│
└─────────────────────────────────────────────────────────────┘
```

### 3.4 API Design
- **Protocol**: REST + WebSocket
- **Auth**: HTTP Basic Auth (token-based, in-memory only, 24h expiry)
- **Main Endpoints**:
  - `GET /healthz` — Health check
  - `GET /ws` — WebSocket for real-time state updates
  - `POST /api/session/ask` — Send user message to agent
  - `POST /api/session/cancel` — Cancel current task
  - `POST /api/session/clear` — Clear session
  - `GET /api/agents` — List agents
  - `GET /api/agents/{id}` — Get agent details
  - `GET /api/config` — Get configuration (JSON)
  - `GET /api/config/toml` — Get configuration (TOML)
  - `GET /api/skills` — List skills
  - `GET /api/files/content?path=<path>` — Read file contents
  - `GET /api/mcp` — Get MCP configuration
  - `GET|POST|PUT|DELETE /api/proxy/` — Reverse proxy management
  - `GET|POST|PUT|DELETE /api/cron/` — Scheduled task management
  - `GET|POST|PUT|DELETE /api/teams/` — Team CRUD
  - `GET|POST|PUT|DELETE /api/projects/` — Project CRUD
  - `GET|POST|PUT|DELETE /api/config/providers/` — Provider CRUD
  - `GET|POST|PUT|DELETE /api/config/models/` — Model CRUD

## 4. Key Design Patterns & Decisions

### 4.1 Architectural Patterns
- **Actor Model**: Each agent is an independent actor with a mailbox, processing jobs sequentially. Eliminates concurrent state mutation races without explicit locks.
- **Streaming-First Design**: Both LLM and Agent systems prioritize streaming APIs. Blocking APIs are wrappers over event accumulation, avoiding divergence between sync/stream paths.
- **Continuation-Passing Pattern**: Async delegation (L1→L2) uses mailbox-based continuation instead of thread blocking. The L1 agent yields, L2 executes independently, then posts completion back to L1's priority mailbox.
- **Functional Options**: Constructors use the `With*` variadic pattern for dependency injection (e.g., `WithTools`, `WithMailboxCap`, `WithSkills`, `WithAgentWorkDir`).
- **Event-Driven Architecture**: Typed event streams (`llm.Event → agent.AgentEvent`) form clear contract boundaries between agent/session/server layers, and between parent/child agents during delegation.
- **Factory Pattern**: `DefaultFactory` centralizes multi-system agent assembly (LLM client, tools, skills, prompt config).

### 4.2 Critical Design Decisions
1. **Single Binary with Embedded Web UI**: The Go binary embeds the built web dist via `//go:embed`. Self-contained deployment with zero external static file dependencies.
2. **DeepSeek Provider Only**: The LLM transport is DeepSeek-specific (SSE parsing, wire format). No OpenAI adapter layer — the project is built for DeepSeek.
3. **SQLite for Everything**: Shared SQLite connection used by vector store, todo/plan store, team store, scheduled tasks, and permanent memory. No external database required.
4. **Markdown-Defined Agents and Skills**: Agent templates and skills are plain Markdown with YAML frontmatter. Version-controllable, user-editable, hot-reloadable.
5. **Hierarchical Task Routing (L0-L3)**: Automatic complexity classification determines which model tier handles the request, enabling cost optimization (flash for simple, pro-max for complex).
6. **Config Hot-Reload**: fsnotify-based watching on settings.toml, mcp.json, agent templates, and skill files. Changes take effect without server restart.
7. **Tool Confirmation Pipeline**: Three-layer bypass: template-level (`permission: true`), global flag (`--bypass`), per-ask context. Sensitive tools block and emit confirmation requests to the UI.
8. **Auth Tokens in Memory Only**: Server restart invalidates all sessions. No persistent token storage. Hardcoded 24h expiry.

### 4.3 Concurrency Model
- **Agent Mailbox**: Each agent has a FIFO channel-based mailbox (or priority mailbox for L1). Only one job executes at a time per agent.
- **inFlight CAS Lock**: Session-level atomic.Int32 Compare-And-Swap ensures only one concurrent Ask per session.
- **Supervisor Lifecycle**: Orphan prevention — if parent L1 stops, the supervisor cascades termination to all running L2/L3 children.
- **Parallel Tool Execution**: Optional errgroup-based concurrent execution of multiple tool_calls in one LLM response (disabled by default, enabled via `WithParallelTools`).
- **Panic Recovery**: Agent goroutines catch panics via defer/recover to prevent crash propagation.

## 5. Dependencies Analysis

### 5.1 Core Dependencies
| Dependency | Purpose | Risk Level |
|-----------|---------|------------|
| go-chi/chi/v5 | HTTP routing | Low (stable, minimal) |
| spf13/cobra | CLI framework | Low (mature) |
| gorilla/websocket | WebSocket | Low (mature) |
| modernc.org/sqlite | Pure Go SQLite | Medium (pure Go reimplementation) |
| pkoukk/tiktoken-go | Token counting | Medium (third-party, not official) |
| mark3labs/mcp-go | MCP protocol | Medium (evolving spec) |
| pelletier/go-toml/v2 | TOML parsing | Low (mature) |
| fsnotify/fsnotify | File watching | Low (mature) |
| React 19 + Vite 7 | Frontend | Medium (cutting-edge) |
| TailwindCSS v4 | CSS framework | Medium (recent major version) |

### 5.2 External Services
| Service | Purpose | SLA/Terms |
|---------|---------|-----------|
| DeepSeek API | LLM inference (all agent levels) | DeepSeek API terms |
| QQ Bot API | QQ messaging integration | Tencent QQ Bot platform |

## 6. Configuration & Deployment

### 6.1 Configuration
- **Config File**: `~/.soloqueue/settings.toml` (TOML format)
- **MCP Config**: `~/.soloqueue/mcp.json`
- **Agent Templates**: `~/.soloqueue/agents/*.md` (YAML frontmatter + Markdown)
- **Skills**: `~/.soloqueue/skills/*.md`
- **Timeline Logs**: `~/.soloqueue/logs/timelines/`
- **Config Loading Order**: compiled defaults → `settings.toml` → `settings.local.toml`
- **Key Settings**: LLM providers (credentials, endpoints), model catalog (context windows), default model roles (expert/superior/universal/fast), tool limits, auth, QQ Bot, MCP servers

### 6.2 Build & Run
```bash
# Development server (two terminals)
go run ./cmd/soloqueue serve --port 8765    # Backend
cd web && pnpm install && pnpm dev           # Frontend (port 5173, proxies /api → 8765)

# Production build (single binary)
make build                                    # pnpm build + copy dist + go build
./soloqueue serve --port 57647

# Tests
go test ./...                                 # All Go tests
cd web && pnpm check && pnpm test            # Frontend typecheck + lint + tests
```

## 7. Testing Strategy

### 7.1 Test Framework
- **Go**: Standard `testing` package (no TestMain, no shared fixtures, self-contained per package)
- **Go Mock LLM**: `FakeLLM` in `internal/agent/llm.go` — scripted LLM stub for testing tool loops, streaming, reasoning
- **Frontend Unit**: Vitest 3.x with @testing-library/react
- **Frontend E2E**: Playwright 1.60

### 7.2 Test Coverage
- **Go**: Per-package test files exist for agent, memory, timeline, config, and frontend (stores/hooks)
- **Frontend**: Test files present for stores (agentStore, planStore, runtimeStore, toolsAndSkillsStore, mcpConfigStore), hooks (useAgents, useAgentConfig, useAgentProfile, useAgentStream, usePlans, useRuntime, useTeams, useToolsAndSkills, useMCPConfig), and components (FilesPage)
- **Platform-Specific**: Shell execution tests are platform-aware (unix vs windows build tags)

## 8. Known Issues & Technical Debt

- **DeepSeek-Specific Transport**: LLM layer is hard-coupled to DeepSeek's SSE format. Adding another provider requires implementing a new transport from scratch.
- **QQ Bot `expires_in` as String**: The QQ API returns `expires_in` as a string (not integer), requiring special handling in the access token response parser.
- **No Refresh Token Mechanism**: Web UI auth token stored in localStorage without refresh support — HTTP 401 triggers auto-logout.
- **No Database Migrations**: SQLite schema is created inline without a migration system.
- **Frontend Package Manager**: Strictly requires pnpm (not npm/yarn compatible for lockfile).

## 9. Development Guidelines

### 9.1 Code Standards
- **Go**: Standard idioms, functional options pattern, categorized logging (`CatApp`, `CatActor`, `CatLLM`, `CatTool`, `CatMCP`, `CatConfig`, `CatHTTP`, `CatMessages`)
- **Frontend**: TypeScript strict mode, ESLint + Prettier, component colocation of tests
- **Test Conventions**: No TestMain, no shared fixtures, self-contained per package
- **Agent State Machine**: Idle → Processing → (Idle | Stopping → Stopped). Start restarts after Stop.

### 9.2 Git Workflow
- **Branch Strategy**: Main-branch development (single developer)
- **Commit Conventions**: Conventional commits style (feat:, refactor:, fix:)
- **Co-author**: Commits include `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
