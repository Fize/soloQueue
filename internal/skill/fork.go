package skill

import (
	"context"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Fork Mode ─────────────────────────────────────────────────────────────

// SkillForkSpawnFn creates a temporary child agent for executing a skill in fork mode.
//
// The concrete agent creation logic is injected by the Factory to avoid circular dependencies between the skill and agent packages.
// Parameters:
//   - ctx: context
//   - s: the skill being called
//   - content: pre-processed skill instructions (as the child agent's system prompt)
//   - args: user-provided arguments (as the child agent's user message)
//
// Returns the child agent's Locatable interface and a cleanup function.
type SkillForkSpawnFn func(ctx context.Context, s *Skill, content, args string) (iface.Locatable, func(), error)

// ExecuteFork executes a skill in fork mode within an isolated child agent.
//
// Called by SkillTool. The spawnFn is injected by the Factory.
// Execution flow:
//  1. Call spawnFn to create a child agent
//  2. Send an AskStream request
//  3. Accumulate content events
//  4. Clean up the child agent
func ExecuteFork(ctx context.Context, s *Skill, content, args string, spawnFn SkillForkSpawnFn) (string, error) {
	child, cleanup, err := spawnFn(ctx, s, content, args)
	if err != nil {
		return "", err
	}
	defer cleanup()

	// Provide a default prompt if args is empty
	prompt := args
	if prompt == "" {
		prompt = "Execute the skill instructions above."
	}

	resultCh, err := child.AskStream(ctx, prompt)
	if err != nil {
		return "", err
	}

	var result strings.Builder
	for event := range resultCh {
		if delta, ok := event.(interface{ ContentDelta() (string, bool) }); ok {
			if d, ok2 := delta.ContentDelta(); ok2 {
				result.WriteString(d)
			}
		}
	}

	return result.String(), nil
}

// ─── allowed-tools filtering ────────────────────────────────────────────────────

// FilterTools filters tools based on the allowed-tools whitelist.
//
// Supported patterns:
//   - "Bash" — matches the Bash tool
//   - "Bash(git:*)" — matches the Bash tool (command prefix constraints are not enforced for now, only the tool name is matched)
//   - "Edit(src/**/*.ts)" — matches the Edit tool (path constraints are not enforced for now, only the tool name is matched)
//   - "mcp__server" — matches all tools for that MCP server
//   - "mcp__server__tool" — matches a specific MCP tool
func FilterTools(allTools []tools.Tool, allowed []string) []tools.Tool {
	var filtered []tools.Tool
	for _, t := range allTools {
		if ToolMatchesAllowed(t.Name(), allowed) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ToolMatchesAllowed checks if toolName matches any pattern in the allowed list.
func ToolMatchesAllowed(toolName string, allowed []string) bool {
	for _, pattern := range allowed {
		// Extract the tool name part (before the parenthesis)
		baseName := pattern
		if idx := strings.Index(pattern, "("); idx > 0 {
			baseName = pattern[:idx]
		}

		// Exact match
		if toolName == baseName {
			return true
		}

		// MCP prefix match: mcp__server matches mcp__server__tool
		if strings.HasPrefix(baseName, "mcp__") && strings.HasPrefix(toolName, baseName+"__") {
			return true
		}

		// MCP exact match
		if toolName == pattern {
			return true
		}
	}
	return false
}