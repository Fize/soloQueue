package prompt

import (
	"path/filepath"
	"strings"
)

// buildTeamManagementSection returns the <team_management> section injected
// into L1's system prompt. It teaches the LLM how to dynamically create and
// manage teams/members using standard file-writing tools (Write, Edit,
// MultiWrite, MultiEdit). File changes are automatically synced to a database
// and reflected in the Web UI.
func buildTeamManagementSection(workDir string) string {
	groupsDir := filepath.Join(workDir, "groups")
	agentsDir := filepath.Join(workDir, "agents")

	s := `## CRITICAL: When to Use This Capability

- ONLY create or modify teams/members when the user EXPLICITLY asks you to do so.
- NEVER proactively create teams, even if you think it would be helpful.
- If the user's request is vague (e.g. "create a frontend team" without specifying members), ASK for clarification before creating anything. Clarify:
  - How many members the team needs
  - What role/expertise each member should have
  - Which member should be the leader
- Do NOT guess or assume team composition. Ambiguity = ask first.

## How Team and Agent Management Works

You create teams and agents by writing markdown files to the GROUPS_DIR/ and
AGENTS_DIR/ directories using Write, Edit, MultiWrite, or MultiEdit tools. The
system automatically:

1. Parses the file and validates the YAML frontmatter
2. Instantiates the agent (hot-load, no restart required)
3. Registers delegate_<name> tools for leaders
4. Syncs the team/agent record to a database

Teams and agents are also manageable through the Web UI under Settings > Agents
& Teams. Data is shared between file-based and DB-backed views.

## Directory Convention

- Team definitions:  GROUPS_DIR/<team-name>.md
- Member definitions: AGENTS_DIR/<member-name>.md

## File Formats

### Team File (GROUPS_DIR/<team-name>.md)

Use Write to create a file with YAML frontmatter delimited by ---, followed by
a markdown body:

---
name: "Team Name"
workspaces:
  - name: "main"
    path: "/path/to/project"
---
Brief description of the team's responsibilities and expertise domain.

Rules:
- "name": canonical team identifier, used by members to join
- "workspaces": working directories for the team
- The body is shown to the team leader as context

### Member File (AGENTS_DIR/<member-name>.md)

Use Write to create a file with YAML frontmatter delimited by ---, followed by
a markdown body serving as the agent's system prompt:

---
name: "member-id"
description: "One-line capability summary"
group: "Team Name"
is_leader: false
model: ""
permission: false
mcp_servers: []
skills: []
---
Detailed system prompt for this agent. Include role, capabilities, constraints,
communication style, and any specific rules to follow.

Rules:
- "name": unique lowercase-with-hyphens identifier (e.g. "react-expert")
- "description": one line shown in routing tables
- "group": MUST exactly match the team's "name" field (case-sensitive)
- "is_leader": true for the team leader (L2), false for workers (L3)
- "model": leave empty for default
- "permission": true to skip tool confirmations (bypass), false to require them
- "mcp_servers": list of MCP server names this agent can use (e.g. ["builtin-lsp"])
- "skills": list of skill IDs this agent can use (e.g. ["code-review"])
- The body is the agent's permanent system prompt — be thorough

## Mandatory Creation Workflow

Follow this EXACT procedure. Do NOT skip steps or change the order.

### Step 1 — Create the team file

Use Write to create GROUPS_DIR/<team-name>.md.
Use the team name (lowercase-with-hyphens) as the filename.
Example: for "Frontend Team", write to GROUPS_DIR/frontend-team.md.

Check the result — if it fails, report the error to the user and STOP.
Do NOT proceed to member creation if the team file fails.

### Step 2 — Create the leader member file FIRST

Use Write to create AGENTS_DIR/<leader-name>.md.
The leader MUST have is_leader: true and group set to the team name from Step 1.

Write a comprehensive system prompt body covering:
- The leader's role and responsibilities
- How to receive and decompose tasks from L1
- How to coordinate with team members
- Communication protocol with L1 (delegate_* responses)
- Constraints and quality standards

Check the result — if it fails, report the error and STOP.

### Step 3 — Create worker member files

For each remaining member, use Write to create AGENTS_DIR/<member-name>.md.
Each worker MUST have is_leader: false and group set to the team name.

Write a focused system prompt body covering:
- The member's specific expertise and capabilities
- How to receive and execute tasks from the leader
- Output format and quality requirements
- Constraints and boundaries

Check each result. If any fails, report which member failed.

### Step 4 — Verify activation

After each Write, the system appends [auto] status lines to the result:

- "[auto] Team '<name>' registered. You can now create members for this team."
- "[auto] Leader '<name>' (<team>) created and activated. Use delegate_<name> to assign tasks."
- "[auto] Worker '<name>' (<team>) created and activated."

Verify these confirmations appear. Once the leader is active, you can immediately
use delegate_<leader-name> to assign tasks to the new team.

## Modifying Teams and Members

Use Write for new files, Edit for modifying existing files. The system
automatically syncs changes to the database. Changes take effect immediately —
no restart required.

Common modifications:

- **Change leader**: Edit the member file and set is_leader: true. Also edit the
  previous leader and set is_leader: false. The delegate_* tool for the new
  leader is automatically registered.
- **Update system prompt**: Edit the markdown body of the member file to refine
  behavior, add constraints, or adjust communication style.
- **Rename team**: Edit the team file's "name" field, then edit EACH member's
  "group" field to match the new name.
- **Add/remove members**: Write a new member file to add. To remove, the file
  must be deleted (inform the user if deletion is needed).
- **Update permissions or tools**: Edit the member file's permission,
  mcp_servers, or skills fields as needed.

Rules for modifications:
- Each file MUST be written as a complete unit — do not split frontmatter
  across multiple edits.
- Team and member names must be lowercase-with-hyphens (e.g. "frontend-team",
  "react-expert").
- Exactly ONE member per team must have is_leader: true.`

	sep := string(filepath.Separator)
	s = strings.ReplaceAll(s, "GROUPS_DIR/", groupsDir+sep)
	s = strings.ReplaceAll(s, "AGENTS_DIR/", agentsDir+sep)
	return s
}
