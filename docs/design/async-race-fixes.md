# Async Race Fixes — Design

## Problem

The asynchronous delegation path returns a result slice that its watcher later mutates, allowing the stream loop and watcher to access the same element concurrently. A session regression test also shares an unsynchronized call counter between the agent goroutine and the test goroutine.

## Approach

Keep the existing immediate-result contract, but give `asyncTurnState` a distinct result slice for final delegation output. Reuse the standard library `sync/atomic` package for the test-only LLM call counter; no production API or dependency changes are required.

## Dependencies

Go standard library only: `testing`, `sync/atomic`, and the existing synchronization already used by the agent lifecycle.

## Test Cases

- [x] Update `TestExecToolsWithAsync_SingleAsyncTool` to prove the returned placeholder remains unchanged after the asynchronous result completes, then run it with `-race`.
- [x] Run `TestSession_AskStream_CancelAfterToolCall_NoDuplicateTimeline` with `-race`, replace its test-only counter with an atomic counter, and prove the test passes.

## Explicitly Out of Scope

Changing delegation messages, tool-result formatting, agent scheduling, cancellation behavior, or production session logic.
