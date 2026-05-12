# SoloQueue - AI Multi-Agent Collaboration Platform

<div align="center">

**A Go and React based AI multi-agent collaboration tool supporting intelligent task routing, hierarchical agent architecture, and extensible tool/skill systems.**

[Quick Start](#-quick-start) • [Architecture](#-architecture) • [Documentation](#-documentation) • [Contributing](#-development)

</div>

---

# ✨ Key Features

**🤖 Hierarchical Agent Architecture (L0-L3)** - Intelligently routes tasks to models of different capabilities based on complexity
**🔧 Extensible Tool System** - Built-in tools for file operations, shell execution, web search, etc.
**📦 Skill System** - Markdown-defined reusable task recipes (skills)
**💬 Event-Driven Architecture** - Typed event streams connecting Agent, TUI, and Server layers
**🔄 Hot-Reload Config** - Dynamic config updates via fsnotify
**🖥️ TUI + Web UI** - Supports both terminal and React web interfaces
**🚀 Async Delegation** - L1 agents can asynchronously delegate tasks to L2/L3 without blocking user interaction

# 🏗️ Architecture

SoloQueue uses a hierarchical architecture design. The core systems include:

```
┌─────────────────────────────────────────────────────────────┐
│                    TUI / Web UI (React)                    │
├─────────────────────────────────────────────────────────────┤
│                  Server (REST + WebSocket)                  │
├─────────────────────────────────────────────────────────────┤
│                  Session (Session Management)                │
├──────────────────────────┬──────────────────────────────────┤
│     Agent System          │       Tool / Skill System        │
│  (L0/L1/L2/L3)        │    (Extensible Capability Layer) │
├──────────────────────────┴──────────────────────────────────┤
│                   LLM (DeepSeek Provider)                   │
├─────────────────────────────────────────────────────────────┤
│              Config (Hot-reload Config System)              │
└─────────────────────────────────────────────────────────────┘
```

### Core Subystems

| System              | Path                | Description                                                        |
| ------------------ | ------------------- | ------------------------------------------------------------------- |
| **Agent System**    | `internal/agent/`   | Actor-model agent runtime, manages LLM tool loops, async delegation, lifecycle |
| **LLM System**      | `internal/llm/`     | Provider-agnostic protocol layer + DeepSeek HTTP/SSE transport      |
| **Tool System**     | `internal/tools/`   | Executable primitive layer (file, shell, HTTP, search)              |
| **Skill System**    | `internal/skill/`   | Markdown-defined reusable task recipes, fork execution mode           |
| **Config System**   | `internal/config/`  | Layered TOML config, hot-reload, type-safe access                   |
| **Session**         | `internal/session/` | Session management, context window, conversation ordering            |
| **Router**          | `internal/router/`  | Intelligent task classification and model routing (L0-L3)           |

## 🚀 Quick Start

### Prerequisites

- Go 1.25.8+
- Node.js 18+ and pnpm

### Go Application (CLI + Server)

```bash
# Clone the repository
git clone https://github.com/xiaobaitu/soloqueue.git
cd soloqueue

# Run (TUI mode)
go run ./cmd/soloqueue

# Run (Server mode)
go run ./cmd/soloqueue serve --port 8765
```

### Web UI

```bash
cd web
pnpm install
pnpm dev
```

The Web UI will start at `http://localhost:5173`.

### Configuration

Config file is located at `~/.soloqueue/settings.toml`. The app supports zero-config startup (uses built-in defaults).

## 📁 Project Structure

```
soloqueue/
├── cmd/soloqueue/          # CLI entrypoint (cobra)
│   ├── main.go             # Application entrypoint
│   └── cli/               # CLI subcommands
├── internal/               # Core Go packages
│   ├── agent/              # Agent runtime (LLM + tool loop)
│   ├── config/             # Config system (hot-reload)
│   ├── ctxwin/             # Context window (tiktoken, compaction)
│   ├── llm/               # LLM client (DeepSeek)
│   ├── router/             # Task routing and classification
│   ├── server/             # HTTP/WebSocket router (chi)
│   ├── session/            # Session management
│   ├── skill/              # Skill system
│   ├── tools/              # Tool implementations
│   └── tui/               # Terminal UI
├── web/                    # React web UI
│   ├── src/
│   └── public/
├── docs/                   # Detailed architecture docs
├── go.mod
└── README.md
```

## 📖 Documentation

Detailed documentation is available in the `docs/` directory:

### Architecture Docs

- [**Architecture**](docs/architecture.md) - Agent, LLM, Tool, Skill system architecture overview
- [**Configuration**](docs/config.md) - Config system, layered loading, hot-reload
- [**Routing**](docs/routing.md) - L0-L3 task classification, fast track, hybrid sticky logic

### Additional Docs

- [**Soul Profiles**](docs/roles/README.md) - Sample agent persona definitions (soul.md)

## 🧠 Task Routing (L0-L3)

SoloQueue uses an intelligent task classification system to route user input to the appropriate processing level:

| Level   | Name                | Typical Scope                     | Model                       |
| ------- | ------------------- | --------------------------------- | --------------------------- |
| **L0** | Conversation        | Q&A, explanation, discussion      | deepseek-v4-flash          |
| **L1** | Simple Single File  | Fix bugs, add fields              | deepseek-v4-flash-thinking |
| **L2** | Medium Multi-File   | Refactoring, implementing features (2-5 files) | deepseek-v4-pro |
| **L3** | Complex Refactoring | Architecture, rewrites (5+ files) | deepseek-v4-pro-max |

Classification uses a **dual-channel strategy**:

1. **Fast Track** - Pattern-based rule matching (zero latency)
2. **LLM Fallback** - Semantic understanding (4s timeout)

See [docs/routing.md](docs/routing.md) for details.

## 🔧 Development Guide

### Go Commands

```bash
# Build binary
go build ./cmd/soloqueue

# Run all tests
go test ./...

# Run specific package tests
go test ./internal/agent/...

# Run a single test
go test -run TestReplayInto ./internal/timeline/...
```

### Web UI Commands

```bash
cd web

# Install dependencies
pnpm install

# Development server
pnpm dev

# Production build
pnpm build

# Full CI check (tsc + eslint + prettier)
pnpm check

# Lint check
pnpm lint
```

### Important Notes

- The web project uses `pnpm` (lockfile is `pnpm-lock.yaml`), **not** npm
- Go module path: `github.com/xiaobaitu/soloqueue`
- Config directory: `~/.soloqueue/`

## 📝 Configuration System

The configuration system supports layered loading and merging:

```toml
# ~/.soloqueue/settings.toml
[session]
max_history = 50

[tools]
max_file_size = 10485760

[providers]
  [providers.deepseek]
  base_url = "https://api.deepseek.com"
  api_key = "your-api-key"

[models]
  [models."deepseek-v4-flash"]
  context_window = 65536
```

**Configuration Hierarchy** (lowest to highest priority):

1. Compiled defaults (`internal/config/defaults.go`)
2. `settings.toml` (shared config)
3. `settings.local.toml` (local override)

See [docs/config.md](docs/config.md) for details.

## 🤝 Contributing

Contributions are welcome! Please follow these guidelines:

1. **Code Style**
   - Go: Use `go fmt` and `golangci-lint`
   - TypeScript/React: Use the project-configured ESLint and Prettier

2. **Testing**
   - All new features must include tests
   - Tests should be self-contained and not rely on shared fixtures

3. **Documentation**
   - Architecture changes must update relevant documentation
   - Documentation must be written in English

4. **Commits**
   - Use conventional commit format
   - Keep commits atomic

## 📄 License

[Add license information]

---

## 🔗 Quick Links

- **Project Homepage**: [https://github.com/xiaobaitu/soloqueue](https://github.com/xiaobaitu/soloqueue)
- **Issue Tracker**: [https://github.com/xiaobaitu/soloqueue/issues](https://github.com/xiaobaitu/soloqueue/issues)
- **Discussions**: [https://github.com/xiaobaitu/soloqueue/discussions](https://github.com/xiaobaitu/soloqueue/discussions)

---

<div align="center">

**Built with ❤️ using Go, React, and AI**

</div>
