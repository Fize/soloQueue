# AGENTS.md

Tactical guidance for AI coding agents working in this repository.

## Build

### Linux / macOS (make)

```bash
make build            # Build Go binary with portal embedded (default)
make build-web        # Build lightweight web portal — copies into internal/server/dist/
make build-desktop    # Build Electron desktop web UI
make build-all        # Build Go binary AND desktop web UI
make build-go         # Build Go binary only (assumes portal dist already exists)
make build-win        # Cross-compile Go for Windows
make package-desktop  # Package Electron app (PLATFORM=mac|win|linux)
make clean            # Remove all build artifacts
```

The Go binary embeds `internal/server/dist/` via `//go:embed`. `make build-web` also copies `skills/` into dist so embedded skills are available at runtime.

### Windows (PowerShell)

```powershell
./scripts/build.ps1              # Build Go binary with portal embedded
./scripts/build.ps1 build-web    # Build web portal
./scripts/build.ps1 build-go     # Build Go binary only
./scripts/build.ps1 clean        # Remove all build artifacts
```

## Go tests

```bash
go test ./...                          # all packages
go test ./internal/timeline/...        # single package
go test -run TestReplayInto ./internal/timeline/...  # single test
```

Use `rtk go test ./...` for compact output (hides pass lines, shows only failures).

## Running locally

```bash
go run ./cmd/soloqueue serve --port 8765    # start server (separate terminal)

# Desktop UI (Electron + React) — separate terminal:
cd desktop && pnpm install && pnpm dev

# Lightweight portal (embedded in Go binary):
cd portal && pnpm install && pnpm dev
```

Open `http://localhost:5173`. The desktop Vite dev server proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

## Go module & binary

`github.com/xiaobaitu/soloqueue`. Go 1.25.8.

`soloqueue serve` is the primary mode. Default port 57647; dev convention uses `--port 8765` to match Vite proxy. Binds `127.0.0.1`.
Other subcommands: `version`.
`serve` flags: `--bypass` (skip tool confirmations), `--verbose` / `-v` (logs to stderr).

## Config & data

- Work directory: `~/.soloqueue/` (`config.DefaultWorkDir()`)
- Agent templates: `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown; hot-reload via fsnotify)
- Config: `~/.soloqueue/settings.toml` (TOML; hot-reload via fsnotify)
- MCP servers: `~/.soloqueue/mcp.json` (hot-reload)
- Skills: `~/.soloqueue/skills/*.md` (hot-reload)
- Timeline JSONL: `~/.soloqueue/logs/timelines/`
- Task level persistence: `logs/timelines/l2-<id>/level` — stores last routing level so restarted sessions preserve task context (prevents "这个功能做完了吗" being misclassified as L0)
- Shared SQLite: `~/.soloqueue/permanent_memory/entries.db`
- Config loading order (low→high priority): compiled defaults → `settings.toml` → `settings.local.toml`
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

## Architecture

### L0–L3 hierarchical routing (`internal/router/`)

The router classifies each user prompt and selects the appropriate model:

| Level | Use case                              |
| ----- | ------------------------------------- |
| L0    | Conversation, simple queries          |
| L1    | Single-file tasks, quick edits        |
| L2    | Multi-file changes, medium complexity |
| L3    | Complex refactoring, large-scale work |

Agents at each level use different system prompts and tool sets. The router output determines which model config to use.

**Hybrid sticky logic**: When `priorLevel` is set (from a previous routing decision), the classifier prevents accidental downgrade — e.g., a follow-up question about a complex L2 task won't get classified as L0 unless confidence ≥ 96. This protection is only active when `priorLevel != LevelUnknown`, which requires `lastLevel` to be set (now persisted to the `level` file in the timeline directory).

### Dependency container (`internal/runtime/`)

`Stack` is built once at startup and holds all shared dependencies (LLM client, tools config, agent registry, skill registry, MCP managers, tokenizer, compactor, shared SQLite DB). Hot-reload replaces the LLM client and tools config via `sync.RWMutex`.

### Directory layout

```
cmd/soloqueue/          cobra entrypoint (main.go + cli/)
internal/agent/         actor-model agent (LLM + tool loop + mailbox)
internal/compactor/     LLM-based context compression
internal/config/        hot-reload config (TOML schema + settings)
internal/ctxwin/        context window (tiktoken, dual-waterline compaction)
internal/iface/         shared interfaces (breaks agent↔tools cycle)
internal/llm/           provider-agnostic LLM protocol + DeepSeek transport
internal/logger/        structured logging (file + console)
internal/mcp/           MCP server manager + config
internal/memory/        short-term memory manager (daily .md files)
internal/memoryengine/  long-term memory: BM25 (FTS5) + KG + optional vector
internal/prompt/        prompt assembly, templates, team management
internal/qqbot/         QQ official bot WebSocket integration
internal/router/        L0-L3 task classification & model routing
internal/runtime/       shared dependency container (Stack, built once)
internal/server/        REST + WebSocket HTTP router (chi/v5)
internal/session/       session manager (single active, inFlight atomic CAS)
internal/simulation/    Generative Agents simulation engine
internal/skill/         Claude Code-compatible skill system
internal/sqlitedb/      shared SQLite wrapper + schema migrations
internal/timeline/      append-only JSONL event sourcing
internal/tools/         Tool implementations + Sandbox execution backend
desktop/                Electron app (React 19 + TypeScript + Vite + TailwindCSS v4 + Zustand)
portal/                 Lightweight web portal (React 19 + Vite + TailwindCSS v4, embedded in Go binary)
```

### Simulation engine (`internal/simulation/`)

Seed text → LLM extraction → persona generation → GA agent loop (Perceive→Retrieve→Decide→Execute→Reflect per tick).

Key gotchas:
- **`SuggestedAgent.Goals`** extracted by Phase 2 are character-specific objectives, NOT abstract positions. In `buildPersonas`, seed-extracted `Goals` **override** the persona-gen LLM's goals.
- **Goal transitions**: `SeedLifecycleEvent` with `type: "goal_transition"` carries `NewGoals []string`. `handleGoalTransition` updates the SimAgent's pointer AND `lm.allPersonas` (for mid-simulation spawns).
- `allPersonas` is passed by **value** to each `GAAgentLoop`; other agents don't see each other's goals — only name/role/bio.
- Lifecycle event types: `agent_spawn`, `agent_death`, `goal_transition`, `simulation_end`. Scheduler runs every 2 seconds.
- `FakeLLM` (from `internal/agent/llm.go`) is used in simulation tests to avoid real API calls. No `TestMain` or shared fixtures.

### Memory engine (`internal/memoryengine/`)

Config-driven hybrid search: BM25 (SQLite FTS5) + Knowledge Graph (in-process, pure Go) + optional vector (OpenAI embeddings).

- Config: `[embedding] provider = "none"` (default, zero deps) or `"openai"` (remote).
- `Engine` is the single entry point, constructed in `runtime.buildMemoryEngine()`, shared via `Stack.MemoryEngine`.
- Embedder is injected — returns `nil` for `"none"`. All code paths check `nil` before using.
- Vector search is optional — `VectorSearcher.Enabled()` returns false when embedder or vecStore is nil.
- RRF fusion (k=60) deduplicates by `content_hash`; same memory found by multiple pipelines gets a combined score.
- Salience uses Ebbinghaus decay computed at query-time — no background job needed.

## Critical invariants

1. **System prompts must NOT be written to timeline.** The session builder pushes them with `replayMode=true`.
2. **`filterCompletePairs`** removes orphaned tool_calls from LLM payloads to prevent HTTP 400 errors.
3. **`inFlight atomic.Int32` CAS lock** in Session ensures only one concurrent Ask per session. Returns `ErrSessionBusy`.
4. **`runJob` goroutine catches panics** via `defer/recover`. Agent's `RunCommand` with `Cancel` not nil must use `exec.CommandContext`.
5. **Auth tokens are in-memory only** — server restart invalidates all sessions. 24h expiry, no idle timeout.
6. **Web UI auth token** stored in `localStorage` under `soloqueue_token`. No refresh — 401 triggers auto-logout.

## Key patterns

- **Functional options**: `WithTools`, `WithMailboxCap`, `WithSkills`, `WithTableName`, etc.
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, `logger.CatTool`, `logger.CatLLM`, `logger.CatMCP`.
- **Config hot-reload**: callers read latest via `cfg.Get()`; fsnotify under the hood.
- **Agent state machine**: `Idle → Processing → (Idle | Stopping → Stopped)`.
- **Agent bypass** — three layers: template (`permission: true`), global (`--bypass`), per-ask (`agent.WithBypassConfirmCtx(ctx)`).
- **FakeLLM** (`internal/agent/llm.go`): scripted LLM stub for testing — use instead of mocking across packages.
- **Platform-specific RunCommand**: `exec_unix.go` (`/bin/sh -c`, Setpgid+SIGKILL) vs `exec_windows.go`. Build tags handle selection.
- **Environment info in prompts**: `internal/prompt/environment.go` injects `<environment>` into system prompts with OS, arch, shell, working directory, explore directory.
- **QQ returns `expires_in` as a string** — do not parse as integer.
- **Test conventions**: no `TestMain` or shared fixtures. Self-contained per package.
- **Package manager**: `pnpm` (NOT npm). Lockfile: `pnpm-lock.yaml`.
- **Frontend**: State management via Zustand stores (`desktop/src/stores/`). Real-time updates via WebSocket. `@/` path alias maps to `src/`.
