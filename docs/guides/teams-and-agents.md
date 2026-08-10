# Teams and agents

> 中文：[团队与 Agent](../zh/guides/teams-and-agents.md)

I use teams and agent templates as SoloQueue's reusable execution layer. I use
a team to group agents for a role or project, and an agent template to supply
the identity, model, tools, and instructions used when I create an agent.

## Manage them in the UI

I open Settings → Agents to inspect or edit agent definitions and teams. The
chat and workflow editors use the same catalog, so an agent I select in the UI
must be available in that catalog.

## Manage them on disk

I keep these directories in my default work directory:

- agents/ — agent template files.
- groups/ — team/group definitions.
- persona/ — the active profile and related state.

I define agent templates with YAML frontmatter followed by Markdown
instructions. SoloQueue watches these files and can trigger a prompt/catalog
rebuild while the server is running. I keep backups before making broad edits.

## Delegation

I can delegate a bounded subtask from my primary session to another agent. I
use the supervisor to track child lifecycle and return the result to my parent
conversation. I use delegation for parallel research, review, or a well-scoped
implementation step; I still review the child's output.

When I write an agent or team prompt, I specify:

- the goal and the files or project scope;
- the expected evidence or output;
- constraints such as read-only behavior or required checks;
- what should happen when the task is blocked.

## Common failure modes

- I may see a workflow reference an agent ID that is not in the catalog.
- I may see an agent with a valid template but no enabled provider/model.
- I may see a delegated task infer a different work directory from its parent.
- I may see a valid template change before the server completes its reload; I
  check the server log and refresh the UI.
