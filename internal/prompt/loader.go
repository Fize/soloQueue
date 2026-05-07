package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptConfig specifies which main Agent's prompt configuration is currently instantiated.
type PromptConfig struct {
	RoleID  string // e.g. "main_assistant"
	BaseDir string // e.g. "/Users/xxx/.soloqueue/prompts"
}

// soulPath returns the soul.md path for the current role.
func (p *PromptConfig) soulPath() string {
	return filepath.Join(p.BaseDir, "roles", p.RoleID, "soul.md")
}

// RulesPath returns the rules.md path for the current role.
func (p *PromptConfig) RulesPath() string {
	return filepath.Join(p.BaseDir, "roles", p.RoleID, "rules.md")
}

// userCtxPath returns the global/user.md path.
func (p *PromptConfig) userCtxPath() string {
	return filepath.Join(p.BaseDir, "global", "user.md")
}

// BuildPrompt assembles the complete system prompt.
// leaders come from the runtime agent registry and are used to build the routing table.
// recentMemory is the short-term memory directory (may be empty).
// permanentMemory is a long-term memory flag (when non-empty, injects RecallMemory/Remember tool instructions).
func (p *PromptConfig) BuildPrompt(leaders []LeaderInfo, recentMemory, permanentMemory, planDir string) (string, error) {
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
	workDir := filepath.Dir(p.BaseDir) // BaseDir = <workDir>/prompts
	teamMgmt := buildTeamManagementSection(workDir)

	// 6. Assemble XML
	return assembleWithXML(soul, userCtx, recentMemory, permanentMemory, routingTable, teamMgmt, rules, planDir), nil
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
		filepath.Join(p.BaseDir, "global"),
		filepath.Join(p.BaseDir, "roles", p.RoleID),
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
		return false, &SoulNeededError{RoleID: p.RoleID}
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
	dir := filepath.Dir(p.soulPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create soul dir: %w", err)
	}
	content := BuildProfile(answers)
	if err := os.WriteFile(p.soulPath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write soul: %w", err)
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
// It looks for the pattern "You are <name>," at the beginning of the file.
// Returns an empty string if the file doesn't exist or no name is found.
func ReadSoulName(promptCfg *PromptConfig) string {
	data, err := os.ReadFile(promptCfg.soulPath())
	if err != nil {
		return ""
	}
	return extractSoulName(string(data))
}

// extractSoulName parses the assistant name from soul.md content.
// The soul starts with "You are <name>, ..." — we extract <name>.
func extractSoulName(content string) string {
	const prefix = "You are "
	idx := strings.Index(content, prefix)
	if idx == -1 {
		return ""
	}
	after := content[idx+len(prefix):]
	// Find the comma that separates the name from the role description.
	// The role description typically starts with " a " or " an " after the comma,
	// e.g. "You are 小Q, a personal assistant" or "You are one of 小Q,大Q, an assistant".
	// We look for ", a " or ", an " to avoid splitting on commas within the name itself.
	for _, sep := range []string{", a ", ", an "} {
		if commaIdx := strings.Index(after, sep); commaIdx != -1 {
			name := strings.TrimSpace(after[:commaIdx])
			if name != "" {
				return name
			}
		}
	}
	// Fallback: simple comma split for patterns like "You are Name, something"
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
