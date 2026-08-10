# Skill store and management

The current skill registry lives under internal/agenttools/skill. Skills are
loaded from the embedded catalog and the user's ~/.soloqueue/skills/ directory.

The Skills settings surface supports catalog inspection, local import, Git
installation where supported, editing, toggling, and invocation telemetry.
Built-in skills are read-only; user overrides are stored in the user's skill
directory.

Each skill must contain a valid SKILL.md. Keep descriptions narrow and
actionable so the model can select the skill without adding unnecessary prompt
overhead. Use soloqueue skills report to inspect invocation and description
quality.

For the user workflow and MCP relationship, see
[Skills, MCP, and LSP](guides/skills-and-mcp.md).
