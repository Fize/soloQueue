# SoloQueue Documentation

Welcome to the SoloQueue documentation hub. This directory contains detailed architecture docs and design notes.

## 📖 Architecture Docs

Detailed system design documents, organized by subsystem:

| Document                                   | Description                                                                                 |
| ------------------------------------------ | ------------------------------------------------------------------------------------------- |
| [Architecture](architecture.md)            | Agent, LLM, Tool, Skill system architecture overview                                        |
| [Configuration](config.md)                 | Config system, layered loading, hot-reload, type-safe access                                |
| [Context Window](ctxwin.md)                | Token count calibration, middle-out JSON truncation, Turn-based FIFO sliding window         |
| [Memory Engine](memory.md)                 | Short-term daily summaries, long-term BM25 + KG + optional vector engine, temporal tracking |
| [Timeline](timeline.md)                    | Event-sourced append-only JSONL log, session replay, orphaned tool call repair              |
| [Skill Store & Management](skill_store.md) | Centralized catalog, Git/Github clones, local symlinking, shadowing override pattern        |

## 🎯 Feature Docs

| Document                  | Description                                                                               |
| ------------------------- | ----------------------------------------------------------------------------------------- |
| [Routing](routing.md)     | L0-L3 task classification system, fast track, hybrid sticky logic, explicit level locking |
| [QQ Bot Client](qqbot.md) | WebSocket gateway loop, bridge active/passive reply queue, rate limiter, media uploads    |
| [MCP & LSP](mcp.md)       | Model Context Protocol servers loading, LSP JSON-RPC tool binding                         |

## 🎭 Role Definitions

The `roles/` directory contains sample agent persona definitions (soul profiles):

- [Roles README](roles/README.md) - How to use and customize soul profiles
- `hanli.md` - Han Li (protagonist of the cultivation novel "A Record of a Mortal's Journey to Immortality")
- `jiyin.md` - Ancestor Ji Yin
- `nangongwan.md` - Nangong Wan
- `xuangu.md` - Venerable Xuan Gu
- `yuanyao.md` - Yuan Yao
- `ziling.md` - Zi Ling

## 🔗 Quick Navigation

- [Back to Main README](../README.md)
- [GitHub Repository](https://github.com/xiaobaitu/soloqueue)

---

<div align="center">

Need help? Please ask on [GitHub Discussions](https://github.com/xiaobaitu/soloqueue/discussions).

</div>
