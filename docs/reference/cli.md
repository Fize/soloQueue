# CLI reference

The primary executable is soloqueue. Run soloqueue --help for the command tree
of the checked-out revision.

## Server

~~~bash
soloqueue serve [flags]
~~~

| Flag | Default | Purpose |
| --- | --- | --- |
| --port, -p | 57647 | HTTP server port; 0 chooses a random port |
| --host | 127.0.0.1 | Bind address |
| --verbose, -v | false | Write logs to stderr |
| --bypass | false | Bypass all tool confirmations |

## Version

~~~bash
soloqueue version
~~~

Prints the application version.

## Skills

~~~bash
soloqueue skills report [--work-dir path] [--db path] [--days 30]
~~~

Prints JSON governance data for installed skills, including invocation
lookback and description quality.

## Memory

~~~bash
soloqueue memory audit [--db path]
soloqueue memory cleanup [--db path] [--manifest path] [--project-root path] [--apply]
~~~

Audit is read-only. Cleanup writes a manifest; --apply creates a database
backup before applying the planned legacy-memory changes.

## WeChat

~~~bash
soloqueue wechat login [--id default] [--name WeChat] [--bind-type l1|l2] [--bind-agent id]
~~~

The command prints a QR login URL, polls the iLink service, optionally asks for
a verification code, and saves the confirmed account to settings.yaml.

## Development conventions

The server creates or uses the active work directory on startup. For isolated
development or tests, set SOLOQUEUE_WORK_DIR to a temporary absolute path.
Commands that inspect the database default to ~/.soloqueue/soloqueue.db.
