# Memory System

**Location**: `internal/memory/` (short-term), `internal/memoryengine/` (long-term engine + embedding + vectorstore)

## Architecture Overview

```
                    Context Compaction OR /clear
                                 │
                                 ▼
                     ┌──────────────────────┐
                     │  Short-Term Memory   │
                     │ (~/.soloqueue/memory)│
                     │  daily .md files     │
                     └───────────┬──────────┘
                                 │
                         Session summaryHook
                    (writes to MemoryEngine)
                                 │
                                 ▼
            ┌────────────────────────────────────┐
            │         MemoryEngine               │
            │  (~/.soloqueue/permanent_memory/   │
            │   entries.db)                      │
            │                                    │
            │  ┌──────────┐ ┌──────────┐        │
            │  │ BM25     │ │ KG       │┌──────┐│
            │  │ (FTS5)   │ │ (graph)  ││Vector││
            │  └──────────┘ └──────────┘│(opt) ││
            │       │             │     └──────┘│
            │       └──────┬──────┘        │    │
            │              ▼               │    │
            │         RRF Fusion ◄─────────┘    │
            │              │                    │
            │              ▼                    │
            │      Ranked Results               │
            └────────────────────────────────────┘
```

SoloQueue uses a **tiered memory architecture**: short-term daily text files for recent context, and a long-term engine combining BM25 full-text search, Knowledge Graph traversal, and optional vector similarity.

The old embedding-dependent permanent memory (`internal/permanent/`, `internal/vectorstore/`) has been replaced by `internal/memoryengine/`, which packages BM25, KG, embedding, and vector storage together under one directory.

---

## Tier 1: Short-Term Memory

Short-term memory preserves a rolling log of user interactions in daily Markdown files at `~/.soloqueue/memory/YYYY-MM-DD.md`.

### Triggers
1. **Context Window Compaction**: When the context window soft waterline is exceeded, compressed daily segments are written to memory.
2. **`/clear` Command**: Current conversation segments are summarized and stored before clearing.

### Consolidation
The system uses an LLM-driven merge pipeline to prevent duplicate records:
1. Read the existing daily file.
2. Format the new conversation segment with `[YYYY-MM-DD HH:MM]` markers.
3. Send both to the LLM via `buildMergePrompt`.
4. The LLM consolidates entries, mapping timestamps into ranges (e.g., `## 14:22 — 14:35`).
5. Save atomically via temp-file-write + rename.

### Retention
- Files older than 7 days are candidates for long-term storage.
- The session builder's `summaryHook` writes compacted segments directly to the MemoryEngine.
- A `DailyMemoryFlusher` runs at midnight, flushing unpersisted messages and triggering engine consolidation.

---

## Tier 2: Memory Engine (Long-Term)

The memory engine is a **config-driven hybrid search system** that can operate in three modes:

| Mode | Config | Pipelines | Dependencies |
|---|---|---|---|
| **None** (default) | `provider = "none"` | BM25 + KG (dual-hybrid) | Zero |
| **Local ONNX** | `provider = "onnx"` | BM25 + KG + Vector (tri-hybrid) | ONNX Runtime + model file |
| **Remote API** | `provider = "openai"` | BM25 + KG + Vector (tri-hybrid) | Network + API key |

### Config

```toml
[embedding]
provider = "none"     # default: zero-dependency dual-hybrid

[embedding.onnx]
model_path = ""       # empty = auto-download intfloat/multilingual-e5-large (~560MB)

[embedding.openai]
base_url = "https://api.deepseek.com/v1"
api_key = "${DEEPSEEK_API_KEY}"
model = "text-embedding-3-small"
dimension = 1536
```

### Design Rationale

**Why not pure vector search?** Vector search handles semantic similarity but fails at exact keyword matching and cannot answer relational queries ("what do I know about project X?"). BM25 excels at keyword precision; the KG excels at relational reasoning. Together they cover each other's blind spots.

**Why embed the KG in SQLite instead of an external graph DB?** A single file with adjacency-list tables (`kg_nodes`, `kg_edges`) is zero-operational-overhead. Neo4j/ArangoDB would require separate infrastructure. For agent-scale data (tens of thousands of entities), SQLite's BFS and in-memory PPR are sufficient.

**Why agent-driven entity extraction?** The engine never calls an LLM internally. The agent extracts entities and relationships from conversation context and indexes them via the `KGIndex` tool. This avoids hidden LLM costs and gives the agent full control over what gets indexed. Same design as Kioku Lite.

### Data Model

All tables reside in the shared SQLite database (`~/.soloqueue/permanent_memory/entries.db`):

```
mem_entries  — id, content, content_hash (SHA-256, UNIQUE), date, tags,
               event_time, salience, last_recalled_at, created_at
mem_fts      — FTS5 virtual table over mem_entries(content, date)
mem_vec      — content_hash, embedding BLOB (only when provider != "none")
kg_nodes     — id, name (UNIQUE), type (open schema), mention_count,
               first_seen, last_seen, confidence
kg_edges     — id, source→target, rel_type, weight, evidence, source_hash,
               event_time, valid_from, valid_until,
               UNIQUE(source, target, rel_type)
kg_aliases   — alias → canonical entity name
```

- **content_hash** is the universal dedup key, computed as SHA-256 of content.
- **salience** implements Ebbinghaus forgetting: `salience = S0 * e^(-t / half_life)`. Recall adds 0.3 (capped at 2.0).
- **valid_from / valid_until** on edges enable temporal validity. Queries default to `valid_until IS NULL OR valid_until > now()`.

### Search Flow

```
Query string
  ├─ BM25 pipeline:  tokenize → FTS5 MATCH → normalize scores to [0,1]
  ├─ KG pipeline:    tokenize → match entity names → BFS from matches →
  │                  score by edge weight
  │                  OR if entities provided → PPR (damping=0.85, 20 iters) →
  │                  map to content_hash via incident edges
  └─ Vector pipeline: embed query → cosine similarity scan → normalize to [0,1]
       │
       ▼
  RRF Fusion (k=60): dedup by content_hash, accumulate 1/(k+rank+1) per source
       │
       ▼
  Temporal filter: exclude results with event_time > as_of or outside [from, to]
       │
       ▼
  Salience boost: score *= salience
       │
       ▼
  Hydration: bulk-fetch full content from mem_entries by content_hash
```

### Search Algorithms

**BM25** — SQLite FTS5 with `unicode61` tokenizer. Query tokens are cleaned of FTS5 special characters and individually quoted. BM25 scores are normalized to [0,1] relative to the best match in the result set.

**Knowledge Graph** — Two routing strategies:
- **Entity PPR**: When `SearchQuery.Entities` is provided, resolve entity names (through aliases), run Personalized PageRank (power iteration, damping=0.85, convergence tolerance=1e-8), accumulate source_hash scores weighted by edge weight × PPR score.
- **Token fallback**: Tokenize query, filter English+Chinese stopwords, match against entity names via `LIKE`, BFS from matches up to 2 hops, collect incident edge source_hashes.

**Vector** (optional) — Embed query via configured provider, brute-force cosine similarity scan over `mem_vec` table (same algorithm as the old system, using min-heap for top-K selection). Vector results are normalized to [0,1] before RRF fusion.

**RRF (Reciprocal Rank Fusion)** — Each result at rank position `r` in pipeline `p` receives score `1/(60 + r + 1)`. Scores accumulate across pipelines. Dedup by content_hash. Results sorted descending. RRF works with 2 or 3 input lists transparently.

### Temporal Features

1. **event_time** — when the event happened (distinct from `created_at` = when recorded). Enables timeline queries.
2. **valid_from / valid_until** on kg_edges — relationships expire. Queries default to current-valid edges.
3. **Ebbinghaus forgetting curve** — `salience(t) = S0 * e^(-t / half_life)`. Each recall boosts salience by 0.3 (capped at 2.0). Low-salience memories rank lower in search results.
4. **Time-travel** — `SearchQuery.AsOf` enables "what did I know at time T?" queries. Edge queries use temporal filtering, and memories with event_time > as_of are excluded.
5. **Timeline** — `MemoryStore.Timeline(from, to)` returns chronological entries sorted by event_time.

### Agent Tools

| Tool | Description |
|---|---|
| `Remember` | Save content to memory. Optionally include extracted entities/relations for KG indexing. |
| `RecallMemory` | Hybrid search across all configured pipelines. Returns ranked, hydrated results with scores and sources. |
| `KGIndex` | Index entities and relationships into the KG. Entity types and relation types are open schema (agent-defined). |
| `RecallEntity` | Traverse KG from an entity to find all related memories via BFS + incident edges. |
| `ConnectEntities` | Find shortest path between two entities in the KG (BFS with path tracking). |
| `MemoryTimeline` | List memories chronologically within a date range. |
| `ConsolidateMemories` | Run maintenance: edge weight decay, stale memory removal, community detection. |

### Consolidation (Daily Maintenance)

The `DailyMemoryFlusher` triggers consolidation at midnight:
1. **Edge decay**: `weight *= 0.5^(days_since_reinforced / half_life)`. Edges not reinforced naturally lose weight.
2. **Stale removal**: Delete `mem_entries` with salience < 0.1 and event_time older than 90 days.
3. **Community detection**: Connected components via BFS flood-fill on the undirected KG.

### Migration from Old System

The v13 database migration handles transitioning from the old `memories` table:
- Content is copied from `memories` to `mem_entries`.
- `content_hash` is set to `"legacy:" + id` (new entries use SHA-256).
- Old embeddings are discarded (they depended on the old embedding model).
- If a new embedding provider is configured, entries are re-embedded lazily on subsequent writes.

---

## ONNX Local Embedding

When `provider = "onnx"`, the engine uses `yalue/onnxruntime_go` (CGo wrapper for ONNX Runtime) to run `intfloat/multilingual-e5-large` in-process.

### Requirements
- **macOS**: `brew install onnxruntime`
- **Linux**: `apt install libonnxruntime-dev` or download `.so`
- **Build**: `go build` (ONNX is built in by default; requires `brew install onnxruntime`)

### ONNXEmbedder
- Loads the ONNX model file via `ort.NewAdvancedSession`.
- Implements mean pooling (sentence-transformers standard).
- L2-normalizes output vectors.
- Tokenizer loads vocabulary from HuggingFace `vocab.txt` (pure Go, no Python).
- E5 convention: prefixes queries with `"query: "` and stored content with `"passage: "`.
- Session is not thread-safe; all calls are serialized via mutex.

### Model Download
- Default model: `intfloat/multilingual-e5-large` (1024-dim, 100+ languages, ~560MB).
- Auto-downloaded from HuggingFace CDN to `~/.soloqueue/models/` on first run.
- Set `model_path` in config to use a custom model.

---

## Key Design Decisions

1. **Default is zero-dependency** — `provider = "none"` works out of the box. Users opt into embeddings by installing ONNX Runtime or configuring an API key.
2. **Single SQLite file** — all memory data (text, FTS index, KG, vectors) in one file. Simple backup, no external services.
3. **Engine never calls LLM** — entity extraction is agent-driven via tools. No hidden API costs.
4. **RRF is pipeline-agnostic** — adding or removing pipelines doesn't change the fusion logic.
5. **Salience at query-time** — no background decay job needed. Decay is computed from `last_recalled_at` during search.
6. **Open KG schema** — entity types and relation types are free-form strings defined by the agent. No hardcoded ontology.

## Trade-offs

| Aspect | Advantage | Limitation |
|---|---|---|
| BM25 vs Vector | Exact keyword match, zero deps | No semantic generalization without KG |
| KG in SQLite | Zero ops, co-located with data | No horizontal scaling, in-memory PPR |
| Agent-driven extraction | No hidden LLM costs, full control | Entity coverage depends on agent diligence |
| Salience decay | Old/unused memories fade naturally | Can lose rarely-accessed but important facts |
| Single SQLite file | Simple backup, no infra | Writer contention under high concurrency |
| ONNX local inference | Fully offline, no API costs | ~800MB RAM, requires CGo build |
