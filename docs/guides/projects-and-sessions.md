# Projects and sessions

> 中文：[项目与会话](../zh/guides/projects-and-sessions.md)

I keep the agent runtime in one local work directory, while I point each project
at a repository or other absolute filesystem path. I use this separation to
reuse the same teams and models across projects.

## Projects

1. Open Settings → Projects.
2. Add a project with a name and an absolute path.
3. Select that project when starting a chat or creating a workflow run.

I treat the project path as execution scope, not just a label. I review it
before allowing a write, shell, network, or delegated action.

## Chat sessions

- **Chat** starts a normal project-aware conversation.
- **Assistant** exposes the long-running primary assistant session.
- The session tree lets me return to existing conversations.
- I can see the resolved task type and model for the active request in a session.

I rely on the server to serialize work per session and stream progress to my
desktop client over WebSocket. After restarting the server, I retain durable
history and local metadata, but I must establish in-memory authentication and
active connections again.

## Local workdir and project workdir

I use ~/.soloqueue/ as my default runtime work directory and keep settings,
agent/team files, SQLite, logs, memory, plans, and generated artifacts there. I
keep a project path separate and normally choose an existing directory that I
intend the agent to inspect or modify.

For delegated or workflow work, I use an explicit project path. I do not rely
on a model-generated path or a path under ~/.soloqueue/ when I mean a repository.

## Good session practice

- I start with a read-only request to verify the selected project.
- I state the goal and acceptance criteria for a change.
- I keep unrelated repositories in separate projects.
- I inspect the final diff and any generated artifacts myself.
