package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptConfig 定义了当前实例化哪个主 Agent 的 prompt 配置。
type PromptConfig struct {
	RoleID  string // 例如 "main_assistant"
	BaseDir string // 例如 "/Users/xxx/.soloqueue/prompts"
}

// profilePath 返回当前角色的 profile.md 路径。
func (p *PromptConfig) profilePath() string {
	return filepath.Join(p.BaseDir, "roles", p.RoleID, "profile.md")
}

// RulesPath 返回当前角色的 rules.md 路径。
func (p *PromptConfig) RulesPath() string {
	return filepath.Join(p.BaseDir, "roles", p.RoleID, "rules.md")
}

// userCtxPath 返回 global/user.md 路径。
func (p *PromptConfig) userCtxPath() string {
	return filepath.Join(p.BaseDir, "global", "user.md")
}

// BuildPrompt 组装完整系统提示词。
// leaders 来自运行时 agent 注册数据，用于动态构建路由表。
// recentMemory 为短期记忆目录路径（可为空，表示无历史记忆）。
// permanentMemory 为长期记忆文本（可为空）。
func (p *PromptConfig) BuildPrompt(leaders []LeaderInfo, recentMemory, permanentMemory, planDir string) (string, error) {
	// 1. 加载 profile（必需）
	profile, err := readMD(p.profilePath())
	if err != nil {
		return "", fmt.Errorf("load profile: %w", err)
	}

	// 2. 加载 user context（可选，缺失跳过）
	userCtx, _ := readMD(p.userCtxPath())

	// 3. 加载 rules（必需）
	rules, err := readMD(p.RulesPath())
	if err != nil {
		return "", fmt.Errorf("load rules: %w", err)
	}

	// 4. 动态构建路由表
	routingTable := buildRoutingTable(leaders)

	// 5. 团队管理指南
	workDir := filepath.Dir(p.BaseDir) // BaseDir = <workDir>/prompts
	teamMgmt := buildTeamManagementSection(workDir)

	// 6. XML 组装
	return assembleWithXML(profile, userCtx, recentMemory, permanentMemory, routingTable, teamMgmt, rules, planDir), nil
}

// EnsureFiles 检查并补齐缺失的 prompt 文件。
// 返回值: (rulesCreated bool, err error)
// - profile.md 缺失时返回 ProfileNeededError，由调用方处理交互
// - rules.md 缺失时自动创建默认内容，返回 rulesCreated=true
// - 目录结构缺失时自动创建
func (p *PromptConfig) EnsureFiles() (bool, error) {
	rulesCreated := false

	// 确保目录结构存在
	dirs := []string{
		filepath.Join(p.BaseDir, "global"),
		filepath.Join(p.BaseDir, "roles", p.RoleID),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, fmt.Errorf("create dir %s: %w", dir, err)
		}
	}

	// 检查 profile.md
	profileExists, err := fileExists(p.profilePath())
	if err != nil {
		return false, err
	}
	if !profileExists {
		return false, &ProfileNeededError{RoleID: p.RoleID}
	}

	// 检查 rules.md
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

// WriteProfile 根据用户回答写入 profile.md。
func (p *PromptConfig) WriteProfile(answers ProfileAnswers) error {
	// 确保目录存在
	dir := filepath.Dir(p.profilePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}
	content := BuildProfile(answers)
	if err := os.WriteFile(p.profilePath(), []byte(content), 0o644); err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	return nil
}

// readMD 读取 markdown 文件内容。
func readMD(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadProfileName reads profile.md and extracts the assistant name.
// It looks for the pattern "You are <name>," at the beginning of the file.
// Returns empty string if the file doesn't exist or no name is found.
func ReadProfileName(promptCfg *PromptConfig) string {
	data, err := os.ReadFile(promptCfg.profilePath())
	if err != nil {
		return ""
	}
	return extractProfileName(string(data))
}

// extractProfileName parses the assistant name from profile.md content.
// The profile starts with "You are <name>, ..." — we extract <name>.
func extractProfileName(content string) string {
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

// fileExists 检查文件是否存在。
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
