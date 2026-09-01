# AGENTS.md

Tactical guidance for AI coding agents working in this repository.

> For detailed architecture docs, browse [docs/](docs/README.md).

## Build

### Frontend build approvals

The Web Console and Status UI declare pnpm's `allowBuilds.esbuild: true` in
their local `pnpm-workspace.yaml` files. No interactive approval step is
required before running the Makefile targets.

### Linux / macOS (make)

```bash
make build            # Build both browser UIs and Go binary
make build-web        # Build the full Web Console
make build-status     # Build the read-only Status UI
make build-assets     # Build both UIs and embed Skills
make build-go         # Build Go binary only (assumes assets already exist)
make build-win        # Cross-compile Go for Windows
make start            # Start backend and both browser UIs on one port
make clean            # Remove all build artifacts
```

The Go binary embeds independent `internal/assets/dist/web`, `status`, and `skills` bundles. Always run `make build-assets` (or `make build`) before a production build.

### Windows (PowerShell)

```powershell
./scripts/build.ps1              # Build browser assets and Go binary
./scripts/build.ps1 build-web    # Build Web Console
./scripts/build.ps1 build-status # Build Status UI
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

### Frontend lint & tests

```bash
cd web && pnpm lint                # ESLint
cd web && pnpm test                # Vitest
cd web && pnpm test:watch          # Vitest watch mode
cd web && pnpm format              # Prettier
```

The status-ui has no lint script configured.
No Go linter (golangci-lint, go vet) is wired into the Makefile.

## Running locally

```bash
go run ./cmd/soloqueue serve --port 8765    # backend + Status UI

# Full Web Console (separate terminal):
cd web && pnpm install && pnpm dev

# Read-only Status UI (separate terminal):
cd status-ui && pnpm install && pnpm dev
```

Open the Web Console Vite dev server at `http://localhost:5173`; it proxies `/api` and `/ws` to the backend.

**Test setup**: Vitest uses `@` alias → `src/`, jsdom environment, setup file at `src/test-setup.ts`, test files match `src/**/*.test.{ts,tsx}`.

## Go module & binary

`github.com/xiaobaitu/soloqueue`. Go 1.25.8.

`soloqueue start` is the combined browser mode. `serve` defaults to port 57647 and `web` to 57648. All bind `127.0.0.1` by default.
Other subcommands: `version`.
`serve` flags: `--verbose` / `-v` (logs to stderr).

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

### Task routing (`internal/router/` & `internal/tasktype/`)

The router classifies each user prompt by work nature (not difficulty) to select the appropriate model configuration:

| TaskType | Use case |
| -------- | -------- |
| `general` | Conversation, writing, translation, summarizing |
| `engineering` | Code, debugging, testing, API, deployment |
| `research` | Web search, documentation lookup, current info |

The classifier uses Local FastTrack rules (code blocks, tracebacks, commands) first, falling back to a lightweight LLM classifier when ambiguous. `priorLevel` preserves session context for follow-up prompts.

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
internal/router/        task classification & model routing (TaskType: general/engineering/research)
internal/runtime/       shared dependency container (Stack, built once)
internal/server/        REST + WebSocket HTTP router (chi/v5)
internal/session/       session manager (single active, inFlight atomic CAS)
internal/simulation/    Generative Agents simulation engine
internal/team/          auto-reload for LLM-written agent/group files
internal/team/store/    filesystem-backed team & agent persistence
web/                    Full browser Web Console (React 19 + TypeScript + Vite + TailwindCSS v4 + Zustand)
status-ui/              Independent read-only backend status page
skills/                 Bundled skill definitions, copied into embedded dist at build time
```

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
5. **HTTP boundary** — production commands bind only to `127.0.0.1`; external authentication, TLS, CORS, and public access belong to a user-managed ingress.

## Key patterns

- **Functional options**: `WithTools`, `WithMailboxCap`, `WithSkills`, `WithTableName`, etc.
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, `logger.CatTool`, `logger.CatLLM`, `logger.CatMCP`.
- **Config hot-reload**: callers read latest via `cfg.Get()`; fsnotify under the hood.
- **Agent state machine**: `Idle → Processing → (Idle | Stopping → Stopped)`.
- **FakeLLM** (`internal/agent/llm.go`): scripted LLM stub for testing — use instead of mocking across packages.
- **Platform-specific RunCommand**: `exec_unix.go` (`/bin/sh -c`, Setpgid+SIGKILL) vs `exec_windows.go`. Build tags handle selection.
- **Environment info in prompts**: `internal/prompt/environment.go` injects `<environment>` into system prompts with OS, arch, shell, working directory, explore directory.
- **QQ returns `expires_in` as a string** — do not parse as integer.
- **Test conventions**: no `TestMain` or shared fixtures. Self-contained per package.
- **Package manager**: `pnpm` (NOT npm). Lockfile: `pnpm-lock.yaml`.
- **Frontend**: State management via Zustand stores (`web/src/stores/`). Real-time updates via WebSocket. `@/` path alias maps to `src/`.
- **TelemetryClient** (`internal/telemetry/`): wraps `agent.LLMClient` to log token usage to SQLite on every Chat/ChatStream call.

## Known discrepancies

- The full browser console lives in `web/`; the independent status page lives in `status-ui/`.

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
