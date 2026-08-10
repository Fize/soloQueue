# Data, logs, and backup

SoloQueue stores application state under the work directory. The default is
~/.soloqueue/; set SOLOQUEUE_WORK_DIR to use another directory.

## Important paths

| Path | Contents |
| --- | --- |
| settings.yaml | Providers, models, routes, tool policy, channels, and auth |
| soloqueue.db | Shared SQLite state, memory, teams, cron, and workflow records |
| logs/ | Application, HTTP, timeline, and scheduled-task logs |
| agents/ and groups/ | User agent templates and team definitions |
| skills/ | Installed user skills |
| plan/ and workspace/ | Plans and runtime workspace material |
| artifacts/ | Generated files and media where applicable |

The exact set of subdirectories changes as features are used. Treat the whole
work directory as sensitive.

## Safe backup

1. Stop the server and wait for the process to exit.
2. Copy the complete work directory to an encrypted, access-controlled backup.
3. Record the SoloQueue revision and OS alongside the backup.
4. Restart the server and verify that the UI can read the database and config.

Stopping first lets SQLite finish its WAL checkpoint and avoids copying a
partially written timeline or workflow run.

## Restore

Stop SoloQueue, preserve the current directory as a separate recovery copy,
restore the backup to the configured work directory, and start the same or a
compatible revision. Check the server log for migration errors before allowing
agents to modify a restored project.

## Memory cleanup

The memory cleanup command is reversible by design:

~~~bash
soloqueue memory cleanup --project-root /absolute/path/to/project
soloqueue memory cleanup --project-root /absolute/path/to/project --apply
~~~

The apply path creates a database backup and writes a cleanup manifest. Keep
both until the audit and a manual sample review are complete.

## What not to back up publicly

Do not publish settings.yaml, SQLite, raw JSONL timelines, channel tokens,
provider keys, or generated artifacts without removing secrets and user data.
