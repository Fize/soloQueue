# Workflow v2 implementation

This implementation is the durable entry point for the built-in engineering
quality loop and user-authored workflows.

## Contract

Every new run accepts `task.goal` and at least one
`task.acceptance_criteria`; `task.constraints` and `task.delivery` are
optional. Legacy `input` is accepted only for the existing v1 API/tool
compatibility path and does not create a v2 isolated run.

## Execution boundary

1. Persist `preparing_worktree` and the task contract.
2. Create a dedicated Git worktree and branch from the requested base ref.
3. Execute the DAG in that worktree.
4. Persist state snapshots, normalized node attempts, checkpoints, and a
   hash-chained JSONL audit record.
5. Run delivery actions only when explicitly enabled by `task.delivery`.

The original checkout is never used as the writer workspace. Worktrees and
audit files are retained until an explicit cleanup request.

## Recovery

On process startup, pending/in-flight runs become `interrupted` with
`resume_available=true`; no run is automatically resumed. The user chooses
Resume, Restart, Abandon, or explicit worktree cleanup. Resume checks for
worktree drift and requires `allow_dirty=true` when the worktree no longer
matches its recorded base. Restart creates a new run and worktree linked via
`restarted_from_run_id` and `successor_run_id`.

## Built-in workflow

`engineering-quality-loop` is catalogued but is not installed or executed at
startup. Installation is explicit and idempotent. Its bounded loop is:

`analyze → plan → develop → completion_check ⇄ completion_fix → review ⇄ review_fix → test ⇄ test_fix → final_check`.

Each check can route to a fix node; the final node reports completed, blocked,
or failed. Commit, push, and pull-request operations are not graph nodes.

## Data and rollback

The SQLite schema adds `workflow_node_runs`, `workflow_run_events`,
`workflow_run_checkpoints`, `workflow_worktrees`, and
`workflow_confirmations`, plus run metadata columns. The migration is
idempotent. To roll back application code, stop the server and restore the
previous binary; the additive tables and columns are ignored by the v1 reader.
Do not delete worktrees or audit files as part of a code rollback; inspect and
clean them explicitly after confirming the corresponding run is no longer
needed.
