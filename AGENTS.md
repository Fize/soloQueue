# AGENTS.md

Short tactical guidance for OpenCode sessions.

## Build

```bash
make build        # pnpm build + cp dist + cp skills → internal/server/dist → go build
make build-go     # go build only (needs internal/server/dist already in place)
make build-web    # pnpm build + cp dist + cp skills to internal/server/dist
make clean        # rm soloqueue web/dist internal/server/dist
```

The Go binary embeds `internal/server/dist/` via `//go:embed`. Run `make build-web` before `go build` if dist is missing. `make build-web` also copies `skills/` into the dist so embedded skills are available at runtime.

## Go tests

```bash
go test ./...                     # all packages
go test ./internal/timeline/...   # single package
go test -run TestReplayInto ./internal/timeline/...  # single test
```

Use `rtk go test ./...` for compact output (hides pass lines, shows only failures).

RTK is also used at startup to compress Bash tool outputs (60-90% token reduction). Automatically detected if `rtk` is on PATH.

## Web UI (React 19 + TypeScript + Vite + TailwindCSS v4)

```bash
cd web && pnpm install && pnpm dev       # dev server (port 5173)
cd web && pnpm build                     # tsc + vite build
cd web && pnpm check                     # tsc --noEmit + eslint + prettier
cd web && pnpm lint                      # eslint only
cd web && pnpm test                      # vitest
cd web && pnpm test:e2e                  # playwright e2e tests
```

**Important**: uses `pnpm`, NOT npm. Lockfile: `pnpm-lock.yaml`.

Dev server proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

## Go module

`github.com/xiaobaitu/soloqueue`. Go 1.25.8.

## Binary modes

`soloqueue serve` is the primary mode. Default port 57647 (0 = random); dev convention uses `--port 8765` to match Vite proxy. Binds `127.0.0.1` by default.
Other subcommands: `version`.
`serve` flags: `--bypass` (skip tool confirmations), `--verbose` / `-v` (logs to stderr).

## Config & data

- Work directory: `~/.soloqueue/` (from `config.DefaultWorkDir()`)
- Agent templates: `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown; hot-reload)
- Config: `~/.soloqueue/settings.toml` (TOML, hot-reload via fsnotify)
- MCP servers: `~/.soloqueue/mcp.json` (hot-reload)
- Skills: `~/.soloqueue/skills/*.md` (hot-reload)
- Timeline JSONL: `~/.soloqueue/logs/timelines/`
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

Config loading order (low→high priority): compiled defaults → `settings.toml` → `settings.local.toml`.

## Directory layout

```
cmd/soloqueue/      cobra entrypoint (main.go + cli/)
internal/agent/     actor-model agent (LLM + tool loop + mailbox)
internal/compactor/ LLM-based context compression engine
internal/config/    hot-reload config
internal/cron/      scheduled cron & timer tasks (SQLite-backed)
internal/ctxwin/    context window (tiktoken, dual-waterline compaction)
internal/iface/     shared interfaces (breaks agent↔tools cycle)
internal/llm/       provider-agnostic LLM protocol + DeepSeek transport
internal/logger/    structured logging (file + console)
internal/mcp/       MCP server manager + config + LSP integration
internal/memory/    short-term memory manager
internal/permanent/ embedding-based permanent memory + scheduler
internal/prompt/    prompt assembly, templates, team management, parser
internal/qqbot/     QQ official bot WebSocket integration
internal/router/    L0-L3 task classification & model routing
internal/runtime/   shared dependency container (Stack, built once)
internal/server/    REST + WebSocket HTTP router (chi/v5)
internal/session/   session manager (single active, inFlight atomic CAS)
internal/skill/     Claude Code-compatible skill system
internal/sqlitedb/  shared SQLite wrapper (used by vectorstore + todo)
internal/team/      team group reload
internal/timeline/  append-only JSONL event sourcing
internal/todo/      plan/task store (SQLite-backed)
internal/tools/     Tool implementations + Sandbox execution backend
internal/vectorstore/ SQLite-backed vector store for permanent memory
web/                React web UI (Vite dev server)
```

## Critical invariants

1. **System prompts must NOT be written to timeline.** The session builder pushes them with `replayMode=true` to prevent contamination.
2. **`filterCompletePairs`** removes orphaned tool_calls from LLM payloads to prevent HTTP 400 errors. Check this when modifying context window assembly.
3. **`inFlight atomic.Int32` CAS lock** in Session ensures only one concurrent Ask per session. Returns `ErrSessionBusy` on conflict.
4. **`runJob` goroutine catches panics** via `defer/recover`. If an LLM panic occurs, the agent's `RunCommand` with `Cancel` not nil must be created by `exec.CommandContext` (Go 1.25 requirement).
5. **Auth tokens are in-memory only** — server restart invalidates all sessions. Hardcoded 24h expiry, no idle timeout (removed).
6. **Web UI auth token** stored in `localStorage` under `soloqueue_token`. No refresh mechanism — 401 triggers auto-logout.

## Key patterns

- **Functional options**: constructors use variadic `With*` pattern (e.g., `WithTools`, `WithMailboxCap`, `WithSkills`).
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, `logger.CatTool`, `logger.CatLLM`, `logger.CatMCP`.
- **Config hot-reload**: callers read latest via `cfg.Get()`; fsnotify under the hood.
- **Agent state machine**: `Idle → Processing → (Idle | Stopping → Stopped)`. Start restarts after Stop.
- **QQ returns `expires_in` as a string** (not int) in the access token response. Do not parse as integer.
- **Agent bypass** — three layers: template (`permission: true`), global (`--bypass`), per-ask (`agent.WithBypassConfirmCtx(ctx)`).
- **Test conventions**: no `TestMain` or shared fixtures. Tests are self-contained per package.
- **FakeLLM** (`internal/agent/llm.go`): scripted LLM stub for testing tool loops, streaming, reasoning — use instead of mocking across packages.
- **Platform-specific RunCommand**: `internal/tools/exec_unix.go` (`/bin/sh -c`, Setpgid+SIGKILL) vs `exec_windows.go` (auto-detects powershell.exe/cmd.exe). Build tags handle selection.
- **Environment info in prompts**: `internal/prompt/environment.go` injects `<environment>` (L1) / `# Environment` (L2/L3) into all system prompts. Shows OS, arch, shell, working directory, explore directory, path separator. Uses `runtime.GOOS` / `runtime.GOARCH`.
- **Explore directory**: `internal/prompt/ExploreDir(workDir)` → `workDir/explore` (e.g., `~/.soloqueue/explore`). Used by all three layers via `{{EXPLORE_DIR}}` placeholder, replaced at prompt assembly time.
