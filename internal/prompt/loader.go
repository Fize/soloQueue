package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptConfig specifies the prompt configuration for the main Agent.
type PromptConfig struct {
	RolesDir  string // e.g. "/Users/xxx/.soloqueue/prompts/roles"
	GlobalDir string // e.g. "/Users/xxx/.soloqueue/prompts/global"
}

// soulPath returns the soul.md path.
func (p *PromptConfig) soulPath() string {
	return filepath.Join(p.RolesDir, "soul.md")
}

// RulesPath returns the rules.md path.
func (p *PromptConfig) RulesPath() string {
	return filepath.Join(p.RolesDir, "rules.md")
}

// userCtxPath returns the global/user.md path.
func (p *PromptConfig) userCtxPath() string {
	return filepath.Join(p.GlobalDir, "user.md")
}

// BuildPrompt assembles the complete system prompt.
// leaders come from the runtime agent registry and are used to build the routing table.
// recentMemory is the short-term memory directory (may be empty).
// permanentMemory is a long-term memory flag (when non-empty, injects RecallMemory/Remember tool instructions).
func (p *PromptConfig) BuildPrompt(leaders []LeaderInfo, recentMemory, permanentMemory, planDir string, mcpServers []string) (string, error) {
	// 1. Load soul (required)
	soul, err := readMD(p.soulPath())
	if err != nil {
		return "", fmt.Errorf("load soul: %w", err)
	}

	// 2. Load user context (optional, skip if missing)
	userCtx, _ := readMD(p.userCtxPath())

	// 3. Load rules (required)
	rules, err := readMD(p.RulesPath())
	if err != nil {
		return "", fmt.Errorf("load rules: %w", err)
	}

	// 4. Build routing table dynamically
	routingTable := buildRoutingTable(leaders)

	// 5. Team management guide
	// Get workDir from RolesDir: RolesDir = <workDir>/.soloqueue/roles
	workDir := filepath.Dir(filepath.Dir(p.RolesDir))
	teamMgmt := buildTeamManagementSection(workDir)

	// 6. Assemble XML
	return assembleWithXML(soul, userCtx, recentMemory, permanentMemory, routingTable, teamMgmt, rules, planDir, workDir, mcpServers), nil
}

// EnsureFiles checks and fills in any missing prompt files.
// Return value: (rulesCreated bool, err error)
//   - Returns SoulNeededError when soul.md is missing; the caller handles the interactive flow.
//   - Creates a default rules.md when it is missing, returning rulesCreated=true.
//   - Missing directory structure is created automatically.
func (p *PromptConfig) EnsureFiles() (bool, error) {
	rulesCreated := false

	// Make sure the directory structure exists.
	dirs := []string{
		p.GlobalDir,
		p.RolesDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// Check soul.md
	soulExists, err := fileExists(p.soulPath())
	if err != nil {
		return false, err
	}
	if !soulExists {
		return false, &SoulNeededError{RoleID: "default"}
	}

	// Check rules.md
	rulesExists, err := fileExists(p.RulesPath())
	if err != nil {
		return false, err
	}
	if !rulesExists {
		if err := os.WriteFile(p.RulesPath(), []byte(DefaultRules), 0o644); err != nil {
			return false, fmt.Errorf("write default rules: %w", err)
		}
		rulesCreated = true
	}

	return rulesCreated, nil
}

// WriteSoul writes the soul.md file based on the user's questionnaire answers.
func (p *PromptConfig) WriteSoul(answers ProfileAnswers) error {
	// Ensure the directory exists.
	if err := os.MkdirAll(p.RolesDir, 0o755); err != nil {
		return fmt.Errorf("create soul dir: %w", err)
	}
	content := BuildProfile(answers)
	if err := os.WriteFile(p.soulPath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write soul: %w", err)
	}
	return nil
}

// WriteSoulContent writes raw content to the soul.md file.
func (p *PromptConfig) WriteSoulContent(content string) error {
	if err := os.MkdirAll(p.RolesDir, 0o755); err != nil {
		return fmt.Errorf("create soul dir: %w", err)
	}
	if err := os.WriteFile(p.soulPath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write soul: %w", err)
	}
	return nil
}

// WriteRulesContent writes raw content to the rules.md file.
func (p *PromptConfig) WriteRulesContent(content string) error {
	if err := os.MkdirAll(p.RolesDir, 0o755); err != nil {
		return fmt.Errorf("create rules dir: %w", err)
	}
	if err := os.WriteFile(p.RulesPath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write rules: %w", err)
	}
	return nil
}

// readMD reads the contents of a markdown file.
func readMD(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadSoulName reads soul.md and extracts the assistant name.
// It first checks the "- Name:" field in the Personalization section;
// if not found, falls back to "You are <name>," from the first line.
// Returns an empty string if the file doesn't exist or no name is found.
func ReadSoulName(promptCfg *PromptConfig) string {
	data, err := os.ReadFile(promptCfg.soulPath())
	if err != nil {
		return ""
	}
	return extractSoulName(string(data))
}

// extractSoulName parses the assistant name from soul.md content.
// Priority: "- Name:" field in Personalization > "You are <name>," in first line.
func extractSoulName(content string) string {
	// 1. Try "- Name:" in Personalization section (user's configured display name)
	const namePrefix = "- Name:"
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, namePrefix) {
			name := strings.TrimSpace(trimmed[len(namePrefix):])
			if name != "" {
				return name
			}
		}
	}

	// 2. Fallback: "You are <name>, ..."
	const youPrefix = "You are "
	idx := strings.Index(content, youPrefix)
	if idx == -1 {
		return ""
	}
	after := content[idx+len(youPrefix):]
	// e.g. "You are 小Q, a personal assistant" or "You are one of 小Q,大Q, an assistant"
	for _, sep := range []string{", a ", ", an "} {
		if commaIdx := strings.Index(after, sep); commaIdx != -1 {
			name := strings.TrimSpace(after[:commaIdx])
			if name != "" {
				return name
			}
		}
	}
	commaIdx := strings.Index(after, ",")
	if commaIdx == -1 {
		return ""
	}
	name := strings.TrimSpace(after[:commaIdx])
	if name == "" {
		return ""
	}
	return name
}

// fileExists reports whether the given path exists.
func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
