# Architecture & Design

English | [简体中文](zh/architecture.md)

This document provides a technical overview of SoloQueue's internal architecture, process boundaries, memory engine, task routing, and platform integrations.

It describes `main`, which excludes simulation. The repository state before extraction is preserved on `experimental/simulation`. Existing `simulation.db` files and their `-wal` or `-shm` sidecars are retained, but `main` does not open or initialize them. Use a separate `SOLOQUEUE_WORK_DIR` when running that branch so its settings do not affect the work directory used by `main`.

---

## 1. Process Boundary & Layering

SoloQueue comprises a Go backend server, an embedded browser Web Console (`web/`), and an independent read-only Status UI (`status-ui/`). Browser assets are embedded under `internal/assets/`; Skills are installed as external packages in the work directory.

```text
Web Console / Status UI
       │ HTTP + WebSocket
       ▼
HTTP Server & Loopback CORS (internal/server)
       │
       ▼
Session Manager (internal/session)
       │
       ├── Agent Actor Loop & Supervisors (internal/agent)
       │       ├── Task Router & Model Clients (internal/router, internal/llm)
       │       ├── Native Tools, Skills, MCP/LSP (internal/agenttools)
       │       └── Deterministic tool safety checks
       ├── Cron Runtime (internal/cron)
       ├── Channel Bridges (internal/channel/qq, internal/channel/wechat, internal/channel/telegram)
       └── Memory, Timeline, SQLite DB & Logger (internal/infra, internal/memory)
```

The server constructs a shared dependency container (`runtime.Stack`) at startup. Shared subsystems—LLM clients, tool registries, memory engines, SQLite databases, and channel handlers—are injected into the session manager and HTTP endpoints.

---

## 2. Task Router (`internal/router`)

Prompts are classified into work categories (`general`, `engineering`, `research`) and mapped to configured model routes:

1. **Local Fast-Track Classifier**: Evaluates structural patterns such as code blocks, stack tracebacks, path mentions, and terminal commands.
2. **LLM Classifier Fallback**: If pattern matching is ambiguous, the configured classifier model classifies the prompt.
3. **Session Context Continuity**: The previous task classification is supplied when classifying follow-up turns.

---

## 3. Context Window & Compaction (`internal/memory/ctxwin`)

The context manager counts payload tokens and compacts history at configured thresholds:

- **Token Counting**: Uses model-specific tokenizers to calculate payload size.
- **Dual Waterline Compaction**: Triggers summarization of historical turns when token consumption breaches upper thresholds.
- **Payload Sanitization**: Filters orphaned tool-call/result pairs before dispatching payloads to external LLM APIs.

---

## 4. Memory Subsystem (`internal/memory`)

SoloQueue separates ephemeral context from durable search and audit logs:

- **Short-Term Memory (`internal/memory/conversation`)**: Stores LLM-generated conversation summaries written during context window compaction.
- **Long-Term Memory (`internal/memory/engine`)**: Pure Go hybrid search engine combining SQLite FTS5 BM25 search with an in-process Knowledge Graph. Optional vector search is enabled when an external embedding provider is configured and is disabled by default.
- **Timeline (`internal/memory/timeline`)**: Append-only JSONL event stream recording tool calls, session state changes, routing resolutions, and agent delegation events. Raw system prompts are not written to the timeline.

---

## 5. Channel Integration Architecture (`internal/channel`)

Channel bridges normalize external messaging protocols into the internal session event stream:

- **QQ Bot (`internal/channel/qq`)**: Implements Tencent Bot Gateway protocol. Handles passive response windows and queues active outbound message bursts.
- **WeChat iLink (`internal/channel/wechat`)**: Connects through Tencent's official iLink Bot API. Supports long-poll update streams, QR login pairing, and typing state keepalives.
- **Telegram (`internal/channel/telegram`)**: Connects to the Telegram Bot API. Uses long polling for updates and supports text and media delivery.
