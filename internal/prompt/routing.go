package prompt

import (
	"fmt"
	"sort"
	"strings"
)

// buildRoutingTable dynamically builds routing table text from a LeaderInfo list.
// The primary Agent only needs the team's display name, canonical target ID,
// and short capability description. Team/group bodies are intentionally not
// injected here; L2 receives its own team context separately.
// The groups argument remains for API compatibility with PromptConfig callers.
func buildRoutingTable(leaders []LeaderInfo, _ map[string]GroupFile) string {
	if len(leaders) == 0 {
		return "No matching teams are currently available. Handle all tasks yourself."
	}

	// Sort by Group and canonical target ID to ensure stable output.
	sorted := make([]LeaderInfo, len(leaders))
	copy(sorted, leaders)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Group != sorted[j].Group {
			return sorted[i].Group < sorted[j].Group
		}
		return routingTargetID(sorted[i]) < routingTargetID(sorted[j])
	})

	var b strings.Builder
	b.WriteString("Available Teams for matching-domain work or explicit Team requests. Use only these listed Team targets; if none matches, handle the work directly:\n")

	// Keep the group heading as a compact classification label, but include
	// only each leader's own description. Never copy the group's full markdown
	// body into the L1 prompt.
	var currentGroup string
	for _, l := range sorted {
		if l.Group != "" && l.Group != currentGroup {
			currentGroup = l.Group
			fmt.Fprintf(&b, "\n## %s\n", l.Group)
		}
		if l.Group != "" {
			fmt.Fprintf(&b, "- Team: %s (target=%s): %s → use delegate(target=\"%s\", task_name=\"<stable-task-name>\", task=\"...\", work_dir=\"...\")\n", l.Name, routingTargetID(l), l.Description, routingTargetID(l))
		} else {
			fmt.Fprintf(&b, "\n- Team: %s (target=%s): %s → use delegate(target=\"%s\", task_name=\"<stable-task-name>\", task=\"...\")", l.Name, routingTargetID(l), l.Description, routingTargetID(l))
		}
	}

	return b.String()
}

func routingTargetID(l LeaderInfo) string {
	if strings.TrimSpace(l.ID) != "" {
		return strings.ToLower(strings.TrimSpace(l.ID))
	}
	return strings.ToLower(strings.TrimSpace(l.Name))
}
