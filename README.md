# SoloQueue

**A local-first personal AI agent harness and multi-agent workspace.**

SoloQueue is a complete application that grew out of the author's Harness
Engineering learning and daily practice. Inspired by [OpenClaw](https://github.com/openclaw/openclaw),
it explores how routing, delegation, tools, skills, memory, workflows,
scheduled tasks, messaging channels, and observability can work together in
one long-running personal agent system.

SoloQueue is actively used by its author. It is useful for developers who want
to study or operate a self-hosted agent harness, but it should currently be
treated as an evolving personal project rather than a production-ready
enterprise platform.

## What SoloQueue is

- A local-first runtime for persistent agent sessions.
- A multi-agent workspace with teams, agent templates, delegation, and tool
  confirmations.
- A harness for experimenting with task routing, workflows, memory, skills,
  MCP/LSP tools, scheduled tasks, and channel delivery.
- A desktop console plus an embedded browser portal for local or controlled
  remote use.

## What it is not

SoloQueue is not intended to replace mature terminal coding tools, and it is
not currently positioned as a multi-tenant SaaS, enterprise security product,
or compatibility clone of OpenClaw. Its value is the integrated harness and
the experiments it makes observable.

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

Open <http://127.0.0.1:57647>. The first start creates the local work
directory and settings file under `~/.soloqueue/`.

`make build` builds the lightweight portal first and embeds it into the Go
binary. Running `go run ./cmd/soloqueue serve` without a built portal is useful
for backend development, but the browser UI will be empty until `make
build-web` has been run.

### Develop with the desktop console

Use two terminals:

```bash
# Terminal 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
```

The desktop development server proxies API and WebSocket traffic to port
`8765`. For a packaged desktop build, use `make build-all` or choose one
platform, for example `make package-desktop PLATFORM=mac`.

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

Start with the [user documentation hub](docs/README.md):

- [Install and first run](docs/getting-started/installation.md)
- [First useful task](docs/getting-started/first-task.md)
- [Core feature guides](docs/guides/)
- [Operations and security](docs/operations/)
- [Configuration and CLI reference](docs/reference/)
- [Architecture notes](docs/architecture/overview.md)

## Testing

```bash
go test ./...
cd desktop && pnpm test && pnpm build
```

The repository is primarily source-distributed. Check the current build and
test status before treating a revision as a release artifact.

## License

SoloQueue is released under the [MIT License](LICENSE).
