# Context and Memory

English | [简体中文](zh/context-and-memory.md)

SoloQueue keeps several kinds of state with different lifetimes. The active context window is the payload for the current model turn; the timeline is the durable conversation/event history; daily conversation memory is a short-term summary; and the memory engine stores selectively admitted durable facts for retrieval.

## State layers

| Layer | Main implementation | Purpose and lifetime |
| --- | --- | --- |
| Active context | `internal/memory/ctxwin` | In-memory message sequence sent to the model; rebuilt from the timeline when a session resumes. |
| Timeline | `internal/memory/timeline` | Append-only JSONL events used for history display and context replay. |
| Conversation memory | `internal/memory/conversation` | Date-named Markdown summaries, retained for recent days. |
| Long-term memory | `internal/memory/engine` | Scoped facts and decisions indexed for semantic/keyword/entity retrieval in shared SQLite. |

These layers are related but not interchangeable. In particular, a compacted context summary is not the entire timeline, and the long-term memory engine is not a transcript archive.

## Active context window

### Message accounting and capacity

`ContextWindow` stores typed messages, timestamps, tool-call metadata, attachments, and ephemeral markers under an `RWMutex`. Token counts include message content, reasoning content, and serialized tool calls. `maxTokens` is the hard model capacity; `bufferTokens` reserves room for output; and `summaryTokens` is a softer threshold for background summarization. When no soft threshold is configured, it is derived from model size (85% for smaller windows and 75% for windows at or above the large-window threshold).

On every push, the window estimates the new message size. An oversized individual message can be truncated, and exceeding the hard input capacity triggers synchronous eviction: first middle-out truncation, then removal at turn granularity. If the soft threshold is crossed and a compactor exists, a single asynchronous compaction is scheduled. `BuildPayload` returns a copy and filters incomplete assistant-tool-call/tool-result groups before a provider request, preventing malformed tool histories from reaching the API.

The session resizes the context window after routing selects a model with a different capacity. Token calibration and payload building happen at the provider boundary so actual usage can be corrected against provider-reported usage where available.

### Compaction

Automatic compaction takes a snapshot so normal message pushes are not blocked while the LLM summarizes. It removes oversized tool output from the summary input, strips already recalled-memory blocks, groups messages by calendar date, and splits large date groups by token budget. A continuity anchor carries forward the prior compacted state so later summaries do not lose the active goal and unfinished work.

The LLM compactor uses the configured fast model, omits reasoning content, and asks for a structured state summary. It validates required headings and a non-empty current goal, strips any emitted reasoning block, and rejects invalid output. Recent (within seven days) segment summaries are merged into the active context; older segments remain available through persistence hooks. Recent invoked Skill instructions can be restored after compression so their active workflow remains available to the model.

Compression is applied only after a usable summary exists. The context window reconciles the summary with messages appended since the snapshot, then calls its summary hook outside the lock. If compaction fails or produces no valid recent summary, the current context is retained rather than replaced with empty state.

## Timeline and session recovery

Session push hooks append message events to JSONL timeline files under `logs/timelines/`. The events preserve message roles, timestamps, tool calls/results, files, request IDs, and related metadata. Control events mark boundaries such as compaction or clear. The system prompt is pushed into the in-memory context but is deliberately not written to the timeline.

When building a session, SoloQueue loads the system prompt, reads timeline segments since the most recent compact/clear boundary, and replays them into the context window. Replay skips incomplete tool-call/result groups. The Web Console can page through the same history independently of the context-window replay limit.

Clearing a session resets its active context while retaining the system prompt and records a clear boundary; it does not delete the existing timeline files. Compaction replaces older active context with a summary but preserves the durable event history.

## Conversation memory

The conversation `Manager` writes date-named Markdown under `<workdir>/memory/`. When a recent timeline segment is recorded, the fast model merges it with the existing daily file; writes use a temporary file followed by rename. Duplicate recording is controlled by a message-time cursor. The system prompt contains the memory directory path and tells the Agent to read or grep it only when relevant; it does not inline the files' contents. Old date files are cleaned up after the retention window.

For the L1 path, the context summary hook also emits one consolidated summary event to the timeline. Segments within seven days can be recorded in short-term conversation memory. Older segments stay in the timeline, while extracted durable `<memories>` candidates may be sent to the long-term engine. The L2 summary hook is timeline-oriented and does not use the L1 conversation-memory pipeline.

## Long-term memory engine

### Admission and ownership

Long-term memory is separate from the daily Markdown summaries. The engine validates candidate content, owner, scope, type, source, time fields, and replacement metadata before storing it. It canonicalizes content for deduplication; automatic candidates have a size limit, and routine task-result records are skipped unless explicitly requested by the user. Supported memory types include preferences, decisions, stable facts, and reusable solutions.

Agent tools receive an immutable `Access` capability bound to an owner and scope, rather than unrestricted access to the engine. L1 and L2-group access are bound separately; owner/scope values are applied server-side to both ingest and search. This prevents a tool from selecting an arbitrary owner by editing its query arguments.

### Retrieval

The hybrid searcher runs BM25, knowledge-graph, and (when configured) vector search concurrently. It fuses ranked lists with Reciprocal Rank Fusion, hydrates results from the authoritative memory store, filters by time, lifecycle status, and scope, applies query-time salience decay, and trims to the requested limit. Entity queries may also return relevant graph edges.

The graph and BM25 search run without an embedding provider. Vector retrieval is optional: runtime construction enables it only when an embedding provider/model is configured and available. Vector search fails closed when the store cannot enforce owner/scope filters. Shared SQLite stores memory entries and graph records; optional vector entries use the configured vector table in that shared database.

## Storage and code map

- `<workdir>/logs/timelines/`: append-only session events and replay boundaries.
- `<workdir>/memory/`: recent Markdown conversation summaries.
- `<workdir>/soloqueue.db`: shared SQLite data for the engine and other runtime subsystems.
- [`internal/memory/ctxwin`](../internal/memory/ctxwin): token window, eviction, compaction, and payload sanitization.
- [`internal/memory/timeline`](../internal/memory/timeline): JSONL writing, reading, and replay.
- [`internal/memory/conversation`](../internal/memory/conversation): daily summary merge and retention.
- [`internal/memory/engine`](../internal/memory/engine): candidate policy, scopes, hybrid retrieval, and lifecycle.
- [`internal/runtime/build_memory.go`](../internal/runtime/build_memory.go) and [`internal/session/builder.go`](../internal/session/builder.go): construct and connect these layers.
