# CLI reference

> 中文：[CLI 参考](../zh/reference/cli.md)

I use the soloqueue executable as the primary command. I run soloqueue --help
to see the command tree for the checked-out revision.

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

I use it to print the application version.

## Skills

~~~bash
soloqueue skills report [--work-dir path] [--db path] [--days 30]
~~~

I use it to print JSON governance data for installed skills, including
invocation lookback and description quality.

## Memory

~~~bash
soloqueue memory audit [--db path]
soloqueue memory cleanup [--db path] [--manifest path] [--project-root path] [--apply]
~~~

I use Audit as a read-only operation. I use Cleanup to write a manifest; with
--apply, I create a database backup before applying my planned legacy-memory
changes.

## WeChat

~~~bash
soloqueue wechat login [--id default] [--name WeChat] [--bind-type l1|l2] [--bind-agent id]
~~~

I use the command to print a QR login URL, poll the iLink service, optionally
enter a verification code, and save the confirmed account to settings.yaml.

## Development conventions

I let the server create or use the active work directory on startup. For
isolated development or tests, I set SOLOQUEUE_WORK_DIR to a temporary
absolute path. My database-inspection commands default to
~/.soloqueue/soloqueue.db.
