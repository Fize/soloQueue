# AGENTS.md

Short tactical guidance for OpenCode sessions.

## Build

```bash
make build        # pnpm build + cp → internal/server/dist → go build
make build-go     # go build only (needs web/dist already in place)
make build-web    # pnpm build + cp dist to internal/server/dist
make clean        # rm soloqueue web/dist internal/server/dist
```

The Go binary embeds `internal/server/dist/` via `//go:embed`. Run `make build-web` before `go build` if dist is missing.

## Go tests

```bash
go test ./...                     # all packages
go test ./internal/timeline/...   # single package
go test -run TestReplayInto ./internal/timeline/...  # single test
```

Use `rtk go test ./...` for compact output (hides pass lines, shows only failures).

## Web UI (React 19 + TypeScript + Vite + TailwindCSS v4)

```bash
cd web && pnpm install && pnpm dev       # dev server (port 5173)
cd web && pnpm build                     # tsc + vite build
cd web && pnpm check                     # tsc --noEmit + eslint + prettier
cd web && pnpm lint                      # eslint only
cd web && pnpm test                      # vitest
```

**Important**: uses `pnpm`, NOT npm. Lockfile: `pnpm-lock.yaml`.

Dev server proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

## Go module

`github.com/xiaobaitu/soloqueue`. Go 1.25.8.

## Binary modes

`soloqueue serve --port 8765` is the primary mode. Binds `127.0.0.1` by default.
Other subcommands: `version`, `cleanup` (remove Docker sandbox containers).
Flags: `--bypass` (skip tool confirmations), `--verbose` / `-v` (logs to stderr).

## Config & data

- Work directory: `~/.soloqueue/` (from `config.DefaultWorkDir()`)
- Agent templates: `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown)
- Config: `~/.soloqueue/settings.toml` (TOML, hot-reload via fsnotify)
- Timeline JSONL: `~/.soloqueue/logs/timelines/`
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

Config loading order (low→high priority): compiled defaults → `settings.toml` → `settings.local.toml`.

## Directory layout

```
cmd/soloqueue/      cobra entrypoint (main.go + cli/)
internal/agent/     actor-model agent (LLM + tool loop + mailbox)
internal/config/    hot-reload config
internal/ctxwin/    context window (tiktoken, dual-waterline compaction)
internal/llm/       provider-agnostic LLM protocol + DeepSeek transport
internal/session/   session manager (single active, inFlight atomic CAS)
internal/timeline/  append-only JSONL event sourcing
internal/server/    REST + WebSocket HTTP router (chi/v5)
internal/tools/     Tool implementations (Bash, Edit, Write, Read, Grep, etc.)
internal/router/    L0-L3 task classification & model routing
internal/qqbot/     QQ official bot WebSocket integration
internal/skill/     Claude Code-compatible skill system
internal/runtime/   shared runtime.Stack (built once per process)
internal/sandbox/   LocalExecutor + DockerExecutor
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
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, `logger.CatTool`, `logger.CatLLM`.
- **Config hot-reload**: callers read latest via `cfg.Get()`; fsnotify under the hood.
- **QQ returns `expires_in` as a string** (not int) in the access token response. Do not parse as integer.
- **Agent bypass** — three layers: template (`permission: true`), global (`--bypass`), per-ask (`agent.WithBypassConfirmCtx(ctx)`).
- **Test conventions**: no `TestMain` or shared fixtures. Tests are self-contained per package.
