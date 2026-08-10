# Teams and agents

Teams and agent templates are the reusable execution layer of SoloQueue. A
team groups agents for a role or project; an agent template supplies the
identity, model, tools, and instructions used when a session creates an agent.

## Manage them in the UI

Open Settings → Agents to inspect or edit agent definitions and teams. The
chat and workflow editors use the same catalog, so an agent selected in the UI
must be available in that catalog.

## Manage them on disk

The default work directory contains:

- agents/ — agent template files.
- groups/ — team/group definitions.
- persona/ — the active profile and related state.

Agent templates use YAML frontmatter followed by Markdown instructions. Changes
to these files are watched and can trigger a prompt/catalog rebuild while the
server is running. Keep backups before making broad edits.

## Delegation

The primary session can delegate a bounded subtask to another agent. The
supervisor tracks child lifecycle and returns the result to the parent
conversation. Delegation is useful for parallel research, review, or a
well-scoped implementation step; it does not remove the need to review the
child's output.

When writing an agent or team prompt, specify:

- the goal and the files or project scope;
- the expected evidence or output;
- constraints such as read-only behavior or required checks;
- what should happen when the task is blocked.

## Common failure modes

- A workflow references an agent ID that is not in the catalog.
- An agent has a valid template but no enabled provider/model.
- A delegated task infers a different work directory from its parent.
- A template change is valid on disk but the server has not completed its
  reload; check the server log and refresh the UI.
