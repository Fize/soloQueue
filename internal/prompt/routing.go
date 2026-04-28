package prompt

import (
	"fmt"
	"sort"
	"strings"
)

// buildRoutingTable 从 LeaderInfo 列表动态构建路由表文本。
// 主 Agent 只需知道每个团队"能做什么"，不需要知道工具细节。
func buildRoutingTable(leaders []LeaderInfo) string {
	if len(leaders) == 0 {
		return "No Team Leaders are currently available. You must handle all tasks yourself."
	}

	// 按 Group 排序保证输出稳定
	sorted := make([]LeaderInfo, len(leaders))
	copy(sorted, leaders)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Group != sorted[j].Group {
			return sorted[i].Group < sorted[j].Group
		}
		return sorted[i].Name < sorted[j].Name
	})

	var b strings.Builder
	b.WriteString("Team Leaders you can delegate tasks to (use the corresponding delegate tool):\n")

	for _, l := range sorted {
		toolName := "delegate_" + l.Name
		if l.Group != "" {
			fmt.Fprintf(&b, "\n- %s (%s): %s → call %s(task=\"...\")", l.Name, l.Group, l.Description, toolName)
		} else {
			fmt.Fprintf(&b, "\n- %s: %s → call %s(task=\"...\")", l.Name, l.Description, toolName)
		}
	}

	return b.String()
}
