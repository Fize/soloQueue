package prompt

import (
	"fmt"
	"strings"
	"time"
)

// assembleWithXML 将各段 prompt 内容用 XML 标签组装为最终系统提示词。
// userCtx 为空时跳过 <user_context> 段。
// recentMemory 为空时跳过 <recent_memory> 段。
func assembleWithXML(profile, userCtx, recentMemory, routingTable, teamMgmt, rules string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<identity>\n%s\n</identity>", strings.TrimSpace(profile))

	if userCtx != "" {
		fmt.Fprintf(&b, "\n\n<user_context>\n%s\n</user_context>", strings.TrimSpace(userCtx))
	}

	if recentMemory != "" {
		now := time.Now().Format("2006-01-02 15")
		fmt.Fprintf(&b, "\n\n<recent_memory>\nCurrent time: %s (for finer precision, use available time tools).\n\nShort-term memory of recent conversations is stored as daily markdown files. Use the Read tool to check these files when the user references past work, asks about previous sessions, or when you need context about what was done before.\n\nLocation: %s\n\nFile format: YYYY-MM-DD.md, one file per day. Only the last 3 days of files are retained (older files are auto-cleaned).\n\nEntry format: each entry begins with a level-2 markdown header containing the full datetime:\n\n  ## YYYY-MM-DD HH:MM\n  - bullet-point summary of what happened\n\nEntries are stored in the file matching the entry's date. Content older than 3 days is stored in today's file but the timestamp in the header remains accurate — it reflects when the entry was originally recorded, not when the file was written.\n\nTo find past context: use the Read tool to read specific date file(s), or use the Grep tool to search across memory files by keyword or pattern. The full datetime headers let you locate entries within a file by time.\n</recent_memory>", now, strings.TrimSpace(recentMemory))
	}

	fmt.Fprintf(&b, "\n\n<available_teams>\n%s\n</available_teams>", strings.TrimSpace(routingTable))

	fmt.Fprintf(&b, "\n\n<team_management>\n%s\n</team_management>", strings.TrimSpace(teamMgmt))

	fmt.Fprintf(&b, "\n\n<rules>\n%s\n</rules>", strings.TrimSpace(rules))

	return b.String()
}
