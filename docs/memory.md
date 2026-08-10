# Memory subsystem

> 中文：[记忆子系统](zh/memory.md)

I separate conversation summaries, long-term memory, and timeline events in
SoloQueue.

## Short-term conversation memory

I use internal/memory/conversation to create LLM-driven summaries when context
compaction requires them. I store summaries under the active work directory
and use them to rehydrate a long conversation without replaying every message.

## Long-term memory

I use internal/memory/engine as the shared entry point for SQLite-backed memory.
I combine BM25 full-text search with a knowledge graph and can optionally fuse
vector search when I enable an embedding provider. My default embedding
configuration is disabled, so I use BM25 and graph search without a remote
embedding API.

I perform ingestion, recall, consolidation, and salience at query time or in
the runtime. I do not require a background vector job in my default
configuration.

## Timeline

I use internal/memory/timeline to write append-only JSONL events for replay and
diagnosis, and I exclude system prompts from the user timeline. I use
[Timeline and replay](timeline.md).

## Maintenance

I use the read-only audit and reversible cleanup commands documented in
[Memory and stats](guides/memory-and-stats.md). Back up SQLite and logs before
applying a cleanup manifest.
