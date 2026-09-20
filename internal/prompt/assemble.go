package prompt

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var defaultRulesComment = regexp.MustCompile(`(?s)<!--\s*(Add your custom rules here\.\s*System rules are built-in automatically and do not need to be copied here\.|在此添加自定义规则。\s*系统规则会自动内置，无需复制到此处。)\s*-->`)

func cleanUserRules(rules string) string {
	return strings.TrimSpace(defaultRulesComment.ReplaceAllString(rules, ""))
}

// assembleWithXML assembles various prompt content sections into a final system prompt using XML tags.
// If userCtx is empty, the <user_context> section is skipped.
// recentMemory is the path to the short-term memory directory (if not empty, injects file location + Read/Grep tool instructions, but not actual content).
// If permanentMemory is not empty, injects instructions for RecallMemory/Remember long-term memory tools (but not actual content).
func assembleWithXML(profile, userCtx, recentMemory, permanentMemory, routingTable, rules, planDir, workDir, exploreDir string, mcpServers []string, userRules map[string]string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "<identity>\n%s\n</identity>", escapePromptData(strings.TrimSpace(profile)))

	fmt.Fprintf(&b, "\n\n<working_directory>\nRelative file paths (e.g., `report.md`, `explore/findings.md`) use the current workspace as their base. Use relative paths for files within your workspace.\n</working_directory>")

	// Keep the stable execution contract before dynamic memories, user rules and
	// target catalogs so providers can reuse the largest stable cache prefix.

	fmt.Fprintf(&b, "\n\n<rules>\n%s\n%s\n%s\n%s\n</rules>", strings.ReplaceAll(SharedAgentRules, "{{EXPLORE_DIR}}", exploreDir), escapePromptData(strings.TrimSpace(DefaultRules)), escapePromptData(cleanUserRules(rules)), HardcodedAssistantRules)

	fmt.Fprintf(&b, "\n\n%s", EnvSection(workDir, exploreDir, true, true))

	if userCtx != "" {
		fmt.Fprintf(&b, "\n\n<user_context>\n%s\n</user_context>", escapePromptData(strings.TrimSpace(userCtx)))
	}

	if len(userRules) > 0 {
		b.WriteString("\n\n<user_rules>")
		// Sort keys for deterministic output
		keys := make([]string, 0, len(userRules))
		for k := range userRules {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			fmt.Fprintf(&b, "\n## %s\n%s\n", escapePromptData(name), escapePromptData(strings.TrimSpace(userRules[name])))
		}
		b.WriteString("</user_rules>")
	}

	if recentMemory != "" {
		fmt.Fprintf(&b, "\n\n<recent_memory>\nRecent conversation context is available at %s. Read or grep it only when relevant.\n</recent_memory>", recentMemory)
	}

	if permanentMemory != "" {
		b.WriteString("\n\n<permanent_memory>\nDurable memory tools are available; use them only when prior context materially helps.\n</permanent_memory>")
	}

	fmt.Fprintf(&b, "\n\n<available_teams>\n%s\n</available_teams>", escapePromptData(strings.TrimSpace(routingTable)))

	if len(mcpServers) > 0 {
		b.WriteString("\n\n<mcp_servers>\n")
		for _, name := range mcpServers {
			fmt.Fprintf(&b, "- %s\n", escapePromptData(name))
		}
		b.WriteString("</mcp_servers>")
	}

	// Plan Before Action section
	if planDir != "" {
		fmt.Fprintf(&b, "\n\n<plan_before_action>\nThe selected executor owns planning under the Plan Before Action rule, including permitted direct-execution fallbacks.\n\n**Exploratory tasks are EXEMPT.** Reading files, searching code, investigating issues, or answering questions do NOT require a plan.\n\n## Default Plan Location\nUse an explicit user location (including a cloud workspace) first, otherwise reuse the supplied or existing plan, then use this configured default: %s/<feature-name>.md. Use a local fallback only without an override or existing plan; never create a local duplicate of a cloud plan\n\n", planDir)
		b.WriteString(PlanDocumentFormat)
		b.WriteString("\n\n## Reviewing Team Plans\nStraightforward authorized plans proceed autonomously. A Team returns PLAN_REVIEW_REQUIRED with its plan path and unresolved trade-offs when a decision is needed. Present the decision to the user, then re-delegate with \"Plan <path> approved. Proceed with execution.\" and the decision.\n\n## Direct execution\nApply the same planning policy to all permitted direct-execution cases. Simple narrow changes and read-only investigation do not require a plan.\n</plan_before_action>")
	}

	// Exploration Artifacts section
	fmt.Fprintf(&b, "\n\n<exploration_artifacts>\nWhen you perform exploration tasks (reading files, searching code, investigating issues), save an artifact only when creation is authorized and the findings are complex or reusable. Follow explicit user storage rules first, otherwise reuse a supplied or existing artifact, then the configured default location. Only without any of these use the local fallback %s. Do not create a local duplicate of a cloud artifact; use the tools and format appropriate to the selected storage. A read-only question or investigation does not authorize artifact writes by itself.\n\n## When to Save\n- Complex investigations with many files or nuanced conclusions\n- Investigations whose results may be reused by other agents in the same session\n- Simple one-off lookups can skip saving\n\n## Local Fallback Document Naming\nFormat: %s/<task-slug>.md\nExamples:\n- %s/explore_auth_flow.md\n- %s/investigate_race_condition.md\n\n## Document Content\n- Created at: use current time when saving\n- Updated at: use current time when updating\n- Freshness window: same-day\n- Task: the original or summarized task description\n- Key Findings, Files Inspected, Reusable Context, Open Questions\n\n## Reuse Rules\n1. Before starting a new exploration, check the selected storage location for an existing artifact with the same task-slug (local fallback: %s).\n2. If an artifact exists and was created today, read it first and reuse its findings when appropriate.\n3. If you create or reuse an artifact, include its path or URL in your response so other agents can access it.\n</exploration_artifacts>", exploreDir, exploreDir, exploreDir, exploreDir, exploreDir)

	return b.String()
}
