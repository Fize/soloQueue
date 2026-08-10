# Architecture overview

SoloQueue is a local-first application composed of a Go runtime, a React
desktop console, and a lightweight React portal embedded in the Go binary.

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

The server is started by soloqueue serve. It builds one runtime Stack and
shares its LLM clients, config service, team store, skill registry, MCP
manager, memory subsystems, workflow runtime, and simulation engine with the
session manager and HTTP handlers.

The default listener is loopback. Remote requests pass through the auth
middleware; localhost is intentionally treated differently. See
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

An agent is a long-lived actor with a mailbox. A request enters a session,
resolves a task type and model, runs a streamed tool loop, and returns events
to the WebSocket client. One session request is active at a time; delegated
work is tracked by a supervisor and can resume the parent through a priority
mailbox.

Tool confirmations are emitted as events and resume the waiting request after
the user chooses an action. The bypass flag changes this policy globally and
must be treated as an operator decision.

## Persistence and observability

SQLite stores shared configuration-backed records, teams, cron tasks, memory,
and workflow run state. Timeline events are append-only JSONL. Rotating
application and HTTP logs provide operational evidence. The Stats and
workflow-run pages are views over these records; they do not replace reviewing
the project filesystem or external provider logs.

## Frontends

- desktop/ is the Electron + React console for local and configured remote
  connections.
- portal/ is a lightweight React portal built into internal/server/dist/ and
  served by the Go binary.

The frontends use the same backend contracts. A desktop development server
expects the backend on port 8765; the embedded portal defaults to port 57647.
