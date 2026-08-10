# Projects and sessions

SoloQueue keeps the agent runtime in one local work directory, while each
project points the agent at a repository or other absolute filesystem path.
This separation lets you reuse the same teams and models across projects.

## Projects

1. Open Settings → Projects.
2. Add a project with a name and an absolute path.
3. Select that project when starting a chat or creating a workflow run.

The project path is execution scope, not just a label. Review it before
allowing a write, shell, network, or delegated action.

## Chat sessions

- **Chat** starts a normal project-aware conversation.
- **Assistant** exposes the long-running primary assistant session.
- The session tree lets you return to existing conversations.
- A session can show the resolved task type and model for the active request.

The server serializes work per session and streams progress to the desktop
client over WebSocket. Restarting the server preserves durable history and
local metadata, but in-memory authentication and active connections must be
established again.

## Local workdir and project workdir

The runtime work directory defaults to ~/.soloqueue/ and contains settings,
agent/team files, SQLite, logs, memory, plans, and generated artifacts. A
project path is separate and should normally be an existing directory that the
agent is meant to inspect or modify.

For delegated or workflow work, use an explicit project path. Do not rely on a
model-generated path or a path under ~/.soloqueue/ when you mean a repository.

## Good session practice

- Start with a read-only request to verify the selected project.
- State the goal and acceptance criteria for a change.
- Keep unrelated repositories in separate projects.
- Inspect the final diff and any generated artifacts yourself.
