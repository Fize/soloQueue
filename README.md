# SoloQueue

**A local-first personal AI agent harness and multi-agent workspace.**

English | [简体中文](README.zh-CN.md)

I built SoloQueue as a complete application while learning and practicing
Harness Engineering. Inspired by [OpenClaw](https://github.com/openclaw/openclaw),
I use it to explore how routing, delegation, tools, skills, memory,
scheduled tasks, messaging channels, and observability can work together in
one long-running personal agent system.

I use SoloQueue every day. I share it for developers who want to study or
operate a self-hosted agent harness, while I still treat it as an evolving
personal project rather than a production-ready enterprise platform.

## Features

- SoloQueue runs a local-first runtime for persistent agent sessions.
- SoloQueue uses a multi-agent workspace with teams, agent templates, delegation, and
  tool confirmations.
- SoloQueue supports task routing, memory, skills, MCP/LSP tools,
  scheduled tasks, and channel delivery.
- SoloQueue provides a full browser Web Console plus an embedded read-only Status UI for
  local use. Remote access is provided through a user-managed reverse proxy.

## Non-goals

SoloQueue is not intended to replace mature terminal coding tools, and it is
not positioned as a multi-tenant SaaS, enterprise security product, or
compatibility clone of OpenClaw. It is valued for the integrated harness and the
experiments it makes observable.

## Quick start from source

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

Open <http://127.0.0.1:57647>. On the first start, let SoloQueue create the local work
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

## Useful commands

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
- [Features / 核心功能](docs/features.md) · [中文](docs/zh/features.md)
- [Architecture / 架构与设计](docs/architecture.md) · [中文](docs/zh/architecture.md)
- [Reference / 参考手册](docs/reference.md) · [中文](docs/zh/reference.md)


## Testing

```bash
go test ./...
cd web && pnpm test && pnpm build
cd status-ui && pnpm test && pnpm build
```

The repository is distributed primarily as source. Check the current build and
test status before treating a revision as a release artifact.

## License

SoloQueue is released under the [MIT License](LICENSE).
