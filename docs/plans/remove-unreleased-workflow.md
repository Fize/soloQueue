# Unreleased Workflow Removal Record

SoloQueue's experimental YAML DAG Workflow feature was removed before release. The supported
personal automation path is Cron for scheduling, Skills for procedures, and Team/Delegate for
dynamic agent collaboration.

## Completed scope

- Removed the Workflow Web UI, API routes, L1 tools, runtime wiring, engine, persistence schema,
  telemetry origin, frontend dependencies, tests, and product documentation.
- Preserved Cron, Skill, Team, Delegate, Simulation, memory, channel, and ordinary agent behavior.
- Kept the database schema version unchanged because the removed schema was never distributed.
- Removed local-only development and smoke-test records after an explicit, recoverable backup.

## Local cleanup evidence

The only persisted records were three local development runs: two failed
`engineering-quality-loop` runs and one completed `codex-workflow-smoke-*` run. Before cleanup,
the active database contained 79 Workflow-origin metrics, six Workflow table families, three run
rows, three node-run rows, 17 events, 17 checkpoints, three worktree rows, and no confirmations.

The local cleanup created
`~/.soloqueue/backups/workflow-retirement-20260828T132355+0800/` containing a consistent SQLite
backup, a manifest, the experimental YAML definition, eight audit logs, and three broken
test-worktree remnants. The two clean Kumquat worktrees had no branch-only commits and were
removed through Git before their branches were deleted non-forcibly.

## Verification result

Verified on 2026-08-28:

- A fresh database contained no `workflow_%` schema objects.
- The active local database passed `PRAGMA integrity_check`, retained schema version 22, and
  contained zero Workflow-origin metrics and zero `workflow_%` schema objects after restart.
- `/api/workflows` returned the ordinary `404 {"error":"route not found"}` response.
- The two removed Kumquat worktrees and local branches were absent after cleanup.
- Production reference searches found only deliberate negative regression tests, not entrypoints
  or persistence contracts.
- Focused and full Go tests, 245 Web tests, the Web build, and `CI=true make build` passed.

Cron, Skills, Team/Delegate, Simulation, memory, channels, and ordinary chat were outside the
removal scope. Their automated test coverage passed, but no separate manual chat, delegation,
scheduled-run, notification, or adjacent-page walkthrough was performed as part of this cleanup.

## Recovery

The archive manifest records the source paths, database counts, Kumquat branch names, and exact
branch commits. To restore the retired local state:

1. Stop SoloQueue completely. Create a new timestamped rollback directory outside the active data
   path, for example `~/.soloqueue/backups/workflow-restore-attempt-<timestamp>/`.
2. Move the current `~/.soloqueue/soloqueue.db`, `soloqueue.db-wal`, and `soloqueue.db-shm` into that
   rollback directory. A sidecar that does not exist can be ignored, but do not delete any of these
   files. Confirm that no old `soloqueue.db-wal` or `soloqueue.db-shm` remains at the active path.
3. Copy only the consistent archive main database `soloqueue-before-cleanup.db` to
   `~/.soloqueue/soloqueue.db`. Do not copy an archived WAL or SHM sidecar into the active path.
4. Restart SoloQueue, run `PRAGMA integrity_check`, and verify the archived Workflow counts: 79
   Workflow-origin metrics, six Workflow tables, 3 runs, 3 node runs, 17 events, 17 checkpoints,
   3 worktrees, and 0 confirmations.
5. Only after the database checks pass, move the archived YAML and audit logs back to the source
   paths recorded in `manifest.md` if needed. Recreate either deleted Kumquat branch at its recorded
   commit, then use Git to add its worktree again. The three archived test-worktree remnants remain
   broken because their temporary repositories no longer exist.

The primary Kumquat worktree and its unrelated changes were not modified.

The source removal was committed locally after verification. No push was made, and the local
SQLite, archive, process, and Kumquat cleanup state was not added to Git.
