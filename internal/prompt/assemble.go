package prompt

import (
	"fmt"
	"strings"
)

// assembleWithXML 将各段 prompt 内容用 XML 标签组装为最终系统提示词。
// userCtx 为空时跳过 <user_context> 段。
func assembleWithXML(profile, userCtx, routingTable, rules string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<identity>\n%s\n</identity>", strings.TrimSpace(profile))

	if userCtx != "" {
		fmt.Fprintf(&b, "\n\n<user_context>\n%s\n</user_context>", strings.TrimSpace(userCtx))
	}

	fmt.Fprintf(&b, "\n\n<available_teams>\n%s\n</available_teams>", strings.TrimSpace(routingTable))

	fmt.Fprintf(&b, "\n\n<rules>\n%s\n</rules>", strings.TrimSpace(rules))

	return b.String()
}
