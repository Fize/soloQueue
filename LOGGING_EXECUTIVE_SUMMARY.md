# SoloQueue Logging System - Executive Summary

## Overview

The SoloQueue project has a **well-architected logging system** with thoughtful design (three-layer hierarchy, structured categories, context injection), but **critical gaps in coverage** leave significant operational blindness in session management, task delegation, and configuration.

**Status:** 🟡 **PARTIAL** - Good foundation, critical gaps need urgent attention

---

## Key Metrics

- **Total logging calls:** ~110 substantive calls across the codebase
- **Subsystems with logging:**
  - ✅ Agent lifecycle & execution (35+ calls)
  - ✅ LLM client operations (8 calls)
  - ✅ HTTP server (15+ calls)
  
- **Subsystems with ZERO logging:**
  - ❌ Session layer (critical)
  - ❌ Delegate tool (critical)
  - ❌ Config system (critical)
  - ❌ Context window compaction (critical)

---

## Critical Issues (Fix Immediately)

### 1. **Router Architecture Inconsistency** ⚠️ BLOCKER

The Router/Classifier package uses raw `*slog.Logger` instead of `*logger.Logger`:

```go
// ❌ Router (inconsistent)
type DefaultClassifier struct {
    logger *slog.Logger
}

// ✅ Agent (consistent)
type Agent struct {
    Log *logger.Logger
}
```

**Impact:** Cannot use project's logging categories, breaks trace injection, inconsistent with codebase pattern.

**Fix:** Change Router to use `*logger.Logger` with CatApp category.

---

### 2. **Session Layer Has Zero Logging** 🔴 CRITICAL

Session is the primary user-facing API. No logging of:
- Ask/AskStream invocation
- Session busy errors
- Success/failure outcomes
- Error details

**Example missing log:**
```
Session.Ask() → log("session ask start", trace_id, prompt_len)
                → LLM execution
                → log("session ask done", duration_ms, response_len)
```

**Impact:** Impossible to debug session issues, no visibility into request flow, no audit trail.

**Fix:** Add logging at Ask entry/exit, log ErrSessionBusy.

---

### 3. **Delegate Tool Has Zero Logging** 🔴 CRITICAL

Delegation is async and opaque. No logging of:
- Delegation start/completion
- Target agent location/spawn
- Timeout expiration
- Confirmation relay

**Impact:** L1→L2 delegation is invisible, impossible to debug async flows, no metrics on delegation success rates.

**Fix:** Add logging for delegation lifecycle (start, locate/spawn, done/failed, timeout).

---

### 4. **Config System Has Zero Logging** 🔴 CRITICAL

No visibility into:
- Config file load success/failure
- Hot-reload detection
- Hot-reload application
- Parse errors

**Impact:** Config changes are invisible, cannot debug config-related bugs, no alerting for parse failures.

**Fix:** Add logging for load, hot-reload, apply, error events.

---

## High-Priority Gaps (Fix Next)

### 5. Context Window Compaction Not Logged
- No visibility into which messages are compacted
- Tokens saved/lost not tracked
- Compaction failures not reported
- Truncation strategy not logged

**Impact:** Debugging context window issues is impossible, no metrics on compaction effectiveness.

---

### 6. Ask/AskStream Entry/Exit Not Logged
- Agent.Ask() method entry/exit not logged
- No request tracing beyond LLM call itself
- Cannot trace full request lifecycle

**Impact:** Request tracing incomplete, debugging request hangs difficult.

---

### 7. Tool Errors Go to LLM, Not Logged
- Tool execution returns errors as strings to LLM
- No system-level tool metrics
- No debugging for repeated tool failures

**Impact:** No visibility into tool health, no metrics on tool success rates.

---

## Strengths (Well Done)

✅ **Logger Architecture:**
- Clean three-layer hierarchy (System/Team/Session)
- Well-designed category system (10 categories with layer constraints)
- Automatic context injection (trace_id, actor_id)
- Proper JSONL output format
- File rotation support
- Thread-safe handler implementation

✅ **Good Coverage Areas:**
- Agent lifecycle (start/stop/panic/registry operations)
- LLM client (chat start/done/retry/error)
- HTTP server (session create/delete, WS events, routing decisions)
- Tool execution (start/done/error, confirmation flow)

✅ **Consistency Where It Exists:**
- Most of codebase uses `*logger.Logger` correctly
- Proper category usage in logged subsystems
- Appropriate log levels (DEBUG for detailed flow, INFO for operations, ERROR for failures)

---

## Recommendations Priority Order

### Priority 1 (Fix This Week)
1. Fix Router to use `*logger.Logger` instead of `*slog.Logger`
2. Add Session.Ask() entry/exit logging
3. Add DelegateTool logging (start/locate/done/timeout)
4. Add Config loader logging (load/hot-reload/apply/error)

### Priority 2 (Fix Next Week)
5. Add Context Window compaction logging
6. Add tool confirmation logging
7. Add tool error logging before returning to LLM
8. Add Ask/AskStream entry/exit logging

### Priority 3 (Nice to Have)
9. Add HTTP request/response logging (method, path, code, duration)
10. Add message assembly logging
11. Add team-layer operations logging

---

## Quick Fix Examples

### Fix Router (20 lines)
```go
// Change Router fields from *slog.Logger to *logger.Logger
// Update calls from .DebugContext() to .DebugContext() with category
// Example: dc.log.DebugContext(ctx, logger.CatApp, "...")
```

### Add Session Logging (40 lines)
```go
// In Session.Ask(), add:
//   Entry: log("session ask start", trace_id, prompt_len)
//   Exit: log("session ask done" or "session ask failed", duration_ms)
//   Busy: log("session busy", retry_count)
```

### Add Delegate Logging (60 lines)
```go
// In DelegateTool.Execute(), add:
//   Start: log("delegation start", task, target, timeout)
//   Locate/Spawn: log result
//   Done: log("delegation done", duration_ms, response_len)
//   Timeout: log("delegation timeout", duration_ms)
```

### Add Config Logging (50 lines)
```go
// In Config Loader, add:
//   Load: log("config loaded", file, success/failure)
//   Reload: log("config hot-reload detected", file)
//   Apply: log("config applied")
//   Error: log("config parse error", file, error)
```

---

## Testing Checklist

After implementing fixes, verify:
- [ ] Session Ask shows full request lifecycle in logs
- [ ] Delegation start/completion appears in logs
- [ ] Config load/hot-reload events are logged
- [ ] Context window compaction decisions are tracked
- [ ] All ERROR-level events have corresponding log entries
- [ ] Logger output includes trace_id for request correlation
- [ ] Log levels are appropriate (DEBUG/INFO/WARN/ERROR)
- [ ] No fmt.Println or log.Print calls in new code
- [ ] All new logging uses correct Category

---

## Success Criteria

When complete:
1. ✅ 100% of subsystems using `*logger.Logger` (no raw slog)
2. ✅ Critical paths (Session/Delegate/Config) have entry/exit logging
3. ✅ All error returns have corresponding ERROR-level log entry
4. ✅ Context window decisions (compaction/truncation) are observable
5. ✅ Request tracing possible from HTTP request → agent → LLM → tool
6. ✅ Zero operational blindness for async/delegation flows

---

## Related Documentation

- Full evaluation: `LOGGING_EVALUATION.md`
- Logger package: `internal/logger/logger.go`
- Category definitions: `internal/logger/layer.go`
- Handler implementation: `internal/logger/handler.go`

