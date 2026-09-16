<p align="center">
  <img src="web/public/logo.png" alt="SoloQueue logo" width="128">
</p>

<h1 align="center">SoloQueue</h1>

<p align="center">
  A self-hosted AI workspace for ongoing conversations, team-based task handling,
  scheduled tasks, and messaging apps.
</p>

<p align="center">
  <a href="README.md">English</a> ·
  <a href="README.zh-CN.md">简体中文</a> ·
  <a href="docs/README.md">Documentation</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25.8-00ADD8?style=flat-square&logo=go" alt="Go 1.25.8">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Fize/soloQueue?style=flat-square" alt="MIT License"></a>
</p>

## What SoloQueue Does

| Feature | Description |
| --- | --- |
| Conversations | Keeps conversation history across restarts and displays responses and tool activity as they arrive |
| Project work | Uses a selected project as the working directory for files and commands, and can access web resources |
| Team collaboration | Delegates tasks to configurable teams and returns their results to the conversation |
| Model selection | Selects a configured model according to the type of request |
| Scheduled tasks | Runs one-off or recurring tasks and keeps their execution history |
| Messaging | Connects QQ Bot, WeChat iLink, and Telegram accounts |
| Browser interfaces | Provides a Web Console for operation and a read-only page for runtime status |

## Build and Run

### Requirements

- Go 1.25.8
- Node.js and `pnpm`
- Git
- An API key for at least one enabled LLM provider

### Build from Source

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

Open <http://127.0.0.1:57689>. On first start, SoloQueue creates the work
directory and initial `settings.yaml`.

### Docker

See [Docker deployment](deploy/docker/README.md) for the standalone image build
and `docker run` example.

### Initial Setup

1. Open **Settings → Models** and configure an enabled provider and model.
2. Open **Settings → Projects** and register a repository by absolute path.
3. Open **Chat**, select the project, and submit a prompt.
4. Configure Teams, Cron tasks, or channel accounts from their Web Console pages as needed.

## Commands

```bash
./soloqueue start   # Start SoloQueue
./soloqueue --help  # List commands and flags
```

## Skills

SoloQueue loads global Skills from
`${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` and compatible project Skills
from `<project>/.claude/skills/`. Global `SKILL.md` definitions reload when
Skill directories or recognized entrypoints change.

Install and update global Skills with the standalone
[ClawHub](https://github.com/openclaw/clawhub) CLI:

```bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
```

See the [Feature Guide](docs/features.md) and [Reference Manual](docs/reference.md)
for discovery rules and lifecycle commands.

## Documentation

| Topic | English | 简体中文 |
| --- | --- | --- |
| Build, setup, and troubleshooting | [Getting Started](docs/getting-started.md) | [快速入门](docs/zh/getting-started.md) |
| Runtime capabilities | [Features](docs/features.md) | [功能](docs/zh/features.md) |
| Process and subsystem boundaries | [Architecture](docs/architecture.md) | [架构与设计](docs/zh/architecture.md) |
| Configuration, CLI, storage, and backup | [Reference](docs/reference.md) | [参考手册](docs/zh/reference.md) |

## Runtime Boundaries

- Services bind to `127.0.0.1` by default.
- SoloQueue does not provide application-level HTTP authentication, TLS termination,
  or a public listener. A user-managed ingress provides those functions for remote access.
- SoloQueue does not create an execution sandbox. Host deployments run tools with the
  permissions of the SoloQueue process; a configured container or VM provides an isolation boundary.

## Development

```bash
# Backend and Status UI
go run ./cmd/soloqueue serve --port 8765 --verbose

# Web Console development server
cd web
pnpm install
pnpm dev
```

The Web Console development server proxies `/api` and `/ws` to port `8765`.

### Validation

```bash
go test ./...
cd web && pnpm test && pnpm build
cd status-ui && pnpm test && pnpm build
```

These commands validate the checked-out source tree. They do not rebuild or test
an installed desktop application.

## License

[MIT](LICENSE)
