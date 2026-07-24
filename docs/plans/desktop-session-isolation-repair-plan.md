# Desktop Session Isolation Repair Plan

Status: proposed; ready for implementation after approval
Scope: L1/L2 session identity, lifecycle, request ownership, WebSocket delivery, context-window metrics, desktop state isolation, rendering, reconnect recovery, Cron/channel reuse, and regression coverage
Primary symptom: L1 `ctxwin` sometimes displays a fixed value and active replies sometimes stop updating

## 1. Decision summary

Adopt the following decisions:

1. Use one canonical external session ID everywhere: `l1` or `l2:<uuid>`.
2. Enforce exactly one live `Session` and one leader agent instance for each active L2 session.
3. Bind every chat request to one session and one assistant message:
   `session_id -> request_id -> assistant_message_id`.
4. Treat persisted history, live request state, and aggregate runtime metrics as separate data sources. A renderer must not substitute one for another.
5. Remove the global “active L2 session” as a source of context-window or UI state.
6. Make session deletion a complete lifecycle operation, not just removal from a map and deletion of timeline files.
7. Preserve queued-input behavior, but make the active response update its assigned assistant message instead of “the last assistant message”.
8. Use deterministic reconnect recovery through per-session runtime state and history reload. Streaming replay/resume is out of scope for this repair.
9. Reuse an interactive session from Cron or a channel only by exact session ID. Never select a session by group or agent template alone.
10. Keep genuinely global data global: connection status, auth, model/configuration catalogs, teams, skills, language, and layout preferences do not need per-session copies.

The repair is complete only when identity and cleanup invariants hold across the backend, protocol, store, and renderer. Fixing the displayed `ctxwin` number alone is insufficient.

## 2. Goals and non-goals

### Goals

- An L2 session cannot be activated into two `Session` or leader-agent instances, even when history, list, and send requests race.
- L1 and every L2 session report their own context-window usage.
- A request event can update only the session and assistant message that own the request.
- Switching sessions cannot redirect Stop, tool confirmation, streamed content, history, attachments, design context, or changes data to another session.
- Deleting a session cancels its work, reaps children, stops and unregisters its agent, removes runtime watches and supervisors, closes resources, clears frontend state, and then removes persisted data.
- WebSocket disconnects cannot leave a session permanently marked as streaming.
- Cron and channel traffic cannot mutate an arbitrary desktop conversation.
- Existing timeline and `meta.json` data remain readable. The repair does not require destructive data migration.
- Tests reproduce every confirmed failure before the corresponding behavior is changed.

### Non-goals

- Persisting historical model/level badges for every old message.
- Resuming and replaying partial token streams after reconnect.
- Supporting multiple simultaneous active requests inside the same session.
- Replacing Zustand, WebSocket, the actor model, or timeline event sourcing.
- Redesigning the entire desktop application around a new state framework.
- Changing L0-L3 routing semantics, compaction policy, or memory behavior.
- Cleaning unrelated dead code or reorganizing unrelated packages.

## 3. Current failure model

The present implementation mixes five identities:

| Identity | Current representation | Failure |
| --- | --- | --- |
| Session | `l1`, `l2:<uuid>`, bare UUID, empty string | Different endpoints interpret invalid/empty values differently |
| Runtime session | `*session.Session` | Concurrent L2 activation can create more than one instance |
| Agent | template ID and instance ID | Some lookups use non-unique template ID |
| Request | WebSocket `request_id` | Desktop keeps one component-global current request |
| Render target | “last assistant message” | Queued input or history replacement changes the target |

The resulting broken path is:

```text
history GET
    -> lazy L2 activation races
    -> two Session/Agent instances
    -> one instance overwrites the store entry
    -> another request may still use the orphan instance
    -> global activeSession points at whichever history request ran last
    -> global runtime ctxwin is rendered as L1
```

The second broken path is:

```text
request A in session A
    -> switch to session B
    -> request B overwrites activeRequestIdRef
    -> switch back to A and press Stop
    -> request B ID is sent with session A ID
    -> backend cancels A and completes B's handler
```

The third broken path is:

```text
assistant response is streaming
    -> user submits queued input
    -> queued user message becomes the last message
    -> response handler updates "last assistant"
    -> target is no longer an assistant
    -> content/thinking/tool deltas are dropped
```

## 4. Target invariants

These invariants are the contract for all implementation and tests.

### 4.1 Identity

1. External APIs and WebSocket messages use only `l1` or `l2:<uuid>`.
2. Internal L2 storage may use the bare UUID, but conversion happens through one parser.
3. Every active L2 session has exactly one `Session`, one leader `Agent.InstanceID`, one supervisor, one context window, and one timeline writer.
4. Agent metadata is joined to a session by `Agent.InstanceID`, never by template ID.

### 4.2 Request ownership

1. A session has at most one active request.
2. An active request record contains `SessionID`, `RequestID`, `AgentInstanceID`, and cancellation state.
3. Every chat event contains `session_id` and `request_id`.
4. A desktop request record additionally contains the target assistant message ID.
5. Cancel and tool-confirm operations must match the owning session and request before mutating anything.
6. A late event from an old request may be logged and ignored; it must not alter a newer request.

### 4.3 Data ownership

| Data | Owner/key |
| --- | --- |
| Messages and history cursor | session ID |
| Active request, route, streaming, delegation | session ID plus request ID |
| Content/thinking/tool segments | session ID plus assistant message ID/call ID |
| Context-window usage | session ID |
| Agent stream | agent instance ID, joined through session runtime metadata |
| Draft, mentions, attachments | session ID |
| Design file, strokes, selected target | session ID |
| Changes request and result | session ID plus request generation |
| Connection/auth/config/team/skill state | application global |

### 4.4 Lifecycle

```text
inactive -> activating -> active -> deleting -> deleted
                 |           |
                 +-> failed  +-> cancelling/reaping/closing
```

- Only one goroutine owns `activating`.
- Waiters receive the same created instance or the same activation error.
- A failed activation can be retried.
- Delete prevents new activation and waits for an in-progress activation to finish or cancel.
- Delete is idempotent at the internal lifecycle layer.
- No map entry or global pointer may reference a deleted session.

## 5. Backend design

### 5.1 Canonical session ID parsing

Add a small parser in `internal/session`, for example:

```go
type SessionKind uint8

type SessionRef struct {
    Kind SessionKind
    L2ID string
}

func ParseSessionID(raw string) (SessionRef, error)
func (r SessionRef) String() string
```

Rules:

- `l1` is the only canonical L1 value.
- `l2:<uuid>` must contain a valid non-empty UUID.
- Empty values are accepted only by explicitly documented legacy REST call sites during one compatibility release; they are normalized to `l1` before resolution.
- WebSocket send/cancel/confirm and uploads reject empty or malformed IDs.
- REST and WebSocket resolvers use the same parser and error mapping.

Do not add a general-purpose ID framework. This helper exists only to remove the current divergent parsing paths.

### 5.2 Single-flight L2 activation

Extend `L2SessionEntry` with private lifecycle state sufficient to coordinate activation and deletion:

```go
state          l2LifecycleState
activationDone chan struct{}
activationErr  error
activationStop context.CancelFunc
```

Required behavior:

1. The first caller changes `inactive` to `activating`, creates the completion channel, and builds outside the store lock.
2. Concurrent callers wait on the same completion channel, respecting their own context cancellation.
3. On success, the builder publishes exactly one `Session`.
4. On failure, every waiter receives the error and the entry returns to a retryable state.
5. If deletion starts during activation, it cancels the build context, prevents publication, waits for cleanup, and leaves no registered agent/supervisor.
6. A built instance that loses ownership must be fully destroyed before returning. It must never be returned as an orphan.

Required tests:

- 20-100 concurrent `Get` calls produce one builder call and one pointer.
- Activation failure is shared by waiters and can be retried.
- Delete during activation leaves no session, agent, supervisor, watcher, or timeline handle.
- Context cancellation of one waiter does not cancel the shared activation unless deletion occurs.

### 5.3 Complete L2 destruction

Keep `Session.Close` as resource closure, but add one runtime-aware teardown path owned by the builder/store, for example `Builder.DestroyL2`.

Teardown order:

1. Mark the entry `deleting`; reject new asks/activation.
2. Cancel the current request tree and clear pending prompts.
3. Reap supervisor children with a bounded timeout.
4. Stop the leader agent with a bounded timeout.
5. Unregister the leader agent from `AgentRegistry`.
6. Remove its runtime stream watch through the registry callback.
7. Remove the supervisor from `runtime.Stack`.
8. Close the timeline writer and session logger.
9. Remove the entry from the store.
10. Remove the timeline directory only for explicit user deletion. Shutdown must preserve it.

If a step fails:

- Continue best-effort cleanup.
- Return/join the errors after all cleanup attempts.
- Do not restore the session to active state.
- Log `session_id`, `agent_instance_id`, cleanup step, and error.
- A user deletion response is successful only when runtime ownership is gone. A timeline deletion failure returns an error but must not resurrect runtime state.

`Shutdown` must use the same teardown path with `deletePersistentData=false`.

Deployment note: restarting the backend after this phase is required to clear runtime orphans created by older binaries. No persisted data cleanup is needed.

### 5.4 Remove the global active-session pointer

Delete `L2SessionStore.activeSession`, `ActiveSession`, and `SetActiveSession`.

History reads must not mutate runtime selection. Reading `/api/session/history` should:

- read timeline data;
- obtain context usage from the exact resolved session when it is active;
- use a persisted/replayed snapshot when activation is explicitly required;
- never change another API's notion of “current”.

If avoiding activation for a history-only request is feasible after the single-flight fix, prefer a read-only token calculation or metadata snapshot. If replay is still required for accurate usage, activation is allowed but remains session-local.

### 5.5 Per-session runtime snapshot

Keep aggregate runtime metrics for application-wide status, but add session-scoped state:

```go
type SessionRuntimeStatus struct {
    SessionID       string `json:"session_id"`
    State           string `json:"state"`
    AgentInstanceID string `json:"agent_instance_id,omitempty"`
    RequestID       string `json:"request_id,omitempty"`
    CtxwinUsed      int    `json:"ctxwin_used"`
    CtxwinLimit     int    `json:"ctxwin_limit"`
    TaskLevel       string `json:"task_level,omitempty"`
    ModelID         string `json:"model_id,omitempty"`
}
```

Expose it as a map keyed by canonical session ID in the existing state message or a dedicated `session_state` message.

Compatibility:

- Keep `runtime.current_tokens` and `runtime.max_tokens` for one release if the portal still consumes them.
- Desktop L1/L2 chat must stop reading these global fields.
- Mark the legacy fields as aggregate/compatibility fields and remove the `ActiveSession` update loop.

Updates must be emitted on:

- session activation/deletion;
- request start/route/done/error/cancel;
- agent state transitions;
- context-window mutations, compaction, clear, and rewind.

### 5.6 Agent-to-session mapping

Build agent responses from instance ownership:

- L1 maps its known leader instance to `l1`.
- Each active L2 entry maps `Session.Agent.InstanceID` to `l2:<uuid>`.
- `AgentInfoResponse` gains `session_id`.
- Replace `FindActiveSessionByAgentID(templateID)` with exact instance lookup or a runtime snapshot map.
- Never infer `is_qbot`, level lock, or last level from the first session sharing a template.

Template IDs remain valid for configuration and team composition, not runtime identity.

### 5.7 Request ownership in the WebSocket hub

Extend the server-side active request record:

```go
type activeRequest struct {
    SessionID       string
    RequestID       string
    AgentInstanceID string
    Cancel          context.CancelFunc
    Delegating      bool
}
```

Protocol rules:

- `chat_send` registers `{session_id, request_id}` atomically before streaming.
- A duplicate `request_id` is rejected.
- `chat_cancel` succeeds only when both IDs match an active record owned by that client.
- `tool_confirm` includes `request_id` and is validated against the request/session/call.
- Mismatch returns a request-scoped error and performs no cancellation or confirmation.
- Slash-command early-return paths always remove the active request via one `defer`.
- Every outbound request event includes `session_id`.

Use one finalization helper for done, error, cancel, disconnect, and slash commands so active-request cleanup cannot be skipped.

### 5.8 WebSocket state broadcast

Replace the reset-on-every-notification trailing debounce with bounded coalescing:

- Start one short timer when the first notification arrives.
- Ignore/coalesce subsequent notifications while it is pending.
- Broadcast no later than the configured maximum interval.
- Guarantee an immediate or bounded final broadcast on request terminal state.

Tests must run a continuous notification producer and prove that processing and done snapshots are both delivered.

### 5.9 Cron and channel session resolution

Remove `FindByGroup` as a way to select an interactive session.

Rules:

- Reuse requires an exact canonical bound session ID.
- Validate that the bound session belongs to the expected channel/account before reuse.
- If no exact binding exists, Cron uses `BuildL2ForCron` and its isolated timeline/context.
- Ambiguous group matches are an error, not “first map entry wins”.
- Cron metadata and results must not appear in a desktop session unless that exact session was configured as the target.

The plan does not require redesigning notification delivery; it only fixes execution-session ownership.

### 5.10 Persist stable metadata

Persist `created_at` in `meta.json` for newly created L2 sessions.

For legacy sessions without it:

1. Use the earliest timeline event timestamp when available.
2. Otherwise use directory metadata as a read-time fallback.
3. Write the resolved timestamp on the next ordinary metadata update.

Do not bulk-rewrite existing timeline files.

## 6. Desktop state design

### 6.1 Session-owned chat state

Replace scattered request flags with one session-keyed runtime record:

```ts
interface ActiveChatRequest {
  requestId: string
  assistantMessageId: string
  agentInstanceId?: string
  status: 'starting' | 'streaming' | 'delegating' | 'cancelling'
  route?: ChatRouteInfo
  systemCommand: boolean
}

interface SessionChatRuntime {
  activeRequest?: ActiveChatRequest
  historyGeneration: number
  historyLoading: boolean
  historyHasMore: boolean
  historyCursor: string | null
}
```

Keep `messages` keyed by session ID. A complete migration to a deeply nested store is not required if it makes selectors harder; the mandatory change is that request-lifecycle state is grouped and updated atomically per session.

Remove:

- component-global `activeRequestIdRef`;
- fallback setters that silently use `activeSessionId`;
- updates based on “the last assistant message”.

### 6.2 Exact render targets

When sending:

1. Resolve and freeze `sessionId`.
2. Create `requestId`.
3. Create the assistant placeholder and capture `assistantMessageId`.
4. Store the active request atomically under that session.
5. Register the handler with the same three IDs.

Every handler:

- checks that the session still owns the request;
- updates the exact assistant message by ID;
- ignores late events from a superseded request;
- finalizes only the matching request.

Tool completion continues locating a segment by `callId`, but it must remain inside the request's assistant message when possible.

### 6.3 Queued input behavior

Preserve the current server pending queue with an explicit UI contract:

- A queued user message may be appended after the active assistant placeholder.
- The active response continues updating its original assistant message by ID.
- The queued send receives a `chat_queued` acknowledgment instead of an unhandled `chat_error`.
- The queued message displays a queued state until the active turn consumes it.
- Stop cancels the active request and clears queued prompts for that session unless product behavior is explicitly changed later.

Do not create a second active request handler for the same session.

### 6.4 History request coordination

Maintain an in-flight history request per session:

```ts
Map<SessionID, {
  generation: number
  controller: AbortController
  promise: Promise<void>
}>
```

Rules:

- Identical full-history loads coalesce.
- A newer full reload aborts or invalidates the older generation.
- A response applies only if its generation is still current.
- `historyLoading` remains true until the current generation finishes.
- `loadMore` is single-flight per session/cursor.
- Messages merge/deduplicate by stable server message ID.
- History never overwrites an active local request.
- Terminal request handling reloads history only after the matching request state is cleared.

### 6.5 Rendering source precedence

For each visible session:

1. Persisted/local `messages[sessionId]` is the primary conversation.
2. The active request handler mutates its exact assistant message.
3. Agent streams are diagnostic/fallback data only and must match the session's `agent_instance_id`.
4. The existence of any historical assistant message must not suppress current live content.
5. A fallback virtual message, if retained, is keyed by request ID and shown only when the current request lacks a local assistant target.

L1 and L2 use the same rendering rules. `AssistantPage` must read `sessionRuntime['l1']`, not aggregate runtime context fields.

### 6.6 Session switching

Switching changes only `activeSessionId`; it must not:

- overwrite another session's request;
- unregister another session's handler;
- clear another route;
- reuse another request's Stop target;
- load history under an empty-string key.

The agent-state transition tracker must be keyed by session ID rather than one `prevAgentState` ref shared across session switches.

Route navigation without a session uses `null`, not `""`. `setActiveSession` must not issue history requests for null/invalid IDs.

### 6.7 Reconnect recovery

Use a deterministic non-resume policy:

1. On socket close, mark each client-owned active request as detached and stop accepting its token events.
2. Do not keep handlers alive under the assumption that a new socket will receive the old stream.
3. On reconnect, fetch the per-session runtime snapshot.
4. If the backend reports the session idle, clear local active state and reload history immediately.
5. If it reports processing, show a session-scoped “processing after reconnect” state and reload history when the terminal runtime transition arrives.
6. Add a bounded fallback poll so a missed state event cannot leave the UI stuck.

The backend may continue the task after client disconnect because `AskStream` intentionally uses a non-cancelled execution context. This plan preserves that behavior while making the UI honest about the detached stream.

### 6.8 Title authority

Make the backend session name authoritative:

- Use the existing `session_name` event and session-list response.
- Do not derive “title already generated” only from ephemeral desktop memory.
- Do not rename a non-empty session after desktop reload.
- Remove the client fallback title generation after server behavior is covered by tests.

### 6.9 Drafts, attachments, and mentions

Choose one of these two explicit behaviors; use the first unless product direction changes:

1. **Per-session drafts:** store text, mentions, and attachment metadata under session ID and restore them on switch.
2. **Reset on switch:** clear and revoke everything when session ID changes.

The implementation plan selects per-session drafts because it avoids accidental loss while preserving isolation.

Attachment rules:

- Capture the owning session ID before upload.
- Ignore or discard a completion result if the draft/session was deleted.
- Never send a path uploaded for session A in session B.
- Revoke object URLs on draft deletion, session deletion, and component unmount.

### 6.10 Design and inspector state

Session-key or reset the following:

- active design tab;
- closed design tabs;
- design submode when it affects output;
- strokes;
- selected DOM target;
- active design file;
- parent `designContextRef`;
- selected changes file;
- changes response/error/loading generation.

Global enablement of Design Mode may remain a preference. Design content and selected targets may not.

`SessionChangesPanel` must abort or invalidate requests when `sessionId` changes. Polling cleanup must prevent an already-running old response from applying.

### 6.11 Session deletion in the store

After backend deletion succeeds, one action removes all data for the session:

- metadata entry;
- messages/history state;
- active request and route;
- streaming/delegation/system-command state;
- draft/attachments/mentions;
- design and changes state;
- cached agent/session runtime mapping.

If the deleted session was active, navigate to the new-chat route and set `activeSessionId` to `null`.

A stale session-list response must not re-add a deleted session. Track a request generation or deletion tombstone until a newer authoritative list response arrives.

## 7. API and protocol changes

Additive server-to-client fields/messages:

- `session_id` on every chat event.
- `session_runtime` map or `session_state` message.
- `session_id` on `AgentInfoResponse`.
- `chat_queued` acknowledgment.
- Request-scoped ownership errors with stable codes.

Client-to-server changes:

- `tool_confirm` gains `request_id`.
- All chat operations require canonical `session_id`.

Suggested stable error codes:

| Code | Meaning |
| --- | --- |
| `invalid_session_id` | Session ID is malformed |
| `session_not_found` | Canonical session does not exist |
| `session_activating` | Optional retryable activation state |
| `session_deleting` | Operation rejected during deletion |
| `duplicate_request_id` | Request ID is already registered |
| `request_session_mismatch` | Request does not belong to session |
| `request_not_active` | Request is already terminal or unknown |
| `tool_confirmation_mismatch` | Call/request/session ownership failed |

Unknown additive fields remain safe for older clients. Because desktop and backend ship together, no dual-write request protocol is required, but keep legacy aggregate runtime fields for the portal compatibility window.

## 8. Error and concurrency strategy

- Invalid identity: reject before resolving or mutating a session.
- Duplicate activation: wait for the owner; never build a second instance.
- Activation failure: clean partial resources, publish one error, allow retry.
- Delete during work: cancel, reap, stop, unregister, close, then remove files.
- Late WebSocket event: ignore after ownership check and emit a sampled diagnostic log.
- History race: generation check prevents stale application.
- Disconnect: detach UI stream; recover from session runtime/history.
- Slow WebSocket client: dropping aggregate snapshots is allowed only because the next bounded snapshot is guaranteed. Request terminal events should use the request channel and must not depend solely on aggregate state.
- Cleanup timeout: continue remaining cleanup, return a joined error, and expose the failed step in logs.

No operation should silently fall back to L1 because a session ID is malformed.

## 9. Observability

Use the same correlation fields in backend logs:

```text
session_id
request_id
agent_instance_id
client_id
event_type
lifecycle_state
```

Add counters or structured diagnostics for:

- activation owner/waiter count;
- duplicate activation prevented;
- active session count;
- active request count by session;
- request/session mismatch;
- ignored late events;
- session cleanup failures by step;
- reconnect recovery;
- stale history response discarded;
- broadcast delay/max coalescing interval.

Do not log prompts, auth tokens, attachment contents, or tool-confirmation secrets.

## 10. Implementation phases

### Phase 0 — Characterization and guardrail tests

Changes:

- Add failing backend tests for concurrent activation, deletion cleanup, strict session parsing, request ownership, and debounce starvation.
- Add failing desktop tests for A/B switching, queued input, stale history, reconnect, and exact assistant targets.
- Capture current log/API reproduction steps in test comments or a short fixture note.

Exit criteria:

- Every P0 failure has a deterministic failing test.
- Tests do not depend on real LLM calls.
- `FakeLLM` and fake builders/registries are used.

### Phase 1 — Backend identity and lifecycle

Changes:

- Canonical parser.
- Single-flight activation state.
- Complete `DestroyL2`.
- Shutdown reuse.
- Remove global `activeSession`.
- Exact agent-instance mapping.

Exit criteria:

- Concurrent activation creates one instance.
- Delete and shutdown leave no registry/supervisor/watch entries.
- No `ActiveSession` symbols remain.
- Existing timelines replay unchanged.

### Phase 2 — Protocol ownership and session runtime

Changes:

- Bind active requests to session/agent.
- Validate cancel and confirmation ownership.
- Add `session_id` to all events.
- Add per-session runtime state.
- Fix slash-command cleanup.
- Fix bounded state coalescing.
- Remove group/template runtime selection.

Exit criteria:

- Cross-session cancel/confirm attempts are rejected without side effects.
- L1 and L2 context values can be observed simultaneously and independently.
- Continuous notifications cannot starve a terminal snapshot.

### Phase 3 — Desktop request and rendering state

Changes:

- Replace `activeRequestIdRef`.
- Store exact assistant message targets.
- Update handlers and Stop logic.
- Implement queued acknowledgment.
- Remove “last assistant” writes.
- Unify L1/L2 rendering source precedence.

Exit criteria:

- A and B can stream concurrently without crossed content or Stop actions.
- Queued input does not stop the visible response.
- Old request completion cannot clear a new route/request.
- L1 ctxwin comes from `sessionRuntime['l1']`.

### Phase 4 — History, reconnect, and peripheral isolation

Changes:

- History generations/abort/coalescing.
- Deterministic reconnect recovery.
- Server-authoritative title.
- Per-session drafts/attachments/mentions.
- Per-session design and changes state.
- Complete frontend deletion cleanup and stale-list protection.

Exit criteria:

- Stale history/changes/upload responses cannot alter another or newer session.
- Reconnect ends in either processing recovery or authoritative idle history, never permanent local streaming.
- Switching sessions preserves only that session's draft/design data.

### Phase 5 — Compatibility cleanup and documentation

Changes:

- Remove desktop consumers of legacy global ctxwin.
- Search for old active-session, template-ID lookup, and last-assistant symbols.
- Update architecture/session protocol documentation.
- Decide portal migration timing before removing legacy runtime fields.

Exit criteria:

- Repository-wide search finds no forbidden old ownership paths.
- Migration and compatibility comments have removal conditions.
- Final regression suite and manual matrix pass.

## 11. File-level change map

Expected primary files:

| Area | Files |
| --- | --- |
| Session identity/lifecycle | `internal/session/l2_store.go`, new focused ID/lifecycle tests, `internal/session/builder.go`, `internal/session/session.go` |
| Runtime/registry | `internal/runtime/stack.go`, `internal/agent/registry.go`, `internal/server/agent_handlers.go` |
| WebSocket protocol | `internal/server/hub.go`, `internal/server/chat_ws.go`, server WebSocket tests |
| REST/history/upload | `internal/server/session_handlers.go`, handler tests |
| Cron ownership | `cmd/soloqueue/cli/commands.go`, Cron/session wrapper tests |
| Desktop protocol types | `desktop/src/types/chat.ts`, `desktop/src/types/agent.ts` |
| Desktop transport | `desktop/src/lib/websocket.ts`, WebSocket tests |
| Desktop chat state | `desktop/src/stores/chatStore.ts`, `chatStore.test.ts` |
| Request lifecycle | `desktop/src/hooks/useChatStream.ts`, new hook tests |
| Main renderers | `desktop/src/components/AssistantPage.tsx`, `desktop/src/components/ChatPage.tsx` |
| Draft/design/changes | `ChatInput.tsx`, `ChatDesignPanel.tsx`, `SessionChangesPanel.tsx` and focused tests |
| Documentation | this plan and session/protocol architecture docs |

This list is a boundary, not permission for unrelated refactoring. A phase should touch only files required by its tests and contract.

## 12. Test matrix

### Backend unit and race tests

- 100 concurrent activations for one L2 ID.
- Concurrent activation for different L2 IDs remains parallel.
- Activation failure and retry.
- Delete while inactive, activating, active-idle, processing, delegating, and awaiting tool confirmation.
- Repeated delete is internally idempotent.
- Shutdown preserves timelines but removes runtime ownership.
- Strict IDs: empty, bare UUID, malformed prefix, empty L2 UUID, valid L1/L2.
- Cancel with correct and incorrect request/session pairs.
- Confirm with correct and incorrect request/session/call triples.
- Slash commands leave zero active requests.
- Continuous `Notify` traffic still broadcasts within the maximum interval.
- Cron without an exact binding creates an isolated session.
- Two sessions with the same leader template report independent metadata.

Run relevant packages with `-race` where the environment permits:

```bash
GOCACHE=/tmp/soloqueue-go-cache go test -race ./internal/session ./internal/server
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/cron ./cmd/soloqueue/cli/...
```

### Desktop unit/component tests

- Send in A, switch to B, send, switch to A, Stop: only A is cancelled.
- A completion after B starts does not clear B.
- Queued user message does not redirect A's assistant deltas.
- Tool events update the exact assistant/request.
- Two concurrent history loads coalesce or discard the stale generation.
- History completion cannot overwrite a live assistant placeholder.
- Session deletion clears every keyed map and pending handler.
- Stale list response cannot resurrect a deleted session.
- Fast reconnect while backend continues processing.
- Reconnect after backend has completed.
- Design/changes/upload response from A cannot apply to B.
- Existing non-empty title is not regenerated after reload.
- L1 and L2 display different simultaneous ctxwin values.
- Historical assistant content does not suppress the current live fallback.

Commands:

```bash
cd desktop
pnpm test
pnpm build
```

### Manual end-to-end matrix

| Scenario | Expected result |
| --- | --- |
| Open several L2 histories rapidly | One agent instance per session |
| L1 and L2 both contain history | Each displays its own ctxwin |
| Stream in A while viewing B | A continues; B remains unchanged |
| Stream in A and queue another prompt | Existing reply continues updating |
| Stop A after visiting B | Only A stops |
| Delete a processing L2 | Work stops and agent disappears from runtime |
| Disconnect for under 8 seconds | UI recovers without a stuck stream |
| Disconnect until task completes | Reconnect loads final persisted history |
| Run Cron for an L2 team | No arbitrary desktop timeline is modified |
| Switch with draft/image/design target | State remains owned by its original session |
| Restart backend | Stable titles/order/history and no orphan runtime objects |

Inspect actual timeline JSONL, agent registry responses, session list/history APIs, and structured logs during this matrix. UI appearance alone is not sufficient evidence.

## 13. Acceptance criteria

The complete repair is accepted only when all are true:

1. No code path can create two live L2 runtime instances for one ID.
2. No global active-session pointer is used for ctxwin or routing display.
3. Every active request has a verified session owner and exact render target.
4. No chat stream mutation uses “last assistant message”.
5. Delete and shutdown lifecycle tests prove registry/supervisor/watch cleanup.
6. Invalid session IDs never fall back to L1.
7. Cron/channel reuse requires an exact binding.
8. History, reconnect, upload, changes, and design async results are generation/session guarded.
9. L1/L2 context usage is independently rendered from session-scoped state.
10. Old completion, error, cancel, and disconnect events cannot affect a newer request.
11. All new regression tests, existing tests, desktop build, and `git diff --check` pass.
12. Repository searches show no obsolete ownership symbols except documented compatibility fields.

Suggested final searches:

```bash
rg -n "ActiveSession|SetActiveSession|FindByGroup|FindActiveSessionByAgentID" .
rg -n "activeRequestIdRef|appendToLastAssistant|updateLastAssistant" desktop/src
rg -n "current_tokens|max_tokens" desktop/src/components desktop/src/hooks
rg -n "strings.HasPrefix\\(.*l2:" internal/server
```

Any remaining match must have a documented, tested reason.

## 14. Rollout, compatibility, and rollback

### Rollout

1. Ship backend lifecycle and additive protocol fields first within the same release branch.
2. Ship the desktop consumer immediately after; packaged desktop/backend builds should be released together.
3. Restart the backend during upgrade to clear old in-memory orphan agents.
4. Preserve all timeline directories and `meta.json`.
5. Monitor mismatch, cleanup-failure, and reconnect-recovery logs.
6. Remove legacy global ctxwin fields only after confirming the portal has migrated.

### Rollback

- Phases should be separate scoped commits so protocol/frontend changes can be reverted independently.
- Additive fields are safe to leave in the backend during a desktop rollback.
- Do not roll back by restoring the global active-session pointer.
- If session runtime broadcasting causes load issues, temporarily poll the session runtime endpoint; retain correct per-session ownership.
- Timeline data requires no rollback migration.

## 15. Commit boundaries

Recommended commits:

1. `test: characterize desktop session isolation failures`
2. `fix: serialize l2 activation and teardown`
3. `fix: bind runtime and websocket requests to sessions`
4. `fix: scope desktop chat requests and render targets`
5. `fix: isolate history reconnect and session ui state`
6. `docs: document session ownership and lifecycle`

Each commit must:

- include its focused tests;
- avoid unrelated formatting or cleanup;
- pass the relevant targeted suite;
- leave compatibility behavior explicit;
- not stage unrelated user changes from a dirty worktree.

## 16. Open decisions before implementation

These are bounded choices, not blockers to the overall design:

1. Whether session runtime is embedded in the existing `state` payload or emitted as a dedicated message. Prefer the existing payload if size remains small.
2. Whether legacy empty REST session IDs are supported for one release or rejected immediately. Prefer one-release normalization for REST only.
3. Whether queued user messages show a visible “queued” badge. The protocol acknowledgment is required; the badge is optional presentation.
4. Whether per-session drafts survive application restart. In-memory per-session isolation is required; persistence can remain out of scope.

No implementation should begin by changing these into broader product features.
