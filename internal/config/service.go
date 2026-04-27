package config

import (
	"fmt"
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

// DefaultModel 返回指定 type 中 isDefault=true 的模型
// modelType 为空时忽略 type 过滤
func (s *GlobalService) DefaultModel(modelType string) *LLMModel {
	settings := s.Get()
	for i := range settings.Models {
		m := settings.Models[i]
		if !m.Enabled {
			continue
		}
		if modelType != "" && m.Type != modelType {
			continue
		}
		if m.IsDefault {
			return &m
		}
	}
	// 无 isDefault 则返回第一个匹配的
	for i := range settings.Models {
		m := settings.Models[i]
		if !m.Enabled {
			continue
		}
		if modelType != "" && m.Type != modelType {
			continue
		}
		return &m
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
