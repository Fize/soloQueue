# SoloQueue

**A local-first personal AI agent harness and multi-agent workspace.**

中文入口：[中文 README](README.zh-CN.md) · [中文文档](docs/zh/README.md)

I built SoloQueue as a complete application while learning and practicing
Harness Engineering. Inspired by [OpenClaw](https://github.com/openclaw/openclaw),
I use it to explore how routing, delegation, tools, skills, memory, workflows,
scheduled tasks, messaging channels, and observability can work together in
one long-running personal agent system.

I use SoloQueue every day. I share it for developers who want to study or
operate a self-hosted agent harness, while I still treat it as an evolving
personal project rather than a production-ready enterprise platform.

## What I built

- I run a local-first runtime for persistent agent sessions.
- I use a multi-agent workspace with teams, agent templates, delegation, and
  tool confirmations.
- I experiment with task routing, workflows, memory, skills, MCP/LSP tools,
  scheduled tasks, and channel delivery.
- I provide a desktop console plus an embedded browser portal for local or
  controlled remote use.

## What I do not position it as

I do not intend SoloQueue to replace mature terminal coding tools, and I do
not position it as a multi-tenant SaaS, enterprise security product, or
compatibility clone of OpenClaw. I value it for the integrated harness and the
experiments it makes observable.

## Quick start from source

### Prerequisites

- Go 1.25.8 or newer in the 1.25 series.
- Node.js with `pnpm` available on `PATH`.
- An API key for the provider configured in `settings.yaml` (the defaults use
  `DEEPSEEK_API_KEY`).

### Build and run the embedded portal

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
```

I open <http://127.0.0.1:57647>. On the first start, I let SoloQueue create the local work
directory and settings file under `~/.soloqueue/`.

I use `make build` to build the lightweight portal first and embed it into the
Go binary. I can run `go run ./cmd/soloqueue serve` without a built portal for
backend development, but I expect the browser UI to remain empty until I run
`make build-web`.

### Develop with the desktop console

I use two terminals:

```bash
# Terminal 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
```

I use the desktop development server to proxy API and WebSocket traffic to
port `8765`. For a packaged desktop build, I run `make build-all` or choose
one platform, for example `make package-desktop PLATFORM=mac`.

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

I start with the [English documentation hub](docs/README.md), or read the
[中文文档中心](docs/zh/README.md):

- [Installation / 安装](docs/getting-started/installation.md) ·
  [中文](docs/zh/getting-started/installation.md)
- [First useful task / 第一个任务](docs/getting-started/first-task.md) ·
  [中文](docs/zh/getting-started/first-task.md)
- [Feature guides / 功能指南](docs/guides/) ·
  [中文](docs/zh/guides/)
- [Operations and security / 运维与安全](docs/operations/) ·
  [中文](docs/zh/operations/)
- [Configuration and CLI / 配置与 CLI](docs/reference/) ·
  [中文](docs/zh/reference/)
- [Architecture / 架构](docs/architecture/overview.md) ·
  [中文](docs/zh/architecture/overview.md)

## Testing

```bash
go test ./...
cd desktop && pnpm test && pnpm build
```

I distribute the repository primarily as source. I check the current build and
test status before treating a revision as a release artifact.

## License

I release SoloQueue under the [MIT License](LICENSE).
