# AGENTS.md

Short tactical guidance for OpenCode sessions. See `CLAUDE.md` for detailed architecture.

## Quick commands

```bash
# Full build (web + Go, one-shot)
make build

# Go-only rebuild (after web dist already exists)
make build-go

# Build web UI only
make build-web

# Clean build artifacts
make clean

# Go
go test ./...                           # all Go tests
go test ./internal/timeline/...         # single package
go test -run TestReplayInto ./internal/timeline/...  # single test

# Web UI (React 19 + TypeScript + Vite + TailwindCSS v4)
cd web && pnpm install && pnpm dev       # dev server (port 5173, proxies /api + /ws to Go)
cd web && pnpm build                     # production build (tsc + vite)
cd web && pnpm check                     # full CI check (tsc --noEmit + eslint + prettier)
cd web && pnpm lint                      # eslint only
```

**Important**: the web project uses `pnpm` (lockfile is `pnpm-lock.yaml`), NOT npm.
The Go binary embeds `web/dist/` via `//go:embed` — run `make build-web` before `go build` if dist is missing.

## Go module

Module path: `github.com/xiaobaitu/soloqueue`. Go 1.25.8.

## Binary modes

The same cobra binary runs in two modes:

- `soloqueue` (no subcommand) — TUI mode. HTTP server starts on random port by default (`--port` / `-p` to pin).
- `soloqueue serve --port 8765` — headless REST + WebSocket server. Binds `127.0.0.1` by default (`--host` to change). Random port by default (`--port 0`).

Other subcommands: `soloqueue version`, `soloqueue cleanup` (remove Docker sandbox containers).

CLI flags: `--bypass` (skip tool confirmations for all agents, both modes), `--verbose` / `-v` (print logs to stderr, serve only).

Both modes share the same `runtime.Stack` (dependency container), initialized by `runtime.Build()`.

## Directory map

```
cmd/soloqueue/      cobra entrypoint (main.go + cli/ subpackage)
internal/agent/     Agent actor model (LLM + tool loop + mailbox)
internal/config/    hot-reload config (fsnotify)
internal/ctxwin/    context window (tiktoken-based, dual-waterline compaction)
internal/session/   session manager (single active session, inFlight CAS lock)
internal/timeline/  append-only JSONL event sourcing
internal/server/    REST + WebSocket HTTP router (chi/v5)
internal/tools/     Tool implementations (Bash, Edit, Write, Read, Grep, etc.)
internal/qqbot/     QQ official bot WebSocket integration
internal/skill/     Claude Code-compatible skill system
internal/runtime/   shared runtime.Stack — built once per process
web/                React web UI, served separately by Vite dev server
```

## Config and data

- Work directory: `~/.soloqueue/` (from `config.DefaultWorkDir()`)
- Agent templates loaded from `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown)
- Timeline JSONL files in `~/.soloqueue/logs/timelines/`
- QQ bot logs in `~/.soloqueue/logs/qqbot/` (via `logger.WithLogSubdir("qqbot")`)
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

## Critical invariants

1. **System prompts must NOT be written to timeline.** The session builder pushes the system prompt with `replayMode=true` to prevent contamination.
2. **`filterCompletePairs`** removes orphaned tool_calls from LLM payloads to prevent HTTP 400 errors. If you modify the context window assembly, check this.
3. **`inFlight atomic.Int32` CAS lock** in Session ensures only one concurrent Ask per session. Returns `ErrSessionBusy` on conflict — no external mutex needed.

## Test conventions

- No `TestMain`, no shared fixtures. Tests are self-contained per package.
- Some packages have `*_test.go` files using package `<pkg>_test` (external test packages).
- `tui/testhelpers_test.go` provides test utilities for TUI tests.

## Key patterns

- **Functional options**: constructors use variadic `With*` pattern.
- **Logger categories**: `logger.CatApp`, `logger.CatActor`, `logger.CatMessages`, `logger.CatConfig`, etc.
- **Config hot-reload**: callers read latest via `cfg.Get()`; config uses fsnotify under the hood.
- **QQ returns `expires_in` as a string** (not int) in the access token response. Do not parse as integer.
- **Web UI is embedded** — the Go binary serves the web UI via `//go:embed`. Run `make build-web` before `make build-go` if dist is missing.
- **Agent bypass**: three layers — agent-level (`permission: true` in template), global (`--bypass`), ask-level (`agent.WithBypassConfirmCtx(ctx)`).
