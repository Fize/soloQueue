# Architecture overview

> 中文：[架构概览](../zh/architecture/overview.md)

I built SoloQueue as a local-first application composed of a Go runtime, a
React desktop console, and a lightweight React portal embedded in the Go
binary.

~~~text
Desktop / Portal
       │ HTTP + WebSocket
       ▼
HTTP server and auth boundary
       │
       ▼
Session manager
       │
       ├── Agent actors and supervisors
       │       ├── task router and model clients
       │       ├── native tools, skills, MCP/LSP
       │       └── confirmation and project scope
       ├── workflows, cron, and simulations
       ├── QQ / WeChat channel bridges
       └── memory, timeline, SQLite, and logs
~~~

## Process boundary

I start the server with soloqueue serve. I build one runtime Stack and share
my LLM clients, config service, team store, skill registry, MCP manager, memory
subsystems, workflow runtime, and simulation engine with the session manager
and HTTP handlers.

I use a loopback listener by default. Remote requests pass through the auth
middleware; I intentionally treat localhost differently. See
[Remote access](../operations/remote-access.md).

## Runtime layers

| Layer | Current package area | Responsibility |
| --- | --- | --- |
| Transport | internal/server | REST, WebSocket, auth, static portal |
| Sessions | internal/session | Conversation state, request serialization, history |
| Agents | internal/agent | Actor lifecycle, mailboxes, streaming, delegation |
| Routing | internal/router, internal/tasktype | general/engineering/research classification and model selection |
| Capabilities | internal/agenttools | Native tools, skills, MCP, and LSP |
| Prompt/team | internal/prompt, internal/team | Prompt assembly, agent templates, team persistence |
| Memory | internal/memory | Context compaction, summaries, long-term search, timeline |
| Runtime | internal/runtime | Shared dependency construction and hot reload |
| Automation | internal/cron, internal/workflow, internal/simulation | Scheduled tasks, YAML DAG runs, generative simulations |
| Channels | internal/channel | QQ and WeChat ingress and reply contracts |
| Infrastructure | internal/infra | SQLite, logs, telemetry, workdir, and platform helpers |

## Agent lifecycle

I model an agent as a long-lived actor with a mailbox. A request enters a
session, resolves a task type and model, runs a streamed tool loop, and returns
events to my WebSocket client. One session request is active at a time;
delegated work is tracked by a supervisor and can resume the parent through a
priority mailbox.

I emit tool confirmations as events and resume the waiting request after I
choose an action. The bypass flag changes this policy globally, so I treat it
as an operator decision.

## Persistence and observability

I store shared configuration-backed records, teams, cron tasks, memory, and
workflow run state in SQLite. I write timeline events as append-only JSONL.
Rotating application and HTTP logs provide operational evidence. I use the
Stats and workflow-run pages as views over these records; I still review the
project filesystem and external provider logs.

## Frontends

- I use desktop/ as the Electron + React console for local and configured remote
  connections.
- I use portal/ as a lightweight React portal built into internal/server/dist/
  and served by the Go binary.

I make the frontends use the same backend contracts. My desktop development
server expects the backend on port 8765; my embedded portal defaults to port
57647.
