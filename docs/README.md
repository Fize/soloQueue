# SoloQueue documentation

中文入口：[中文文档中心](zh/README.md)

I use SoloQueue as a local-first personal AI agent harness and multi-agent
workspace. I began it as a Harness Engineering learning project, learned from
OpenClaw, and turned it into a complete application that I use personally.
I maintain three documentation audiences:

- **Users**: install, configure, and operate the application.
- **Maintainers**: understand the current runtime and distribution boundaries.
- **Historical notes**: design records that explain why an earlier decision was
  made; they are not a description of the current product.

## Start here

| Need | English | 中文 |
| --- | --- | --- |
| Build from source | [Installation](getting-started/installation.md) | [安装](zh/getting-started/installation.md) |
| Start the server and make the first request | [First run](getting-started/first-run.md) | [第一次运行](zh/getting-started/first-run.md) |
| Complete a first project task | [First task](getting-started/first-task.md) | [第一个任务](zh/getting-started/first-task.md) |
| Find a feature guide | [Guides](guides/) | [功能指南](zh/guides/) |
| Run the service remotely or recover data | [Operations](operations/) | [运维](zh/operations/) |
| See every supported config section | [Configuration reference](reference/configuration.md) | [配置参考](zh/reference/configuration.md) |
| See CLI commands and flags | [CLI reference](reference/cli.md) | [CLI 参考](zh/reference/cli.md) |
| Understand the current runtime | [Architecture overview](architecture/overview.md) | [架构概览](zh/architecture/overview.md) |

## User guides

| Topic | English | 中文 |
| --- | --- | --- |
| Projects, sessions, and local workspaces | [Projects and sessions](guides/projects-and-sessions.md) | [项目与会话](zh/guides/projects-and-sessions.md) |
| Teams and agent templates | [Teams and agents](guides/teams-and-agents.md) | [团队与 Agent](zh/guides/teams-and-agents.md) |
| Providers, models, and routing | [Models and routing](guides/models-and-routing.md) | [模型与路由](zh/guides/models-and-routing.md) |
| YAML workflows and run history | [Workflows](guides/workflows.md) | [工作流](zh/guides/workflows.md) |
| Cron and scheduled tasks | [Scheduled tasks](guides/scheduled-tasks.md) | [定时任务](zh/guides/scheduled-tasks.md) |
| Skills, MCP, and LSP | [Skills and MCP](guides/skills-and-mcp.md) | [Skills、MCP 与 LSP](zh/guides/skills-and-mcp.md) |
| Memory and usage statistics | [Memory and stats](guides/memory-and-stats.md) | [记忆与统计](zh/guides/memory-and-stats.md) |
| QQ and WeChat channels | [Channels](guides/channels.md) | [渠道](zh/guides/channels.md) |

## Operations and reference

- [Remote access](operations/remote-access.md) · [远程访问](zh/operations/remote-access.md)
- [Data, logs, and backup](operations/data-and-backup.md) · [数据、日志与备份](zh/operations/data-and-backup.md)
- [Security and permissions](operations/security-and-permissions.md) · [安全与权限](zh/operations/security-and-permissions.md)
- [Troubleshooting](operations/troubleshooting.md) · [故障排查](zh/operations/troubleshooting.md)
- [Configuration](reference/configuration.md) · [配置](zh/reference/configuration.md)
- [CLI](reference/cli.md) · [CLI](zh/reference/cli.md)
- [License](../LICENSE)

## Maintainer and architecture notes

- [Architecture overview](architecture/overview.md) · [架构概览](zh/architecture/overview.md)
- [Task routing](routing.md) · [任务路由](zh/routing.md)
- [Context windows](ctxwin.md) · [上下文窗口](zh/ctxwin.md)
- [Memory subsystem](memory.md) · [记忆子系统](zh/memory.md)
- [Timeline and replay](timeline.md) · [时间线与回放](zh/timeline.md)
- [MCP details](mcp.md) · [MCP 详情](zh/mcp.md)
- [macOS signing](macos-signing.md) · [macOS 签名](zh/macos-signing.md) (maintainer-only; not an end-user install path)

I keep the files under `docs/plans/`, `docs/design/`, and the repository-level
`workflow.md` as historical or implementation notes. They may contain an older
design vocabulary, so I do not use them as current user instructions.
