package prompt

import (
	"fmt"
	"sort"
	"strings"
)

// buildRoutingTable dynamically builds routing table text from a LeaderInfo list.
// The primary Agent only needs to know what each team "can do", not the tool details.
// When group info is present, display partitioned by group (block format); without group, fall back to old format (inline).
func buildRoutingTable(leaders []LeaderInfo) string {
	if len(leaders) == 0 {
		return "No Team Leaders are currently available. You must handle all tasks yourself."
	}

	// Sort by Group to ensure stable output
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

	// Determine if any leader has group description info
	hasGroupInfo := false
	for _, l := range sorted {
		if l.Group != "" && l.GroupDescription != "" {
			hasGroupInfo = true
			break
		}
	}

	if hasGroupInfo {
		// Block format: display partitioned by group
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
		}
	} else {
		// Old format: inline (backward compatible)
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
