# Memory subsystem

SoloQueue separates conversation summaries, long-term memory, and timeline
events.

## Short-term conversation memory

internal/memory/conversation creates LLM-driven summaries when context
compaction requires them. Summaries are stored under the active work directory
and can be used to rehydrate a long conversation without replaying every
message.

## Long-term memory

internal/memory/engine is the shared entry point for SQLite-backed memory. It
combines BM25 full-text search with a knowledge graph and can optionally fuse
vector search when an embedding provider is enabled. The default embedding
configuration is disabled, so BM25 and graph search work without a remote
embedding API.

Ingestion, recall, consolidation, and salience are query-time/runtime
operations. There is no requirement for a background vector job in the default
configuration.

## Timeline

internal/memory/timeline writes append-only JSONL events for replay and
diagnosis. System prompts are excluded from the user timeline. See
[Timeline and replay](timeline.md).

## Maintenance

Use the read-only audit and reversible cleanup commands documented in
[Memory and stats](guides/memory-and-stats.md). Back up SQLite and logs before
applying a cleanup manifest.
