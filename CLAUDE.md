# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & development

```bash
make build        # pnpm build + copy dist + copy skills → internal/server/dist → go build
make build-go     # go build only (needs internal/server/dist already in place)
make build-web    # pnpm build + cp dist + cp skills to internal/server/dist
make clean        # rm soloqueue web/dist internal/server/dist
```

The Go binary embeds `internal/server/dist/` via `//go:embed`. Run `make build-web` before `go build` if dist is missing. `make build-web` also copies `skills/` into the dist so embedded skills are available at runtime.

## Testing

```bash
go test ./...                          # all packages
go test ./internal/timeline/...        # single package
go test -run TestReplayInto ./internal/timeline/...  # single test

cd web && pnpm check                   # tsc --noEmit + eslint + prettier
cd web && pnpm lint                    # eslint only
cd web && pnpm test                    # vitest
cd web && pnpm test:e2e                # playwright
```

RTK (`rtk go test ./...`) produces compact output — only failures are shown.

## Running locally

```bash
go run ./cmd/soloqueue serve --port 8765    # start server
cd web && pnpm install && pnpm dev          # web UI (separate terminal, port 5173)
```

Open `http://localhost:5173`. Vite proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

Default server port: 57647. Bind address: `127.0.0.1`. Subcommands: `serve`, `version`. Serve flags: `--bypass` (skip tool confirmations), `--verbose` / `-v` (logs to stderr).

## Tech stack

- **Backend**: Go 1.25.8, `github.com/xiaobaitu/soloqueue`
- **Frontend**: React 19 + TypeScript + Vite + TailwindCSS v4
- **Package manager**: `pnpm` (NOT npm). Lockfile: `pnpm-lock.yaml`
- **Database**: SQLite (`modernc.org/sqlite`, pure Go)
- **HTTP router**: `go-chi/chi/v5`
- **CLI**: `spf13/cobra`
- **LLM protocol**: Provider-agnostic abstraction + DeepSeek transport

## Architecture

### Hierarchical multi-agent system (L0–L3)

SoloQueue uses a 4-tier task classification and routing system:

| Level | Role      | Use case                              | Thinking mode |
| ----- | --------- | ------------------------------------- | ------------- |
| L0    | fast      | Conversation, simple queries          | disabled      |
| L1    | universal | Single-file tasks, quick edits        | high          |
| L2    | superior  | Multi-file changes, medium complexity | high          |
| L3    | expert    | Complex refactoring, large-scale work | max           |

The `internal/router/` package classifies each user prompt, then selects the appropriate model from config. Agents at each level use different system prompts and tool sets.

### Agent model (actor-model)

`internal/agent/` implements an actor-model agent: each agent has a mailbox, processes one Ask at a time, and runs an LLM tool loop. State machine: `Idle → Processing → (Idle | Stopping → Stopped)`. Supervisors manage child agents, reaping them on shutdown.

Key types: `Registry` (agent lookup), `DefaultFactory` (agent creation), `Supervisor` (lifecycle management), `RoutingClient` (provider-aware LLM dispatch).

### Dependency container: runtime.Stack

`internal/runtime/` provides `Stack` — built once at startup, holds all shared dependencies (LLM client, tools config, agent registry, skill registry, MCP managers, tokenizer, compactor, shared SQLite DB, etc.). Hot-reload replaces the LLM client and tools config concurrency-safely via `sync.RWMutex`.

### Context window & compaction

`internal/ctxwin/` manages token budgets using tiktoken with a dual-waterline strategy. The `LLMCompactor` (`internal/compactor/`) performs LLM-based context compression when the window is full.

`internal/prompt/` assembles prompts per level, injects `<environment>` blocks (OS, arch, shell, working directory), and manages team/group configuration.

### Config & hot-reload

`~/.soloqueue/` is the work directory (`config.DefaultWorkDir()`). Config loading order (low→high priority): compiled defaults → `settings.toml` → `settings.local.toml`. Agent templates (`~/.soloqueue/agents/*.md`), MCP servers (`~/.soloqueue/mcp.json`), and skills (`~/.soloqueue/skills/*.md`) all hot-reload via fsnotify.

Data paths under `~/.soloqueue/`: timeline JSONL in `logs/timelines/`, shared SQLite in `permanent_memory/entries.db`. Git-ignored locally: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`.

### Memory engine

- **Short-term**: `internal/memory/` — conversation-scoped memory manager
- **Long-term**: `internal/memoryengine/` — config-driven hybrid search: BM25 (SQLite FTS5) + Knowledge Graph (in-process entity-relationship graph with PPR/BFS traversal) + optional vector search. Replaces the old `internal/permanent/` system. Configured via `[embedding] provider = "none" | "openai"` in settings.toml. Default "none" uses dual-hybrid BM25+KG with zero dependencies.
- **Embeddings** (`internal/memoryengine/embedding/`): Embedder interface with OpenAI (remote API) implementation.
- **Vector store** (`internal/memoryengine/vectorstore/`): SQLite-backed cosine similarity over BLOBs. Only active when embedding provider != "none".

### Shared interfaces

`internal/iface/` defines shared interfaces to break import cycles between `internal/agent/` and `internal/tools/`. All cross-package contracts live here.

### Skill system

`internal/skill/` implements a Claude Code-compatible skill system. Skills are Markdown files with YAML frontmatter, hot-reloaded from `~/.soloqueue/skills/*.md`. The skill registry is injected via `runtime.Stack`.

### Session management

`internal/session/` manages agent sessions with an `inFlight atomic.Int32` CAS lock — only one concurrent Ask per session. Returns `ErrSessionBusy` on conflict.

### Event sourcing

`internal/timeline/` provides append-only JSONL event sourcing. All agent interactions are recorded. System prompts are pushed with `replayMode=true` to prevent timeline contamination.

### Web UI

`web/` is a React 19 + TypeScript + Vite app with TailwindCSS v4. State management uses Zustand stores (`web/src/stores/`). Real-time updates via WebSocket.

## Critical invariants

1. **System prompts must NOT be written to timeline.** The session builder pushes them with `replayMode=true` to prevent contamination.
2. **`filterCompletePairs`** removes orphaned tool_calls from LLM payloads to prevent HTTP 400 errors. Check this when modifying context window assembly.
3. **`inFlight atomic.Int32` CAS lock** in Session ensures only one concurrent Ask per session. Returns `ErrSessionBusy` on conflict.
4. **`runJob` goroutine catches panics** via `defer/recover`. If an LLM panic occurs, the agent's `RunCommand` with `Cancel` not nil must be created by `exec.CommandContext` (Go 1.25 requirement).
5. **Auth tokens are in-memory only** — server restart invalidates all sessions. Hardcoded 24h expiry, no idle timeout.
6. **Web UI auth token** stored in `localStorage` under `soloqueue_token`. No refresh mechanism — 401 triggers auto-logout.

## Key patterns

- **Functional options**: constructors use variadic `With*` pattern (e.g., `WithTools`, `WithMailboxCap`, `WithSkills`).
- **Config hot-reload**: callers read latest via `cfg.Get()`; fsnotify under the hood.
- **Agent bypass** — three layers: template (`permission: true`), global (`--bypass`), per-ask (`agent.WithBypassConfirmCtx(ctx)`).
- **FakeLLM** (`internal/agent/llm.go`): scripted LLM stub for testing — use instead of mocking across packages.
- **Platform-specific RunCommand**: `exec_unix.go` (`/bin/sh -c`, Setpgid+SIGKILL) vs `exec_windows.go` (auto-detects powershell.exe/cmd.exe). Build tags handle selection.
- **Environment info in prompts**: `internal/prompt/environment.go` injects `<environment>` (L1) / `# Environment` (L2/L3) into system prompts with OS, arch, shell, working directory, explore directory.
- **Test conventions**: no `TestMain` or shared fixtures. Self-contained per package.
- **QQ bot**: `internal/qqbot/` — QQ access token response returns `expires_in` as a string (not int). Do not parse as integer.
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, `logger.CatTool`, `logger.CatLLM`, `logger.CatMCP`.
