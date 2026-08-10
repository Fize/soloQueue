# Skill store and management

> 中文：[Skill Store 与管理](zh/skill_store.md)

I keep the current skill registry under internal/agenttools/skill. I load
skills from the embedded catalog and my ~/.soloqueue/skills/ directory.

I use the Skills settings surface for catalog inspection, local import, Git
installation where supported, editing, toggling, and invocation telemetry.
I treat built-in skills as read-only and store local overrides in my skill
directory.

I require each skill to contain a valid SKILL.md. I keep descriptions narrow
and actionable so the model can select the skill without adding unnecessary
prompt overhead. I use soloqueue skills report to inspect invocation and
description quality.

For the user workflow and MCP relationship, see
[Skills, MCP, and LSP](guides/skills-and-mcp.md).
