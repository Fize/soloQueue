# Workflow Execution Hardening — Design

## Problem

Workflow agents can submit undeclared outcomes because `workflow_handoff` exposes an unconstrained string and accepts it before the engine performs strict routing. The same boundary also terminates on tool errors, permits duplicate or mixed terminal-tool batches, does not enforce output limits, drops child stream errors, and silently ignores durable-write failures.

## Approach

Implement three independently revertible changes. First, make the handoff tool node-aware, validate exact outcomes and byte limits at the tool and engine boundaries, terminate only after success, and preserve child stream errors. Second, make terminal tools exclusive within an agent tool-call batch so no sibling side effect can race a handoff. Third, make workflow persistence transactional and failure-visible while deriving top-level run errors from terminal node failures.

## Dependencies

- Go standard library `testing`, `sync`, `encoding/json`, and `sort`
- Existing `agenttest.FakeLLM`, `agent.DefaultFactory`, `tools.TurnTerminator`, workflow engine, SQLite wrapper, and audit store
- No new runtime or test dependency

## Test Cases

### Stage 1: Handoff contract

- [ ] The handoff schema and system prompt expose the node's sorted allowed outcomes.
- [ ] An undeclared outcome is rejected without terminating the agent turn, then a corrected outcome succeeds.
- [ ] Invalid JSON does not terminate the agent turn.
- [ ] A second successful handoff returns `HANDOFF_DUPLICATE`.
- [ ] Content above the effective byte limit returns `OUTPUT_TOO_LARGE` at both the tool and engine boundaries.
- [ ] A child `ErrorEvent` is returned instead of being rewritten as `HANDOFF_MISSING`.
- [ ] A FakeLLM-driven workflow traverses the real agent executor with exact outcomes and leaves no registered temporary agent.

### Stage 2: Terminal tool isolation

- [ ] A single terminal tool executes and terminates normally.
- [ ] Multiple terminal tools in one response execute none of them.
- [ ] A terminal tool mixed with a regular tool executes neither tool and returns a retryable error.
- [ ] Existing regular-tool parallel execution remains unchanged.

### Stage 3: Persistence visibility

- [ ] Initial persistence failure prevents engine startup and rolls back the clean worktree.
- [ ] Any failed SQL statement rolls back the whole workflow snapshot transaction.
- [ ] Runtime persistence failure cancels the run and exposes `workflow_persistence_failed`.
- [ ] A failed node populates top-level `error_code` and `error_message` while retaining node detail.
- [ ] Restart readback preserves the same terminal error evidence.

## Explicitly Out of Scope

- Database schema migrations or rewriting historical workflow runs
- Workflow editor or run-dialog redesign
- Automatic retries for persistence failures
- New test frameworks, containers, metrics, or CI configuration
- Changes to L1/L2 workflow tool registration
