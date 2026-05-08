# Auto-Clear Idle Context on Startup — Design

## Problem

When the TUI starts up, it replays the full conversation history into both the TUI display and the LLM context window. If the user resumes after a long gap (e.g., 1 hour+), the old context is still sent to the LLM, wasting tokens on irrelevant history. There is currently no mechanism to detect idle gaps and clear stale context on startup.

## Approach

Add a configurable idle threshold (`contextIdleThresholdMin`, default 30 minutes). On startup, after receiving `SandboxInitMsg` but before replaying history to the TUI:
1. Check the timestamp of the last message in the context window
2. If the time gap exceeds the threshold AND the context window has enough tokens (>= dynamically calculated `minTokens`), silently clear the LLM context (preserving the system prompt) and trigger the memory hook for short-term memory storage
3. If token count is small (< `minTokens`), skip clearing — the cost of memory hook (8k+ tokens) exceeds the savings

History is still replayed to the TUI for display, but marked as `isHistory = true` and rendered with a muted style.

### Token Threshold Design

**Problem**: Fixed threshold (e.g., 8192) is unsuitable for large-context models (200k-1M). A 1M-context model may have 50k-200k tokens after multiple compressions, and 8192 is too low (almost always clears).

**Solution**: Dynamic `minTokens` calculation:
```
minTokens = cw.SummaryTokens() * 1 / 100
Clamp to [4096, 16384]
```

**Rationale**:
- `summaryTokens` scales with `maxTokens` (75% for >=512k, 85% for <512k)
- 1% of `summaryTokens` gives a reasonable threshold:
  - 32k model: `summaryTokens` = 27k → minTokens = 4096 (clamped)
  - 200k model: `summaryTokens` = 170k → minTokens = 1700 (still low)
  - 1M model: `summaryTokens` = 750k → minTokens = 7500
- Clamping prevents extremes: very small models get 4096, very large models cap at 16384

**Memory hook cost**: The `Record()` call consumes ~8k tokens (conversationText + existing + prompt + output). Clearing only makes sense when the saved tokens > 8k.

### Key Changes

1. **`internal/config/schema.go`** — Add `ContextIdleThresholdMin int` field to `SessionConfig`:
   ```go
   type SessionConfig struct {
       TimelineMaxFileMB      int `json:"timelineMaxFileMB"`
       TimelineMaxFiles       int `json:"timelineMaxFiles"`
       ContextIdleThresholdMin int `json:"contextIdleThresholdMin"` // default 30 (minutes)
   }
   ```
   - Default value set in `internal/config/defaults.go`: `ContextIdleThresholdMin: 30,`

2. **`internal/ctxwin/ctxwin.go`** — Add two getter methods to `ContextWindow`:
   - `func (cw *ContextWindow) CurrentTokens() int`: returns `cw.currentTokens` (read-locked).
   - `func (cw *ContextWindow) SummaryTokens() int`: returns `cw.summaryTokens` (read-locked).

3. **`internal/session/session.go`** — Add three methods:
   - `LastMessageTime() time.Time`: returns the timestamp of the last non-system message from `cw.messages`. Returns `time.Time{}` if no messages exist.
   - `ShouldClearContext(idleTimeout time.Duration, minTokens int) bool`: checks if (a) the last message time is older than `idleTimeout` AND (b) `cw.currentTokens >= minTokens`. Returns true only if BOTH conditions are met.
   - `ClearSilent() error`: clears the context window (calls `cw.Reset()`), triggers `memoryHook` if set, but does NOT write a control event to the timeline. This is the "silent" clear used on startup.

4. **`internal/tui/app.go`** — Modify the `message` struct to add `isHistory bool` field. In `Update()` when handling `SandboxInitMsg`, check the idle threshold:
   - Read `contextIdleThresholdMin` from config (via `cfg.Session`)
   - Calculate timeout: `timeout := time.Duration(configVal) * time.Minute`
   - Calculate `minTokens`: `cw.SummaryTokens() * 1 / 100`, clamped to [4096, 16384]
   - Call `session.LastMessageTime()`; if gap > timeout, proceed to token check
   - Call `session.ShouldClearContext(timeout, minTokens)`:
     - If returns true: call `session.ClearSilent()` and set `m.contextCleared = true`
     - If returns false: skip clear (context is short or recent)
   - Proceed to `replayHistoryIntoMessages()` as normal
   - If `m.contextCleared`, also append a system notification message to `m.messages` informing the user that context was cleared to save tokens

5. **`internal/tui/history_replay.go`** — Modify `loadMessagesFromHistory()` to accept an `isHistory bool` parameter and set `msg.isHistory = isHistory` for all messages. When called from `replayHistoryIntoMessages()`, pass `isHistory = m.contextCleared` (i.e., mark as history if context was cleared).

6. **`internal/tui/render.go`** — Modify `renderUserMessage()` and `renderMessage()` to check `msg.isHistory`. If `true`, render the message content with a muted/dimmed style (gray text) to visually distinguish stale history from the fresh conversation.

### Data Flow

```
TUI startup
  → SandboxInitMsg received
  → Read contextIdleThresholdMin from config (default 30)
  → Calculate timeout: timeout = time.Duration(configVal) * time.Minute
  → Calculate minTokens: max(4096, min(16384, cw.SummaryTokens() * 1 / 100))
  → Call session.LastMessageTime()
  → If lastMsgTime is zero or recent (< timeout):
      → Skip clear, proceed to replay
  → If lastMsgTime is old (> timeout):
      → Call session.ShouldClearContext(timeout, minTokens)
      → If shouldClear == false (tokens < minTokens):
          → Skip clear (context is short, no need to clear)
      → If shouldClear == true:
          → Call session.ClearSilent()
          → Set m.contextCleared = true
  → replayHistoryIntoMessages() with isHistory = contextCleared
  → (if cleared) Append system notification to messages
  → User sees grayed-out history + notification
  → Next user message: LLM context is fresh (no stale tokens)
```

## Dependencies

- Existing: `internal/config` (for reading `SessionConfig`), `internal/session` (for `Session.ClearSilent()`, `LastMessageTime()`, `ShouldClearContext()`), `internal/ctxwin` (for `CurrentTokens()`, `SummaryTokens()`), `charm.land/lipgloss/v2` (for muted text styling in TUI)
- New: None

## Test Cases

### `internal/ctxwin/ctxwin_test.go`
- [ ] `TestCurrentTokens_ReturnsZeroForEmptyWindow`
- [ ] `TestCurrentTokens_ReturnsCorrectValue`
- [ ] `TestSummaryTokens_ReturnsConfiguredValue`

### `internal/session/session_test.go`
- [ ] `TestSession_LastMessageTime_ReturnsZeroForEmptySession`
- [ ] `TestSession_LastMessageTime_ReturnsLastNonSystemMessageTime`
- [ ] `TestSession_ShouldClearContext_ReturnsFalseForEmptySession`
- [ ] `TestSession_ShouldClearContext_ReturnsFalseForRecentMessage`
- [ ] `TestSession_ShouldClearContext_ReturnsFalseForLowTokenCount`
- [ ] `TestSession_ShouldClearContext_ReturnsTrueForOldMessageAndHighTokens`
- [ ] `TestSession_ClearSilent_DoesNotWriteToTimeline`
- [ ] `TestSession_ClearSilent_TriggersMemoryHook`
- [ ] `TestSession_ClearSilent_PreservesSystemPrompt`

### `internal/tui/history_replay_test.go`
- [ ] `TestLoadMessagesFromHistory_MarksMessagesAsHistory`
- [ ] `TestLoadMessagesFromHistory_DoesNotMarkWhenIsHistoryFalse`

### `internal/tui/render_test.go`
- [ ] `TestRenderMessage_RendersHistoryWithMutedStyle`
- [ ] `TestRenderUserMessage_RendersHistoryWithMutedStyle`

### Integration tests (manual or using TUI test infrastructure)
- [ ] `TestStartup_IdleTimeoutExceeded_ClearsContext` (TUI startup with idle session, verifies context cleared)
- [ ] `TestStartup_IdleTimeoutNotExceeded_KeepsContext` (TUI startup with recent session, verifies context kept)
- [ ] `TestStartup_LowTokenCount_NoClear` (TUI startup with old session but low tokens, verifies no clear)

## Explicitly Out of Scope

- Auto-clearing context during an active session (only on startup)
- Config hot-reloading for `contextIdleThresholdMin` (requires restart)
- Any changes to the memory hook signature or behavior (reuse as-is)
- Persistent storage of short-term memory (handled by the existing `MemoryHook` callback)
- Adjusting `maxTokens` in `memory.go mergeAndSummarize()` (separate issue, out of scope for this design)
