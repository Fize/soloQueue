# L1 Delegation Concurrency — Design

## Problem

Commit `e97f4e8` made `runJob` wait for every asynchronous delegation goroutine after the leader tool loop yielded. This prevents the L1 mailbox from processing Desktop, QQ, or WeChat messages until delegation completes. L1 channel routes are also held until the full response stream closes, so a delegation from one messaging channel blocks another channel even after the session releases `inFlight`.

## Approach

1. Separate normal job completion from shutdown cleanup: a yielded job releases the agent run loop immediately, while agent cancellation still waits for the job and tracked asynchronous tasks with the existing watchdog.
2. Keep channel route ownership for normal turns, but release an L1 route once `DelegationStartedEvent` is observed. The original request continues consuming its own event stream, so its response remains bound to the original QQ or WeChat message.
3. Reuse the existing Desktop `request_id` registry and L1 multi-request state. No protocol or frontend production change is required.
4. Preserve L2 channel behavior, normal non-delegation serialization, same-route pending merge, and session-wide cancellation semantics.

## Dependencies

- Go standard library `testing`, `context`, `sync`, and `time`.
- Existing `agenttest.FakeLLM`, async tool interfaces, session adapters, and WebSocket request registry.
- Existing Desktop Vitest tests.
- No new dependency.

## Affected Files

- `internal/server/request_ownership_test.go` — Desktop/WebSocket delegation concurrency regression test.
- `internal/agent/run.go` — release the mailbox after yield without weakening shutdown cleanup.
- `internal/session/qqbot_adapter_test.go` — QQ-to-WeChat delegation concurrency and response ownership regression test.
- `internal/session/qqbot_adapter.go` — release only L1 channel route ownership on delegation start.
- `desktop/src/hooks/useChatStream.test.ts` — existing multi-request test is verification-only unless the new backend behavior exposes a frontend gap.

## Test Cases

- [x] Desktop: while request A is blocked in asynchronous delegation, request B reaches the L1 LLM and owns an independent request stream before A completes.
- [x] QQ/WeChat: while a QQ request is blocked in asynchronous delegation, a WeChat request reaches L1 before the QQ delegation completes.
- [x] Channel ownership: QQ output is returned only to the QQ caller and WeChat output only to the WeChat caller.
- [x] Normal channel turn: a different route still waits while a non-delegating turn is active, and same-route pending merge remains unchanged.
- [x] Shutdown/cancellation: existing async lifecycle and session cancellation tests remain green under `go test -race`.
- [x] Desktop frontend: existing L1 multi-request completion/cancellation test remains green.

## Explicitly Out of Scope

- Parallelizing L2-bound channel sessions beyond their existing isolation model.
- Changing message persistence, timeline ordering, routing classification, notification delivery, or channel configuration.
- Adding a new queue, request protocol, dependency, retry policy, or UI state.
