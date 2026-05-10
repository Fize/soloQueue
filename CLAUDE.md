# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build / Test / Lint

```bash
# Go (build, test, vet)
go build ./cmd/soloqueue                # build the binary
go test ./...                           # all tests
go test ./internal/timeline/...         # single package
go test -run TestReplayInto ./internal/timeline/...  # single test

# Web UI (React 19 + TypeScript + Vite + TailwindCSS v4)
cd web && npm install && npm run dev    # dev server
cd web && npm run build                 # production build
cd web && npm run lint                  # eslint
cd web && npm run check                 # full CI check (tsc + eslint + prettier)
```

## Two modes

The same binary runs in two modes via cobra:

- **TUI mode** (`soloqueue` with no subcommand) — interactive Bubble Tea terminal UI. Starts sandbox + session init in a background goroutine; the TUI renders a spinner until `SandboxInitMsg` arrives.
- **Serve mode** (`soloqueue serve --port 8765`) — headless REST + WebSocket server. Used for production/web UI backend.

Both modes share the same `runtime.Stack` built by `runtime.Build()`, which initializes the LLM client, prompt system, agent registry, supervisors, skill registry, memory managers, and optional task router.

## Core architecture: Actor model + multi-agent hierarchy

### Agent (`internal/agent/`)

An `Agent` is an LLM + tools + mailbox loop. Key characteristics:

- **Serialized execution**: all work (Ask/Submit) enters a `mailbox chan job` and is processed sequentially in the `run()` goroutine. No concurrent tool execution within a single agent.
- **Parallel tools**: when `parallelTools` is true, multiple `tool_calls` in one assistant response are executed via `errgroup` in `execTools()`.
- **Lifecycle**: `NewAgent` → `Start` → `[Ask/Submit]*` → `Stop`. `Start` is restartable after `Stop`.
- **InstanceID**: UUID per agent instance, separate from `Def.ID` (template/role identifier). Enables multiple instances of the same template in parallel.
- **Streaming**: `AskStream()` returns an `EventChan`; the caller consumes typed events (`ContentDeltaEvent`, `ThinkingDeltaEvent`, `ToolCallEvent`, `ToolResultEvent`, etc.) from a buffered channel.
- **Async delegation**: L1 agents can delegate to L2 asynchronously via `DelegateTool`. When the delegate tool returns `async_result` indicating delegation started, L1's turn yields, allowing new user messages while L2 works. Results are injected back when the delegation turn completes.
- **Priority mailbox**: L1 agents use `PriorityMailbox` so queued messages from async delegation can interleave ahead of new user asks.
- **Confirm mechanism**: tools implementing `Confirmable` block in `execToolStream` until the user approves/denies via `Confirm()`.

### Agent hierarchy (L1 → L2 → L3)

```
L1 (coordinator / default agent)
 ├── DelegateTool → spawns L2 leaders dynamically
 │   L2 (domain leader, e.g. "dev", "design")
 │    └── Supervisor → SpawnChild → L3 workers
 └── SkillTool → fork-mode creates isolated child agents
```

- **`AgentTemplate`**: loaded from `~/.soloqueue/agents/*.md` (YAML frontmatter + markdown body). Fields: ID, Name, SystemPrompt, IsLeader, Group.
- **`AgentFactory`** (`factory.go`): creates Agent + ContextWindow from template. The `DefaultFactory` caches LLM client, tools config, and model info.
- **`Supervisor`** (`supervisor.go`): L2 domain leader. Manages L3 child lifecycle (Spawn/Reap). Each child has independent CW and mailbox, enabling parallel work.
- **`Registry`** (`registry.go`): concurrent-safe InstanceID→Agent map + templateID→[]InstanceID index. `LocateIdle(name)` returns an idle agent; `Locate(name)` returns the first match.
- **`DelegateTool`** (`tools/delegate_tool.go`): L1→L2 delegation. Prefers idle instances; falls back to creating new ones. Has a `SpawnFn` closure wired at session build time.
- **Auto-reload** (`team/`): wraps Write/Edit tools so writes to `agents/` or `groups/` dirs trigger automatic template parsing and supervisor creation.

### Context window (`internal/ctxwin/`)

The `ContextWindow` manages conversation history as a doubly-linked list of `Message` nodes with tiktoken-based token counting.

- **Dual waterline compaction**: when `currentTokens > highWaterline`, evicts from the front (oldest non-ephemeral) until below `lowWaterline`. Ephemeral messages (verbose tool output marked `IsEphemeral`) are evicted first.
- **`replayMode`**: when true, `Push` does NOT invoke the push hook (prevents writing to timeline during replay). Used in `builder.go` when injecting system prompt + replaying history.
- **`pushHook`**: callback invoked on every `Push` (except in replayMode); used to persist messages to the timeline JSONL file.
- **`summaryHook`**: callback invoked when compaction occurs; writes a `summary` control event to timeline + records the evicted messages to short-term memory.
- **`filterCompletePairs`**: removes orphaned tool_calls (assistant with tool_calls but missing corresponding tool results) from the LLM payload to prevent HTTP 400 errors.

### Session (`internal/session/`)

A `Session` binds an Agent, ContextWindow, and timeline Writer into a single conversation unit.

- **`SessionManager`**: holds the single active session. `Init()` tears down any existing session and creates a new one via the factory.
- **Concurrency**: `inFlight atomic.Int32` CAS lock ensures only one Ask at a time per session. Returns `ErrSessionBusy` on conflict.
- **`Ask()` / `AskStream()`**: push user message → build payload → submit to agent → collect response → push assistant/tool messages to CW.
- **Task routing**: optional `TaskRouterFunc` classifies prompts into L0-L3 levels before submitting to the agent. The result (model ID, thinking mode, reasoning effort) overrides defaults for that turn.
- **History replay**: on session creation, `ReadLastSegments` reads timeline files backward to find the last `/clear` or `summary` cut point, then `ReplayInto` pushes those messages into the fresh CW.
- **`/clear`**: writes a `clear` control event to timeline, resets CW (keeps system prompt), records evicted messages to memory hook.
- **Idle auto-clear**: `ShouldClearContext` returns true when last message is older than `idleTimeout` AND tokens exceed `minTokens`.

### Timeline (`internal/timeline/`)

Append-only JSONL event sourcing with rotating files.

- **Events**: `message` (LLM conversation turn) and `control` (clear, summary). Each event is a JSON line in `timeline.jsonl`.
- **`Writer`**: wraps `rotating.Writer`; `AppendMessage` and `AppendControl` serialize to JSONL.
- **`ReadLastSegments`**: scans all rotation files in order, finds the last cut point (clear or summary control event), returns messages after it as a single Segment.
- **`ReplayInto`**: pushes segment messages into a CW. Handles orphaned tool_calls (assistant with tool_calls but missing subsequent tool results), skips duplicate system prompts (`<identity>` and `[Conversation Summary]`), and skips Invalid assistant messages (empty content, no tool_calls).
- **Critical invariant**: system prompts must NOT be written to timeline. The `builder.go` pushes the system prompt with `replayMode=true` to prevent contamination. Old timeline files may have stale copies; `ReplayInto` filters them out.

### Skills (`internal/skill/`)

Aligns with Claude Code's Skill mechanism. Skills are either builtin (Go code) or user (SKILL.md files). LLM activates them via `SkillTool` (function calling), not by manually reading SKILL.md.

- **Execution modes**: `inline` (instructions injected into current conversation) or `fork` (isolated child agent with filtered tools).
- **Preprocessing**: `$ARGUMENTS` substitution, `` !`command` `` shell execution for dynamic content, `@file` references.
- **`SkillRegistry`**: concurrent-safe name→Skill map shared across sessions.
- **Allowed tools**: fork-mode skills can restrict which tools the child agent has access to.

### Runtime stack (`internal/runtime/`)

`Stack` is the shared dependency container built once by `Build()` and used by both TUI and serve modes. It holds: LLM client, tools config, agent registry, agent factory, supervisors, leaders, templates, groups, system prompt, tokenizer, compactor, skill registry, memory managers, task router, todo store, and sandbox references.

- **Hot-reload** (`hotreload.go`): registers a config change callback that updates tools config, default model, LLM client (if provider changes), log level, and embedding/PermanentMemory settings without restart.
- **`Build()`** (`build.go`): orchestrates initialization order — config → LLM client → prompt system → agent registry → supervisors → memory → router → sandbox prep.

### Tools (`internal/tools/`)

`Tool` interface: `Name()`, `Description()`, `Parameters()` (JSON Schema), `Execute(ctx, args) (result, error)`. Implementations: Bash, Edit, Write, MultiWrite, MultiEdit, Glob, Grep, Read, WebSearch, WebFetch, SkillTool, DelegateTool, InspectAgentTool, TodoWrite, etc.

- **`Confirmable`**: optional interface for tools needing user approval before execution.
- **Fallback prefix**: L2/L3 tools get a `fallback_` prefix to prevent namespace collisions; L1 tools use the canonical names.
- **`WithFallbackPrefix`**: wraps tools so they register under both original and `fallback_` names.

### Web UI (`web/`)

React 19 + TypeScript + Vite + TailwindCSS v4. Uses shadcn/ui components (`@base-ui/react`), dnd-kit for drag-and-drop, Lucide icons.

## Key patterns

- **Functional options**: most constructors accept variadic `With*` option functions.
- **`logger.Cat*` constants**: structured logging categories — `CatApp`, `CatActor`, `CatMessages`, `CatConfig`, etc. Use `logger.System(workDir, ...)` to create a session-scoped logger.
- **`iface.Locatable`**: interface with `InstanceID()` and `DefID()` used for agent location in the registry.
- **Config hot-reload**: `config.GlobalService` uses fsnotify; callers read latest settings with `cfg.Get()`.
- **Agent events** (`events.go`): typed event structs sent over `EventChan` during streaming. Includes lifecycle events (`AgentStartedEvent`, `AgentStoppedEvent`) and turn events.
