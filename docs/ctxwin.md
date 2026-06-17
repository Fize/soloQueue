# Context Window & Truncation Subsystem

**Location**: `internal/ctxwin/` (context window manager), `internal/compactor/` (LLM-based compressor)

The Context Window is an in-memory, rule-based context manager that tracks chat messages, counts active tokens using `tiktoken`, and manages size limits using a two-stage eviction policy and segmented async compression.

---

## Core Components

### 1. `ContextWindow` (`ctxwin.go`)

Maintains the list of active messages, current token estimation, and configuration limits. It is protected by a read-write lock (`sync.RWMutex`) to allow concurrent reads (e.g., preparing API payloads) and asynchronous writes (e.g., background compaction).

- **`maxTokens`**: Hard waterline representing the physical context window limit (e.g., 64k or 128k).
- **`bufferTokens`**: Reserved headroom for model generation/output (e.g., 8k).
- **`summaryTokens`**: Soft waterline that triggers background context compression (typically 75% or 85% of `maxTokens`).

### 2. `Tokenizer` (`token.go`)

Handles BPE token estimation using `tiktoken` (specifically supporting OpenAI/DeepSeek-compatible vocabularies).

### 3. `Compactor` (`compactor.go` & `internal/compactor/`)

An interface for LLM-based summary generation. The `LLMCompactor` translates context history to compression prompts, asks a cheap model to summarize it, and returns the result.

---

## Concurrency & Calibration

### Token Count Calibration

Since token estimation is performed client-side using approximate rules, the total count can drift from the actual tokens consumed by the provider API.
To prevent this, the `ContextWindow` exposes a **`Calibrate(promptTokens int)`** method:

1. When the LLM returns its usage stats (e.g., `PromptTokens` in `EventDone`), `Calibrate` is called.
2. It sets `currentTokens` to the exact value.
3. It redistributes the difference proportionally across all existing messages so that the sum of message tokens matches the exact total.
4. **Ordering Rule**: Calibration must run _before_ pushing subsequent assistant replies or tool results to ensure that FIFO eviction calculations subtract accurate numbers.

### Incomplete Tool-Call Filtering (`filterCompletePairs`)

When constructing the API payload (`BuildPayload`), the system scans and filters out incomplete tool-calling sequences to prevent HTTP 400 errors from LLM providers:

- **Assistant-Tool Matching**: If an assistant message contains `tool_calls` but the subsequent matching `tool` results are missing (e.g., if a worker crashed or was cancelled), the assistant `tool_calls` message is omitted.
- **Orphaned Tool Results**: Any `tool` result message without a matching prior assistant `tool_calls` is filtered out.
- **Strict Structural Order**: Enforces that all tool results must immediately follow their assistant `tool_calls` message without interleaved user/assistant turns. If a violation is detected, the entire transaction is truncated.

---

## Eviction Policy & Truncation Flow

If pushing a message causes the context window to exceed its capacity (`maxTokens - bufferTokens`), the system runs a synchronous three-stage eviction sequence (`evictTo`):

```
                       Exceed Hard Waterline
                                │
                                ▼
             ┌────────────────────────────────────┐
             │ 1. Prune Ephemeral Tool Content    │
             │    - Keep recent turns protected   │
             │    - Replace older large fields    │
             └────────────────#───────────────────┘
                               │ Still Over limit?
                               ▼
             ┌────────────────────────────────────┐
             │ 2. Truncate Middle-Out             │
             │    - Keep JSON skeletons           │
             │    - Skip middle array elements    │
             └────────────────#───────────────────┘
                               │ Still Over limit?
                               ▼
             ┌────────────────────────────────────┐
             │ 3. Slide FIFO (Turn Granularity)   │
             │    - Evict oldest complete Turns   │
             │    - Clean orphaned tool messages  │
             └────────────────────────────────────┘
```

### Stage 1: Prune Ephemeral Content (`pruneOlderTurnsEphemeralContent`)

- Identifies a boundary of protected recent turns (default: 2 turns).
- For turns _before_ the boundary, if a message is marked `IsEphemeral` (e.g., raw files or terminal outputs), the system strips its content:
  - If the content is valid JSON, it traverses the keys, matching those in `largeFields` (like `content`, `stdout`, `stderr`, `body`, `data`), and replaces them with `"[evicted]"`.
  - If the content is raw text, it replaces the entire body with `"[evicted to save space]"`.
- Re-calculates message tokens and updates the context window size.

### Stage 2: JSON-Aware Middle-Out Truncation (`truncateMiddleOut`)

If still over capacity, the system scans remaining ephemeral messages and applies skeleton-preserving truncation:

- **JSON Object Truncation**: Keys in `largeFields` containing long strings are truncated using character-level middle-out truncation, keeping the outer JSON syntax intact.
- **JSON Array Truncation**: Arrays exceeding 10 elements are trimmed, preserving the first 10% and last 20% of elements while inserting `"[...omitted N elements...]"` in the middle.
- **Fallback**: If JSON parsing fails, falls back to direct character-level middle-out truncation (`charLevelTruncate`, keeping 10% head and 20% tail).

### Stage 3: Turn-Granularity FIFO (`slideFIFO`)

If the token count still exceeds the limit, the system drops the oldest conversation history in units of complete **Turns**:

- **Turn Definition**: Starts with a `user` message and ends right before the next `user` message.
- **Safety Invariant**:
  - The system prompt (index 0) is never evicted.
  - When a turn is evicted, any subsequent `tool` messages in later turns that refer to the evicted turn's `tool_call_ids` (orphaned tools) are automatically purged to prevent API errors.
  - **Aggressive Fallback**: If only one turn remains, it cannot be fully deleted. Instead, the system applies aggressive character truncation (keeping 5% head + 5% tail) to its ephemeral messages, then long messages, or replaces the longest message entirely with a placeholder.

---

## Segmented Compaction (`compactSegments`)

When `currentTokens` exceeds the soft waterline (`summaryTokens`), the system asynchronously triggers background compaction to compress conversation history:

1. **Oversized Filter**: Drops tool outputs longer than 2000 runes since the compactor only needs to know "what tool was called" rather than processing mega-bytes of raw payload.
2. **Date Grouping**: Group all historical messages (excluding system prompt) by calendar date and sort oldest-first.
3. **Daily Compaction**: For each date group:
   - If the group exceeds the soft waterline, split it into sub-batches.
   - Run the LLM compactor to summarize each sub-batch/group into a single daily summary segment.
4. **Summary Merging**:
   - Collect compressed segments within the last 7 days.
   - Perform a second-pass LLM merge call (`mergeSummaries`) to combine daily summaries into a single unified summary.
   - If the merge fails, concatenate summaries using `---` separators as a fallback.
5. **Context Replacement**:
   - Replace all conversation history in the active `ContextWindow` with a single system message: `[Conversation Summary]\n<merged_summary>`.
6. **Hook Persistence**:
   - Call `summaryHook` with all generated segments (including those older than 7 days) to persist them in short-term daily memory files or vector stores.
