# Memory & Vector Store Subsystem

**Location**: `internal/memory/` (short-term), `internal/permanent/` (long-term orchestrator), `internal/vectorstore/` (vector database), `internal/sqlitedb/` (shared SQLite connection)

SoloQueue implements a tiered memory architecture comprising **Short-Term Memory** (daily text files) and **Long-Term Permanent Memory** (vector-embedded database records). This allows agents to maintain context across both recent turns and historic sessions.

---

## Architecture Overview

```
                      Context Compaction OR /clear
                                   │
                                   ▼
                       ┌──────────────────────┐
                       │  Short-Term Memory   │
                       │ (~/.soloqueue/memory)│
                       └───────────┬──────────┘
                                   │
                           Expired (>7 days)
                                   │
                                   ▼
                       ┌──────────────────────┐
                       │  Permanent Scheduler │
                       └───────────┬──────────┘
                                   │
                            LLM Summarizer
                                   │
                                   ▼
                       ┌──────────────────────┐
                       │   Vector Embedder    │
                       └───────────┬──────────┘
                                   │
                                   ▼
                       ┌──────────────────────┐
                       │  SQLite Vector Store │
                       │    (memories DB)     │
                       └──────────────────────┘
```

---

## Tier 1: Short-Term Memory

Short-term memory preserves a rolling log of user interactions, accomplishments, and file modifications in daily Markdown files located at `~/.soloqueue/memory/{date}.md`.

### Triggers
Short-term memory recording is triggered by two main events:
1. **Context Window Compaction**: When the context window soft waterline is exceeded, the compressed daily segments are written to memory.
2. **`/clear` Command**: Before the active memory is cleared, current conversation segments are summarized and stored.

### Consolidation & Merging
To prevent duplicate records and keep the logs clean, the system uses an LLM-driven merge pipeline:
1. Read the existing daily file `~/.soloqueue/memory/YYYY-MM-DD.md` (if it exists).
2. Format the new conversation segment (converting turn payloads into a chronological text stream with `[YYYY-MM-DD HH:MM]` markers).
3. Send both to the LLM with the `buildMergePrompt` instruct template.
4. The LLM consolidates entries by grouping identical tasks, mapping earliest/latest timestamps into a range (e.g. `## 14:22 — 14:35`), and returning the complete merged Markdown content.
5. Save the updated content atomically using a temporary file write followed by an OS rename operation.

---

## Tier 2: Long-Term Permanent Memory

Long-term memory is backed by embedding vectors, allowing semantic retrieval of historic interactions. It acts as an archival repository for short-term memories.

### The Migration Pipeline
A background **`Scheduler`** runs daily to migrate old memory files:
1. **File Scanning**: Scan `~/.soloqueue/memory/` for Markdown files older than 7 days (`maxAgeDays`).
2. **Segment Splitting**: For each expired file, parse the content and split it into individual entries by searching for `##` level-2 headers.
3. **LLM Summarization**: Send each entry to the LLM to compress it into a single-line factual summary (max 300 characters) to remove redundant log details and standardize length.
4. **Vector Embedding**: Send the summaries to the embedding model (`internal/embedding/`) to generate high-dimensional vectors.
5. **Database Upsert**: Write the summary, generated vector, timestamp, and source filename into the SQLite database.
6. **Cleanup**: Upon successful upsert of all entries in a file, delete the source daily short-term Markdown file.

### Scheduler & Robustness
- **Execution Period**: Every 24 hours.
- **Error Backoff**: If migration fails (e.g. network timeout calling LLM or embedding model), the scheduler executes an exponential backoff retry loop (starting at 1 minute, doubling up to a maximum of 1 hour, capping at 10 retries).
- **User Alerts**: If retries are exhausted, a notification is sent to the user via a registered callback.

---

## Vector Storage & Cosine Similarity

The vector database is implemented in `SQLiteStore` (`internal/vectorstore/sqlite_store.go`), using a local SQLite file.

### Schema
Embeddings are serialized and stored in the `memories` table:
```sql
CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    embedding BLOB NOT NULL,     -- serialized float32 slice in little-endian format
    timestamp TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT ''
);
```

### Retrieval & Query Engine
Because SQLite does not natively support high-dimensional vector search, SoloQueue implements a fast, in-memory query engine:
1. **Embedding**: The query query is sent to the embedder.
2. **L2 Normalization**: If normalization is enabled, the query vector is pre-normalized to unit length.
3. **Database Scan**: Execute a full table scan, loading the primary fields and the `embedding` BLOB.
4. **BLOB Decoding**: Deserialize the BLOB back to a `[]float32` slice using little-endian byte ordering.
5. **Similarity Metric**: Compute the cosine similarity score between the query vector and the entry vector. The pre-normalized query vector allows the dot product to be calculated in one pass (`dotAndNormB`), avoiding redundant normalization.
6. **Min-Heap Selection**: Maintain a min-heap of size $K$ (`topK`). If a row's similarity score is greater than the threshold (`minSim`) and the heap root (minimum score), it replaces the root and reheaps (executing in $O(N \log K)$).
7. **Prompt Formatting**: Return the sorted top matches, formatted as bullet points for insertion into the system prompt: `- [YYYY-MM-DD] <summary>`.

---

## Database Connection Management

Both the vector store and the todo/plan store access the same SQLite database file (`~/.soloqueue/entries.db`). SoloQueue centralizes this access in `internal/sqlitedb/`:

- **Single Connection Pool**: A shared `*sql.DB` connection pool is initialized once. WAL (Write-Ahead Logging) is enabled to allow concurrent readers.
- **Write Serialization**: SQLite permits only a single concurrent writer. The `sqlitedb.DB` structure provides a shared write mutex (`WMu`). All write queries in the vector store and todo store must acquire this mutex before executing database writes to eliminate `SQLITE_BUSY` conflicts.
- **Centralized Schema Migrations**: DB schema versions are tracked using `PRAGMA user_version`. At startup, migrations (from initial table creation to column alterations) are applied transactionally in order, preventing race conditions during bootstrap.
