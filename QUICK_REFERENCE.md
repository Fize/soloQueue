# SoloQueue - Quick Reference Guide

## 🚀 Quick Start Commands

```bash
# Install dependencies
npm install

# Development
npm run dev              # Start Vite dev server (port 1420)
npm run tauri dev       # Run desktop app with hot reload

# Testing
npm run test            # Run all tests
npm run test:coverage   # Generate coverage report

# Code Quality
npm run lint            # Check code
npm run format          # Format with Prettier
npm run check           # Full TypeScript + ESLint + Prettier check

# Build
npm run build           # Production build
npm run preview         # Preview built app
```

## 📁 Project Structure at a Glance

```
/src              → React frontend (port 1420)
/server           → TypeScript backend (13,954 LOC)
  ├── actors/     → Actor system & message dispatch
  ├── machines/   → XState state machines
  ├── llm/        → LLM provider management
  ├── logger/     → Advanced logging
  └── storage/    → SQLite + ORM layer
/src-tauri        → Rust desktop app
/tests            → Test suites
```

## 🏗️ Architecture Quick View

### Three-Layer Backend
```
HTTP/WebSocket API (Phase 9)
     ↓
Actor System (✅ Phase 7 Complete)
     ↓
Storage Layer (SQLite) + LLM Providers
```

### Message Flow
```
Frontend → Tauri IPC → ActorSystem.dispatch() → State Machine → LLM/Storage
```

## 🔑 Key Files

| File | Purpose | Lines |
|------|---------|-------|
| `server/actors/actor-system.ts` | Core actor orchestration | 696 |
| `server/machines/agent-machine.ts` | Agent lifecycle FSM | 150+ |
| `server/storage/db.ts` | SQLite connection | 80+ |
| `server/logger/core.ts` | Logging system | Multi |
| `server/llm/llm-config.service.ts` | LLM management | 100+ |
| `src/App.tsx` | React root | 50 |

## 🛠️ Technology Highlights

| Layer | Tech | Purpose |
|-------|------|---------|
| Desktop | Tauri 2 | Cross-platform app |
| Frontend | React 19 + TailwindCSS | UI |
| Backend | Node.js + TypeScript | Server |
| State | XState 5 | Finite state machines |
| Database | sql.js + Drizzle | SQLite ORM |
| LLM | Vercel AI SDK | Streaming responses |
| Testing | Vitest | Unit tests (72%+ coverage) |

## 📊 Current Status

**Phase**: 7 (Actor System Core) - 🔄 IN PROGRESS  
**Code**: 79 TypeScript files, 13,954 LOC  
**Tests**: >72% coverage with Vitest  
**Next**: Phase 8 (LLM Integration) → Phase 9 (WebSocket API)

## 🔧 Configuration Files

```
.env                 → LLM API keys, embedding config
tauri.conf.json     → Desktop app config
package.json        → Dependencies & scripts
tsconfig.json       → TypeScript settings
vite.config.ts      → Build settings
vitest.config.ts    → Test settings
eslint.config.ts    → Linting rules
```

## 📚 Core Concepts

### Actor Model
- Each agent = independent actor with own state
- Communication via async message passing
- No shared state between actors
- Supervision tree for fault tolerance

### State Machines (XState v5)
- Deterministic state transitions
- Agent lifecycle: idle → processing → responding
- Type-safe event handling

### Message Types
- `task` - Delegate work to actor
- `delegate` - Create child actor
- `result` - Return from actor
- `broadcast` - Send to all actors

### Storage Layer (Drizzle ORM)
- Pure JavaScript SQLite (sql.js)
- Schema: Teams, Agents, Configs
- Repository pattern for type safety
- Automatic migrations

## 🚨 Important Notes

1. **No Native Compilation**: Uses sql.js (pure JS SQLite)
2. **Type-Safe**: Full TypeScript with Zod validation
3. **Extensible**: Factory pattern for custom actor types
4. **Streaming**: Vercel AI SDK for real-time LLM responses
5. **Desktop First**: Tauri for lightweight cross-platform app

## 📍 Development Workflow

1. Make changes in `/src` (frontend) or `/server` (backend)
2. Run `npm run check` to validate
3. Run `npm run test` to verify
4. Use `npm run tauri dev` to test desktop app
5. Commit with descriptive messages
6. Push to main branch

## 🎯 Next Priority Tasks

1. **Phase 8**: Implement LLM streaming integration
2. **Phase 9**: Build WebSocket API with Fastify
3. **Phase 10**: Create React UI components
4. **Phase 11**: Build pages (Dashboard, Chat, Settings)
5. **Phase 12**: Security & supervision strategies
6. **Phase 13**: Performance optimization
7. **Phase 14**: Comprehensive E2E testing

## 🔗 Useful Links

- **Documentation**: `todo.md` (detailed roadmap)
- **Full Overview**: `PROJECT_OVERVIEW.md` (comprehensive guide)
- **Code Coverage**: Run `npm run test:coverage`
- **Type Definitions**: Check `.ts` files in `/server`

## 💡 Common Tasks

```bash
# Run tests with coverage
npm run test:coverage

# Check specific file
npm run lint -- server/actors/actor-system.ts

# Format all files
npm run format

# Type check without emitting
npm run build

# See test results
npm run test -- --reporter=verbose
```

## 🆘 Troubleshooting

| Issue | Solution |
|-------|----------|
| Port 1420 in use | `npm run dev` uses different port |
| Build fails | Run `npm install` then `npm run build` |
| Tests fail | Run `npm run test` individually to identify |
| Type errors | Run `npm run check` for full validation |

---

**Last Updated**: April 21, 2026  
**Project**: SoloQueue - AI Multi-Agent Desktop App  
**Status**: Active Development 🚀
