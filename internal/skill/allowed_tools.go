package skill

import "strings"

// ParseAllowedTools parses the allowed-tools configuration string
//
// Input format: a comma-separated list of tool patterns
//
//	"Bash(git:*),Read,Edit(src/**/*.ts),mcp__server__tool"
//
// Output: a slice of raw pattern strings; no further parsing is performed.
// The matching logic is executed in FilterTools.
func ParseAllowedTools(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}