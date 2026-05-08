package prompt

import (
	"fmt"
	"strings"
	"time"
)

// assembleWithXML 将各段 prompt 内容用 XML 标签组装为最终系统提示词。
// userCtx 为空时跳过 <user_context> 段。
// recentMemory 为短期记忆目录路径（非空时注入文件位置 + Read/Grep 工具使用说明，不注入实际内容）。
// permanentMemory 非空时注入长时记忆的 RecallMemory/Remember 工具使用说明（不注入实际内容）。
func assembleWithXML(profile, userCtx, recentMemory, permanentMemory, routingTable, teamMgmt, rules, planDir string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<identity>\n%s\n</identity>", strings.TrimSpace(profile))

	if userCtx != "" {
		fmt.Fprintf(&b, "\n\n<user_context>\n%s\n</user_context>", strings.TrimSpace(userCtx))
	}

	if recentMemory != "" {
		now := time.Now().Format("2006-01-02 15")
		fmt.Fprintf(&b, "\n\n<recent_memory>\nCurrent time: %s (for finer precision, use available time tools).\n\nShort-term memory of recent conversations is stored as daily markdown files. Use the Read tool to check these files when the user references past work, asks about previous sessions, or when you need context about what was done before.\n\nLocation: %s\n\nFile format: YYYY-MM-DD.md, one file per day. Only the last 7 days of files are retained (older files are auto-migrated to permanent memory).\n\nEntry format: each entry begins with a level-2 markdown header containing the full datetime:\n\n  ## YYYY-MM-DD HH:MM\n  - bullet-point summary of what happened\n\nEntries are stored in the file matching the entry's date. Content older than 7 days is stored in today's file but the timestamp in the header remains accurate — it reflects when the entry was originally recorded, not when the file was written.\n\nTo find past context: use the Read tool to read specific date file(s), or use the Grep tool to search across memory files by keyword or pattern. The full datetime headers let you locate entries within a file by time.\n</recent_memory>", now, strings.TrimSpace(recentMemory))
	}

	if permanentMemory != "" {
		fmt.Fprintf(&b, "\n\n<permanent_memory>\nLong-term memory stores condensed summaries from conversations older than 7 days, auto-migrated from short-term memory files. Use the RecallMemory tool to search these entries by keyword or topic when:\n- The user refers to past conversations or previous sessions\n- You need historical context about past decisions, preferences, or project history\n- The user asks about something you discussed before but can't recall\n\nYou can save new information to permanent memory using the Remember tool.\n</permanent_memory>")
	}

	fmt.Fprintf(&b, "\n\n<available_teams>\n%s\n</available_teams>", strings.TrimSpace(routingTable))

	fmt.Fprintf(&b, "\n\n<team_management>\n%s\n</team_management>", strings.TrimSpace(teamMgmt))

	fmt.Fprintf(&b, "\n\n<rules>\n%s\n</rules>", strings.TrimSpace(rules))

	// Plan Before Action section
	if planDir != "" {
		fmt.Fprintf(&b, "\n\n<plan_before_action>\nYou MUST follow the \"Plan Before Action\" rule for any task that involves file modifications, code changes, or system alterations.\n\n**Exploratory tasks are EXEMPT.** Reading files, searching code, investigating issues, or answering questions do NOT require a plan.\n\n## Plan Directory\nAll design documents MUST be saved to: %s/<feature-name>.md\n\n## Design Document Structure\nEvery design document MUST contain:\n- **Goal**: What the task aims to achieve\n- **Approach**: How you plan to implement it\n- **Impact**: What files/modules will be affected\n- **Steps**: Ordered list of implementation steps\n\n## Decision Rule\nWhen a delegated team (L2) presents a plan:\n- If straightforward → explicitly reply \"approved\" so they can proceed.\n- If the decision has significant trade-offs or risks → present the options to the user.\n\n## Procedure\n1. When you receive a task that involves changes, ensure a design document is written to the plan directory first.\n2. Review the plan from L2. If straightforward, reply \"approved\". If uncertain, escalate to the user.\n3. Only after approval, instruct L2 to proceed with execution.\n</plan_before_action>", planDir)
	}

	return b.String()
}
