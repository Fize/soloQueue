# Simulation Subsystem — TODO

## Completed

- [x] **Phase 1: Core types & event-driven architecture**  
  `Persona`, `SimulationConfig`, `SimulationState`, `AgentMemory`, `WorldState`, `MessageBus`, `SimAgent` wrapper around `agent.Agent`, `SimulationEngine` lifecycle.

- [x] **Phase 1: LLM cache optimization**  
  System prompt pushed once to ContextWindow → prefix-cache hits every round after the first. Direct CW eviction (FIFO), AgentMemory retains full history.

- [x] **Phase 2: Event-driven mode**  
  `EventLoop` with goroutine-per-agent, `TriggerPolicy` (Reactive/Selective/RateLimited), `AgentScheduler` with semaphore pool.

- [x] **Phase 3: Notification-based wakeup + goroutine pool**  
  `MessageBus` with `notifyCh` per agent → agents block on channel receive instead of ticker polling. Semaphore-controlled LLM concurrency (default 20).

- [x] **Phase 3: Agent relationship graph**  
  `RelationGraph` with directed edges (mention/rebuttal/agree/propose). Built incrementally during simulation, fed into final report.

- [x] **SQLite persistence**  
  `SQLiteStore` with separate DB file (user-specified path), WAL mode, `busy_timeout=10000`. Does NOT share the existing `entries.db`.

- [x] **Config, runtime, API, CLI integration**  
  `SimulationConfigFile` in `settings.toml`, `SimulationEngine` in `runtime.Stack`, REST endpoints (`/api/simulations`), `soloqueue simulate` CLI.

- [x] **Tests**  
  41 unit + integration tests, all passing with `-race`. Existing 514 tests unaffected.

---

## Next: Seed Information → Auto World Construction

**Goal:** From a document or text input, automatically extract entities, build the initial WorldState, and generate agent personas — leveraging the existing MemoryEngine KG.

### 1.1 Seed Information Injection

- [ ] **`SeedExtractor`** — LLM-powered entity extraction from seed text
  - Accept raw text or file path
  - Call existing LLM with extraction prompt → `EntityExtraction[]` (name, type, confidence, relations)
  - Populate initial `WorldState` from extracted entities/relations
  - Optionally persist to MemoryEngine KG via `SaveWithEntities`
  - File: `internal/simulation/seed.go`

- [ ] **API endpoint** — `POST /api/simulations/from-seed`
  - Accept `{ "seed_text": "...", "persona_count": 5 }`
  - Return `{ "simulation_id": "...", "entities": [...], "personas": [...] }`

- [ ] **CLI flag** — `soloqueue simulate --seed doc.md --persona-count 5`

### 1.2 Persona Auto-Generation

- [ ] **`PersonaGenerator`** — generates N personas from extracted entities + topic
  - Each persona gets a unique stance (pro/con/neutral toward each entity)
  - Traits randomly sampled with constraints (at least one contrarian, one mediator)
  - System prompts auto-generated from stance + entity relationships
  - File: `internal/simulation/persona_gen.go`

### 1.3 KG as World Model
- [ ] Wire `MemoryEngine` into the simulation lifecycle
  - On simulation start: seed KG entities as world model
  - During simulation: agent `[PROPOSE ...]` mutations optionally indexed into KG
  - On simulation end: final report + graph persisted to KG for future recall
  - Use existing `Engine.ConnectEntities` / `Engine.RecallEntity` for cross-simulation knowledge

---

## Next: Post-Simulation Deep Interaction

- [ ] **Agent replay** — query any simulation agent's memory after simulation ends
  - API: `POST /api/simulations/{id}/agents/{personaId}/ask`
  - Uses the agent's `AgentMemory` as context for follow-up questions
  - Agent responds in-character based on their simulation history

- [ ] **"What if" re-simulation** — fork a simulation with modified parameters
  - API: `POST /api/simulations/{id}/fork` with new WorldState or added personas
  - Reuses existing personas but starts from a different initial state

---

## Next: Frontend Visualization

- [ ] **Simulation dashboard** — real-time agent message stream
  - WebSocket or SSE push agent messages as they happen
  - Show active agents, speaking status, WorldState changes

- [ ] **Relationship graph visualization** — D3.js or Cytoscape force-directed graph
  - Nodes = agents, sized by message count
  - Edges = relationships, colored by type, thickness by weight
  - Animates as simulation progresses

- [ ] **Timeline view** — scrollable history of all agent messages
  - Color-coded by agent, filterable

---

## Next: Scale Validation

- [ ] **Benchmark** 100/500/1000 agent simulations
  - Measure: goroutine count, memory usage, SQLite write throughput, LLM API rate limiting
  - Tune: pool size, MessageBus buffer, SQLite pragmas for high concurrency

- [ ] **Graceful degradation** — when LLM API rate-limited, agents queue and retry rather than failing
  - Exponential backoff per agent
  - Simulation continues with available agents; degraded agents catch up when API capacity returns

---

## File Map

```
internal/simulation/
  types.go          # Persona, SimulationConfig, SimulationState, AgentMemory
  errors.go         # Simulation-specific errors
  worldstate.go     # Thread-safe shared KV store
  messagebus.go     # Peer-to-peer messaging + notification channels
  store.go          # Store interface + in-memory SimulationStore
  sqlite_store.go   # SQLite-backed persistent store
  simagent.go       # SimAgent: wrapper around agent.Agent + CW + memory
  prompts.go        # System/user/report prompt templates
  event_loop.go     # EventLoop: notification-driven agent goroutines
  scheduler.go      # AgentScheduler: goroutine lifecycle + semaphore pool
  trigger.go        # TriggerPolicy: Reactive/Selective/RateLimited
  graph.go          # RelationGraph: agent interaction edges
  simulation.go     # SimulationEngine: full lifecycle orchestration

  seed.go           # (TODO) Seed information extraction
  persona_gen.go    # (TODO) Persona auto-generation

internal/runtime/
  build_simulation.go  # Phase 4.5: SimulationEngine construction
  stack.go             # +SimulationEngine field
  build.go             # +buildSimulationEngine() call

internal/config/
  schema.go         # +SimulationConfig section
  defaults.go       # +Simulation defaults

internal/server/
  simulation_handlers.go  # REST endpoints

cmd/soloqueue/cli/
  simulate.go       # CLI command
```
