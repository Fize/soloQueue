# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project overview

SoloQueue is an AI multi-agent collaboration platform with hierarchical task routing (L0–L3), built with Go (backend) and React 19 + TypeScript (frontend). The Go module is `github.com/xiaobaitu/soloqueue`.

## Build

```bash
make build            # Build Go binary with portal embedded (default)
make build-web        # Build lightweight web portal → copies into internal/server/dist/
make build-desktop    # Build Electron desktop web UI
make build-go         # Build Go binary only (assumes portal dist already exists)
make build-win        # Cross-compile Go for Windows
make package-desktop  # Package Electron app (PLATFORM=mac|win|linux)
make clean            # Remove all build artifacts
```

The Go binary embeds `internal/server/dist/` via `//go:embed`. Always run `make build-web` (or `make build`) first for a working UI — running `go run ./cmd/soloqueue serve` without it produces a blank portal.

**Prerequisite**: `.npmrc` requires `onlyBuiltDependencies` for `electron` and `esbuild`. The Makefile handles this automatically.

## Go tests

```bash
go test ./...                          # all packages
go test ./internal/timeline/...        # single package
go test -run TestReplayInto ./internal/timeline/...  # single test
```

Use `rtk go test ./...` for compact output (hides pass lines, shows only failures).

## Frontend lint & tests

```bash
cd desktop && pnpm lint              # ESLint
cd desktop && pnpm test              # Vitest
cd desktop && pnpm test:watch        # Vitest watch mode
```

The portal (`portal/`) has no lint or test scripts configured. No Go linter is wired into the Makefile.

## Running locally

```bash
go run ./cmd/soloqueue serve --port 8765    # start server (separate terminal)

# Desktop UI (Electron + React) — separate terminal:
cd desktop && pnpm install && pnpm dev

# Lightweight portal (embedded in Go binary):
cd portal && pnpm install && pnpm dev
```

Open `http://localhost:5173`. The desktop Vite dev server proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

## Go binary

`soloqueue serve` is the primary mode. Default port 57647; dev convention uses `--port 8765` to match Vite proxy. Binds `127.0.0.1`.

Subcommands: `serve`, `version`, `wechat`, `weixin`, `memory`.

`serve` flags: `--bypass` (skip tool confirmations), `--verbose` / `-v` (logs to stderr).

## Config & data

- Work directory: `~/.soloqueue/`
- Agent templates: `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown; hot-reload via fsnotify)
- Config: `~/.soloqueue/settings.yaml` (hot-reload via fsnotify)
- MCP servers: `~/.soloqueue/mcp.json` (hot-reload)
- Skills: `~/.soloqueue/skills/*.md` (hot-reload)
- Timeline JSONL: `~/.soloqueue/logs/timelines/`
- Shared SQLite: `~/.soloqueue/soloqueue.db`
- Config loading order (low→high priority): compiled defaults → `settings.yaml`
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

## Architecture

### L0–L3 hierarchical routing (`internal/router/`)

| Level | Model                      | Use case                             |
| ----- | -------------------------- | ------------------------------------ |
| L0    | deepseek-v4-flash          | Conversation, simple queries         |
| L1    | deepseek-v4-flash-thinking | Single-file tasks, quick edits       |
| L2    | deepseek-v4-pro            | Multi-file changes, medium complexity |
| L3    | deepseek-v4-pro-max        | Complex refactoring, large-scale work |

Router uses fast-track (pattern-based) rules as primary path, with LLM fallback on timeout. Hybrid sticky logic prevents accidental downgrade of follow-up messages.

### Dependency container (`internal/runtime/`)

`Stack` is built once at startup and holds all shared dependencies (LLM client, tools config, agent registry, skill registry, MCP managers, tokenizer, compactor, shared SQLite DB). Hot-reload replaces the LLM client and tools config via `sync.RWMutex`.

### Agent system (`internal/agent/`)

Actor-model agents process tasks sequentially via mailboxes, emitting typed event streams. Key patterns:

- **Async delegation**: L1 agents spawn L2 child agents asynchronously using continuation-passing over mailboxes. When the child completes, it submits a result back to the parent's mailbox.
- **Supervisor**: Manages child agent lifecycles — handles cascading termination on cancel/session stop.
- **Streaming-first**: Both agent and LLM systems prioritize streaming APIs; blocking APIs are wrappers over event accumulation.
- **Confirmation pipeline**: Tools requiring authorization block and emit `ToolNeedsConfirmEvent`, resuming when `Agent.Confirm` is called.
- **FakeLLM** (`internal/agent/llm.go`): Scripted LLM stub for testing — use this instead of mocking across packages.

### Workflow engine (`internal/workflow/`)

YAML-defined DAG workflows with outcome-based routing and bounded loops. Not a strict DAG — `loop: true` edges allow finite cycles. v1 supports outcome routing, fan-out/fan-in, bounded loops, and node-level retry. See `workflow.md` for design doc.

### Memory engine (`internal/memoryengine/`)

Hybrid search: BM25 (SQLite FTS5) + Knowledge Graph (in-process, pure Go) + optional vector (OpenAI embeddings). RRF fusion (k=60) deduplicates by `content_hash`. Salience uses Ebbinghaus decay computed at query-time.

### Simulation engine (`internal/simulation/`)

Seed text → LLM extraction → persona generation → Generative Agents loop (Perceive→Retrieve→Decide→Execute→Reflect per tick).

### Channel system (`internal/channel/`)

Transport-neutral messages with QQ Bot (WebSocket) and WeChat (iLink QR login, HTTP API, long polling) implementations.

## Critical invariants

1. **System prompts must NOT be written to timeline.** The session builder pushes them with `replayMode=true`.
2. **`filterCompletePairs`** removes orphaned `tool_calls` from LLM payloads to prevent HTTP 400 errors.
3. **`inFlight atomic.Int32` CAS lock** in Session ensures only one concurrent Ask per session. Returns `ErrSessionBusy`.
4. **`runJob` goroutine catches panics** via `defer/recover`. Agent's `RunCommand` with `Cancel` not nil must use `exec.CommandContext`.
5. **Auth tokens are in-memory only** — server restart invalidates all sessions. 24h expiry, no idle timeout.
6. **Web UI auth token** stored in `localStorage` under `soloqueue_token`. No refresh — 401 triggers auto-logout.

## Key patterns

- **Functional options**: `WithTools`, `WithMailboxCap`, `WithSkills`, `WithTableName`, etc.
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, `logger.CatTool`, `logger.CatLLM`, `logger.CatMCP`.
- **Config hot-reload**: callers read latest via `cfg.Get()`; fsnotify under the hood.
- **Agent state machine**: `Idle → Processing → (Idle | Stopping → Stopped)`.
- **Agent bypass**: three layers — template (`permission: true`), global (`--bypass`), per-ask (`agent.WithBypassConfirmCtx(ctx)`).
- **Platform-specific RunCommand**: `exec_unix.go` (`/bin/sh -c`, Setpgid+SIGKILL) vs `exec_windows.go`. Build tags handle selection.
- **Package manager**: `pnpm` (NOT npm). Lockfile: `pnpm-lock.yaml`.
- **Frontend**: State via Zustand stores (`desktop/src/stores/`). Real-time updates via WebSocket. `@/` path alias maps to `src/`.
- **Wiki**: Learnings, errors, and gotchas belong in `AGENTS.md` under a relevant section — not written back into `CLAUDE.md`.
- **README is stale**: `README.md` references `cd web` — there is no `web/` directory. Use `portal/` (lightweight, embedded in Go) or `desktop/` (Electron).
