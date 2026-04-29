package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// GlobalService 是全局配置服务，嵌入 Loader[Settings]
// 自动继承所有 Loader 方法：Load / Save / Get / Set / OnChange / Watch / Close 等
// 在此基础上提供业务相关的便捷查询接口
type GlobalService struct {
	*Loader[Settings]
}

// New 创建 GlobalService
// workDir 通常为 ~/.soloqueue
func New(workDir string) (*GlobalService, error) {
	mainPath := filepath.Join(workDir, "settings.toml")
	localPath := filepath.Join(workDir, "settings.local.toml")

	loader, err := NewLoader(DefaultSettings(), mainPath, localPath)
	if err != nil {
		return nil, fmt.Errorf("config.New: %w", err)
	}

	return &GlobalService{Loader: loader}, nil
}

// ─── Convenience Queries ──────────────────────────────────────────────────────

// DefaultProvider 返回 isDefault=true 的 LLM Provider，无则返回 nil
func (s *GlobalService) DefaultProvider() *LLMProvider {
	settings := s.Get()
	for i := range settings.Providers {
		if settings.Providers[i].IsDefault {
			p := settings.Providers[i]
			return &p
		}
	}
	return nil
}

// DefaultEmbeddingModel 返回 isDefault=true 的 Embedding 模型
func (s *GlobalService) DefaultEmbeddingModel() *EmbeddingModel {
	settings := s.Get()
	for i := range settings.Embedding.Models {
		m := settings.Embedding.Models[i]
		if m.IsDefault {
			return &m
		}
	}
	return nil
}

// ProviderByID 按 ID 查找 LLM Provider
func (s *GlobalService) ProviderByID(id string) *LLMProvider {
	settings := s.Get()
	for i := range settings.Providers {
		if settings.Providers[i].ID == id {
			p := settings.Providers[i]
			return &p
		}
	}
	return nil
}

// ModelByID 按 ID 查找 LLM Model
func (s *GlobalService) ModelByID(id string) *LLMModel {
	settings := s.Get()
	for i := range settings.Models {
		if settings.Models[i].ID == id {
			m := settings.Models[i]
			return &m
		}
	}
	return nil
}

// ModelByProviderID 按 providerID + modelID 双键查找 LLM Model
func (s *GlobalService) ModelByProviderID(providerID, modelID string) *LLMModel {
	settings := s.Get()
	for i := range settings.Models {
		m := settings.Models[i]
		if m.ProviderID == providerID && m.ID == modelID {
			return &m
		}
	}
	return nil
}

// DefaultModelByRole 按角色解析默认模型
//
// role 支持: "expert", "superior", "universal", "fast"。
//
// 解析优先级：角色配置值 → Fallback → 硬编码默认值。
// 配置值格式为 "provider:id"，provider 和 id 必须存在于配置文件中。
// 返回 nil 表示未找到对应模型。
func (s *GlobalService) DefaultModelByRole(role string) *LLMModel {
	settings := s.Get()

	// 1. 获取角色配置值
	ref := roleField(settings.DefaultModels, role)

	// 2. 角色未配置 → 尝试 Fallback
	if ref == "" {
		ref = settings.DefaultModels.Fallback
	}

	// 3. Fallback 也为空 → 使用硬编码默认值
	if ref == "" {
		ref = roleDefault(role)
	}

	if ref == "" {
		return nil
	}

	// 4. 解析 "provider:id" 并查找
	providerID, modelID, ok := parseProviderModelID(ref)
	if !ok {
		return nil
	}
	return s.ModelByProviderID(providerID, modelID)
}

// roleField 返回角色对应的配置值
func roleField(dm DefaultModelsConfig, role string) string {
	switch role {
	case "expert":
		return dm.Expert
	case "superior":
		return dm.Superior
	case "universal":
		return dm.Universal
	case "fast":
		return dm.Fast
	default:
		return ""
	}
}

// roleDefault 返回角色的硬编码默认值
func roleDefault(role string) string {
	defaults := map[string]string{
		"expert":    "deepseek:deepseek-v4-pro-max",
		"superior":  "deepseek:deepseek-v4-pro",
		"universal": "deepseek:deepseek-v4-flash-thinking",
		"fast":      "deepseek:deepseek-v4-flash",
	}
	return defaults[role]
}

// ─── Init & DefaultWorkDir ────────────────────────────────────────────────────

// DefaultWorkDir 返回 ~/.soloqueue，不存在则创建
func DefaultWorkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".soloqueue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir %s: %w", dir, err)
	}
	return dir, nil
}

// Init 创建 GlobalService 并完成加载、热加载和首次保存
func Init(workDir string) (*GlobalService, error) {
	cfg, err := New(workDir)
	if err != nil {
		return nil, err
	}

	if err := cfg.Load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Watch(); err != nil {
		// Non-fatal: config changes will require restart.
	}

	settingsPath := filepath.Join(workDir, "settings.toml")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			// Non-fatal: don't pollute terminal before logger is ready.
		}
	}

	return cfg, nil
}
