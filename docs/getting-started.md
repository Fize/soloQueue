# Getting Started

English | [简体中文](zh/getting-started.md)

This guide covers prerequisites, building from source, first-run configuration, remote access setup, and common troubleshooting.

---

## Prerequisites

- **Go**: 1.25.8 or compatible 1.25 release.
- **Node.js & pnpm**: Node.js environment with `pnpm` installed.
- **Git**.
- **API Key**: At least one enabled LLM provider API key (e.g., `DEEPSEEK_API_KEY`).

---

## Installation & Build

### 1. Embedded Web Portal & Server (Default)

Build the browser portal, embed it into the Go server, and launch:

```bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue

make build
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
```

Open `http://127.0.0.1:57647` in a browser. On initial launch, SoloQueue automatically creates the work directory at `~/.soloqueue/` and populates `settings.yaml`.

> **Note**: Running `go run ./cmd/soloqueue serve` directly without building portal assets will result in a blank browser UI. Run `make build-web` first when portal UI is needed.

### 2. Desktop Development Client

Run the desktop frontend (Electron + React) alongside the Go backend:

```bash
# Terminal 1: Backend server
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2: Desktop client
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
```

The Vite dev server proxies `/api` and `/ws` requests to `http://localhost:8765`.

### 3. Build Targets

| Command | Output |
| --- | --- |
| `make build-web` | Builds portal frontend and copies into `internal/server/dist/` |
| `make build-go` | Builds Go binary (assumes portal assets exist) |
| `make build` | Builds portal assets and Go binary |
| `make build-desktop` | Builds Electron desktop renderer assets |
| `make build-all` | Builds portal, Go binary, and desktop renderer |
| `make package-desktop PLATFORM=mac` | Packages desktop application (`mac`, `win`, or `linux`) |

---

## First Run & Basic Workflow

### 1. Model Provider Setup
Open **Settings → Models** in the UI to confirm an enabled provider and model. The default configuration uses DeepSeek with key read from `DEEPSEEK_API_KEY`. Route format follows `provider:model` (e.g., `deepseek:deepseek-v4-flash-thinking`).

### 2. Registering a Project
Open **Settings → Projects**, add a repository using its absolute filesystem path, and assign a short name. The project path defines the execution scope for file and shell operations.

### 3. Creating a Session
Navigate to **Chat**, select the registered project, and submit a prompt:
```text
Inspect README.md and list the build commands. Do not modify files.
```

### 4. Tool Confirmations
When an agent tool call matches a confirmation policy (e.g., shell command execution or file writes), SoloQueue pauses execution and presents a confirmation prompt in the UI. Review the requested scope and command before approving. The `--bypass` flag globally disables confirmations and should only be used in controlled test environments.

---

## Remote Access & Security

By default, SoloQueue binds to `127.0.0.1:57647` and loopback requests bypass authentication.

### Binding Non-Loopback Interfaces

To listen on external network interfaces:

```bash
./soloqueue serve --host 0.0.0.0 --port 57647
```

### Configuring Authentication

Set HTTP Basic Auth credentials in `settings.yaml`:

```yaml
auth:
  user: soloqueue
  password: replace-with-a-long-random-password
```

Or pass via environment variables:

```bash
export SOLOQUEUE_AUTH_USER="soloqueue"
export SOLOQUEUE_AUTH_PASSWORD="replace-with-a-long-random-password"
```

Non-loopback requests require Basic Authentication when credentials are configured. If no credentials are set, non-loopback requests are rejected with `403 Forbidden`.

---

## Troubleshooting

- **Blank Web Portal**: Run `make build-web` and `make build`, then restart the server.
- **Port In Use**: Specify a different port using `./soloqueue serve --port 8765`.
- **No Model Response**: Verify provider API key, check `model_routes` mapping, and inspect server logs.
- **Remote 403 Forbidden**: Ensure `auth.user` and `auth.password` are set before accessing via non-loopback IPs.
- **Tool Blocked**: Review tool confirmation cards or inspect `settings.yaml` under `tools` section for shell/file path policy blocks.
