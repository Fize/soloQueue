# ROOT CAUSE ANALYSIS: HTTP 400 Error in 3-Layer SoloQueue Agent System

## Problem Summary
When testing the SoloQueue 3-layer agent system (L1 → L2 → L3), an HTTP 400 error is returned from the DeepSeek LLM API with a truncated message:
```
"llm: http 400: invalid_request_error: An assistant message with tool_calls..."
```

This error typically means: **"An assistant message with tool_calls must be followed by tool result messages"** or **"An assistant message must follow a user/tool message"**.

## The Culprit: Relay Goroutine Emitting Events Into Parent's Out Channel

### The Issue
In `internal/agent/stream.go`, the `execToolStream` function (lines 954-1006) creates a **relay goroutine** to forward child agent events to the parent agent's event stream:

```go
// Line 961: Create relay channel
relayCh := make(chan interface{}, 16)
toolCtx := tools.WithToolEventChannel(execCtx, relayCh)

// Lines 993-1002: Relay goroutine
go func() {
    defer close(relayDone)
    for ev := range relayCh {
        if agentEv, ok := ev.(AgentEvent); ok {
            a.emit(ctx, out, agentEv)  // ← PROBLEM: Emitting child events to parent out
        }
    }
}()

// Line 1004: Execute tool (e.g., DelegateTool calling child Agent)
result, err := tool.Execute(toolCtx, args)
```

### How Events Flow

**When L2 calls DelegateTool with L3:**

1. `DelegateTool.Execute()` (lines 147-254 in `internal/tools/delegate.go`)
2. Calls `L3.AskStream()` which opens an event channel from L3's `runOnceStreamWithHistory`
3. DelegateTool consumes L3's events and:
   - Emits them to `parentEventCh` (the `relayCh`)
   - This forwards events like `ToolNeedsConfirmEvent`, `ContentDeltaEvent`, `ToolCallDeltaEvent`, etc.

4. L2's relay goroutine receives these events and does:
   ```go
   a.emit(ctx, out, agentEv)
   ```
   This adds **L3's events** to L2's `out` channel.

### The Cascade Through Session.AskStream

In `internal/session/session.go` lines 205-256, Session wraps L2.AskStreamWithHistory:

```go
go func() {
    // ... 
    for {
        select {
        case e, ok := <-srcCh:  // srcCh = L2's out channel
            // ...
            ev = e
        }
        select {
        case out <- ev:         // Forward to Session's out channel
        }
        // This loop consumes **all events from srcCh**, including 
        // relayed L3 events!
    }
    // At the end of the loop:
    if gotDone {
        s.mu.Lock()
        opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
        s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)
        s.mu.Unlock()
    }
}()
```

### The Message Corruption Problem

**L2's runOnceStreamWithHistory adds to ContextWindow:**
- Lines 435-438: Pushes `assistant(tool_calls)` after LLM response
- Lines 469-474: Pushes `tool(result)` after tool execution

**THEN, L2's relay goroutine emits L3's events:**
- These include L3's own `ContentDeltaEvent`, `ToolCallDeltaEvent`, `DoneEvent`
- Session's forwarder in line 236-246 reads these **L3 events**
- When Session finally gets L2's `DoneEvent`, it only has L3's final content!

### Message Sequence Corruption

Here's what the API receives:

**CORRECT sequence (single agent):**
```
user (prompt)
assistant (response with tool_calls: [delegate_tool])
tool (delegate_tool result)
```

**CORRUPTED sequence (with relay goroutine emitting L3 events to L2.out):**
```
user (L2's prompt)
assistant (L2's response with tool_calls: [delegate_tool])
tool (delegate_tool result = L3's final content)

← But now L2's relay goroutine has emitted L3's internal events into L2.out:
   ContentDeltaEvent (L3's content)
   ToolCallDeltaEvent (L3's tool_calls)
   ContentDeltaEvent (L3's more content)
   DoneEvent (L3's done)

← These leaked through Session.AskStream and when the next iteration calls
  cw.BuildPayload(), it includes these partial/intermediate messages!
```

### Why This Causes HTTP 400

When L2 starts its next iteration (or later when L1 calls L2 again), `runOnceStreamWithHistory` does:

```go
payload := cw.BuildPayload()       // Line 524
msgs := payloadToLLMMessages(payload)
```

If the ContextWindow contains intermediate state from L3's events (e.g., partial tool_calls or content fragments), the message sequence becomes invalid:

- Assistant messages might not have complete tool_calls
- Tool result messages might be followed by assistant messages without proper formatting
- Message order violates the pattern: user → assistant (with/without tools) → tool → ...

**Result:** API returns HTTP 400: `invalid_request_error`

## Root Cause: Double Event Emission

The **root cause** is that the relay goroutine in `execToolStream` unconditionally forwards **all AgentEvents** emitted by the child tool (L3) into the parent's out channel (L2).

However, in the runOnceStreamWithHistory flow:
- L2's out channel is monitored by Session.AskStream (or callers)
- Any events pushed to L2's out get consumed by the Session's forwarder
- The Session's forwarder doesn't just forward events—it treats them as if they came from L2
- These events can leak into ContextWindow state management

## Evidence Trail

### 1. Relay Goroutine Creation (stream.go:993-1002)
```go
go func() {
    defer close(relayDone)
    for ev := range relayCh {
        if agentEv, ok := ev.(AgentEvent); ok {
            a.emit(ctx, out, agentEv)  // Emits L3 events to L2.out
        }
    }
}()
```

### 2. DelegateTool Feeding Events to Relay (delegate.go:210-216)
```go
if parentEventCh != nil {
    select {
    case parentEventCh <- ev:  // L3 events → relayCh
    case <-delCtx.Done():
    }
}
```

### 3. Session Treating L2.out Events as L2's Own (session.go:214-246)
```go
for {
    select {
    case e, ok := <-srcCh:  // Consumes all events from L2.out, 
                             // including relayed L3 events
        ev = e
    }
    // ... forward to Session.out
}
// Then at the end, uses whatever final content was accumulated,
// which might be L3's content if DoneEvent came from relayed L3 events!
```

## How ContextWindow Gets Corrupted

When `Session.AskStream` consumes relayed L3 events and L2 starts the next LLM iteration:

1. **Session forwarder emits relayed L3 events** to the outside world
2. **But internally, it accumulates state** from all events in its loop (lines 237-246)
3. **When DoneEvent is relayed from L3**, Session thinks it's L2's response
4. **Session pushes this to ContextWindow** (lines 249-254):
   ```go
   s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)
   ```
5. **Next iteration**, `cw.BuildPayload()` returns the corrupted message sequence
6. **API rejects** with HTTP 400

## Additional Problem: Tool Results Not Being Pushed Correctly

In `runOnceStreamWithHistory` (lines 467-474):
```go
// 同步路径：push tool results 到 cw
for i, tc := range toolCalls {
    cw.Push(ctxwin.RoleTool, results[i],
        ctxwin.WithToolCallID(tc.ID),
        ctxwin.WithToolName(tc.Function.Name),
        ctxwin.WithEphemeral(true),
    )
}
```

The `results[i]` contains the **string result from `execToolStream`**.

But `execToolStream` (line 1004) does:
```go
result, err := tool.Execute(toolCtx, args)
```

For DelegateTool, this returns the **delegated task's string response**. However, if the relay goroutine hasn't finished draining relayed events before `tool.Execute()` returns, there could be:

1. **Race condition**: Relayed events still in flight
2. **Message ordering violation**: Tool results pushed to cw before all L3 events are relayed
3. **Partial state**: L3's ToolCallDeltaEvent events leak into parent's message history

## Summary of Corruption Mechanism

```
L1 calls L2.AskStreamWithHistory(cw)
│
└─> Session.AskStream creates forwarder goroutine consuming L2.out
    │
    ├─> L2 calls LLM API → gets assistant(tool_calls: [delegate_tool])
    │
    ├─> L2.execToolStream calls DelegateTool.Execute
    │   │
    │   ├─> DelegateTool calls L3.AskStream
    │   │   │
    │   │   └─> L3.runOnceStreamWithHistory emits L3 events to DelegateTool
    │   │
    │   └─> DelegateTool forwards L3 events to parentEventCh (relayCh)
    │
    ├─> L2's relay goroutine receives L3 events and emits them to L2.out
    │
    └─> Session forwarder receives relayed L3 events and treats them as L2's
        output, accumulating finalContent/finalReasoning from L3's DoneEvent
        │
        └─> ContextWindow now has corrupted message: L2's assistant message
            followed by partial/incomplete data from L3's intermediate events
            
            When L1 calls L2 again, cw.BuildPayload() returns invalid sequence
            → API HTTP 400 error
```

