package prompt

import (
	"fmt"
	"sort"
	"strings"
)

// buildRoutingTable 从 LeaderInfo 列表动态构建路由表文本。
// 主 Agent 只需知道每个团队"能做什么"，不需要知道工具细节。
// 有 group 信息时按 group 分区展示（区块式），无 group 时退化为旧格式（一行式）。
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

	// 判断是否有任何 leader 有 group 描述信息
	hasGroupInfo := false
	for _, l := range sorted {
		if l.Group != "" && l.GroupDescription != "" {
			hasGroupInfo = true
			break
		}
	}

	if hasGroupInfo {
		// 区块式：按 group 分区展示
		var currentGroup string
		for _, l := range sorted {
			if l.Group != currentGroup {
				currentGroup = l.Group
				if l.Group != "" && l.GroupDescription != "" {
					fmt.Fprintf(&b, "\n## %s — %s\n", l.Group, l.GroupDescription)
				} else if l.Group != "" {
					fmt.Fprintf(&b, "\n## %s\n", l.Group)
				}
			}

			toolName := "delegate_" + l.Name
			fmt.Fprintf(&b, "- Leader: %s → call %s(task=\"...\")\n", l.Name, toolName)

			if l.MatchedWorkspace != nil {
				fmt.Fprintf(&b, "- Workspace: %s\n", l.MatchedWorkspace.Path)
			}
		}
	} else {
		// 旧格式：一行式（向后兼容）
		for _, l := range sorted {
			toolName := "delegate_" + l.Name
			if l.Group != "" {
				fmt.Fprintf(&b, "\n- %s (%s): %s → call %s(task=\"...\")", l.Name, l.Group, l.Description, toolName)
			} else {
				fmt.Fprintf(&b, "\n- %s: %s → call %s(task=\"...\")", l.Name, l.Description, toolName)
			}
		}
	}

	return b.String()
}
