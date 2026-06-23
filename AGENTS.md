# AGENTS.md

Tactical guidance for AI coding agents working in this repository.

## Build

```bash
make build        # pnpm build + cp dist + cp skills → internal/server/dist → go build
make build-go     # go build only (needs internal/server/dist already in place)
make build-web    # pnpm build + cp dist + cp skills to internal/server/dist
make clean        # rm soloqueue web/dist internal/server/dist
```

The Go binary embeds `internal/server/dist/` via `//go:embed`. `make build-web` also copies `skills/` into dist.

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
cd web && pnpm test                      # vitest
cd web && pnpm test:e2e                  # playwright e2e tests
```

Uses `pnpm`, NOT npm. Dev server proxies `/api` → `http://localhost:8765` and `/ws` → `ws://localhost:8765`.

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
- Shared SQLite: `~/.soloqueue/permanent_memory/entries.db`
- Ignored by git: `.soloqueue/`, `.codebuddy/`, `.envsoloqueue`, `logs/`

Config loading order (low→high priority): compiled defaults → `settings.toml` → `settings.local.toml`.

## Directory layout

```
cmd/soloqueue/          cobra entrypoint (main.go + cli/)
internal/agent/         actor-model agent (LLM + tool loop + mailbox)
internal/compactor/     LLM-based context compression engine
internal/config/        hot-reload config (TOML schema + settings)
internal/cron/          scheduled cron & timer tasks (SQLite-backed)
internal/ctxwin/        context window (tiktoken, dual-waterline compaction)
internal/iface/         shared interfaces (breaks agent↔tools cycle)
internal/llm/           provider-agnostic LLM protocol + DeepSeek transport
internal/logger/        structured logging (file + console)
internal/mcp/           MCP server manager + config + LSP integration
internal/memory/        short-term memory manager (daily .md files)
internal/memoryengine/  long-term memory engine (BM25 + KG + optional vector)
  embedding/            Embedder interface + OpenAI implementation
  vectorstore/          SQLite-backed vector store (cosine similarity)
internal/prompt/        prompt assembly, templates, team management, parser
internal/qqbot/         QQ official bot WebSocket integration
internal/router/        L0-L3 task classification & model routing
internal/runtime/       shared dependency container (Stack, built once)
internal/server/        REST + WebSocket HTTP router (chi/v5)
internal/session/       session manager (single active, inFlight atomic CAS)
internal/simulation/    Generative Agents simulation engine (seed→persona→agent loop)
internal/skill/         Claude Code-compatible skill system
internal/sqlitedb/      shared SQLite wrapper + schema migrations
internal/team/          team group reload
internal/timeline/      append-only JSONL event sourcing
internal/tools/         Tool implementations + Sandbox execution backend
web/                    React web UI (Vite dev server)
```

## Simulation Engine (internal/simulation/)

Generative Agents-style multi-agent simulation. Seed text → LLM extraction → persona generation → autonomous agent loop.

### Pipeline

```
seed text → Phase 1 LLM (entities, world_state, key_topics, conflict_areas)
         → Phase 2 LLM (suggested_agents, lifecycle_events, initial_relationships)
         → PersonaGeneration LLM (personas with goals, traits, system prompts)
         → GA agent loop (Perceive→Retrieve→Decide→Execute→Reflect per tick)
```

### Goal system (critical for narrative simulations)

- **`SuggestedAgent.Goals`** is extracted by Phase 2 from the seed text. These are character-specific immediate objectives (e.g., "找到落霞谷的入口"), NOT abstract topic positions.
- In `buildPersonas`, when deduction mode is active (seed has `SuggestedAgents`), seed-extracted `Goals` **override** the persona-gen LLM's goals. The LLM is also instructed to use provided goals directly rather than inventing new ones.
- **Goal transitions**: `SeedLifecycleEvent` with `type: "goal_transition"` carries `NewGoals []string`. When triggered at sim_time, `handleGoalTransition` updates `sa.persona.Goals` (the SimAgent's pointer — read each tick during system prompt rebuild) and `lm.allPersonas` (for mid-simulation spawns).
- `allPersonas` is passed by **value** to each `GAAgentLoop`. Other agents don't see each other's goals in their system prompts — only name/role/bio. So goal transitions only need to update the SimAgent pointer and `lm.allPersonas`, not each loop's local copy.

### Lifecycle events

Scheduled via `SeedLifecycleEvent` with `Trigger`/`TriggerValue`. Types:
- `agent_spawn` — mid-simulation agent creation
- `agent_death` — removes agent
- `goal_transition` — replaces agent's `Goals` with `NewGoals`
- `simulation_end` — terminates the simulation

Scheduler runs every 2 seconds, checks `sim_time`/`wall_time`/`condition` triggers.

### Key types

| Type | Purpose |
|------|---------|
| `SeedExtraction` | Phase 1+2 merged output (entities, world_state, key_topics, suggested_agents, lifecycle_events) |
| `SuggestedAgent` | Per-character extraction: name, role, description, traits, **goals** |
| `Persona` | Final agent definition: ID, name, role, bio, **goals**, traits, system_prompt, MBTI |
| `SeedLifecycleEvent` | Timed event: type, agent_name, trigger, trigger_value, **new_goals** |

### Test approach

- `FakeLLM` (from `internal/agent/llm.go`) used in simulation tests to avoid real API calls.
- No `TestMain` or shared fixtures — self-contained per package.
- `seed_test.go` and `persona_gen_test.go` test extraction and generation independently.

## Memory Engine (internal/memoryengine/)

### Overview

The memory engine replaces the old embedding-dependent `internal/permanent/` system. It provides config-driven hybrid search across three pipelines:

| Pipeline        | Technology                                       | Dependency               |
| --------------- | ------------------------------------------------ | ------------------------ |
| BM25            | SQLite FTS5 over `mem_entries`                   | Zero (built into SQLite) |
| Knowledge Graph | Entity-relationship graph with PPR/BFS traversal | Zero (pure Go)           |
| Vector          | Cosine similarity over `mem_vec` BLOBs           | OpenAI API (remote)      |

### Config: two modes

```toml
[embedding]
provider = "none"     # pure BM25 + KG, zero dependencies (DEFAULT)
# provider = "openai" # remote API (existing OpenAI-compatible)
```

**Mode "none" (default)** — dual-hybrid BM25 + KG. No model files, no API keys. Suitable for most use cases because the KG compensates for the lack of semantic search via entity extraction.

**Mode "openai"** — tri-hybrid BM25 + KG + Vector. Uses the existing OpenAI-compatible embeddings API. Requires network + API key.

### Design rationale

**Why not just vector search?** Pure vector search (like the old system) handles semantic similarity well but fails at exact keyword matching ("DeepSeekRouter" won't match "deepseek router") and cannot answer relational queries ("what do I know about project X?"). BM25 excels at keyword precision; the KG excels at relational reasoning. Together they cover each other's blind spots.

**Why embed the KG in-process instead of using an external graph DB?** A single SQLite file with adjacency-list tables (`kg_nodes`, `kg_edges`) is zero-operational-overhead and co-located with the memory data. Neo4j/ArangoDB would require separate infrastructure. For agent-scale data (tens of thousands of entities, not millions), SQLite's BFS and in-memory PPR are sufficient.

**Why agent-driven entity extraction?** The engine never calls an LLM internally. Instead, the agent extracts entities and relationships from conversation context and indexes them via the `KGIndex` tool. This avoids hidden LLM costs and gives the agent full control over what gets indexed. Same design as Kioku Lite.

### Data model

```
mem_entries  — id, content, content_hash (SHA-256, UNIQUE), date, tags,
               event_time, salience, last_recalled_at, created_at
mem_fts      — FTS5 virtual table over mem_entries(content, date)
mem_vec      — content_hash, embedding BLOB (only when embedding provider != "none")
kg_nodes     — id, name (UNIQUE), type (open schema), mention_count,
               first_seen, last_seen, confidence
kg_edges     — id, source→target, rel_type, weight, evidence, source_hash,
               event_time, valid_from, valid_until, UNIQUE(source, target, rel_type)
kg_aliases   — alias → canonical entity name
```

All tables live in the shared `entries.db` via v12 migration. The old `memories` table data is migrated to `mem_entries` in v13 (content preserved, embeddings discarded — they depended on the old model).

### Search flow

```
User query → Query string
  ├─ BM25 pipeline: tokenize → FTS5 MATCH → normalize scores (0-1)
  ├─ KG pipeline:   tokenize → match entity names → BFS from matches → score by edge weight
  │                 OR if entities provided → PPR (damping=0.85, 20 iters) → map to content_hash
  └─ Vector pipe:   embed query → cosine similarity scan → normalize scores (0-1)
       ↓
  RRF fusion (k=60): dedup by content_hash, accumulate 1/(k+rank+1) per source
       ↓
  Temporal filter: exclude results with event_time > as_of or outside date range
       ↓
  Salience boost: score *= salience (Ebbinghaus decay, recall boosts)
       ↓
  Hydration: bulk-fetch full content from mem_entries by content_hash
```

### Temporal features

1. **event_time** — when the event happened (distinct from `created_at` = when recorded). Enables timeline queries.
2. **valid_from / valid_until** on kg_edges — relationships expire. Queries default to `valid_until IS NULL OR valid_until > now()`.
3. **Ebbinghaus forgetting curve** — `salience = S0 * e^(-t/half_life)`. Each recall adds 0.3 (capped at 2.0). Low-salience memories rank lower.
4. **Time-travel** — `SearchQuery.AsOf` enables "what did I know at time T?" queries.
5. **Timeline** — `MemoryStore.Timeline(from, to)` returns chronological entries.

### Agent tools

| Tool                  | Purpose                                                    |
| --------------------- | ---------------------------------------------------------- |
| `Remember`            | Save content + optionally index entities/relations into KG |
| `RecallMemory`        | Hybrid search (BM25 + KG + optional vector)                |
| `KGIndex`             | Index extracted entities and relationships into the KG     |
| `RecallEntity`        | Traverse KG from an entity to find related memories        |
| `ConnectEntities`     | Find shortest path between two entities in the KG          |
| `MemoryTimeline`      | List memories chronologically within a date range          |
| `ConsolidateMemories` | Run maintenance (edge decay, stale cleanup, communities)   |

### Trade-offs

| Aspect                  | Win                                | Loss                                         |
| ----------------------- | ---------------------------------- | -------------------------------------------- |
| BM25 vs Vector          | Exact keyword match, zero deps     | No semantic generalization without KG        |
| KG vs Graph DB          | Zero ops, co-located with data     | No horizontal scaling, in-memory PPR         |
| Agent-driven extraction | No hidden LLM costs, full control  | Entity coverage depends on agent diligence   |
| Salience decay          | Old/unused memories fade naturally | Can lose rarely-accessed but important facts |
| Single SQLite file      | Simple backup, no infra            | Writer contention under high concurrency     |

### Key patterns

- **`Engine` is the single entry point** — constructed once in `runtime.buildMemoryEngine()`, shared via `Stack.MemoryEngine`.
- **Embedder is injected, not imported** — `embedding.NewFromConfig(cfg)` returns `nil` for `"none"`. All code paths check `nil` before using.
- **Vector search is optional** — `VectorSearcher.Enabled()` returns false when embedder or vecStore is nil. RRF handles 2 or 3 input lists transparently.
- **RRF dedup by content_hash** — same memory found by multiple pipelines gets a combined score rather than appearing multiple times.
- **Salience is query-time** — no background job needed. `EbbinghausSalience()` computes decay from `last_recalled_at` at search time.

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
