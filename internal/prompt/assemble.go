package prompt

import (
	"fmt"
	"strings"
)

// assembleWithXML 将各段 prompt 内容用 XML 标签组装为最终系统提示词。
// userCtx 为空时跳过 <user_context> 段。
// recentMemory 为短期记忆目录路径（非空时注入文件位置 + Read/Grep 工具使用说明，不注入实际内容）。
// permanentMemory 非空时注入长时记忆的 RecallMemory/Remember 工具使用说明（不注入实际内容）。
func assembleWithXML(profile, userCtx, recentMemory, permanentMemory, routingTable, teamMgmt, rules, planDir, workDir, exploreDir string, mcpServers []string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<identity>\n%s\n</identity>", strings.TrimSpace(profile))

	fmt.Fprintf(&b, "\n\n<working_directory>\nYour global working directory is `%s`. All soloQueue configuration, agent definitions, plans, memory, and data files reside under this directory. When writing or reading files within soloQueue's own directories, use `%s` paths.\n</working_directory>", workDir, workDir)

	fmt.Fprintf(&b, "\n\n%s", EnvSection(workDir, exploreDir, true, true))

	if userCtx != "" {
		fmt.Fprintf(&b, "\n\n<user_context>\n%s\n</user_context>", strings.TrimSpace(userCtx))
	}

	if recentMemory != "" {
		fmt.Fprintf(&b, "\n\n<recent_memory>\nShort-term memory of recent conversations is stored as daily markdown files. Use the Read tool to check these files when the user references past work, asks about previous sessions, or when you need context about what was done before.\n\nLocation: %s\n\nFile format: YYYY-MM-DD.md, one file per day. Only the last 7 days of files are retained (older files are auto-migrated to permanent memory).\n\nEntry format: each entry begins with a level-2 markdown header containing the full datetime:\n\n  ## YYYY-MM-DD HH:MM\n  - bullet-point summary of what happened\n\nEntries are stored in the file matching the entry's date. Content older than 7 days is stored in today's file but the timestamp in the header remains accurate — it reflects when the entry was originally recorded, not when the file was written.\n\nTo find past context: use the Read tool to read specific date file(s), or use the Grep tool to search across memory files by keyword or pattern. The full datetime headers let you locate entries within a file by time.\n</recent_memory>", recentMemory)
	}

	if permanentMemory != "" {
		fmt.Fprintf(&b, "\n\n<permanent_memory>\nLong-term memory stores condensed summaries from conversations older than 7 days, auto-migrated from short-term memory files. Use the RecallMemory tool to search these entries by keyword or topic when:\n- The user refers to past conversations or previous sessions\n- You need historical context about past decisions, preferences, or project history\n- The user asks about something you discussed before but can't recall\n\nYou can save new information to permanent memory using the Remember tool.\n</permanent_memory>")
	}

	fmt.Fprintf(&b, "\n\n<delegation_requirement>\n===============================================================================\n🔴 CRITICAL DIRECTIVE: YOU ARE A TASK ROUTER, NOT AN EXECUTOR.\nYOUR PRIMARY AND ONLY DEFAULT ACTION FOR ANY USER TASK IS TO DELEGATE.\n===============================================================================\n- You MUST use delegate_* tools for ALL tasks that fall within any team's domain.\n- Using built-in tools (Read, Bash, Write, Edit, Grep, Glob, WebFetch, WebSearch) when a matching team exists is a STRICT PROTOCOL VIOLATION.\n- Self-execution is ONLY permitted if NO registered team matches the task's domain.\n- When delegating, you MUST include the `work_dir` parameter set to the appropriate workspace path from the team's workspace list. The delegated agent will work in this directory and load project-specific configuration (AGENTS.md, CLAUDE.md, .claude/) from it. Omitting `work_dir` will cause the delegation to fail.\n\n👉 SELECTIVE CONTEXT SYNTHESIS FOR MULTI-TURN DELEGATION:\nL2 agents start with an empty history and only see the `task` string.\nWhen delegating in a multi-turn conversation, you MUST NOT pass the raw user query. You MUST synthesize a self-contained task description that includes:\n1. The overall goal and latest request.\n2. Only directly relevant and useful context from previous turns (such as specific file paths, specific error logs, or key prior findings discussed). Do NOT dump all history or irrelevant details.\n\nExample: delegate_dev(task=\"Fix CSS on login page. Context: user reported layout shift in main.css and we saw line 45 has bad flex properties.\", work_dir=\"/path/to/project\")\n===============================================================================\n</delegation_requirement>")

	fmt.Fprintf(&b, "\n\n<available_teams>\n%s\n</available_teams>", strings.TrimSpace(routingTable))

	fmt.Fprintf(&b, "\n\n<team_management>\n%s\n</team_management>", strings.TrimSpace(teamMgmt))

	fmt.Fprintf(&b, "\n\n<rules>\n%s\n%s\n</rules>", strings.TrimSpace(rules), HardcodedL1Rules)

	if len(mcpServers) > 0 {
		b.WriteString("\n\n<mcp_servers>\n")
		for _, name := range mcpServers {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		b.WriteString("</mcp_servers>")
	}

	// Plan Before Action section
	if planDir != "" {
		fmt.Fprintf(&b, "\n\n<plan_before_action>\nYou review and approve plans from delegated teams (L2). You do NOT create plans yourself unless no team is available.\n\n**Exploratory tasks are EXEMPT.** Reading files, searching code, investigating issues, or answering questions do NOT require a plan.\n\n## Plan Directory\nDesign documents are saved to: %s/<feature-name>.md\n\n## Design Document Structure\nEvery design document MUST contain:\n- **Goal**: What the task aims to achieve\n- **Approach**: How you plan to implement it\n- **Impact**: What files/modules will be affected\n- **Steps**: Ordered list of implementation steps\n\n## Reviewing L2 Plans\nWhen a delegated team (L2) presents a plan with PLAN_ID:\n- If straightforward → reply \"PLAN_ID: <id> approved\" so they can proceed.\n- If the decision has significant trade-offs or risks → present the options to the user.\n\n## Self-execution (no team available)\nOnly create your own plan when no team matches the task. Follow the plan → running → done lifecycle.\n</plan_before_action>", planDir)
	}

	// Exploration Artifacts section
	fmt.Fprintf(&b, "\n\n<exploration_artifacts>\nWhen you perform exploration tasks (reading files, searching code, investigating issues), you SHOULD save a markdown artifact to %s if the exploration is complex or the findings are worth sharing with other agents.\n\n## When to Save\n- Complex investigations with many files or nuanced conclusions\n- Investigations whose results may be reused by other agents in the same session\n- Simple one-off lookups can skip saving\n\n## Document Naming\nFormat: %s/<task-slug>_<agent-id>.md\nExamples:\n- %s/explore_auth_flow_L1.md\n- %s/investigate_race_condition_dev-leader.md\n\n## Document Content\n- Agent: your id/name/layer\n- Created at: use current time when saving\n- Updated at: use current time when updating\n- Freshness window: same-day\n- Task: the original or summarized task description\n- Key Findings, Files Inspected, Reusable Context, Open Questions\n\n## Reuse Rules\n1. Before starting a new exploration, check %s for an existing artifact with the same task-slug and agent-id.\n2. If an artifact exists and was created today, read it first and reuse its findings when appropriate.\n3. If you create or reuse an artifact, include its path in your response so other agents can access it.\n</exploration_artifacts>", exploreDir, exploreDir, exploreDir, exploreDir, exploreDir)

	return b.String()
}


