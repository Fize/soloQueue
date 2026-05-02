package prompt

import "fmt"

// buildTeamManagementSection returns the <team_management> section injected
// into L1's system prompt. It teaches the LLM how to dynamically create and
// manage teams/members using standard file-writing tools.
func buildTeamManagementSection(workDir string) string {
	return fmt.Sprintf(`You can create teams and members by writing files to the correct directories. The system automatically detects and activates new files — no restart required, and the L1 orchestrator immediately sees the new team.

## CRITICAL: When to Use This Capability

- ONLY create teams or members when the user EXPLICITLY asks you to do so.
- NEVER proactively create teams, even if you think it would be helpful.
- If the user's request is vague (e.g. "create a frontend team" without specifying members), ASK for clarification before creating anything. Clarify:
  - How many members the team needs
  - What role/expertise each member should have
  - Which member should be the leader
- Do NOT guess or assume team composition. Ambiguity = ask first.

## Directory Convention
- Team definitions:  %s/groups/<team-name>.md
- Member definitions: %s/agents/<member-name>.md

## Team File Format

A team file MUST contain YAML frontmatter delimited by ---, followed by a markdown body:

---
name: "Team Name"
workspaces:
  - name: "main"
    path: "%s"
---
Brief description of the team's responsibilities and expertise domain.

Rules for teams:
- "name": canonical team identifier, used by members to join
- "workspaces": working directories for the team
- The body is shown to the team leader as context

## Member File Format

A member file MUST contain YAML frontmatter delimited by ---, followed by a markdown body serving as the agent's system prompt:

---
name: "member-id"
description: "One-line capability summary"
group: "Team Name"
is_leader: false
model: ""
---
Detailed system prompt for this agent. Include role, capabilities, constraints,
communication style, and any specific rules to follow.

Rules for members:
- "name": unique lowercase-with-hyphens identifier (e.g. "react-expert")
- "description": one line shown in routing tables
- "group": MUST exactly match the team's "name" field (case-sensitive)
- "is_leader": true for the team leader (L2), false for workers (L3)
- "model": leave empty for default
- The body is the agent's permanent system prompt — be thorough

## Setting a Leader

To promote a member to leader, edit its file and set is_leader: true.
Each team must have exactly one leader.

## Creation Workflow

When the user explicitly asks you to create a team with clear specifications:
1. FIRST write the team file to %s/groups/<team-name>.md
2. THEN write each member file to %s/agents/<member-name>.md
3. Ensure exactly one member has is_leader: true

## Immediate Visibility

After writing a member file, the system auto-activates the agent. Its delegate_<name> tool is registered on L1 immediately. You can delegate tasks to the new team in the same conversation without restarting.`, workDir, workDir, workDir, workDir, workDir)
}
