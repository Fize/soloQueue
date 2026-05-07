# SoloQueue - AI Multi-Agent Collaboration Tool

An AI multi-agent collaboration tool with a Go CLI/server and React web UI.

## Architecture

- **Go App**: Go (net/http + cobra CLI) — CLI-first, embeds REST + WebSocket server
- **Web UI**: React 19 + TypeScript + Vite + TailwindCSS
- **Desktop**: Electron (planned)

## Getting Started

### Go App (CLI + Server)

```bash
go run ./cmd/soloqueue serve --port 8765
```

### Web UI

```bash
cd web
npm install
npm run dev
```

## Project Structure

```
soloqueue/
├── cmd/              # CLI entrypoint (cobra)
│   └── soloqueue/
├── internal/         # Core Go packages
│   ├── agent/        # Agent core (LLM + Tool Loop)
│   ├── config/       # Config system (hot-reload)
│   ├── llm/          # LLM client (DeepSeek)
│   ├── logger/       # Logger (JSONL + rotation)
│   ├── server/       # HTTP/WS router
│   ├── session/      # Session management
│   ├── tools/        # Tools (file, shell, HTTP...)
│   └── tui/          # Terminal UI
├── web/              # React web UI
│   ├── src/
│   └── public/
├── docs/             # Documentation & demos
├── go.mod
└── go.sum
```
