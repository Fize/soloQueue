# AGENTS.md

Tactical guidance for AI coding agents working in this repository.

> For detailed architecture docs, browse [docs/](docs/README.md).

## Build

### Prerequisite: pnpm approve-builds

The `.npmrc` requires `onlyBuiltDependencies` for `electron` and `esbuild`. Before first `pnpm install`, run:

```bash
cd portal && pnpm approve-builds esbuild    # or: pnpm approve-builds
cd desktop && pnpm approve-builds           # approves both electron + esbuild
```

The Makefile does this automatically. Without it, `pnpm install` fails.

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

The Go binary embeds `internal/server/dist/` via `//go:embed`. `go run ./cmd/soloqueue serve` without a pre-built dist works but the portal will be blank. Always `make build-web` (or `make build`) first for a working UI.

### Windows (PowerShell)

```powershell
./scripts/build.ps1              # Build Go binary with portal embedded
./scripts/build.ps1 build-web    # Build web portal
./scripts/build.ps1 build-go     # Build Go binary only
./scripts/build.ps1 clean        # Remove all build artifacts
```

## Testing

### Go tests

```bash
go test ./...                          # all packages
go test ./internal/timeline/...        # single package
go test -run TestReplayInto ./internal/timeline/...  # single test
```

Use `rtk go test ./...` for compact output (hides pass lines, shows only failures).

Workflow tests require explicit cache control:
```bash
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/workflow/... -count=1
GOCACHE=/tmp/soloqueue-go-cache go test -race ./internal/workflow/... -count=1
```

### Frontend lint & tests

```bash
cd desktop && pnpm lint            # ESLint
cd desktop && pnpm test            # Vitest
cd desktop && pnpm test:watch      # Vitest watch mode
cd desktop && pnpm format          # Prettier
```

The portal has no lint or test scripts configured.
No Go linter (golangci-lint, go vet) is wired into the Makefile.

## Running locally

```bash
go run ./cmd/soloqueue serve --port 8765    # start server (separate terminal)

# Desktop UI (Electron + React) — separate terminal:
cd desktop && pnpm install && pnpm dev

# Lightweight portal (embedded in Go binary):
cd portal && pnpm install && pnpm dev
```

Open `http://localhost:5173`. The desktop Vite dev server proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

**Test setup**: Vitest uses `@` alias → `src/`, jsdom environment, setup file at `src/test-setup.ts`, test files match `src/**/*.test.{ts,tsx}` and `electron/**/*.test.mjs`.

## Go module & binary

`github.com/xiaobaitu/soloqueue`. Go 1.25.8.

`soloqueue serve` is the primary mode. Default port 57647; dev convention uses `--port 8765` to match Vite proxy. Binds `127.0.0.1`.
Other subcommands: `version`.
`serve` flags: `--bypass` (skip tool confirmations), `--verbose` / `-v` (logs to stderr).

## Config & data

- Work directory: `~/.soloqueue/` (`config.DefaultWorkDir()`)
- Agent templates: `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown; hot-reload via fsnotify)
- Config: `~/.soloqueue/settings.yaml` (YAML; hot-reload via fsnotify)
- MCP servers: `~/.soloqueue/mcp.json` (hot-reload)
- Skills: `~/.soloqueue/skills/*.md` (hot-reload)
- Timeline JSONL: `~/.soloqueue/logs/timelines/`
- Task level persistence: `logs/timelines/l2-<id>/level` — stores last routing level so restarted sessions preserve task context (prevents "这个功能做完了吗" being misclassified as L0)
- Shared SQLite: `~/.soloqueue/soloqueue.db`
- Config loading order (low→high priority): compiled defaults → `settings.yaml`
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

## Architecture

> **See also**: [docs/architecture.md](docs/architecture.md) for the full system architecture overview.

### CodeGraph (code intelligence)

This repo is indexed by CodeGraph (`.codegraph/` exists). Use before grep/read:

```bash
codegraph explore "<symbol names or question>"
```

Returns verbatim source of relevant symbols plus call paths in one call. Covers dynamic dispatch (callbacks, React re-render, JSX children) that grep can't follow.

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
internal/agenttools/    agent tools subsystem (native tools, mcp, lsp, skill)
internal/agenttools/mcp MCP server manager + config (includes lsp subpackage)
internal/agenttools/skill Claude Code-compatible skill system
internal/agenttools/tools native tool implementations + tool execution engine
internal/channel/       shared channel contracts plus QQ and WeChat implementations
internal/config/        hot-reload config (YAML schema + settings)
internal/cron/          cron-based scheduled task execution
internal/iface/         shared interfaces (breaks agent↔tools cycle)
internal/infra/         technical infrastructure (db, logger, telemetry, workdir)
internal/infra/db/      SQLite wrapper + schema migrations
internal/infra/logger/  structured logging + rotating file writer
internal/infra/telemetry LLM token usage tracking (wraps LLMClient)
internal/infra/workdir/ work directory helper
internal/llm/           provider-agnostic LLM protocol + DeepSeek transport
internal/memory/        memory & context subsystem (ctxwin, conversation, engine, timeline)
internal/memory/conversation/ short-term memory: LLM-driven conversation summaries
internal/memory/ctxwin/       context window (tiktoken, dual-waterline compaction)
internal/memory/engine/       long-term memory: BM25 (FTS5) + KG + optional vector
internal/memory/timeline/     append-only JSONL event sourcing
internal/prompt/        prompt assembly, templates, team management
internal/router/        L0-L3 task classification & model routing
internal/runtime/       shared dependency container (Stack, built once)
internal/server/        REST + WebSocket HTTP router (chi/v5)
internal/session/       session manager (single active, inFlight atomic CAS)
internal/simulation/    Generative Agents simulation engine
internal/team/          auto-reload for LLM-written agent/group files
internal/team/store/    filesystem-backed team & agent persistence
internal/workflow/      YAML DAG workflow engine (v1) with outcome routing + bounded loops
desktop/                Electron app (React 19 + TypeScript + Vite + TailwindCSS v4 + Zustand)
portal/                 Lightweight web portal (React 19 + Vite + TailwindCSS v4, embedded in Go binary)
skills/                 Bundled skill definitions, copied into embedded dist at build time
```

### Workflow engine (`internal/workflow/`)

YAML-defined DAG workflows with outcome-based routing and bounded loops. Each node is an agent task with input/output mapping. `Store` persists workflow state to SQLite. `Engine` executes DAG nodes, `Graph` resolves dependencies, and `Schema`/`Validate` handle definition parsing and validation.

### Simulation engine (`internal/simulation/`)

Seed text → LLM extraction → persona generation → GA agent loop (Perceive→Retrieve→Decide→Execute→Reflect per tick).

> **See also**: [docs/architecture.md](docs/architecture.md) for subsystem details.

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

### Short-term memory (`internal/conversationlog/`)

LLM-driven conversation summaries triggered on context window compaction. `Manager` coordinates summarization and persistence to daily `.md` files under the work directory. Shared via `Stack.MemoryManager`.

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
- **TelemetryClient** (`internal/telemetry/`): wraps `agent.LLMClient` to log token usage to SQLite on every Chat/ChatStream call.

## Known discrepancies

- `README.md` mentions `cd web` — this directory does not exist. Use `portal/` or `desktop/`.

## Cron notification limitations

Cron task results are delivered to channels (QQ/WeChat) via the session's active bridge
(`SendActiveMessage` for QQ, `SendText` for WeChat). This mechanism has several constraints:

1. **Requires agent config**: The agent's template must set `notify_channel` to `"qq"` or
   `"wechat"` (via YAML frontmatter or `~/.soloqueue/persona/roles/channels.yaml` for L1).
   Without this, no channel notification is sent.

2. **Requires recent user interaction**: The channel bridge only registers its sender after
   a user sends a message. If the server has just restarted or no user has messaged the bot
   recently, `SendViaChannel` will be a no-op.

3. **WeChat instability**: WeChat iLink API may not reliably deliver active messages.
   Consider QQ Bot for more consistent channel notifications.

4. **No guaranteed delivery**: If the target user's bridge context is expired or the channel
   API returns an error, the notification is silently dropped. Always check the Web UI for
   definitive task results.
