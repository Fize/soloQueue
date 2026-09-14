# Feature Guide

English | [简体中文](zh/features.md)

This document describes project workspaces, sessions, Teams and Agents, model routing,
scheduled tasks, messaging channels, Skills, MCP, and LSP integration.

---

## 1. Projects & Sessions

SoloQueue separates the global runtime work directory (`~/.soloqueue/`) from project execution scopes:

- **Projects**: Point to absolute repository paths. A selected project becomes the default working directory for that project's Agent tools.
- **Sessions**: Chat sessions stream agent reasoning and tool executions over WebSocket. Session state and message history persist across server restarts.

---

## 2. Teams & Agent Templates

Agent execution relies on agent templates and team definitions stored in the work directory:

- `agents/`: Markdown files with YAML frontmatter for identity, Team membership, leader status, model, MCP servers, channel bindings, and notification channel. The Markdown body is the Agent system prompt.
- `groups/`: Team definitions containing the Team name, shared Skill configuration, and a Markdown description.
- **Delegation**: A primary session can delegate bounded subtasks to Team Agents. A supervisor tracks their execution and returns results to the parent session.
- **Management**: The Web Console and REST API read and write the same definitions. The L1 prompt contains no built-in Team schema; when the user explicitly requests Team or Agent management, L1 is instructed to inspect existing `groups/*.md` and `agents/*.md` files and follow their current format.

---

## 3. Models & Task Routing

Requests are classified by work nature rather than an artificial difficulty ladder:

| Task Type | Work Nature |
| --- | --- |
| `general` | Conversations, text writing, translation, summarization |
| `engineering` | Code inspection, repository edits, debugging, unit testing, deployment |
| `research` | Web search, documentation lookups, current information retrieval |

Classification uses local fast-track rules first (detecting code blocks, tracebacks, paths, shell commands). Ambiguous prompts pass to a configured classifier model. Model routes map task types to `provider:model` pairs in `settings.yaml`.

---

## 4. Scheduled Tasks (Cron)

Cron tasks run recurring or one-off prompts in temporary L1 or L2 sessions built for each execution:

- Manage scheduled jobs via the **Scheduled tasks** interface.
- Jobs execute with specified agent templates and optional project path bounds.
- Execution history, outputs, and status are tracked in the database and UI history view.

---

## 5. Messaging Channels

Channel adapters normalize platform messages and submit them to their configured L1 or L2 session:

- **QQ Bot**: Connects via Tencent Bot Gateway using App ID and App Secret. Normalizes private, group, and guild messages into session inputs.
- **WeChat iLink**: Authorizes via QR code flow (`soloqueue wechat login --id personal`). Uses long-polling for text messages and typing keepalive during runs.

Cron channel notification depends on an active channel sender and successful platform delivery. Execution history remains available in the Web UI.

---

## 6. Skills, MCP, and LSP Extensions

SoloQueue loads Skills and connects MCP and LSP servers through the Agent tool layer:

- **Skills**: Global packages are installed under `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/`; grouped layouts such as `skills/@user/skill/SKILL.md` are supported through six directory levels, with scanning stopping at the first recognized entrypoint. Agents also load compatible project packages from `<project>/.claude/skills/` when they are created. SoloQueue discovers, executes, and displays installed packages; global `SKILL.md` definitions hot-reload when skill directories or recognized entrypoints change. It does not embed a catalog or modify skill files.
- **Skill lifecycle**: Use the standalone [ClawHub](https://github.com/openclaw/clawhub) CLI with `--workdir "$SOLOQUEUE_HOME" --dir skills`, where `SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"`. Use `@owner/slug` for `inspect` and `install`, `update @owner/slug` or `update --all` for updates, and the installed slug for `uninstall`. Search and inspect are read-only; installation, updates, and removal require explicit intent. Only the L1 agent performs these operations directly; L2/L3 agents use installed Skills and report missing Skill IDs to L1.
- **MCP Servers**: Configured in `~/.soloqueue/mcp.json` using standard `mcpServers` map format. Support `stdio` transport servers.
- **LSP Tools**: Language servers configured under `lspmcp` in `settings.yaml` provide code intelligence tools (code completion, symbol search, jump to definition).
