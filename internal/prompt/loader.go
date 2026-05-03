package prompt

import (
	"fmt"
	"os"
	"path/filepath"
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
// recentMemory 为短期记忆摘要（可为空，表示无历史记忆）。
func (p *PromptConfig) BuildPrompt(leaders []LeaderInfo, recentMemory string) (string, error) {
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
	return assembleWithXML(profile, userCtx, recentMemory, routingTable, teamMgmt, rules), nil
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
