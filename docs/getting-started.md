# Getting Started

English | [简体中文](zh/getting-started.md)

This guide covers building from source, first-run setup, local development, and connecting a local Web Console to a remote Core.

---

## Prerequisites

Building from source requires Go 1.25.8, Node.js, `pnpm`, and Make. You can configure a provider API key in the Web Console after the first launch.

---

## Installation & Build

### 1. Embedded Browser Application

Build both browser bundles, embed them into the Go server, and launch:

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

Open `http://127.0.0.1:57689` in a browser. On initial launch, SoloQueue automatically creates the work directory at `~/.soloqueue/` and populates `settings.yaml`.

> **Note**: Run `make build-assets` before building a distributable Go binary so both browser bundles are embedded.

### 2. Docker

See [Docker deployment](../deploy/docker/README.md) for the standalone image
build and `docker run` example.

### 3. Browser Development

Run the Web Console alongside the Go backend:

```bash
# Terminal 1: Backend server
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2: Web Console
cd web
pnpm install
pnpm dev
```

The Vite dev server proxies `/api` and `/ws` requests to `http://localhost:8765`.

### Connecting a Local Web Console to a Remote Core

You can keep the Web Console on your computer and connect it to a Core running elsewhere. Start the standalone local Web Console (`soloqueue web`; default address: `http://127.0.0.1:57648`), then go to **Settings → Connection**, select **Remote**, enter a browser-reachable Core URL (for example, `https://core.example.com`), and save. The connection choice is stored in this browser. The Core must already be running and reachable from the browser.

For a Core reachable only on the remote machine's loopback interface, use an SSH tunnel, then enter `http://127.0.0.1:57689` as the Remote URL. For a Core behind a reverse proxy, use its HTTPS URL and configure the proxy to forward `/api` and WebSocket traffic at `/ws`.

The Core has no built-in authentication. Do not expose it directly to the public internet; use an SSH tunnel or a secured reverse proxy with TLS and authentication. If you need to create the SSH tunnel manually, forward local port `57689` to `127.0.0.1:57689` on the Core host.

### 4. Build Targets

| Command | Output |
| --- | --- |
| `make build-web` | Builds the Web Console |
| `make build-go` | Builds Go binary (assumes browser assets exist) |
| `make build` | Builds browser assets and Go binary |
| `make build-status` | Builds the read-only Status UI |
| `make build-assets` | Builds Web Console and Status UI |
| `make start` | Builds and starts backend plus both browser UIs |

---

## First Run & Workflow

### Managing Skills

Open **Skills** in the Web Console to inspect installed skills. SoloQueue loads global skills from `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` and compatible project skills from `<project>/.claude/skills/`. Skill installation and updates are not available in the Web Console; see the [Reference Manual](reference.md) for the ClawHub workflow.

### 1. Model Provider Setup
Use **Settings → Models** in the Web Console to configure a provider, API key, model, and task routes. The default configuration uses DeepSeek and reads its key from `DEEPSEEK_API_KEY`. Route values use `provider:model` format.

### 2. Registering a Project
Open **Settings → Projects**, add a repository using its absolute filesystem path, and assign a short name. The selected path becomes the project's default working directory; it is not a security sandbox.

### 3. Creating a Session
Navigate to **Chat**, select the registered project, and submit a prompt:
```text
Inspect README.md and list the build commands. Do not modify files.
```

### 4. Tool Execution Safety
SoloQueue does not create a sandbox. In a configured Docker container or VM, that environment provides the isolation boundary. When run directly on the host, tools execute with the SoloQueue process's permissions. Shell blocklists reject configured commands, WebFetch blocks private addresses, and file, path, size, and timeout limits are applied by the tool layer.

### Advanced: Configure `settings.yaml`

The active configuration file is `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/settings.yaml`; SoloQueue creates it on first start. You can edit it directly, and valid file changes are detected automatically. Omitted fields use built-in defaults, while configured lists such as `providers` and `models` replace the corresponding default lists.

For example, add an OpenAI-compatible provider and model, then route tasks to that model:

```yaml
providers:
  - id: my-provider
    name: My Provider
    base_url: https://api.example.com/v1
    api_key_env: MY_PROVIDER_API_KEY
    enabled: true
    is_default: true

models:
  - id: my-model
    provider_id: my-provider
    name: My Model
    context_window: 32768
    enabled: true

model_routes:
  general: my-provider:my-model
  engineering: my-provider:my-model
  research: my-provider:my-model
```

Set `MY_PROVIDER_API_KEY` in the environment before starting SoloQueue. Prefer environment variables to storing API keys directly in YAML. See the [Configuration Reference](reference.md) for all supported fields.

---

## Service Boundary

Native `serve` and `start` commands bind to `127.0.0.1` by default. Use
`--host` to select another listening address. The Docker image passes
`--host 0.0.0.0` so Docker can publish its default port `57689`.
