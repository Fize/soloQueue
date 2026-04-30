# SoloQueue Logging System Evaluation

**Date:** April 30, 2026  
**Scope:** Complete evaluation of logging architecture, coverage, consistency, and gaps

---

## 1. LOGGER ARCHITECTURE

### 1.1 Core Design

**File:** `internal/logger/logger.go`

The logging system implements a **three-layer hierarchical architecture**:

- **Layer:** System, Team, Session (each with isolated log directories)
- **Category:** CatApp, CatLLM, CatHTTP, CatWS, CatTeam, CatAgent, CatActor, CatTool, CatMessages, CatConfig
- **Handlers:** MultiHandler (combines console + file), FileHandler (per-category rotation)

**Strengths:**
- ✅ Clean abstraction with Layer + Category constraints enforced via `ValidCategory()`
- ✅ Built on Go's standard `log/slog` library (modern, fast, structured)
- ✅ Rotation support: separate files per category, configurable size/days
- ✅ Three factory functions: `System()`, `Team()`, `Session()`
- ✅ Context-aware logging: automatic trace_id/actor_id extraction from context
- ✅ Utility methods: `WithTraceID()`, `NewTraceID()`, `LogDuration()`, `LogError()`

**Architecture Details:**
- **Logger struct** (logger.go:51-58):
  - `inner`: underlying `*slog.Logger`
  - `layer`: Layer designation (System/Team/Session)
  - `baseDir`, `teamID`, `sessionID`: hierarchical identifiers
  - `handler`: MultiHandler for delegation
  
- **LogDir Structure**:
  - System: `{baseDir}/logs/system/`
  - Team: `{baseDir}/logs/teams/{teamID}/`
  - Session: `{baseDir}/logs/sessions/{teamID}/{sessionID}/`

- **Category Mapping** (layer.go):
  - System layer: CatApp, CatConfig, CatHTTP, CatWS, CatLLM (client)
  - Team layer: CatTeam, CatAgent
  - Session layer: CatLLM (agent), CatActor, CatTool, CatMessages
  - CatLLM mapped to both System (deepseek client) and Session (agent use)

### 1.2 Handler Implementation

**File:** `internal/logger/handler.go`

- **MultiHandler**: Routes logs to console + file simultaneously
- **FileHandler**: 
  - Lazy-creates per-category `rotateWriter`
  - Validates category legality (fallback to default)
  - Builds JSONL entries with top-level fields (ts, level, layer, category, msg, team_id, session_id, trace_id, actor_id, duration_ms, err)
  - Stores custom fields in `ctx` sub-object
  - Thread-safe via `writerPool` with mutex

**Output Format:** JSONL (JSON Lines)
```json
{
  "ts": "2026-04-30T15:30:45.123456Z",
  "level": "INFO",
  "layer": "session",
  "category": "llm",
  "msg": "deepseek chat done",
  "trace_id": "a1b2c3d4",
  "actor_id": "agent_1",
  "duration_ms": 2341,
  "ctx": { "model": "deepseek-v4-pro", "tokens_used": 1234 }
}
```

### 1.3 Log Levels

**File:** `internal/logger/level.go`

- Parsing: `ParseLogLevel()` converts strings to `slog.Level`
- Supported: DEBUG, INFO, WARN, ERROR (case-insensitive)
- Default: INFO

---

## 2. CATEGORY USAGE ANALYSIS

### 2.1 Defined Categories

| Category | Layers | Primary | Status |
|----------|--------|---------|--------|
| **CatApp** | System | System | ✅ Used (HTTP handlers) |
| **CatConfig** | System | System | ❌ **NOT USED** - no logging in config loader |
| **CatHTTP** | System | System | ✅ Used (server.go, 10+ logging calls) |
| **CatWS** | System | System | ❌ **NOT USED** - WebSocket logs use CatHTTP |
| **CatLLM** | System, Session | Session | ✅ Used (LLM client + agent) |
| **CatTeam** | Team | Team | ❌ **NOT USED** - no logging context for team ops |
| **CatAgent** | Team | Team | ✅ Used (factory, supervisor) |
| **CatActor** | Session | Session | ✅ Used (agent lifecycle, supervisor, registry) |
| **CatTool** | Session | Session | ✅ Used (tool execution pipeline) |
| **CatMessages** | Session | Session | ❌ **NOT USED** - no logging for message assembly |

### 2.2 Usage Patterns

**High Coverage:**
- **CatActor**: Agent lifecycle (Start/Stop, 8+ calls), supervisor, registry
- **CatLLM**: LLM client (deepseek Chat, streaming, retries), agent error handling
- **CatTool**: Tool execution (start/done, errors), confirmation flow
- **CatHTTP**: HTTP server (session create/delete, WebSocket connect/disconnect, error handling)

**No Coverage:**
- **CatConfig**: Config loader has no logging for load success/failure/hot-reload
- **CatMessages**: Message assembly (`internal/ctxwin/`) has no logging
- **CatTeam**: No team-level operations are logged
- **CatWS**: WebSocket uses CatHTTP instead of dedicated CatWS

---

## 3. SUBSYSTEM-BY-SUBSYSTEM COVERAGE ANALYSIS

### 3.1 Router/Classifier (`internal/router/`)

**Logging Status:** ⚠️ **MIXED - Architecture Inconsistency**

**Files:**
- `classifier.go`: DefaultClassifier uses `*slog.Logger` (NOT `*logger.Logger`)
- `router.go`: Router uses `*slog.Logger` (NOT `*logger.Logger`)
- `llm_classifier.go`: LLMClassifier uses `*slog.Logger` (NOT `*logger.Logger`)

**Current Logging:**
```
✅ DefaultClassifier.Classify():
   - Fast-track classification (line 75)
   - Classification complete messages (3 variants: lines 83, 92, 115, 128)
   - LLM fallback invocation (line 99)
   - LLM classifier error + fallback (line 107)

✅ Router.Route():
   - Routing decision made (line 106, debug level)
   - Model not found warning (line 150, warn level)
   
✅ LLMClassifier.Classify():
   - Cache hit (line 95)
   - Classifier failed, fallback (line 123)
   - Classification complete (line 136)
```

**Coverage Gaps:**
- ❌ No INFO-level logging at decision points (only DEBUG)
- ❌ No logging of which model selected or thinking config
- ❌ No error logging if classification fails entirely
- ❌ No timing/duration metrics logged
- ❌ Confidence scores not logged at INFO level for debugging routing issues
- ❌ No CatApp category used despite being system-layer

**Architecture Problem:**
Router package uses raw `*slog.Logger` instead of `*logger.Logger`. This means:
- Cannot use structured categories (CatHTTP, CatApp, etc.)
- Cannot leverage trace_id/actor_id injection from context
- Logs go directly to slog, bypassing project's rotation/handler setup
- **Inconsistent with rest of codebase** which uses `*logger.Logger`

**Recommendation:** Router should accept `*logger.Logger` and use CatApp category.

---

### 3.2 Session Layer (`internal/session/`)

**Logging Status:** ❌ **CRITICAL GAPS**

**Files:**
- `session.go`: Core Session struct (CREATE, DELETE, lifecycle)
- `session.go`: SessionManager (cleanup, reaping)

**Current Logging:**
- Zero logging in entire session package (0 calls found)
- Session.Ask() has no logging of invocation, success, or failure
- Session.AskStream() has no logging
- SessionManager lifecycle has no logging
- Idle cleanup/reaping has no logging

**Missing Critical Flows:**
- ❌ Session creation (Session.Create should log with trace_id)
- ❌ Session deletion (Session.Delete should log)
- ❌ Session Ask start/done (entry/exit of Ask method)
- ❌ Session busy errors (ErrSessionBusy not logged)
- ❌ Context window state changes
- ❌ SessionManager idle detection/reaping
- ❌ Timeline writer state

**Severity:** HIGH - Session is the core request/response abstraction. No visibility into session lifecycle.

---

### 3.3 Agent Lifecycle (`internal/agent/`)

**Logging Status:** ✅ **GOOD** - Mostly complete but some gaps

**Agent Lifecycle:**
```
✅ Start (lifecycle.go:53):
   - "agent started" (INFO, CatActor)
   - Logs: kind, role, model_id, mailbox_cap, priority_mailbox

✅ Stop (lifecycle.go:85-106):
   - "agent stop requested" (INFO, CatActor, timeout_ms)
   - "agent stopped" (INFO, CatActor, wait_ms)
   - "agent stop timeout" (ERROR, CatActor, ErrStopTimeout)
```

**Agent Execution:**
```
✅ LLM Streaming (stream.go:229-259):
   - "llm chat start" (INFO, CatLLM)
   - "llm chat failed" (ERROR, CatLLM)
   - "llm chat done" (INFO, CatLLM, duration_ms)
   - Max iterations exceeded (ERROR, CatLLM)

❌ **Missing:**
   - No logging of Ask/AskStream entry
   - No logging of job submission
   - No logging of context window state
   - No logging of message building (ctxwin)
   - No logging of compaction/overflow

✅ Tool Execution (stream.go:589-789):
   - "tool not found" (ERROR, CatTool)
   - "tool exec start" (INFO, CatTool, tool_name, call_id, arg_len)
   - "tool exec done" (INFO, CatTool, duration_ms, result_len)
   - "tool exec failed" (ERROR, CatTool)
   - Confirmation flow (no explicit logging but events emitted)

❌ **Missing:**
   - No logging of tool confirmation requests
   - No logging of confirmation choices (approved/denied/session-whitelist)
   - No timing for confirmation wait
   - No logging of async tool delegation
```

**Agent Lifecycle Runners:**
```
✅ Run goroutines (run.go):
   - "agent run goroutine panic" (ERROR, CatActor)
   - "agent run loop exit" (INFO, CatActor)

✅ Registry (registry.go):
   - "registry register" (INFO, CatActor)
   - "registry unregister" (INFO, CatActor)
   - "registry start all / stop all / shutdown" (INFO, CatActor)

✅ Supervisor (supervisor.go):
   - "supervisor spawned child" (INFO, CatActor)
   - "supervisor stop child failed" (ERROR, CatActor)
   - "supervisor reaped child" (INFO, CatActor)
```

**Coverage Assessment:**
- Agent lifecycle is well-logged
- Tool execution is logged at key points
- **Gap:** Ask/AskStream entry/exit not logged (causes blind spots in tracing)
- **Gap:** Message building and context window state not tracked
- **Gap:** Confirmation flow not logged explicitly

---

### 3.4 Tools (`internal/tools/`)

**Logging Status:** ⚠️ **PARTIAL** - Core logging present, gaps in delegate and error paths

**General Tool Execution:**
```
✅ Tool registry (registry.go):
   - No direct logging calls, but uses helpers

✅ Common helpers (agent/helpers.go):
   - logInfo() wrapper (line 49)
   - logError() wrapper (line 57)
```

**Delegate Tool (`internal/tools/delegate.go`):**
```
❌ **CRITICAL GAPS - NO LOGGING**
   - No logging of delegation start
   - No logging of target agent location/spawn
   - No logging of timeout application
   - No logging of delegation completion
   - No logging of delegation failure
   - No logging of timeout expiration
   - No logging of event relay status
   - No logging of confirmation forwarding
   
   Lines 95-205 (Execute method):
   - All error returns use fmt.Sprintf, not logging
   - Event consumption loop (line 159-198) has no observability
   - Delegation to SpawnFn has no logging
   - Locator lookup has no logging
   
❌ Lines 243-277 (ExecuteAsync):
   - AsyncExecute has no logging of async task spawning
   - No logging of task state tracking
   - No logging of agent spawning
```

**Severity:** CRITICAL for L1->L2 debugging. Delegation is async and opaque.

**Other Tools:**
- `shell_exec.go`: No tool-specific logging (returns errors as strings to LLM)
- `file_read.go`: No logging (returns errors to LLM)
- `write_file.go`: No logging (returns errors to LLM)
- `http_fetch.go`: No logging (returns errors to LLM)
- `web_search.go`: No logging (returns errors to LLM)
- `multi_replace.go`: No logging (returns errors to LLM)
- `multi_write.go`: No logging (returns errors to LLM)

**Philosophy:** Tools return errors as strings to LLM, not logged. Acceptable for simple tools, but:
- ❌ No system-level metrics (execution count, error rate, timing)
- ❌ No debugging when LLM tool calls go wrong
- ⚠️ Tool logs would help understand failure modes

---

### 3.5 Config (`internal/config/`)

**Logging Status:** ❌ **ZERO LOGGING**

**Files:**
- `loader.go`: `Loader[T]` with file watching and hot-reload
- `service.go`: `ConfigService` wrapping multiple loaders
- `schema.go`: Schema validation

**Missing Logging:**
```
❌ Config load/parse (loader.go):
   - No logging of initial load
   - No logging of parse errors
   - No logging of file watch activation
   - No logging of hot-reload detection
   - No logging of hot-reload application
   - No logging of callback invocation
   - No logging of error handler invocation

❌ ConfigService (service.go):
   - No logging of any operations
   - No logging of Get calls
   - No logging of OnChange subscription
```

**Severity:** HIGH - Config changes are critical for operations, no visibility.

**Example Missing:** When config hot-reloads, no log entry. When parsing fails, no log entry.

---

### 3.6 LLM Client (`internal/llm/deepseek/`)

**Logging Status:** ✅ **GOOD** - Solid coverage

**Files:**
- `client.go`: Main LLM client

**Current Logging:**
```
✅ Chat/ChatStream:
   - "deepseek chat start" (INFO, CatLLM, line 353)
   - Request details: model, max_tokens, temp, top_p
   
   - "deepseek: " + msg (ERROR, CatLLM, line 365)
   - Error details: code, err
   
   - "deepseek retry" (WARN, CatLLM, line 389)
   - Retry details: attempt, next_delay_ms
   
   - "deepseek chat done" (INFO, CatLLM, line 397)
   - Response details: tokens_used, finish_reason
   
   - "deepseek chat failed" (ERROR, CatLLM, line 418)
   - Error type: api_error, network_error, etc.
```

**Coverage Assessment:**
- ✅ Request start/end logged
- ✅ Retry attempts logged
- ✅ Errors logged with context
- ❌ Stream chunks not logged (high volume, understandable)
- ❌ Token usage not logged at INFO level (only in done event)
- ✅ Uses CatLLM correctly for system-layer client

---

### 3.7 Context Window (`internal/ctxwin/`)

**Logging Status:** ❌ **NO LOGGING**

**Files:**
- `ctxwin.go`: Core ContextWindow struct
- `compactor.go`: LLM-based compaction
- `truncate.go`: Truncation logic

**Missing Logging:**
```
❌ Context Window lifecycle:
   - No logging of ContextWindow creation
   - No logging of message Push
   - No logging of BuildPayload
   - No logging of Calibrate

❌ Compaction (`compactor.go`):
   - No logging of compaction start
   - No logging of messages selected for compaction
   - No logging of compaction result (tokens saved)
   - No logging of compaction errors

❌ Overflow handling:
   - No logging when context window exceeds max
   - No logging of which messages are truncated
   - No logging of truncation strategy applied
   - No logging of token count reduction
```

**Severity:** HIGH - Compaction is complex and can silently fail or produce poor results.

---

### 3.8 HTTP Server (`internal/server/`)

**Logging Status:** ✅ **GOOD** - Solid HTTP logging

**Files:**
- `server.go`: HTTP handlers

**Current Logging:**
```
✅ Session management:
   - "session created" (INFO, CatHTTP, line 118)
   - "session deleted" (INFO, CatHTTP, line 136)

✅ WebSocket:
   - "ws accept failed" (ERROR, CatHTTP, line 196)
   - "ws connected" (INFO, CatHTTP, line 206)
   - "ws disconnected" (INFO, CatHTTP, line 207)
   - "ws write error" (ERROR, CatHTTP, line 225)
   - "ws write pong error" (ERROR, CatHTTP, line 233)
   - "ws write cancel error" (ERROR, CatHTTP, line 241)

✅ Routing:
   - "routing classification failed" (INFO, CatHTTP, line 259)
   - "routing decision" (INFO, CatHTTP, line 264, includes level + model)

✅ Error handling:
   - "panic in handler" (ERROR, CatHTTP, line 81)
   - logInfo/logError helpers wrap logger calls (line 439, 447)
```

**Coverage Assessment:**
- ✅ HTTP lifecycle well-logged
- ✅ Errors consistently logged
- ✅ Uses CatHTTP correctly
- ❌ No HTTP request method/path logging (only specific events)
- ❌ No response code/latency logging

---

## 4. INCONSISTENCIES & ANTI-PATTERNS

### 4.1 **CRITICAL: Router Uses Raw `*slog.Logger` Instead of `*logger.Logger`**

**Files Affected:**
- `internal/router/classifier.go` (line 21)
- `internal/router/router.go` (line 26)
- `internal/router/llm_classifier.go` (line 60)

**Problem:**
```go
// ❌ Router pattern (WRONG)
type DefaultClassifier struct {
    logger *slog.Logger  // Raw slog, no categories
}

dc.logger.DebugContext(ctx, "classification complete", ...)

// ✅ Agent pattern (CORRECT)
type Agent struct {
    Log *logger.Logger  // Project logger with categories
}

a.logInfo(ctx, logger.CatActor, "agent started", ...)
```

**Consequences:**
1. Cannot use CatApp/CatHTTP for system-layer logging
2. Cannot leverage trace_id/actor_id context injection
3. Logs bypass project's rotation/categorization
4. Inconsistent with rest of codebase (agent, server, etc.)
5. Debug logging has no category, making filtering impossible

**Fix Required:** Change Router to use `*logger.Logger`.

---

### 4.2 Session Has Zero Logging

**Critical Gap:** Session is the user-facing entry point. No visibility into:
- Ask() entry/exit
- Session busy errors
- Timeline/context window state

**Example:**
```go
// ❌ No logging
func (s *Session) Ask(ctx context.Context, prompt string) (string, error) {
    if !s.inFlight.CompareAndSwap(0, 1) {
        return "", ErrSessionBusy  // ❌ Not logged
    }
    defer s.inFlight.Store(0)
    // ... execution ...
}
```

Should log at minimum:
- Session Ask entry (trace_id, prompt length)
- Success/error exit (duration, error details)
- ErrSessionBusy incidents

---

### 4.3 Delegate Tool Has No Logging

**Critical Gap:** Delegation is async and opaque:
- No logging when delegation starts
- No logging when target agent is located/spawned
- No logging when delegation times out
- No logging of confirmation relay

**Example Missing:**
```go
// ❌ No logging
func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
    // ... spawn target agent (line 109) — not logged
    evCh, err := targetAgent.AskStream(delCtx, dArgs.Task)
    if err != nil {
        // ❌ Error returned, not logged as delegation failure
        return "error: " + err.Error(), nil
    }
    // ... consume events (line 159-198) — not logged
}
```

Should log:
- Delegation start (task, target agent, timeout)
- Target agent location/spawn (synchronous vs async)
- Completion (success/failure, duration, tokens)
- Timeout expiration
- Confirmation relay status

---

### 4.4 Config Loader Has No Logging

**Gap:** No visibility into:
- Config file parsing success/failure
- Hot-reload detection
- Hot-reload application
- Callback execution

**Example Missing:**
```go
// ❌ No logging in Loader.go
func (l *Loader[T]) WatchAndReload() error {
    // No logging of watch activation
    for {
        case event := <-l.watcher.Events:
            // No logging of file change
            l.reload()  // No logging of reload result
        case err := <-l.watcher.Errors:
            l.errHandler(err)  // errHandler may be silent
    }
}
```

---

### 4.5 Context Window Compaction Has No Logging

**Gap:** Compaction is a complex operation. No visibility into:
- Which messages are selected for compaction
- Compaction success/failure
- Tokens saved/lost
- LLM API calls for compaction

---

### 4.6 Tools Return Errors to LLM, Don't Log

**Pattern:**
```go
// ❌ Typical tool error handling
if err != nil {
    return "", fmt.Errorf("validation failed: %w", err)
    // Error returned to LLM, not logged to system
}
```

**Problem:**
- No system-level metrics (tool success rate)
- No debugging for repeated failures
- No alerting for persistent issues

**Acceptable for:** Simple validation errors  
**Unacceptable for:** Network errors, timeouts, system errors

---

### 4.7 Message Assembly Has No Logging

**Gap:** `internal/ctxwin/` builds message payloads with no observability:
- No logging of message types/roles processed
- No logging of message compression/truncation
- No logging of reasoning content handling
- No logging of tool call transformation

---

## 5. LOG LEVEL APPROPRIATENESS

### 5.1 DEBUG Level Usage

**Current:**
- Router classification steps (classifier.go:75-128)
- Router decision made (router.go:106)
- LLM classifier cache hits (llm_classifier.go:95)

**Assessment:** ✅ Appropriate - detailed step-by-step flow, useful for debugging only.

### 5.2 INFO Level Usage

**Current:**
- Agent start/stop (lifecycle.go:53, 94, 101)
- HTTP session create/delete (server.go:118, 136)
- Tool execution start/done (stream.go:600, 789)
- LLM chat start/done (stream.go:229, 259)
- WebSocket connect/disconnect (server.go:206-207)

**Assessment:** ✅ Appropriate - operational events that indicate normal flow.

**Missing INFO that should exist:**
- ❌ Session Ask entry/exit
- ❌ Config hot-reload applied
- ❌ Delegation start/completion
- ❌ Context window compaction

### 5.3 WARN Level Usage

**Current:**
- LLM retry (client.go:389)
- Router model not found (router.go:150)

**Assessment:** ✅ Appropriate - recoverable issues.

**Missing WARN:**
- ❌ Classification low confidence
- ❌ Compaction removing many messages
- ❌ Tool timeout approaching

### 5.4 ERROR Level Usage

**Current:**
- Agent stop timeout (lifecycle.go:106)
- LLM chat failed (stream.go:241, 418)
- Tool not found (stream.go:589)
- Tool exec failed (stream.go:769)
- HTTP errors (server.go:81, 196, etc.)
- Panic recovery (server.go:81)

**Assessment:** ✅ Appropriate - failures needing intervention.

**Missing ERROR:**
- ❌ Session Ask failed with ErrSessionBusy (repeated)
- ❌ Delegation timeout
- ❌ Config parse failure
- ❌ Compaction LLM API error

---

## 6. USAGE STATISTICS

```
Total logging calls found: 277
Real logger calls (excluding fmt.Error, etc.): ~110

By subsystem:
- Router: 10 (but *slog.Logger, inconsistent)
- Agent: 35+ (good coverage)
- LLM Client: 8 (good coverage)
- HTTP Server: 15+ (good coverage)
- Tools: 0 (delegate tool especially critical gap)
- Config: 0 (critical gap)
- Session: 0 (critical gap)
- Context Window: 0 (critical gap)
```

---

## 7. FINDINGS SUMMARY

### ✅ Strengths

1. **Well-designed logger architecture**: Layer/Category constraints, rotation, context injection
2. **Good coverage in core subsystems**: Agent lifecycle, LLM client, HTTP server
3. **Consistent logger usage pattern** in most of codebase (agent, server)
4. **Structured logging** with trace_id/actor_id automatic injection
5. **Appropriate log levels** where logging exists
6. **JSONL output** suitable for log aggregation

### ❌ Critical Gaps

1. **Router uses `*slog.Logger` instead of `*logger.Logger`** — breaks architecture consistency
2. **Zero logging in Session layer** — core request/response abstraction invisible
3. **Zero logging in Delegate tool** — async delegation opaque to operators
4. **Zero logging in Config system** — hot-reload changes invisible
5. **Zero logging in Context Window** — compaction/overflow decisions hidden

### ⚠️ Medium Concerns

1. **Tool errors go to LLM, not logged** — no system-level metrics
2. **Confirmation flow has events but no logging** — user action not tracked
3. **Message assembly not logged** — difficult to debug context window issues
4. **Ask/AskStream method entry/exit not logged** — request tracing gaps

---

## 8. SPECIFIC RECOMMENDATIONS

### Priority 1 (Critical)

1. **Fix Router inconsistency:**
   - Change `classifier.go`, `router.go`, `llm_classifier.go` to use `*logger.Logger`
   - Use `CatApp` category for system-layer operations
   - Add INFO-level logging at key decision points

2. **Add Session logging:**
   - Log Ask entry: `a.Ask(...) → log trace_id, prompt_len`
   - Log Ask exit: `← success | error + duration_ms`
   - Log ErrSessionBusy with count

3. **Add DelegateTool logging:**
   - Start: `CatTool, "delegation start", task, target_agent, timeout`
   - Locate/Spawn: success/failure
   - End: `success | error + duration_ms`
   - Timeout: explicit timeout log entry

4. **Add Config logging:**
   - Parse: `"config loaded", file, version`
   - Hot-reload: `"config hot-reload detected", file`
   - Apply: `"config applied", changes_summary`
   - Error: `"config parse failed", file, error`

### Priority 2 (High)

5. **Add Context Window logging:**
   - Compaction: `"message compaction start/done", messages_count, tokens_before, tokens_after`
   - Overflow: `"context window overflow", current_tokens, max_tokens`
   - Truncation: `"message truncation", strategy, tokens_removed`

6. **Add tool-level metrics:**
   - System-wide: track success/failure/timeout rates
   - On error: log before returning error string to LLM

7. **Add confirmation logging:**
   - Confirmation request: `"tool confirmation requested", tool, prompt, options`
   - User choice: `"tool confirmation chosen", tool, choice`
   - Session whitelist: `"tool added to session whitelist", tool`

### Priority 3 (Medium)

8. **Ask/AskStream entry/exit:**
   - Entry: `"session ask start", session_id, prompt_len`
   - Exit: `success | error + duration_ms, response_len`

9. **HTTP request/response:**
   - Add method, path, response code, duration to server logs

10. **Message assembly visibility:**
    - Log when building payload: message types, compression applied, etc.

---

## 9. CODE EXAMPLES FOR FIXES

### Example 1: Fix Router

```go
// Before
type DefaultClassifier struct {
    logger *slog.Logger
}

// After
type DefaultClassifier struct {
    log *logger.Logger
}

// Use:
dc.log.Info(logger.CatApp, "classification complete",
    slog.String("level", result.Level.String()),
    slog.Int("confidence", result.Confidence),
)
```

### Example 2: Add Session Logging

```go
// In session.Ask()
func (s *Session) Ask(ctx context.Context, prompt string) (string, error) {
    s.agent.Log.InfoContext(ctx, logger.CatActor, "session ask start",
        slog.String("session_id", s.ID),
        slog.Int("prompt_len", len(prompt)),
    )
    
    if !s.inFlight.CompareAndSwap(0, 1) {
        s.agent.Log.WarnContext(ctx, logger.CatActor, "session busy",
            slog.String("session_id", s.ID),
        )
        return "", ErrSessionBusy
    }
    defer s.inFlight.Store(0)
    
    start := time.Now()
    result, err := s.agent.Ask(ctx, prompt)
    
    if err != nil {
        s.agent.Log.ErrorContext(ctx, logger.CatActor, "session ask failed",
            err,
            slog.Int64("duration_ms", time.Since(start).Milliseconds()),
        )
    } else {
        s.agent.Log.InfoContext(ctx, logger.CatActor, "session ask done",
            slog.Int("response_len", len(result)),
            slog.Int64("duration_ms", time.Since(start).Milliseconds()),
        )
    }
    
    return result, err
}
```

### Example 3: Add Delegate Logging

```go
// In DelegateTool.Execute()
func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
    log, _ := logger.Session(baseDir, teamID, sessionID)  // Get logger for this session
    
    log.InfoContext(ctx, logger.CatTool, "delegation start",
        slog.String("task", dArgs.Task),
        slog.String("target_agent", dt.LeaderID),
        slog.Duration("timeout", timeout),
    )
    
    start := time.Now()
    
    // ... spawn/locate
    
    if err := targetAgent.Ask(...); err != nil {
        log.LogError(ctx, logger.CatTool, "delegation failed",
            err,
            slog.Int64("duration_ms", time.Since(start).Milliseconds()),
        )
        return "error: " + err.Error(), nil
    }
    
    log.InfoContext(ctx, logger.CatTool, "delegation done",
        slog.Int("response_len", len(content)),
        slog.Int64("duration_ms", time.Since(start).Milliseconds()),
    )
    
    return content, nil
}
```

---

## 10. CONCLUSION

**Overall Assessment:** 🟡 **PARTIAL - GOOD FOUNDATION, CRITICAL GAPS**

The logging architecture is well-designed and used consistently in core subsystems (agent, LLM, HTTP). However:

1. **Router inconsistency** breaks architectural uniformity
2. **Session/Delegate/Config have zero logging** — critical operational blindness
3. **Context window/tool errors not logged** — makes debugging difficult
4. **Ask/AskStream not logged** — request tracing incomplete

**Impact:**
- ✅ Can debug LLM interactions (LLM client well-logged)
- ✅ Can debug tool execution (tool exec well-logged)
- ❌ Cannot debug session lifecycle (zero logging)
- ❌ Cannot debug delegation flow (zero logging)
- ❌ Cannot debug config changes (zero logging)
- ❌ Cannot debug context window issues (zero logging)

**Recommendation:** Address Priority 1 items (Router, Session, Delegate, Config) to achieve operational visibility. These changes would transform logging from partial to comprehensive coverage.

