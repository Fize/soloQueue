# SoloQueue - AI Multi-Agent Collaboration Tool

An AI multi-agent collaboration tool with a Go backend and React frontend.

## Architecture

- **Backend**: Go (net/http + cobra CLI) — REST + WebSocket API
- **Frontend**: React 19 + TypeScript + Vite + TailwindCSS
- **Desktop**: Electron (planned)

## Getting Started

### Backend

```bash
cd backend
go run ./cmd/soloqueue serve --port 8765
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

## Project Structure

```
soloqueue/
├── backend/          # Go backend
│   ├── cmd/          # CLI entrypoint
│   └── internal/     # Core packages
├── frontend/         # React frontend
│   ├── src/          # Frontend source code
│   └── public/       # Static assets
└── todo.md           # Development plan
```

## Recommended IDE Setup

- [VS Code](https://code.visualstudio.com/) + [Go](https://marketplace.visualstudio.com/items?itemName=golang.go)
