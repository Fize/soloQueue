# Architecture & Design

English | [简体中文](zh/architecture.md)

This document provides a technical overview of SoloQueue's internal architecture, process boundaries, memory engine, task routing, and platform integrations.

---

## 1. Process Boundary & Layering

SoloQueue is structured as a local-first application comprising a Go backend server, an embedded browser Web Console (`web/`), and an independent read-only Status UI (`status-ui/`). Both bundles and the Skill Store are embedded separately under `internal/assets/`.

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
       ├── Cron & Simulation Runtimes (internal/cron, internal/simulation)
       ├── Channel Bridges (internal/channel/qq, internal/channel/wechat)
       └── Memory, Timeline, SQLite DB & Logger (internal/infra, internal/memory)
```

The server constructs a shared dependency container (`runtime.Stack`) at startup. Shared subsystems—LLM clients, tool registries, memory engines, SQLite databases, and channel handlers—are injected into the session manager and HTTP endpoints.

---

## 2. Task Router (`internal/router`)

Prompts are classified into work categories (`general`, `engineering`, `research`) to select optimal model configurations:

1. **Local Fast-Track Classifier**: Evaluates input against high-confidence structural patterns (code blocks, stack tracebacks, path mentions, terminal commands).
2. **LLM Classifier Fallback**: If pattern matching is ambiguous, a lightweight LLM call classifies the prompt.
3. **Session Context Continuity**: Follow-up turns retain task classification context to prevent abrupt model route switching within a conversation turn.

---

## 3. Context Window & Compaction (`internal/memory/ctxwin`)

The context manager protects context window budgets while preserving vital information:

- **Token Counting**: Uses model-specific tokenizers to calculate payload size.
- **Dual Waterline Compaction**: Triggers summarization of historical turns when token consumption breaches upper thresholds.
- **Payload Sanitization**: Filters orphaned tool-call/result pairs before dispatching payloads to external LLM APIs to prevent HTTP 400 errors.

---

## 4. Memory Subsystem (`internal/memory`)

SoloQueue separates ephemeral context from durable search and audit logs:

- **Short-Term Memory (`internal/memory/conversation`)**: Stores LLM-generated conversation summaries written during context window compaction.
- **Long-Term Memory (`internal/memory/engine`)**: Pure Go hybrid search engine combining SQLite FTS5 BM25 search with an in-process Knowledge Graph. Optional vector search fuses embeddings when an external provider is configured (disabled by default for zero external dependencies).
- **Timeline (`internal/memory/timeline`)**: Append-only JSONL event stream recording granular tool calls, session state changes, routing resolutions, and agent delegation events. Excludes raw system prompts to protect user privacy.

---

## 5. Channel Integration Architecture (`internal/channel`)

Channel bridges normalize external messaging protocols into the internal session event stream:

- **QQ Bot (`internal/channel/qq`)**: Implements Tencent Bot Gateway protocol. Handles passive response windows and queues active outbound message bursts.
- **WeChat iLink (`internal/channel/wechat`)**: Connects through Tencent's official iLink Bot API. Supports long-poll update streams, QR login pairing, and typing state keepalives.
