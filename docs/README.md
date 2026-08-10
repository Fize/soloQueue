# SoloQueue documentation

SoloQueue is a local-first personal AI agent harness and multi-agent
workspace. It began as the author's Harness Engineering learning project,
was informed by OpenClaw, and is now used as a complete personal application.
The documentation therefore distinguishes three audiences:

- **Users**: install, configure, and operate the application.
- **Maintainers**: understand the current runtime and distribution boundaries.
- **Historical notes**: design records that explain why an earlier decision was
  made; they are not a description of the current product.

## Start here

| Need | Document |
| --- | --- |
| Build from source | [Installation](getting-started/installation.md) |
| Start the server and make the first request | [First run](getting-started/first-run.md) |
| Complete a first project task | [First task](getting-started/first-task.md) |
| Find a feature guide | [Guides](guides/) |
| Run the service remotely or recover data | [Operations](operations/) |
| See every supported config section | [Configuration reference](reference/configuration.md) |
| See CLI commands and flags | [CLI reference](reference/cli.md) |
| Understand the current runtime | [Architecture overview](architecture/overview.md) |

## User guides

| Topic | Document |
| --- | --- |
| Projects, sessions, and local workspaces | [Projects and sessions](guides/projects-and-sessions.md) |
| Teams and agent templates | [Teams and agents](guides/teams-and-agents.md) |
| Providers, models, and routing | [Models and routing](guides/models-and-routing.md) |
| YAML workflows and run history | [Workflows](guides/workflows.md) |
| Cron and scheduled tasks | [Scheduled tasks](guides/scheduled-tasks.md) |
| Skills, MCP, and LSP | [Skills and MCP](guides/skills-and-mcp.md) |
| Memory and usage statistics | [Memory and stats](guides/memory-and-stats.md) |
| QQ and WeChat channels | [Channels](guides/channels.md) |

## Operations and reference

- [Remote access](operations/remote-access.md)
- [Data, logs, and backup](operations/data-and-backup.md)
- [Security and permissions](operations/security-and-permissions.md)
- [Troubleshooting](operations/troubleshooting.md)
- [Configuration](reference/configuration.md)
- [CLI](reference/cli.md)
- [License](../LICENSE)

## Maintainer and architecture notes

- [Architecture overview](architecture/overview.md)
- [Task routing](routing.md)
- [Context windows](ctxwin.md)
- [Memory subsystem](memory.md)
- [Timeline and replay](timeline.md)
- [MCP details](mcp.md)
- [macOS signing](macos-signing.md) (maintainer-only; not an end-user install path)

The files under `docs/plans/`, `docs/design/`, and the repository-level
`workflow.md` are historical or implementation notes. They may contain an
older design vocabulary and should not be used as user instructions.
