# Data, logs, and backup

> 中文：[数据、日志与备份](../zh/operations/data-and-backup.md)

I store application state under the work directory. I use ~/.soloqueue/ by
default and set SOLOQUEUE_WORK_DIR when I need another directory.

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

I expect the exact set of subdirectories to change as I use features. I treat
the whole work directory as sensitive.

## Safe backup

1. I stop the server and wait for the process to exit.
2. I copy the complete work directory to an encrypted, access-controlled backup.
3. I record the SoloQueue revision and OS alongside the backup.
4. I restart the server and verify that the UI can read the database and config.

Stopping first lets SQLite finish its WAL checkpoint and helps me avoid copying
a partially written timeline or workflow run.

## Restore

I stop SoloQueue, preserve the current directory as a separate recovery copy,
restore the backup to the configured work directory, and start the same or a
compatible revision. I check the server log for migration errors before
allowing agents to modify a restored project.

## Memory cleanup

I use the memory cleanup command as a reversible operation:

~~~bash
soloqueue memory cleanup --project-root /absolute/path/to/project
soloqueue memory cleanup --project-root /absolute/path/to/project --apply
~~~

I use the apply path to create a database backup and write a cleanup manifest. I
keep both until the audit and a manual sample review are complete.

## What not to back up publicly

I do not publish settings.yaml, SQLite, raw JSONL timelines, channel tokens,
provider keys, or generated artifacts until I remove secrets and user data.
