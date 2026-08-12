# L2 Group Memory — Secure Design

## Decision Summary

SoloQueue will keep one physical memory engine and database, but agents will not receive that raw engine. They receive a server-created, immutable memory capability with an exact owner.

- L1 memory owner: `l1`.
- L2 memory owner: a stable UUID persisted with the L2 group.
- Same-group L2 sessions share memory across sessions, projects, work directories, direct chat, restart, and Cron execution.
- Different L2 groups cannot read, write, rank against, deduplicate against, update, or delete each other's memory.
- L1 and L2 cannot read or modify each other's memory.
- L3 agents and skill forks receive no durable-memory tools, prompt instructions, engine pointer, or scoped capability.
- A cross-group peer delegation is memory-disabled by default.
- Every primary and derived record already present in the main `soloqueue.db` memory tables is assigned to L1. The standalone `simulation.db` and its `agent_memories` remain unchanged and outside this feature.

This design preserves the current explicit-memory policy: agents decide when `RecallMemory` or `Remember` is relevant; there is no automatic pre-recall and no new memory mode setting.

## Security Invariants

1. Memory ownership is resolved by trusted server code, never by model/tool arguments.
2. A working directory, project path, session UUID, template display name, or agent instance ID is never an authorization key.
3. Every primary and derived-memory query applies its owner predicate before ranking or traversal.
4. L2 group recall always has `include_global=false`.
5. The zero value for agent memory policy is disabled.
6. L3 and skill-fork construction cannot opt into memory through template content or inherited tool configuration.
7. Session deletion never deletes shared group memory.
8. A deleted and recreated group receives a different owner UUID.
9. Memory content and query text are untrusted data and are not written to logs.

## Current Call-Chain Problems

The current runtime places one `MemoryEngine` in the shared `tools.Config`. `tools.Build` adds `Remember` and `RecallMemory`, and `DefaultFactory.CreateWithOptions` copies that configuration to every ordinary agent.

The factory currently sets only `WorkDir` before building tools. Memory tools derive global/project scope from that path, while recall includes global records. This creates both incorrect sharing and incorrect partitioning for L2 groups.

For non-leader agents, the current factory removes only `SendFile`. Skill forks remove only `SendFile` and Cron tools. Both paths therefore retain memory tools, and the L3 system prompt may advertise permanent memory.

The existing FTS, vector, and knowledge-graph pipelines in the main memory engine search shared candidates before some scope filtering. KG node/edge uniqueness is also global. These are relevance and isolation defects even when final hydration later drops mismatched records.

## Simulation Boundary

Simulation state, rounds, reports, and persona memories are stored in a standalone SQLite database. The default path is `~/.soloqueue/simulation.db`, and `agent_memories` is scoped by simulation and persona IDs.

Production runtime construction does not call `SimulationEngine.SetMemoryEngine`; only tests currently wire that optional shared-engine path. Therefore normal simulation execution does not write to or recall from the main `soloqueue.db` memory engine.

This feature does not modify `simulation.db`, does not introduce a simulation owner in the main memory schema, and does not wire the simulation engine to L1 or L2 memory. Any legacy row already present in main memory tables still receives owner `l1` under the explicit migration rule.

## Auth Strategy

### Memory owner

Store the authoritative group-to-owner mapping in the server-owned `team_memory_owners` SQLite table. Mirror the UUID as `memory_owner_id` in group frontmatter for compatibility and diagnostics, and add it to the corresponding `prompt.GroupFrontmatter` and `team/store.Team` structs. The frontmatter value is never authoritative because agent file tools can rewrite group Markdown.

- `CreateTeam` generates and persists the UUID with the already-installed `github.com/google/uuid` package.
- `UpdateTeam` preserves the SQLite identity and repairs a missing or modified frontmatter mirror.
- `EnsureMemoryOwnerID(groupName)` backfills the authoritative SQLite mapping under the team-store lock before the first memory-enabled L2 is created.
- Backfill writes the same UUID to the group file before returning it to the caller.
- A frontmatter UUID that differs from SQLite is overwritten from SQLite and is never adopted as authority.
- Invalid or duplicate UUIDs fail closed; they never fall back to group name, project, or global memory.
- A recreated group receives a new UUID, so abandoned memory cannot become visible through display-name reuse.
- The UUID is not accepted from HTTP, WebSocket, tool, or frontmatter input and is not exposed by public team APIs.

### Bound capability

Introduce an engine-owned bound interface rather than passing `*engine.Engine` to tools:

```go
type Access interface {
	Remember(context.Context, RememberInput) (IngestResult, error)
	Recall(context.Context, RecallInput) (*SearchResultSet, error)
	Timeline(context.Context, TimelineInput) ([]MemoryEntry, error)
	RecallEntity(context.Context, EntityRecallInput) ([]SearchResult, error)
}
```

The concrete implementation stores an immutable internal policy:

```go
type owner struct {
	Kind string // l1 or l2_group
	ID   string
}

type accessPolicy struct {
	Owner         owner
	ScopeType     string
	ScopeID       string
	IncludeGlobal bool
	Writable      bool
}
```

Only policy-specific constructors are exported:

- `BindL1(scopeType, scopeID, includeGlobal)` preserves current L1 behavior within owner `l1`.
- `BindL2Group(memoryOwnerID)` binds owner `l2_group:<uuid>`, team scope, `includeGlobal=false`, and writes enabled.

The general constructor and policy fields remain internal to the memory package. Tool input schemas do not contain owner, scope, or `include_global` fields.

Administrative audit, migration, consolidation, and cleanup code may continue to use the privileged engine directly. Raw engine access is never stored in an agent tool configuration.

## Tool and Agent Construction

Split tool construction into two explicit sets:

```go
tools.BuildBase(cfg)                 // never contains durable-memory tools
tools.BuildMemory(cfg, access)       // requires a non-nil bound capability
```

`Remember`, `RecallMemory`, and any future `RecallEntity` or `MemoryTimeline` registration live only in `BuildMemory`. A nil capability returns no memory tools.

Add an explicit memory policy to `agent.CreateOptions`. Its zero value is `MemoryDisabled`. Trusted L2 creation paths must request `MemoryL2Group`; no role is inferred from user-controlled prompt text.

| Creation path | Memory policy |
| --- | --- |
| L1 session builder | L1-bound capability |
| User-created L2 session | L2 group capability |
| L1-to-L2 leader delegation | L2 group capability |
| L2 Cron session | L2 group capability |
| Trusted leader reload/workflow execution | Explicit L2 group capability when the target is a real L2 leader |
| L3 through `Supervisor` | Disabled |
| Dynamic L3 worker | Disabled |
| Skill fork | Disabled |
| Cross-group peer called by L2 | Disabled |
| Simulation agent tools | Disabled; standalone `simulation.db` remains unchanged and the shared engine stays unwired |

`buildL3SystemPrompt` will no longer accept `hasPermanentMemory`. It never appends `MemoryEngineSection`. L2 appends the section only when a valid group capability and memory tools were both created.

L2 may pass bounded task facts to L3 in the delegation prompt. L3 results return normally, but no hook automatically stores them. L2 must make a separate, authorized `Remember` call if the result is durable.

## Database and Ownership Model

### Primary memory rows

Advance the SQLite schema version and add these columns to `mem_entries`:

```sql
owner_type TEXT NOT NULL DEFAULT 'l1',
owner_id   TEXT NOT NULL DEFAULT ''
```

Add an index covering `(owner_type, owner_id, scope_type, scope_id, status)`.

Migration rules:

- Every row present in the main memory tables when migration starts becomes owner `l1`.
- Existing `scope_type` and `scope_id` values are preserved as metadata; they do not change the L1 ownership assignment.
- No existing row is converted to `l2_group`.
- Existing L1 content hashes remain valid. New non-L1 hashes include owner type, owner ID, scope type, scope ID, and content.
- Canonical duplicate lookup includes owner and scope, so equal content in L1 or another L2 group does not suppress a write.

For an L2 group, records use:

```text
owner_type = l2_group
owner_id   = <group memory_owner_id>
scope_type = team
scope_id   = <group memory_owner_id>
```

The repeated owner/scope value is intentional: owner controls authorization; scope retains the existing memory classification contract.

### FTS

Keep the external-content `mem_fts` table and its row IDs. Change BM25 queries to join `mem_entries` and apply exact owner/scope/status/expiry predicates in SQL before `ORDER BY rank` and `LIMIT`.

This avoids an FTS rebuild solely for owner columns and prevents another owner from consuming the candidate limit.

### Vector store

Extend vector entries and the SQLite vector table with owner type, owner ID, scope type, and scope ID. Replace unrestricted `Query` use with a scoped query whose SQL `WHERE` clause runs before cosine ranking.

Every existing vector row is assigned to L1. No owner is inferred from `source_hash`, and no remote re-embedding is required by the migration.

### Knowledge graph

Rebuild KG tables transactionally with owner-aware uniqueness:

- Nodes: unique by owner and normalized name.
- Edges: unique by owner, source, target, and relation type.
- Aliases: unique within owner.
- All graph traversal methods require the bound owner.

Every existing graph node, edge, and alias in the main database is assigned to L1. The migration does not infer a different owner from `source_hash` or scope metadata. It cannot reconstruct evidence already overwritten by the old global edge uniqueness rule, and it must not invent L2 ownership.

### Hydration and mutation

Owner predicates are required for content hydration, active-hash lookup, salience updates, delete/archive, timeline reads, and consolidation mutations. A content hash alone is not authorization.

## Input Validation

All model-generated memory tool arguments are untrusted input.

- Group memory owner IDs must parse as UUIDs and must match trusted group storage.
- `Remember.content`: trim whitespace, reject empty input, enforce a hard byte/rune limit, and retain the existing durable-memory admission policy.
- `memory_type`: accept only `preference`, `decision`, `stable_fact`, or `reusable_solution`.
- `timestamp`: accept only the documented format and reject invalid calendar values.
- `RecallMemory.query`: trim whitespace, reject empty or oversized values.
- `limit`: use a small default and reject or clamp values above a fixed maximum.
- Entity lists, if exposed later, must cap count and per-entity length.
- Timeline dates, if exposed later, must use strict `YYYY-MM-DD` parsing and require `from <= to`.
- SQL remains parameterized. Owner and scope values are never concatenated into SQL.

Validation occurs in tools for useful feedback and again in the bound engine for fail-closed authorization.

## Output Encoding

Memory results are untrusted historical content. Tool output will be serialized with Go `encoding/json` rather than assembled by interpolating memory content into prose or HTML.

Each recall result includes a fixed `untrusted: true` marker and provenance metadata, but never exposes internal owner IDs. Default JSON escaping prevents memory text from becoming raw HTML in the tool result. No new frontend rendering path is introduced.

L2 prompts continue to instruct the model to ignore instructions found inside recalled content and to verify time-sensitive claims.

## Error Handling

Return stable errors without SQL, filesystem paths, group UUIDs, or stack traces:

- `memory_not_configured`
- `memory_owner_invalid`
- `memory_access_denied`
- `memory_invalid_arguments`
- `memory_unavailable`

Detailed internal errors may be logged as structured error classes, but user/tool-visible messages remain generic. A missing or invalid L2 owner never falls back to L1, global, project, or another group.

If owner backfill cannot be persisted, L2 activation fails rather than starting with an ambiguous memory capability. If the memory engine is globally disabled by configuration, L2 may run without memory tools, matching current disabled-engine behavior.

## Logging

Allowed fields:

- operation name;
- owner type;
- a one-way truncated hash of owner ID;
- result count and retrieval-pipeline counts;
- duration;
- stable error code.

Forbidden fields:

- remembered content;
- recall query or entities;
- recalled result content;
- L1 snapshot body;
- raw owner UUID;
- SQL statements containing values.

Security-denied operations are logged with the stable error code and owner hash, without the attempted content or query.

## L1 Snapshot Boundary

There is no live L1-memory reader in L2. L1 may place a minimal context snapshot into one delegated task or session context. That snapshot:

- is not written to `mem_entries` automatically;
- is visible only in the target L2 conversational context;
- is marked as untrusted historical context;
- grants no read or write authority over its L1 source;
- reaches group memory only through a later explicit L2 `Remember` decision.

## Lifecycle

- L2 session deletion removes only session/timeline state and leaves group memory intact.
- Group deletion retires its owner before or alongside team removal. Primary and derived records are archived or removed by exact owner.
- Recreating a group generates a new owner UUID and cannot see retired memory.
- Manual deletion of a group file leaves records unreachable because no active group resolves the old UUID.
- Maintenance CLI operations remain privileged, require explicit owner selection for group cleanup, and keep the existing backup/dry-run discipline.

## Dependencies

No new dependency is required. The repository already uses `github.com/google/uuid`. SQLite, FTS5, the current vector store, and the current KG implementation remain in place.

Because no dependency is added, no new dependency CVE scan is required for this feature. Existing dependency scanning remains part of the final security review.

## Implementation Slices

1. Add group `memory_owner_id`, backfill/validation, and focused team-store tests.
2. Add owner columns and the versioned primary/vector/KG migration with migration and integrity tests.
3. Add bound memory capabilities and owner-first BM25/vector/KG queries.
4. Split base and memory tool construction; bind L1 and L2 explicitly.
5. Make L3, skill forks, and cross-group peers memoryless in tools and prompts.
6. Add restart, cross-project sharing, cross-group denial, L1/L2 denial, Cron, and lifecycle tests.
7. Run focused race tests, full Go tests, vulnerability/static checks available in the repository environment, and `git diff --check`.

No frontend setting, REST field, Cron memory switch, or automatic recall mechanism is added.

## Verification and Acceptance Tests

### Ownership behavior

- Same L2 group, different sessions and projects: a write in one is recalled in the other.
- Different groups, same session/project/work directory text: no cross-read, cross-rank, cross-dedup, cross-update, or cross-delete.
- L1 cannot recall L2 memory; L2 cannot recall L1 global, project, or team memory.
- Restart and lazy L2 activation resolve the same group owner.
- Delete one L2 session and verify group memory remains.
- Delete/recreate a group and verify the new owner cannot recall retired memory.

### L3 denial

- Regular L3 has no `Remember`, `RecallMemory`, `RecallEntity`, or `MemoryTimeline` tool.
- L3 prompt contains no permanent-memory section.
- Skill forks have no memory tools or engine capability.
- L3 cannot gain memory by changing work directory, template text, tool arguments, or delegated task content.
- Returning an L3 result does not create a memory row.

### Pipeline isolation

- BM25 applies owner filtering before limit/ranking.
- Vector search applies owner filtering before top-K selection.
- KG nodes, aliases, edges, traversal, and graph context are owner-bound.
- Identical entity relationships in two groups do not overwrite each other.
- Scoped hydration, salience, archive, and delete reject mismatched owners.

### Migration and operations

- Every pre-migration primary, FTS, vector, and KG record in `soloqueue.db` is assigned to L1.
- Existing main-database records are never assigned to L2, regardless of their old scope metadata.
- `simulation.db` schema, row counts, and `agent_memories` content remain unchanged.
- Production runtime still does not wire `SimulationEngine.SetMemoryEngine`.
- `PRAGMA integrity_check` returns `ok`.
- FTS row count and trigger behavior remain consistent with active primary rows.
- Vector and KG references contain no orphaned owner mappings.
- Logs contain counts and stable codes but no inserted content or recall query.

### Commands

Implementation verification should include at least:

```bash
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/memory/... ./internal/agenttools/tools/... ./internal/agent/... ./internal/session/... ./internal/team/store/... -count=1
GOCACHE=/tmp/soloqueue-go-cache go test -race ./internal/memory/... ./internal/agenttools/tools/... ./internal/agent/... ./internal/session/... -count=1
go test ./...
git diff --check
```

Run `gosec` and `govulncheck` only if available in the environment; do not add them as runtime dependencies.
