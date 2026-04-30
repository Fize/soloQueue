# SoloQueue Logging Coverage Matrix

Quick reference showing logging coverage by subsystem and critical flow.

## Coverage Heat Map

```
SYSTEM LAYER (System Logger)
├── Router/Classifier .............. ⚠️  INCONSISTENT (uses raw slog)
├── LLM Client (DeepSeek) .......... ✅ GOOD (8 calls, CatLLM)
└── HTTP Server .................... ✅ GOOD (15+ calls, CatHTTP)

TEAM LAYER (Team Logger)
├── Agent Registry ................. ✅ GOOD (supervisor, lifecycle)
└── Team Operations ................ ❌ UNUSED (0 calls)

SESSION LAYER (Session Logger)
├── Session Manager ................ ❌ ZERO (0 calls) 🔴 CRITICAL
├── Agent Lifecycle ................ ✅ GOOD (start/stop/panic)
├── Agent Execution ................ ✅ PARTIAL (LLM/tools logged)
│   ├── Ask/AskStream Entry/Exit ... ❌ NOT LOGGED 🟡 HIGH
│   ├── LLM Chat ................... ✅ GOOD (start/done/fail)
│   └── Tool Execution ............. ✅ GOOD (start/done/fail)
├── Delegate Tool .................. ❌ ZERO (0 calls) 🔴 CRITICAL
├── Context Window ................. ❌ ZERO (0 calls) 🔴 CRITICAL
│   ├── Message Push ............... ❌ NOT LOGGED
│   ├── BuildPayload ............... ❌ NOT LOGGED
│   ├── Compaction ................. ❌ NOT LOGGED 🟡 HIGH
│   └── Truncation/Overflow ........ ❌ NOT LOGGED 🟡 HIGH
└── Other Tools .................... ❌ ZERO system logging 🟡 MEDIUM

CONFIG SYSTEM (System Logger)
├── Config Load .................... ❌ ZERO (0 calls) 🔴 CRITICAL
├── Config Hot-Reload .............. ❌ ZERO (0 calls) 🔴 CRITICAL
├── Config Apply ................... ❌ ZERO (0 calls) 🔴 CRITICAL
└── Config Error ................... ❌ ZERO (0 calls) 🔴 CRITICAL
```

## Request Trace Flow

What gets logged during a typical request:

```
HTTP Request
    ↓
    ✅ "session created" (INFO, CatHTTP)     [server.go:118]
    ✅ "routing decision" (INFO, CatHTTP)    [server.go:264]
    ✅ "ws connected" (INFO, CatHTTP)        [server.go:206]
    ↓
Session.Ask(prompt)
    ↓
    ❌ NO ENTRY LOG 🔴
    ↓
Agent.Ask(prompt)
    ↓
    ✅ "llm chat start" (INFO, CatLLM)       [stream.go:229]
    ✅ "llm chat done" (INFO, CatLLM)        [stream.go:259]
    ↓
Tool Execution
    ↓
    ✅ "tool exec start" (INFO, CatTool)     [stream.go:600]
    ✅ "tool exec done" (INFO, CatTool)      [stream.go:789]
    ↓
Response Back
    ↓
    ❌ NO SESSION EXIT LOG 🔴
    ✅ "ws write" (INFO, CatHTTP)            [server.go:225]

MISSING: Session entry/exit means request tracing has blind spots
```

## Category Usage

```
Category      Layer    Used   Files                        Count
────────────────────────────────────────────────────────────────
CatApp        System   ✅     server.go                    3
CatConfig     System   ❌     (NO FILES)                   0
CatHTTP       System   ✅     server.go                    15+
CatWS         System   ❌     (uses CatHTTP instead)       0
CatLLM        System   ✅     llm/deepseek/client.go       8
              Session  ✅     agent/stream.go              6
────────────────────────────────────────────────────────────────
CatTeam       Team     ❌     (NO FILES)                   0
CatAgent      Team     ✅     agent/factory.go             2
              Team     ✅     agent/supervisor.go          3
────────────────────────────────────────────────────────────────
CatLLM        Session  ✅     agent/stream.go              6
CatActor      Session  ✅     agent/lifecycle.go           8
              Session  ✅     agent/run.go                 4
              Session  ✅     agent/supervisor.go          3
              Session  ✅     agent/registry.go            7
CatTool       Session  ✅     agent/stream.go              6
CatMessages   Session  ❌     (NO FILES)                   0
```

## Logging by Component

### ✅ Well-Logged Components

| Component | Files | Calls | Status |
|-----------|-------|-------|--------|
| Agent Lifecycle | lifecycle.go, run.go | 12 | ✅ Complete: start/stop/panic |
| Agent Execution | stream.go | 10 | ✅ Complete: LLM/tool lifecycle |
| LLM Client | deepseek/client.go | 8 | ✅ Complete: chat/retry/error |
| HTTP Server | server.go | 15+ | ✅ Complete: session/WS events |
| Agent Registry | registry.go | 7 | ✅ Complete: register/unregister/start/stop |

### ❌ Unlogged Components (Critical)

| Component | Files | Calls | Gap |
|-----------|-------|-------|-----|
| Session | session.go | **0** | Ask entry/exit, errors, lifecycle |
| Delegate Tool | tools/delegate.go | **0** | Start/spawn/done/timeout |
| Config System | config/loader.go | **0** | Load/reload/apply/error |
| Context Window | ctxwin/ctxwin.go | **0** | Push/calibrate/compaction |

### ⚠️ Partially-Logged Components

| Component | Files | Issue |
|-----------|-------|-------|
| Router | router/ | ✅ Logged but uses raw *slog.Logger (not *logger.Logger) |
| Tool Errors | tools/*.go | ✅ Returned to LLM but not logged to system |
| Ask/AskStream | agent/ask.go | ❌ No entry/exit logging |

---

## Error Path Coverage

Which error returns are logged?

```
Location                        Error Type              Logged?
──────────────────────────────────────────────────────────────
agent/lifecycle.go:106          ErrStopTimeout          ✅ ERROR
agent/stream.go:589             Tool not found          ✅ ERROR
agent/stream.go:769             Tool exec failed        ✅ ERROR
agent/stream.go:241             LLM chat failed         ✅ ERROR
session/session.go:??           ErrSessionBusy          ❌ NO
tools/delegate.go:152           Delegation failed       ❌ NO (returns to LLM)
tools/delegate.go:150           Delegation timeout      ❌ NO (returns to LLM)
config/loader.go:??             Parse error             ❌ NO
ctxwin/ctxwin.go:??             Compaction failed       ❌ NO
```

---

## Architecture Consistency

```
CORRECT PATTERN (Most of codebase)
──────────────────────────────────
type Agent struct {
    Log *logger.Logger           ← Project logger
}

a.Log.Info(logger.CatActor, "msg", args...)
           ↓                 ↓
        Category       Structured fields
Result: ✅ Uses rotation, ✅ Category-filtered, ✅ Trace injection

WRONG PATTERN (Router only)
──────────────────────────────────
type DefaultClassifier struct {
    logger *slog.Logger         ← Raw stdlib
}

dc.logger.DebugContext(ctx, "msg", args...)
          ↓
       No category
Result: ❌ Bypasses rotation, ❌ Cannot filter by category, ❌ No trace


MISSING PATTERN (Session, Delegate, Config)
──────────────────────────────────
type Session struct {
    // ← No logger at all
}

// ❌ No logging calls
Result: ❌ No observability
```

---

## Log Level Appropriateness

| Level | Current Usage | Missing | Assessment |
|-------|---------------|---------|------------|
| DEBUG | Classification steps, LLM cache hits | — | ✅ Appropriate (detailed only) |
| INFO | Lifecycle events (start/stop/create/delete), LLM chat, tool exec | Session ask, config load, delegation start | ✅ Appropriate |
| WARN | LLM retries, model not found | Classification low-confidence, compaction warning | ✅ Appropriate |
| ERROR | Failures (timeout, not found, failed) | Session busy repeat, delegation timeout, config parse fail | ✅ Appropriate |

---

## Call Distribution

Total substantive logging calls: ~110

```
Subsystem           Calls    Category      Files
──────────────────────────────────────────────────
Agent (lifecycle)   12       CatActor      lifecycle.go, run.go
Agent (execution)   10       CatLLM/Tool   stream.go
LLM Client          8        CatLLM        deepseek/client.go
HTTP Server         15+      CatHTTP       server.go
Agent (registry)    7        CatActor      registry.go
Agent (supervisor)  3        CatActor      supervisor.go
Agent (factory)     2        CatAgent      factory.go
Router              10       (slog)        classifier.go, router.go, llm_classifier.go
──────────────────────────────────────────────────
TOTAL               ~110

Session             0        (CatActor)    session.go
Delegate            0        (CatTool)     tools/delegate.go
Config              0        (CatConfig)   config/loader.go
Context Window      0        (CatTool)     ctxwin/ctxwin.go
```

---

## Fix Priority

### CRITICAL (🔴 This week)
1. Session logging: 4 entry/exit points
2. Delegate logging: 5 key points (start/locate/spawn/done/timeout)
3. Config logging: 4 key points (load/reload/apply/error)
4. Router refactoring: change to *logger.Logger

### HIGH (🟡 Next week)
5. Context window compaction: 3 key points
6. Ask/AskStream entry/exit: 2 key points
7. Tool confirmation: 3 key points

### MEDIUM (🟠 Nice to have)
8. HTTP request details (method, path, code)
9. Message assembly details
10. Team-layer operations

---

## Validation Checklist

Use this to verify fixes:

- [ ] Router uses `*logger.Logger` (not `*slog.Logger`)
- [ ] Session.Ask has entry log with trace_id, prompt_len
- [ ] Session.Ask has exit log with duration_ms, response_len
- [ ] DelegateTool.Execute has start/locate/spawn/done/timeout logs
- [ ] Config loader logs load/reload/apply/error events
- [ ] All ERROR-level events have log entries (not just returned errors)
- [ ] Context window compaction decisions logged
- [ ] Request can be traced: HTTP → Session → Agent → LLM → Tool
- [ ] No raw slog.Logger used in domain code
- [ ] All new logging uses appropriate Category

---

## Quick Reference: Which Logs to Add

### Session (session.go)

```go
// In Ask() method:
Log Ask entry:    "session ask start", trace_id, prompt_len, session_id
Log Ask exit:     "session ask done", duration_ms, response_len
Log error:        "session ask failed", err, duration_ms
Log ErrSessionBusy: "session busy", session_id
```

### DelegateTool (tools/delegate.go)

```go
// In Execute() method:
Log start:        "delegation start", task, target_agent, timeout
Log spawn:        "delegation agent spawned|located"
Log result:       "delegation done", content_len, duration_ms (or error)
Log timeout:      "delegation timeout", duration_ms
```

### Config (config/loader.go)

```go
// In Load/Reload methods:
Log load:         "config loaded", file
Log error:        "config parse error", file, error
Log reload:       "config hot-reload detected", file
Log apply:        "config applied", changes_summary
```

### Context Window (ctxwin/ctxwin.go)

```go
// In relevant methods:
Log compaction:   "message compaction start/done", before_tokens, after_tokens
Log overflow:     "context window overflow", current_tokens, max_tokens
Log truncation:   "message truncation", strategy, tokens_removed
```

