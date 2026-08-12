# L2 Group Memory Isolation — Threat Model

## Scope and Security Goal

Each configured L2 group has durable memory that survives process restarts. All L2 sessions in the same group share that memory across projects and working directories. Different L2 groups cannot directly read or modify each other's memory, and no L2 group can directly read or modify L1 memory.

An L2 session ID identifies conversational context and timeline state; it is not a long-term memory owner. Project and working-directory identities are metadata and execution boundaries; they are not L2 memory authorization boundaries.

L1 may provide one L2 session with an explicit, minimal, read-only context snapshot when creating or delegating work. The snapshot remains session-local context and is not automatically stored in the group's durable memory. It is not a live view of L1 memory.

L3 agents and skill forks have no durable-memory capability. They receive only the bounded task context that L2 explicitly passes to them. An L3 result may become group memory only when L2 separately evaluates it and invokes its own group-bound memory write.

## Current-State Findings

- The runtime owns one shared `MemoryEngine` backed by the shared SQLite database.
- Built-in tool construction currently registers `Remember` and `RecallMemory` whenever the shared tool configuration is built.
- Agent creation passes only `MemoryEngine` and an effective `WorkDir` to memory tools; it does not pass an immutable L2 group owner.
- Regular L3 construction currently rebuilds the standard tool set and filters only `SendFile`, so L3 still receives memory tools.
- Skill-fork construction currently rebuilds tools from the shared configuration and filters only `SendFile` and Cron tools. With no bound work directory, its memory tools may fall back to global scope.
- L3 system-prompt construction currently advertises permanent memory whenever the shared `MemoryEngine` is configured.
- `Remember`, `RecallMemory`, `RecallEntity`, and `MemoryTimeline` derive memory scope from `WorkDir`.
- A default/global work directory maps to global scope. A project directory maps to project scope.
- Recall tools currently include global memories in scoped searches.
- Multiple L2 groups may use the same project, while one L2 group may work across multiple projects. `WorkDir` therefore both over-shares and over-partitions relative to the intended group boundary.
- L2 already persists its group in session metadata, but the current display/configuration string is not yet an immutable memory-owner identifier.
- L2 compaction currently writes summaries only to the L2 timeline. Long-term writes occur through the explicit `Remember` tool.
- FTS, vector, and knowledge-graph retrieval operate over shared derived indexes and apply some scope filtering after candidate retrieval.
- Deleting an L2 session removes its timeline directory. Group-memory lifecycle is not currently modeled independently from session lifecycle.

## Assets to Protect

- [ ] L1 global and project-scoped durable memories.
- [ ] The durable shared memory of every L2 group.
- [ ] The capability boundary that keeps all durable-memory tools and engine handles out of L3 and skill forks.
- [ ] L1-to-L2 session context snapshots and their provenance.
- [ ] Memory lifecycle metadata, including owner, source, status, and timestamps.
- [ ] Derived FTS, vector, and knowledge-graph records linked to group-owned memories.
- [ ] Stable L2 group ownership across sessions, projects, lazy activation, template reload, and process restart.

## Security Principals and Authorization Rules

| Principal | Allowed reads | Allowed writes |
| --- | --- | --- |
| L1 | Existing L1 global/project policy | Existing L1 global/project policy |
| L2 session | Exact matching L2 group namespace | Exact matching L2 group namespace |
| L3 or skill fork under L2 | No durable memory; bounded task context only | No durable memory |
| Cross-group peer delegation | No durable memory by default | No durable memory by default |
| Cron L2 | Exact matching L2 group namespace through explicit memory tools | Exact matching L2 group namespace through explicit memory tools |

The server constructs and binds the principal from trusted group metadata. Model/tool input must never contain an authoritative owner or namespace field.

## Threats Identified

### T1: Incorrect sharing through working-directory scope

Two different L2 groups using the same project currently derive the same project memory scope and can retrieve each other's memories. Conversely, sessions from the same L2 group using different projects derive different scopes and cannot share the memory they are intended to share.

### T2: L1 disclosure through global recall

Recall currently permits global memories in addition to the derived project scope. An L2 session can receive L1/global memories even when its own namespace is otherwise scoped.

### T3: Cross-owner write or overwrite

If an owner can be selected through tool arguments, mutable work-directory state, a display-name collision, or an unbound engine API, an L2 or child agent can write into another owner's scope. Shared canonicalization, deduplication, hashes, or graph edge conflict keys can also modify another owner's derived state.

### T4: Late filtering in shared retrieval pipelines

FTS, vector, and graph searches may retrieve cross-owner candidates before filtering. Late filtering risks accidental future disclosure, cross-owner ranking interference, candidate starvation, and graph relationship contamination.

### T5: L3, fork, or peer capability leakage

L3 agents, peer leaders, and skill forks are created through paths that rebuild tool configuration. An L3 or skill fork may receive `Remember`, recall tools, a raw engine handle, or a prompt that claims permanent-memory access. A cross-group peer may become a confused deputy and disclose its own group memory to the caller.

### T6: Group identity confusion during rename, reload, or reuse

Ownership may bind to the wrong namespace if it is derived from a mutable group display name, template ID, ephemeral agent instance ID, session ID, or path. Deleting and later recreating a group with the same display name could expose the previous group's memory.

Group Markdown is also writable through agent file tools. Treating its `memory_owner_id` frontmatter as authoritative would let a child attempt namespace switching after reload, so the server-owned SQLite mapping must override the file mirror.

### T7: Incorrect session and group lifecycle coupling

Deleting one L2 session must not delete shared group memory. Conversely, deleting or retiring a group without an explicit memory lifecycle can leave active rows, embeddings, or graph edges that may become visible after identity reuse or a future defect.

### T8: Snapshot over-sharing or confused authority

An unrestricted live L1 view would let an L2 query arbitrary L1 history. Persisting a session-specific L1 snapshot into shared group memory would expose it to every current and future session in that group. A snapshot may also lose provenance or be treated as permission to write back into L1.

### T9: Prompt injection through remembered or copied content

Memory content is untrusted historical data. Project files, prior model output, or copied L1 context may contain instructions intended to override the L2 task or request privileged memory access.

### T10: Migration misattribution

All records already present in the main long-term-memory database belong to L1, regardless of their existing scope metadata. Simulation agent memories live in the standalone simulation database and are outside this migration. Reclassifying a main-database record as L2 would violate the migration contract.

### T11: Sensitive content in logs and errors

Logging queries, recalled content, snapshot bodies, or detailed database errors could bypass owner isolation even when tool results are correctly filtered.

## Trust Boundaries

- [ ] HTTP/WebSocket input to L2 session lookup and activation.
- [ ] Server-owned SQLite team identity mapping to `L2SessionStore` and `Builder.BuildL2`, where a stable group owner is resolved; group Markdown is an untrusted mirror.
- [ ] L1 delegation to L2 creation, where an optional session-local snapshot is selected and copied.
- [ ] Agent factory to L2 tool construction, where an immutable group memory principal must be propagated.
- [ ] L2 to L3/fork creation, where all durable-memory tools, prompts, and engine capabilities must be removed.
- [ ] Cross-group peer delegation, where durable memory must not become an implicit data-transfer channel.
- [ ] Memory tools to the storage engine, where authorization must be enforced independently of prompts.
- [ ] Primary memory rows to FTS, vector, and knowledge-graph derived indexes.
- [ ] Session deletion and group retirement to their distinct durable-memory lifecycle rules.
- [ ] Memory results to model context and timeline persistence, where content remains untrusted data.

## Planned Mitigations

- [ ] Introduce an immutable server-created memory principal containing task level `L2` and a stable L2 group owner ID.
- [ ] Bind L2 memory access through a scoped capability or repository created for that exact group owner; do not expose a raw shared engine to L2 tools.
- [ ] Add an exact L2-group owner scope to memory records and make the owner part of canonical deduplication and content identity.
- [ ] Resolve the owner from the server-owned SQLite mapping, repair rather than trust group frontmatter, and reject missing, malformed, global, project, or mismatched owners on every L2 read and write at the engine boundary.
- [ ] Disable `IncludeGlobal` for L2 recall and timeline operations. Preserve existing L1 behavior behind a separate L1-bound capability.
- [ ] Apply owner predicates inside every FTS/vector/KG query before ranking, fusion, hydration, or graph traversal.
- [ ] Make derived-index records owner-aware and prevent cross-owner conflict keys or updates.
- [ ] Register group-bound memory tools only for L2 leaders. Remove `Remember`, `RecallMemory`, `RecallEntity`, `MemoryTimeline`, and any future durable-memory tool from L3 and skill-fork tool sets.
- [ ] Do not pass a scoped memory capability or raw `MemoryEngine` to L3 and skill forks. Tool filtering alone is not the authorization boundary.
- [ ] Remove permanent-memory instructions from L3 and skill-fork system prompts.
- [ ] Pass only explicit bounded task context from L2 to L3. Returning an L3 result does not write memory; L2 must independently invoke its own group-bound `Remember` tool.
- [ ] Give cross-group peer delegations a memory-disabled capability by default. Any memory transfer requires an explicit, bounded context snapshot rather than ambient access.
- [ ] Keep L1 snapshots as immutable, session-local context with provenance and optional expiry. Do not automatically persist them into group memory or provide a live L1 query interface.
- [ ] Treat recalled and copied memory as quoted historical context, not system instructions or authorization.
- [ ] Make session deletion leave group memory intact. Define group retirement as a separate explicit operation that archives or deletes owned primary rows and removes/rebuilds their derived records.
- [ ] Prevent a retired group owner ID from being silently reused, even if a display name is reused.
- [ ] Assign every pre-feature primary and derived record in the main memory database to owner `l1` while preserving its existing scope fields as metadata. L2 group owners start empty and receive only post-migration writes. Leave the standalone simulation database unchanged.
- [ ] Log only operation type, owner hash or non-content identifier, counts, latency, and stable error codes. Do not log memory queries, content, or snapshot bodies.
- [ ] Add allow-path and deny-path tests at L2 tool, engine, L3/fork construction, peer delegation, restart, and lifecycle boundaries.

## Security Acceptance Criteria

- [ ] Two sessions in the same L2 group can recall the same memory even when they use different projects and working directories.
- [ ] Sessions in different L2 groups cannot read, update, deduplicate against, rank against, or delete each other's memory, even when they use the same project and working directory.
- [ ] An L2 session cannot read any L1 global or project memory through text, timeline, entity, graph-context, or vector retrieval.
- [ ] Direct engine calls with a missing or mismatched L2 group owner fail closed.
- [ ] Restarting and lazily reactivating an L2 session restores access to exactly its group's memory namespace.
- [ ] L3 agents and skill forks do not register or advertise any durable-memory tool and do not receive a memory engine or scoped capability.
- [ ] L3 can use only context explicitly placed in its delegated task; it cannot query L2, L1, or another group's durable memory.
- [ ] Returning an L3 result does not create durable memory unless L2 performs a separate authorized write.
- [ ] Cross-group peer delegation cannot use the peer group's durable memory unless an explicit bounded snapshot is supplied.
- [ ] A copied L1 snapshot is visible only in its target session, is not automatically stored in shared group memory, and never grants access to its L1 source.
- [ ] Deleting one L2 session does not alter group memory. Explicit group retirement removes or invalidates all group-owned primary and derived-index data.
- [ ] Every pre-feature main-database record is owned by L1 and remains invisible to L2 groups. The standalone simulation database is unchanged and remains outside L1/L2 memory access.
- [ ] No memory content or query text appears in normal logs or user-facing internal error details.

## Residual Risks

- All principals run in one server process and share one database connection, so a defect in privileged L1 or maintenance code can still bypass logical isolation.
- A model can repeat content from its current conversation or explicit snapshot; memory isolation cannot retract content already placed in that model context.
- SQLite file access outside SoloQueue is outside this application-level authorization boundary and depends on host filesystem security.
