# SoloQueue Project - Comprehensive Overview

**Date**: April 21, 2026  
**Project Root**: `/Users/xiaobaitu/github.com/soloQueue`  
**Status**: Active Development (Phase 7 - Actor System Core)

---

## 1. PROJECT STRUCTURE

### 📁 Top-Level Directory Layout

```
soloqueue/
├── src/                           # React Frontend + Python Modules
│   ├── App.tsx                    # Main React App component
│   ├── main.tsx                   # React entry point
│   ├── App.css                    # Main app styles
│   ├── index.css                  # Global styles
│   ├── vite-env.d.ts             # Vite type definitions
│   ├── assets/                    # Static assets
│   └── soloqueue/                 # Python orchestration modules
│       ├── channels/              # Communication channels
│       ├── core/                  # Core functionality (adapters, loaders, memory, security, skills, state, tools)
│       ├── orchestration/         # Orchestration logic
│       ├── scheduler/             # Task scheduling
│       └── web/                   # Web interface (api, utils, websocket)
│
├── server/                        # TypeScript Backend - Actor System (13,954 LOC)
│   ├── actors/                    # Actor System Implementation
│   │   ├── actor-system.ts        # Core ActorSystem class (696 lines)
│   │   ├── supervisor.ts          # Supervision & Recovery
│   │   ├── router.ts              # Message routing strategies
│   │   ├── extensions.ts          # Extensibility points
│   │   ├── types.ts               # Type definitions
│   │   ├── factories/             # Actor factory patterns
│   │   ├── *.test.ts             # Comprehensive unit tests
│   │
│   ├── machines/                  # XState Finite State Machines
│   │   ├── agent-machine.ts       # Agent lifecycle states
│   │   ├── llm-machine.ts         # LLM invocation states
│   │   ├── task-machine.ts        # Task processing states
│   │   └── *.test.ts             # Test coverage
│   │
│   ├── llm/                       # LLM Integration Layer
│   │   ├── llm-config.service.ts # LLM configuration management
│   │   ├── types.ts               # LLM type definitions
│   │   ├── defaults.ts            # Default configurations
│   │   └── index.ts               # Service exports
│   │
│   ├── logger/                    # Advanced Logging System
│   │   ├── core.ts                # Logger core
│   │   ├── config.ts              # Logger configuration
│   │   ├── layers.ts              # Logging layers
│   │   ├── transports/            # Console & File transports
│   │   ├── formatters/            # JSONL formatters
│   │   ├── cleaners/              # Log cleaning tasks
│   │   └── utils/                 # Path & metadata utilities
│   │
│   ├── storage/                   # Data Persistence Layer
│   │   ├── db.ts                  # SQLite connection (sql.js)
│   │   ├── schema.ts              # Drizzle ORM schema definition
│   │   ├── config.service.ts      # Configuration service
│   │   ├── agent.service.ts       # Agent service
│   │   ├── repositories/          # Data access objects (DAOs)
│   │   ├── migrations.ts          # Database migrations
│   │   ├── seeds.ts               # Seed data
│   │   └── types.ts               # Storage types
│
├── src-tauri/                     # Tauri Desktop Framework (Rust)
│   ├── src/
│   │   ├── main.rs                # Tauri main entry
│   │   └── lib.rs                 # Rust library
│   ├── Cargo.toml                 # Rust dependencies
│   ├── tauri.conf.json            # Tauri configuration
│   └── icons/                     # Desktop app icons (multi-resolution)
│
├── tests/                         # Test Suites
│   ├── core/                      # Core module tests
│   ├── contract/                  # Contract/API tests
│   ├── e2e/                       # End-to-end tests
│   ├── integration/               # Integration tests
│   ├── orchestration/             # Orchestration tests
│   └── web/                       # Web component tests
│
├── public/                        # Static web assets
│   ├── tauri.svg
│   └── vite.svg
│
├── .venv/                         # Python virtual environment
├── .claude/                       # Claude Code configuration
├── .soloqueue/                    # Runtime data directory
├── .specify/                      # Specification files
├── .codebuddy/                    # CodeBuddy configuration
├── .vscode/                       # VS Code settings
│
├── .env                           # Environment configuration
├── .env.example                   # Example env template
├── package.json                   # Node.js dependencies
├── package-lock.json              # Dependency lock file
├── tsconfig.json                  # TypeScript configuration
├── tsconfig.node.json             # Node.js TypeScript config
├── vite.config.ts                 # Vite build configuration
├── vitest.config.ts               # Vitest test configuration
├── eslint.config.ts               # ESLint configuration
├── index.html                     # Vite entry HTML
├── README.md                      # Project README
├── todo.md                        # Development roadmap
└── .git/                          # Git repository
```

### 📊 Codebase Statistics
- **Server TypeScript**: 79 files, 13,954 total LOC
- **Test Files**: Multiple test suites with >72% coverage
- **Configuration**: Multiple layers (app, LLM, database, logging)

---

## 2. PROJECT OVERVIEW

### 🎯 Purpose & Vision

**SoloQueue** is an **AI Multi-Agent Collaboration Desktop Application** built on the **Actor Model** architecture. It enables:

1. **Independent Agents as Actors**: Each AI agent runs as an autonomous actor with no shared state
2. **Dynamic Actor Pools**: Runtime creation/destruction of agent instances with supervision
3. **Asynchronous Messaging**: All inter-agent communication via message passing
4. **State Machine Lifecycle**: XState v5 manages agent state and transitions
5. **Desktop Integration**: Tauri-based cross-platform desktop app
6. **Persistent State**: SQLite backend with streaming LLM support

### 💡 Core Features

- ✅ **Actor-based Architecture**: Each Agent = independent Actor with message-driven communication
- ✅ **State Machine Driven**: XState v5 for deterministic lifecycle management
- ✅ **Message Passing**: Request-response, pub-sub, broadcast patterns
- ✅ **Supervision Trees**: Fault tolerance with retry/recovery strategies
- ✅ **SQLite Persistence**: Durable state, configuration, and message history
- ✅ **Streaming LLM**: Real-time AI responses via Vercel AI SDK
- ✅ **Logging System**: Dual-channel (console + file) with JSONL format
- ✅ **Type Safety**: Full TypeScript with Zod validation
- ✅ **Extensibility**: Factory patterns for custom agent types

### 📋 Use Case Flow

```
1. User creates Team → defines multiple Agents
2. User initiates conversation → Leader Agent receives message
3. Leader Agent analyzes → can delegate to sub-agents
4. Sub-agents process concurrently → send results back
5. Leader Agent aggregates → sends final response
6. All state automatically persisted to SQLite
```

---

## 3. TECHNOLOGY STACK

### 🔧 Core Technologies

| Layer | Technology | Version | Purpose |
|-------|-----------|---------|---------|
| **Desktop** | Tauri | 2.10.3 | Rust-based lightweight desktop framework |
| **Frontend** | React | 19.1.0 | UI component framework |
| **State Mgmt** | XState | 5.x | Finite state machines + actor system |
| **Backend** | Fastify | 5.x | High-performance HTTP/WebSocket server |
| **LLM SDK** | Vercel AI | 5.x | Unified LLM provider interface |
| **Database** | sql.js | 1.x | Pure JavaScript SQLite (no native deps) |
| **ORM** | Drizzle ORM | 0.38.x | Type-safe schema management |
| **Validation** | Zod | 4.x | Runtime type validation |
| **Styling** | TailwindCSS | 4.x | Utility-first CSS framework |
| **Build Tool** | Vite | 7.x | Ultra-fast module bundler |
| **Testing** | Vitest | 3.x | Unit & integration testing |
| **Linting** | ESLint + Prettier | Latest | Code quality & formatting |
| **Logging** | Winston | 3.x | Structured logging library |
| **Scheduling** | node-cron | 3.x | Task scheduling |
| **UUID** | uuid | 11.x | Unique ID generation |

### 📦 Dependency Tree

```
Node.js Ecosystem:
├── @tauri-apps/api (Tauri IPC)
├── @tauri-apps/plugin-opener (File operations)
├── @tauri-apps/cli (Build tooling)
├── react + react-dom (UI)
├── @xstate/react (React integration)
├── xstate (State machines)
├── fastify (Backend server)
├── ai (Vercel AI SDK for LLM)
├── drizzle-orm (ORM)
├── sql.js (SQLite)
├── winston (Logging)
├── winston-daily-rotate-file (Log rotation)
├── node-cron (Scheduling)
├── zod (Validation)
├── tailwindcss (Styling)
├── uuid (ID generation)
└── [Dev] typescript, vite, vitest, eslint, prettier

Rust Ecosystem (src-tauri):
├── tauri (Desktop app)
├── tauri-plugin-opener (File handling)
├── serde (Serialization)
└── serde_json (JSON handling)

Python Ecosystem:
└── soloqueue/ (Modules for orchestration)
```

---

## 4. BACKEND/FRONTEND SEPARATION

### 🎨 Frontend (React)

**Location**: `/src/`

- **Framework**: React 19 with TypeScript
- **Styling**: TailwindCSS v4
- **Entry Points**: 
  - `src/main.tsx` - React DOM mount
  - `src/App.tsx` - Main component (currently template)
- **Build**: Vite dev server (port 1420)
- **Assets**: Public static files + React SVG components

**Current State**: Template/Skeleton - Placeholder Tauri greeting example

### 🔌 Backend (Node.js + TypeScript)

**Location**: `/server/`

**Architecture Layers**:

1. **Actor System** (`/server/actors/`)
   - `ActorSystem`: Central orchestrator managing all actors
   - `Supervisor`: Monitors actor health, implements recovery strategies
   - `Router`: Route messages to target actors (round-robin, load-balancing)
   - Message dispatch: broadcast, direct, request-response patterns

2. **State Machines** (`/server/machines/`)
   - `AgentMachine`: Agent lifecycle (idle → processing → waitingLLM/delegating → responding)
   - `LLMCallMachine`: LLM request handling
   - `TaskMachine`: Task delegation & child actor coordination

3. **LLM Integration** (`/server/llm/`)
   - Provider management (DeepSeek, OpenAI, etc.)
   - Model selection & fallback strategies
   - Configuration service for model defaults
   - Streaming response handling

4. **Data Persistence** (`/server/storage/`)
   - SQLite via sql.js (no native compilation needed)
   - Drizzle ORM for type-safe queries
   - Schema: Teams, Agents, Configs, Message logs
   - Repository pattern (DAO layer)
   - Automatic migrations

5. **Logging System** (`/server/logger/`)
   - Dual transports: Console + File (daily rotation)
   - Structured JSONL format for machine readability
   - Log levels: DEBUG, INFO, WARN, ERROR
   - Automatic cleanup tasks for old logs

6. **HTTP/WebSocket API** (In development)
   - Fastify server (port TBD)
   - WebSocket for real-time agent communication
   - REST endpoints for agent management

### 🤝 Communication

- **Frontend → Backend**: Tauri IPC + WebSocket
- **Backend Internal**: Actor message passing (async)
- **LLM API**: Streaming via Vercel AI SDK
- **Data Storage**: Direct SQLite via ORM

---

## 5. KEY CONFIGURATION FILES

### `.env` - Environment Variables

```plaintext
# LLM Provider Configuration
OPENAI_API_KEY=sk-3b2bea545ace447cafc85777969d6088
OPENAI_BASE_URL=https://api.deepseek.com/v1
DEFAULT_MODEL=deepseek-reasoner

# Embedding Configuration (L3 semantic memory)
SOLOQUEUE_EMBEDDING_ENABLED=true
SOLOQUEUE_EMBEDDING_PROVIDER=openai-compatible
SOLOQUEUE_EMBEDDING_MODEL=BAAI/bge-large-zh-v1.5
SOLOQUEUE_EMBEDDING_API_BASE=https://api.siliconflow.cn/v1
SOLOQUEUE_EMBEDDING_API_KEY=sk-pzxkzkhjphcmoblabmxslyxtanybooykqhjhwclslzrfgqvg
SOLOQUEUE_EMBEDDING_DIMENSION=1024

# System Configuration
LOG_LEVEL=INFO
REQUIRE_APPROVAL=true
```

### `tauri.conf.json` - Tauri Desktop Configuration

```json
{
  "productName": "soloqueue-temp",
  "version": "0.1.0",
  "identifier": "com.malza.soloqueue-temp",
  "build": {
    "beforeDevCommand": "npm run dev",
    "devUrl": "http://localhost:1420",
    "beforeBuildCommand": "npm run build",
    "frontendDist": "../dist"
  },
  "app": {
    "windows": [
      {
        "title": "soloqueue-temp",
        "width": 800,
        "height": 600
      }
    ]
  }
}
```

### `package.json` - Node.js Project

```json
{
  "name": "soloqueue",
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview",
    "tauri": "tauri",
    "lint": "eslint .",
    "lint:fix": "eslint . --fix",
    "format": "prettier --write .",
    "check": "tsc --noEmit && eslint . && prettier --check .",
    "test": "vitest run",
    "test:coverage": "vitest run --coverage"
  }
}
```

### `vite.config.ts` - Build Configuration

- React plugin + TailwindCSS plugin
- Development server on port 1420
- Ignores src-tauri for watching
- HMR configured for Tauri development

### `vitest.config.ts` - Test Configuration

- Node environment
- Global test APIs enabled
- Coverage: V8 provider, 72%+ target
- Tests in server/** directory

### `tsconfig.json` - TypeScript Configuration

- Target: ES2020
- Strict mode enabled
- JSX React mode
- Module resolution: bundler
- No unused variables/parameters

---

## 6. CURRENT DEVELOPMENT STATUS

### ✅ Completed Phases

**Phase 1**: Project initialization (Tauri + React + TypeScript)  
**Phase 2**: Logging system (dual-channel, JSONL format)  
**Phase 3**: Storage layer / DAO (SQLite + Drizzle ORM)  
**Phase 4**: Configuration system  
**Phase 5**: Default configurations  
**Phase 6**: State machines (Agent, LLM, Task)  
**Phase 7**: Actor system core (IN PROGRESS)

### 📍 Current Focus - Phase 7: Actor System Core

**Completed**:
- ActorSystem class (696 LOC)
  - Registry management
  - Lifecycle (start, stop)
  - Agent creation/deletion
  - Message dispatch (broadcast, direct, request-response)
  - Supervision integration
  - Persistence integration
  - Extensibility (factory pattern, routing strategies)

- Supervisor (monitoring + recovery)
- Router (message routing strategies)
- Type definitions & extensions
- Comprehensive test coverage (>72%)

**Recent Commits** (Last 5):
1. `ca263c7` - feat: P0 bug fixes and architecture improvements
2. `e60dca2` - fix: 完成日志系统统一使用
3. `610d5b0` - fix: 修复日志库配置问题
4. `cbd1862` - fix: 修复状态机测试并补充覆盖率
5. `28bdce2` - docs: 更新 todo.md，标记 Phase 6 状态机完成

### ⏳ Upcoming Phases

**Phase 8**: LLM integration (streaming, tool calling)  
**Phase 9**: Backend HTTP/WebSocket API  
**Phase 10**: Frontend UI components setup  
**Phase 11**: Frontend pages (Dashboard, Chat, Agent Management, Settings)  
**Phase 12**: Security & supervision strategies  
**Phase 13**: Performance optimization  
**Phase 14**: Documentation & comprehensive testing

---

## 7. KEY FILE CONTENTS

### `server/actors/actor-system.ts` (696 lines)

**Core Responsibilities**:
- Manages actor lifecycle (create, start, stop, delete)
- Maintains registry of all active actors
- Implements message dispatch (broadcast, direct, request-response)
- Integrates with supervisor for fault recovery
- Integrates with persistence layer
- Provides extensibility points (factory registration, routing strategies)

**Key Methods**:
- `start()` - Initialize system, restore user agents
- `stop()` - Graceful shutdown
- `createAgent()` - Spawn new agent
- `stopAgent()` - Stop and unregister
- `deleteAgent()` - Delete permanently
- `dispatch()` - Route message to actor
- `ask()` - Request-response pattern
- `broadcast()` - Send to all actors
- `registerFactory()` - Custom agent types
- `setRoutingStrategy()` - Load-balancing strategy

### `server/machines/agent-machine.ts` (150+ lines)

**State Diagram**:
```
idle → processing → [llm | delegate | respond] → responding → idle
       ↓
       error → idle
```

**Context**:
- Message history
- Current task
- Child actors
- LLM call tracking
- Configuration (model, temperature, tokens)

### `server/storage/schema.ts` (82 lines)

**Tables**:
1. `teams` - Team metadata
2. `agents` - Agent definitions
3. `configs` - Global configuration (key-value)

**Design**: Snake_case DB columns → camelCase TypeScript via Drizzle

### `server/llm/llm-config.service.ts` (100+ lines)

**Manages**:
- LLM Provider registry
- Model registry with type/provider indexing
- Agent defaults (model, temperature, tokens)
- Supervisor defaults (strategy, retries, backoff)
- Model selection with fallback

---

## 8. ARCHITECTURE DIAGRAM

### Actor System Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    ActorSystem                           │
│  ┌────────────────────────────────────────────────────┐  │
│  │            Registry (Map<ID, ActorInstance>)       │  │
│  │  Leader         Worker1    Worker2    ... WorkerN  │  │
│  └────────────────────────────────────────────────────┘  │
│              ▲              ▲              ▲              │
│              │ supervise    │              │              │
│  ┌───────────┴──────────────┴──────────────┴─────────┐   │
│  │              Supervisor (Fault Recovery)          │   │
│  │  One-For-One | One-For-All | Resume | Stop       │   │
│  └───────────────────────────────────────────────────┘   │
│              ▲                                            │
│              │ route messages                            │
│  ┌───────────┴─────────────────────────────────────────┐ │
│  │         Router (Load Balancing Strategy)            │ │
│  │  RoundRobin | LeastLoaded | Sticky | Custom        │ │
│  └───────────────────────────────────────────────────┬─┘ │
│                                                      │    │
│  Message Flow:                                       │    │
│  1. Dispatch (via Router)                           │    │
│  2. Ask (request-response pattern)                  │    │
│  3. Broadcast (to all)                              │    │
│  4. Direct (to specific actor)                      │    │
└──────────────────────────────────────────────────────────┘
        │                                    │
        ▼                                    ▼
   SQLite (Persistence)            LLM API (DeepSeek/OpenAI)
```

### Message Flow

```
Frontend (React)
    │
    ▼
Tauri IPC / WebSocket
    │
    ▼
ActorSystem.dispatch(message)
    │
    ├─ task → Router → Target Actor
    ├─ ask → Wait for reply
    └─ broadcast → All Actors
    │
    ▼
Agent Machine (State Machine)
    │
    ├─ processing state
    │  ├─ Call LLM → LLM Machine
    │  ├─ Delegate → Create child actor
    │  └─ Direct response
    │
    ▼
Persist state/logs
    │
    ├─ SQLite (state snapshots)
    ├─ File logs (JSONL format)
    └─ Message history
```

---

## 9. DEPENDENCIES VISUALIZATION

```
package.json
├── Runtime Dependencies (Production)
│   ├── @tauri-apps/api (2.x) - Desktop integration
│   ├── @tauri-apps/plugin-opener (2.x) - File operations
│   ├── ai (5.x) - LLM streaming interface
│   ├── drizzle-orm (0.38.x) - Database ORM
│   ├── fastify (5.x) - HTTP server
│   ├── node-cron (3.x) - Task scheduling
│   ├── react (19.1.0) - UI framework
│   ├── react-dom (19.1.0) - React rendering
│   ├── sql.js (1.x) - SQLite (pure JS)
│   ├── tailwindcss (4.x) - CSS framework
│   ├── uuid (11.x) - UUID generation
│   ├── winston (3.x) - Logging
│   ├── winston-daily-rotate-file (5.x) - Log rotation
│   ├── xstate (5.x) - State machines & actors
│   └── zod (4.x) - Runtime validation
│
└── Dev Dependencies (Development)
    ├── @eslint/js (10.x) - Base ESLint rules
    ├── @tailwindcss/vite (4.x) - Tailwind Vite plugin
    ├── @tauri-apps/cli (2.x) - Tauri build tools
    ├── @types/node (22.x) - Node.js types
    ├── @types/react (19.1.8) - React types
    ├── @types/react-dom (19.1.6) - React DOM types
    ├── @typescript-eslint/* (8.x) - TS linting
    ├── @vitejs/plugin-react (4.x) - React plugin
    ├── @vitest/coverage-v8 (3.x) - Test coverage
    ├── @xstate/react (5.x) - XState React hooks
    ├── eslint (10.x) - Linting
    ├── eslint-config-prettier (10.x) - Prettier integration
    ├── prettier (3.x) - Code formatting
    ├── typescript (5.8.3) - TypeScript compiler
    ├── vite (7.x) - Build tool
    └── vitest (3.x) - Testing framework
```

---

## 10. KEY FEATURES & IMPLEMENTATION STATUS

### ✅ Implemented

| Feature | Status | Location | Notes |
|---------|--------|----------|-------|
| Actor System Core | ✅ Complete | server/actors/ | Full lifecycle management |
| State Machines | ✅ Complete | server/machines/ | XState v5 integration |
| SQLite + ORM | ✅ Complete | server/storage/ | sql.js + Drizzle |
| Logging System | ✅ Complete | server/logger/ | JSONL + daily rotation |
| Configuration | ✅ Complete | server/storage/ | Key-value store |
| LLM Config | ✅ Complete | server/llm/ | Provider & model mgmt |
| Supervision | ✅ Complete | server/actors/supervisor | Recovery strategies |
| Router | ✅ Complete | server/actors/router | Load balancing |
| Type System | ✅ Complete | Throughout | Full TypeScript types |
| Tauri Desktop | ✅ Complete | src-tauri/ | Minimal setup |
| React Template | ✅ Skeleton | src/ | Placeholder example |

### ⏳ In Progress / Planned

| Feature | Status | Target |
|---------|--------|--------|
| Backend API | 🔄 In Dev | Phase 9 |
| WebSocket Server | ⏳ Planned | Phase 9 |
| Frontend Components | ⏳ Planned | Phase 10-11 |
| Dashboard Page | ⏳ Planned | Phase 11 |
| Chat Interface | ⏳ Planned | Phase 11 |
| Agent Manager | ⏳ Planned | Phase 11 |
| Performance Optimization | ⏳ Planned | Phase 13 |
| Comprehensive E2E Tests | ⏳ Planned | Phase 14 |

---

## 11. DEVELOPMENT COMMANDS

```bash
# Development
npm run dev              # Start Vite dev server (port 1420)
npm run tauri dev       # Run desktop app with hot reload

# Building
npm run build           # Compile TypeScript + Vite
npm run preview         # Preview production build

# Code Quality
npm run lint            # Check code
npm run lint:fix        # Auto-fix linting issues
npm run format          # Format with Prettier
npm run check           # Full check (TypeScript + lint + format)

# Testing
npm run test            # Run unit tests
npm run test:coverage   # Generate coverage report

# Git Branches
main                    # Active development
001-web-ui-enhancements # Feature branch
```

---

## 12. PROJECT TIMELINE & ROADMAP

**Phase 7 Completion**: ~100% (Actor System Core)  
**Next Major**: Phase 8-9 (LLM Integration + API)  
**UI Development**: Phase 10-11 (Frontend)  
**MVP Launch Target**: After Phase 11

**Key Milestones**:
- ✅ Core actor system running
- 🔄 Streaming LLM integration
- ⏳ Full WebSocket API
- ⏳ Production UI
- ⏳ Full test coverage (E2E)

---

## 13. NOTES & OBSERVATIONS

### Strengths
1. **Well-architected** - Clean separation of concerns (actors, machines, storage, logging)
2. **Type-safe** - Full TypeScript with Zod validation
3. **Extensible** - Factory patterns and extension points throughout
4. **Production-ready patterns** - Repository, service, and actor patterns
5. **Test-focused** - >72% test coverage from day one
6. **Documentation** - Comprehensive inline comments and todo.md roadmap

### Architecture Highlights
- Pure JavaScript SQLite (sql.js) - no native compilation
- XState v5 for deterministic state management
- Dual-channel logging (console + JSONL file)
- Actor model with supervision and recovery
- Message routing with pluggable strategies

### Next Steps Priority
1. Implement LLM streaming integration
2. Build WebSocket API for frontend
3. Create base UI components
4. Build core pages (Chat, Dashboard)
5. Integration testing
6. Performance tuning

