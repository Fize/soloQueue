# DETAILED INVESTIGATION REPORT: HTTP 400 Error in SoloQueue 3-Layer Agent System

## Executive Summary

**Root Cause:** A relay goroutine in `execToolStream()` unconditionally forwards ALL AgentEvents from child agents (L3) into the parent agent's (L2) event stream. These relayed events are then consumed by Session.AskStream, which treats them as if they came from L2, corrupting the ContextWindow message history with intermediate L3 state. When the corrupted history is sent to the LLM API, it violates the expected message sequence, resulting in HTTP 400.

**Impact:** Multi-layer agent delegation (L1→L2→L3) fails immediately on the second LLM request from any layer.

**Severity:** CRITICAL - All hierarchical agent communications are broken.

---

## Technical Deep Dive

### 1. The Relay Goroutine Problem

**File:** `internal/agent/stream.go` (lines 954-1006)
**Function:** `(a *Agent) execToolStream(ctx context.Context, iter int, tc llm.ToolCall, out chan<- AgentEvent) string`

The relay goroutine was introduced to support tool confirmation propagation through multiple layers:

```go
// Lines 961-962: Create relay channel
relayCh := make(chan interface{}, 16)
toolCtx := tools.WithToolEventChannel(execCtx, relayCh)

// Lines 993-1002: Relay goroutine unconditionally forwards ALL AgentEvents
relayDone := make(chan struct{})
go func() {
    defer close(relayDone)
    for ev := range relayCh {
        if agentEv, ok := ev.(AgentEvent); ok {
            a.emit(ctx, out, agentEv)  // ← EMITS L3 EVENTS TO L2.OUT
        }
    }
}()

// Line 1004: Execute tool (which calls child agent)
result, err := tool.Execute(toolCtx, args)
close(relayCh) // Signal relay goroutine to drain
<-relayDone    // Wait for relay to finish
```

**Problem:** The relay goroutine UNCONDITIONALLY forwards every AgentEvent it receives. This includes:
- `ContentDeltaEvent` (L3's content chunks)
- `ToolCallDeltaEvent` (L3's tool call information)
- `ToolExecStartEvent`, `ToolExecDoneEvent` (L3's tool execution)
- `DoneEvent` (L3's completion)
- `ReasoningDeltaEvent` (L3's reasoning)
- All other AgentEvent types

### 2. Event Flow Through Multiple Layers

**File:** `internal/tools/delegate.go` (lines 147-254)
**Function:** `(dt *DelegateTool) Execute(ctx context.Context, args string) (string, error)`

When L2's tool execution reaches DelegateTool:

```go
// Line 187: Extract parent event channel from context
parentEventCh, _ := ToolEventChannelFromCtx(ctx)

// Line 193: Call L3.AskStream
evCh, err := targetAgent.AskStream(delCtx, dArgs.Task)

// Lines 205-216: Consume L3's events and relay to parent
for ev := range evCh {
    if ev == nil {
        continue
    }
    
    // Forward ALL events to parent relay channel
    if parentEventCh != nil {
        select {
        case parentEventCh <- ev:  // ← SENDS L3 EVENTS TO relayCh
        case <-delCtx.Done():
        }
    }
    
    // ... additional event handling ...
}
```

**The flow:**
1. L2's `execToolStream` creates `relayCh` and injects it into tool context
2. DelegateTool calls L3.AskStream()
3. L3 emits events (ContentDelta, ToolCall, Done, etc.)
4. DelegateTool receives these events and feeds them to `relayCh`
5. L2's relay goroutine receives them and emits to L2.out
6. Session.AskStream consumes L2.out (including relayed L3 events)

### 3. Session Event Consumption

**File:** `internal/session/session.go` (lines 179-257)
**Function:** `(s *Session) AskStream(ctx context.Context, prompt string) (<-chan agent.AgentEvent, error)`

Session.AskStream wraps the agent's event channel with its own logic:

```go
// Lines 204-256: Forwarder goroutine
out := make(chan agent.AgentEvent, 64)
go func() {
    defer close(out)
    defer s.inFlight.Store(0)
    defer s.touch()

    var finalContent string
    var finalReasoning string
    var gotDone bool
    
    for {
        var ev agent.AgentEvent
        select {
        case e, ok := <-srcCh:  // srcCh is from Agent.AskStreamWithHistory
            if !ok {
                goto done
            }
            ev = e
        case <-ctx.Done():
            s.mu.Lock()
            s.cw.PopLast()
            s.mu.Unlock()
            return
        }
        select {
        case out <- ev:
        case <-ctx.Done():
            s.mu.Lock()
            s.cw.PopLast()
            s.mu.Unlock()
            return
        }
        // Process event to extract final state
        switch e := ev.(type) {
        case agent.DoneEvent:
            finalContent = e.Content        // ← Captures final content
            finalReasoning = e.ReasoningContent
            gotDone = true
        case agent.ErrorEvent:
            s.mu.Lock()
            s.cw.PopLast()
            s.mu.Unlock()
        }
    }
done:
    // Push final state to ContextWindow
    if gotDone {
        s.mu.Lock()
        opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
        s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)  // ← PUSHES TO cw
        s.mu.Unlock()
    }
}()
```

**Critical Bug:** Session doesn't distinguish between:
- L2's own `DoneEvent` (from L2's LLM response)
- Relayed L3's `DoneEvent` (from L3's LLM response)

When both are present, whichever `DoneEvent` arrives first will set `finalContent` and `gotDone=true`. If it's the relayed L3's DoneEvent:
1. `finalContent` = L3's response (incorrect!)
2. When loop breaks, Session pushes L3's content to ContextWindow as if it were L2's response
3. Next iteration's `cw.BuildPayload()` includes this corrupted state

### 4. ContextWindow Corruption

**File:** `internal/agent/stream.go` (lines 340-475)
**Function:** `(a *Agent) runOnceStreamWithHistory(...)`

After tool execution, L2 does:

```go
// Lines 467-474: Push tool results to ContextWindow
for i, tc := range toolCalls {
    cw.Push(ctxwin.RoleTool, results[i],  // results[i] = DelegateTool's return
        ctxwin.WithToolCallID(tc.ID),
        ctxwin.WithToolName(tc.Function.Name),
        ctxwin.WithEphemeral(true),
    )
}
```

But before this happens in Session (lines 249-254):

```go
if gotDone {  // gotDone is true if ANY DoneEvent arrived (could be L3's!)
    s.mu.Lock()
    opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
    s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)  // finalContent = L3's content!
    s.mu.Unlock()
}
```

This creates a message sequence violation:
- Session pushes: `assistant("L3's response with tool_calls: [...]")`
- Then L2 tries to push: `tool(result)`
- But the assistant message contains L3's tool_calls, not L2's tool_calls!

### 5. API Request Corruption

**File:** `internal/agent/stream.go` (lines 282-283)
**File:** `internal/agent/helpers.go` (lines 16-30)

When L2 (or L1) calls L2.AskWithHistory again:

```go
// L2.runOnceStreamWithHistory, line 282
payload := cw.BuildPayload()

// L2.runOnceStreamWithHistory, line 283
msgs := payloadToLLMMessages(payload)
```

The ContextWindow now contains corrupted messages. The `BuildPayload()` returns:

```
[
    {"role": "user", "content": "L2's prompt"},
    {"role": "assistant", "content": "L3's response", "tool_calls": "L3's tool_calls"},
    {"role": "tool", "content": "DelegateTool result", "tool_call_id": "..."},
    // If there were relayed events that got partially saved...
    {"role": "assistant", "content": "partial L3 content fragment", ...},
    ...
]
```

This violates the LLM API's message protocol:
- Assistant with tool_calls must be followed immediately by tool results
- Can't have assistant → tool → assistant
- Can't have tool_calls that don't match previous assistant's tool_calls

**Result:** DeepSeek API returns HTTP 400 with error about invalid message sequence.

### 6. Why the Error Message is Truncated

**File:** `internal/llm/deepseek/client.go` (lines 272-292)
**Function:** `parseAPIError(r *http.Response) *llm.APIError`

When API returns 4xx:

```go
func parseAPIError(r *http.Response) *llm.APIError {
    apiErr := &llm.APIError{StatusCode: r.StatusCode}
    
    body, err := io.ReadAll(r.Body)
    if err != nil {
        apiErr.Message = fmt.Sprintf("read body: %v", err)
        return apiErr
    }
    var env wireErrorEnvelope
    if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
        apiErr.Type = env.Error.Type
        apiErr.Code = env.Error.Code
        apiErr.Message = env.Error.Message    // ← API's full error message
        apiErr.Param = env.Error.Param
        return apiErr
    }
    apiErr.Message = truncate(string(body), 500)
    return apiErr
}

func truncate(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "...(truncated)"
}
```

The error message from API is being truncated by `truncate()` to 500 characters, which explains why the user sees:
```
"llm: http 400: invalid_request_error: An assistant message with tool_calls must be followed by tool result messages..."
```

This is the actual API error, truncated.

---

## Evidence: Code Locations

| Component | File | Lines | Issue |
|-----------|------|-------|-------|
| Relay goroutine creation | `internal/agent/stream.go` | 961-1006 | Unconditionally forwards all AgentEvents |
| Event emission to relay | `internal/tools/delegate.go` | 210-216 | Feeds all L3 events to parent relay channel |
| Session event consumption | `internal/session/session.go` | 214-246 | Treats all events as L2's own; accumulates L3 state |
| ContextWindow push | `internal/session/session.go` | 249-254 | Pushes potentially corrupted final content |
| Message building | `internal/agent/stream.go` | 282-283 | Sends corrupted message sequence to API |
| Error truncation | `internal/llm/deepseek/client.go` | 290 | Truncates detailed error message |

---

## Impact Analysis

### Affected Scenarios
1. **L1 → L2 → L3 synchronous delegation** (first attempt fails)
2. **L1 → L2 with tool confirmation propagation** (cascades through all layers)
3. **Any multi-layer agent system using AskWithHistory** (all broken)
4. **Async delegation** (likely broken due to same issue)

### Why First Request Often Works
- First request: L2 has empty ContextWindow (or just system prompt + first user message)
- Events relayed from L3 to L2 don't significantly corrupt the minimal history
- API might accept the slightly malformed message sequence

### Why Second Request Always Fails
- ContextWindow now contains corrupted messages from L3 events
- Message sequence is clearly invalid
- API rejects with HTTP 400

---

## Root Cause Summary

The relay goroutine design was intended to:
- Forward `ToolNeedsConfirmEvent` so tool confirmation can propagate up through layers
- Let parent UI see child agent progress (ContentDelta, etc. for transparency)

But it unconditionally forwards ALL events, treating them as if they came from the parent agent. This breaks the abstraction layer because:

1. Child agent events (L3) are not meant to be visible in parent agent (L2) history
2. Parent agent's ContextWindow should only contain parent's own LLM exchanges
3. Tool result from delegation should be OPAQUE to parent (just a string result)
4. Child's reasoning, tool calls, intermediate state should NOT leak into parent's message history

---

## Recommended Fix Strategy

### Option 1: Filter Relayed Events (Minimal Change)
Only relay specific event types needed for confirmation/UI:
- Allow: `ToolNeedsConfirmEvent` (for confirmation routing)
- Allow: `ContentDeltaEvent` (for UI progress display - optional)
- Block: `ToolCallDeltaEvent`, `DoneEvent`, `ReasoningDeltaEvent`, tool execution events
- Filter at relay goroutine level (line 998-1000)

### Option 2: Separate Confirmation Channel (Best Design)
- Remove relay goroutine from execToolStream
- Create dedicated confirmation channel for ToolNeedsConfirmEvent
- Keep child events completely isolated from parent event stream
- Reduces event channel mixing complexity

### Option 3: Mark Relayed Events (Audit Trail)
- Wrap relayed events with metadata indicating they're from delegation
- Session.AskStream explicitly ignores events marked as "relayed"
- Preserves visibility for debugging while preventing corruption

---

## Testing Recommendations

1. **Unit test:** Verify relay goroutine doesn't emit non-confirmation events
2. **Integration test:** L2→L3 delegation with ContextWindow history check
3. **Regression test:** Multi-iteration L2→L3 delegation (second request)
4. **Edge case:** L2 rejection flow with relayed ErrorEvent
5. **Confirmation test:** Tool confirmation through 3 layers still works

---

## Prevention Measures

1. **Add event filtering in relay goroutine** (immediate fix)
2. **Add ContextWindow validation** (detect when relayed events corrupt it)
3. **Add message sequence validation in payloadToLLMMessages** (catch before API)
4. **Refactor event flow** to separate concerns (long-term)

