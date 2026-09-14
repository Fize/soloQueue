# SoloQueue

SoloQueue is a local-first, single-user AI agent harness with persistent sessions,
Team delegation, scheduled tasks, messaging channels, and browser interfaces.

English | [简体中文](README.zh-CN.md)

The runtime combines task routing, delegation, tools, Skills, memory, Cron,
channel adapters, and runtime inspection in one self-hosted process.

## Branch boundary

`main` excludes simulation. `experimental/simulation` preserves the repository state at
`6efd796`, before simulation was extracted.

Existing simulation database files (`simulation.db` and any `-wal` / `-shm` sidecars) are retained, but `main` does not open or initialize them. Use a separate work directory when experimenting on `experimental/simulation` to prevent settings rewrites from affecting the directory used by `main`. After building that branch, for example:

```bash
SOLOQUEUE_WORK_DIR="$HOME/.soloqueue-simulation" ./soloqueue start
```

## Features

- SoloQueue runs a local-first runtime for persistent agent sessions.
- SoloQueue uses a multi-agent workspace with teams, agent templates, and delegation.
- SoloQueue supports task routing, memory, skills, MCP/LSP tools,
  scheduled tasks, and channel delivery.
- SoloQueue provides a browser Web Console plus an embedded read-only Status UI for
  local use. Remote access is provided through a user-managed reverse proxy.

Skills are installed and updated independently with [ClawHub](https://github.com/openclaw/clawhub). SoloQueue loads packages already present under `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` and hot-reloads their `SKILL.md` definitions when skill directories or recognized entrypoints change; when a project Agent is created, it also loads compatible project Skills from `<project>/.claude/skills/`. The Web Console provides read-only inspection. Set `SOLOQUEUE_WORK_DIR` to use a different SoloQueue work directory. Use `@owner/slug` for owner-qualified ClawHub resources, and the installed skill slug for uninstall.

```bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills inspect @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills uninstall slug
```

## Scope boundaries

SoloQueue does not implement multi-tenant accounts, application-level HTTP
authentication, TLS termination, a public listener, or OpenClaw compatibility.
It does not create an execution sandbox; host deployments run tools with the
permissions of the SoloQueue process.

## Build from source

### Prerequisites

- Go 1.25.8 or newer in the 1.25 series.
- Node.js with `pnpm` available on `PATH`.
- An API key for the provider configured in `settings.yaml` (the defaults use
  `DEEPSEEK_API_KEY`).

### Build and run the embedded browser application

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

Open <http://127.0.0.1:57647>. On the first start, SoloQueue creates the local work
directory and settings file under `~/.soloqueue/`.

Use `make build` to build the Web Console and Status UI and embed them into the
Go binary. `soloqueue serve` starts the backend with the Status UI at `/status/`;
`soloqueue web` starts only the standalone Web Console.

### Develop the browser frontends

Use two terminals:

```bash
# Terminal 1: backend
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2: Web Console
cd web && pnpm install && pnpm dev
```

The Web Console development server proxies API and WebSocket traffic to port
`8765`. The Status UI can be developed independently with `cd status-ui && pnpm dev`.

SoloQueue services bind to `127.0.0.1` and do not provide application HTTP
authentication. If remote access is needed, place nginx or another ingress in
front of the service and configure authentication, TLS, CORS, and WebSocket
proxying there. The Docker setup under `deploy/docker-demo/` is a local demo
with nginx and SoloQueue sharing one network namespace.

## Commands

```bash
./soloqueue version
./soloqueue --help
./soloqueue skills report
./soloqueue memory audit
./soloqueue memory cleanup              # plan only
./soloqueue memory cleanup --apply      # backup, then apply the plan
./soloqueue wechat login --id personal
```

## Documentation

Start with the [English documentation hub](docs/README.md), or read the
[中文文档中心](docs/zh/README.md):

- [Getting Started / 快速入门](docs/getting-started.md) · [中文](docs/zh/getting-started.md)
- [Features / 功能](docs/features.md) · [中文](docs/zh/features.md)
- [Architecture / 架构与设计](docs/architecture.md) · [中文](docs/zh/architecture.md)
- [Reference / 参考手册](docs/reference.md) · [中文](docs/zh/reference.md)


## Testing

```bash
go test ./...
cd web && pnpm test && pnpm build
cd status-ui && pnpm test && pnpm build
```

These commands validate the checked-out source tree. They do not rebuild or test
an already installed desktop application.

## License

SoloQueue is released under the [MIT License](LICENSE).
