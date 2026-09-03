# Getting Started

English | [简体中文](zh/getting-started.md)

This guide covers prerequisites, building from source, first-run configuration, local development, and common troubleshooting.

---

## Prerequisites

- **Go**: 1.25.8 or compatible 1.25 release.
- **Node.js & pnpm**: Node.js environment with `pnpm` installed.
- **Git**.
- **API Key**: At least one enabled LLM provider API key (e.g., `DEEPSEEK_API_KEY`).
- **Optional Skills CLI**: Install [ClawHub](https://github.com/openclaw/clawhub) if you want to manage skills from the command line.

---

## Installation & Build

### 1. Embedded Browser Application (Default)

Build both browser bundles, embed them into the Go server, and launch:

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue start
```

Open `http://127.0.0.1:57647` in a browser. On initial launch, SoloQueue automatically creates the work directory at `~/.soloqueue/` and populates `settings.yaml`.

> **Note**: Run `make build-assets` before a production Go build so both the Web Console and Status UI are embedded.

### 2. Browser Development

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

### 3. Build Targets

| Command | Output |
| --- | --- |
| `make build-web` | Builds the full Web Console |
| `make build-go` | Builds Go binary (assumes browser assets exist) |
| `make build` | Builds browser assets and Go binary |
| `make build-status` | Builds the read-only Status UI |
| `make build-assets` | Builds Web Console and Status UI |
| `make start` | Builds and starts backend plus both browser UIs |

---

## First Run & Basic Workflow

### Managing Skills

SoloQueue reads installed global skills from `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` and hot-reloads their `SKILL.md` definitions when skill directories or recognized entrypoints change. Agents running in a project also load compatible project skills from `<project>/.claude/skills/` when they are created. Set `SOLOQUEUE_WORK_DIR` to change the SoloQueue work directory. Skill installation and updates are intentionally external:

```bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills inspect @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills list
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills uninstall slug
```

The assistant only consults ClawHub when a Skill is actually needed. Skill search and lifecycle operations are performed directly by L1; L2/L3 only use installed Skills and report missing Skill IDs to L1. Mutating operations still require explicit intent.

### 1. Model Provider Setup
Open **Settings → Models** in the UI to confirm an enabled provider and model. The default configuration uses DeepSeek with key read from `DEEPSEEK_API_KEY`. Route format follows `provider:model` (e.g., `deepseek:deepseek-v4-flash-thinking`).

### 2. Registering a Project
Open **Settings → Projects**, add a repository using its absolute filesystem path, and assign a short name. The project path defines the execution scope for file and shell operations.

### 3. Creating a Session
Navigate to **Chat**, select the registered project, and submit a prompt:
```text
Inspect README.md and list the build commands. Do not modify files.
```

### 4. Tool Execution Safety
SoloQueue does not automatically create a sandbox. For deployment, run it inside a configured Docker container or VM and treat that environment as the isolation boundary. When run directly on the host, tools execute with the host process's permissions and without automatic isolation; use this only in a trusted development environment. Deterministic safety checks remain active: shell blocklists reject configured commands, WebFetch blocks private addresses, and file/path/size and timeout limits are enforced.

---

## Service Boundary

SoloQueue binds only to `127.0.0.1`. It does not provide HTTP authentication,
TLS, or a public listener. The local Web Console and Status UI use the
embedded same-origin routes, while Vite and the standalone Web Console use
loopback CORS to reach the backend during development.

For remote access, configure nginx or another deployment ingress to proxy the
Web Console, REST API, WebSocket, and Status UI. The ingress owns external
authentication, TLS, CORS, rate limiting, and access logging. The Docker demo
in `deploy/docker-demo/` shows the local nginx topology and is not a production
deployment.

---

## Troubleshooting

- **Blank Web UI**: Run `make build-assets` and `make build`, then restart the server.
- **Port In Use**: Specify a different port using `./soloqueue serve --port 8765`.
- **No Model Response**: Verify provider API key, check `model_routes` mapping, and inspect server logs.
- **Remote access**: Configure the external reverse proxy and keep SoloQueue bound to its loopback address.
- **Tool Blocked**: Inspect `settings.yaml` under the `tools` section for shell blocklist or file/path policy restrictions.
