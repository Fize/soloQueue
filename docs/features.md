# Feature Guide

English | [简体中文](zh/features.md)

This document details SoloQueue's key capabilities: project workspace management, team & agent customization, model routing, scheduled tasks, messaging channels, and skills/MCP extensions.

---

## 1. Projects & Sessions

SoloQueue separates the global runtime work directory (`~/.soloqueue/`) from project execution scopes:

- **Projects**: Point to absolute repository paths. Project paths establish tool execution scope, ensuring agent writes and shell commands are constrained to intended directories.
- **Sessions**: Chat sessions stream agent reasoning and tool executions over WebSocket. Session state and message history persist across server restarts.

---

## 2. Teams & Agent Templates

Agent execution relies on agent templates and team definitions stored in the work directory:

- `agents/`: Markdown files with YAML frontmatter defining agent identity, model route, permitted tools, and system instructions.
- `groups/`: Team definitions grouping agents for collaborative or delegated tasks.
- **Delegation**: A primary session can delegate bounded subtasks to specialized subagents. A supervisor tracks subagent execution and returns results to the parent session.

---

## 3. Models & Task Routing

Requests are classified by work nature rather than a artificial difficulty ladder:

| Task Type | Work Nature |
| --- | --- |
| `general` | Conversations, text writing, translation, summarization |
| `engineering` | Code inspection, repository edits, debugging, unit testing, deployment |
| `research` | Web search, documentation lookups, current information retrieval |

Classification uses local fast-track rules first (detecting code blocks, tracebacks, paths, shell commands). Ambiguous prompts pass to a configured classifier model. Model routes map task types to `provider:model` pairs in `settings.yaml`.

---

## 4. Scheduled Tasks (Cron)

Cron tasks run recurring or one-off prompts through the standard session, routing, and tool verification policies:

- Manage scheduled jobs via the **Scheduled tasks** interface.
- Jobs execute with specified agent templates and optional project path bounds.
- Execution history, outputs, and status are tracked in the database and UI history view.

---

## 5. Messaging Channels

SoloQueue bridges agent runtimes to messaging platforms without creating disconnected memory systems:

- **QQ Bot**: Connects via Tencent Bot Gateway using App ID and App Secret. Normalizes private, group, and guild messages into session inputs.
- **WeChat iLink**: Authorizes via QR code flow (`soloqueue wechat login --id personal`). Uses long-polling for text messages and typing keepalive during runs.

Channel notifications for Cron runs are delivered on a best-effort basis; the Web UI remains the authoritative record.

---

## 6. Skills, MCP, and LSP Extensions

Extend built-in tools without modifying core runtime code:

- **Skills**: Kept under `~/.soloqueue/skills/<skill-name>/SKILL.md`. Define reusable workflows with instructions, scripts, and assets. Automatically hot-reloaded.
- **MCP Servers**: Configured in `~/.soloqueue/mcp.json` using standard `mcpServers` map format. Support `stdio` transport servers.
- **LSP Tools**: Language servers configured under `lspmcp` in `settings.yaml` provide code intelligence tools (code completion, symbol search, jump to definition).
