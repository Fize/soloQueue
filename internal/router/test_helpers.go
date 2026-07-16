package router

import (
	"github.com/xiaobaitu/soloqueue/internal/config"
)

// MockModelService implements ModelService for testing
type MockModelService struct {
	models map[string]*config.LLMModel
}

// DefaultModelByRole returns the model for a role
func (m *MockModelService) DefaultModelByRole(role string) *config.LLMModel {
	return m.models[role]
}

// NewMockModelService creates a mock with default models that match the
// default configuration (basic/universal/superior/expert/apex/fast roles)
func NewMockModelService() *MockModelService {
	return &MockModelService{
		models: map[string]*config.LLMModel{
			"fast": {
				ID:            "deepseek-v4-flash",
				ProviderID:    "deepseek",
				ContextWindow: 131072,
			},
			"basic": {
				ID:            "deepseek-v4-flash",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-flash",
				ContextWindow: 131072,
				Thinking: config.ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "high",
				},
			},
			"universal": {
				ID:            "deepseek-v4-flash",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-flash",
				ContextWindow: 131072,
				Thinking: config.ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "high",
				},
			},
			"superior": {
				ID:            "deepseek-v4-pro",
				ProviderID:    "deepseek",
				ContextWindow: 1048576,
				Thinking: config.ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "high",
				},
			},
			"expert": {
				ID:            "deepseek-v4-pro-max",
				ProviderID:    "deepseek",
				APIModel:      "deepseek-v4-pro",
				ContextWindow: 1048576,
				Thinking: config.ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "max",
				},
			},
			"apex": {
				ID:            "gpt-5.6",
				ProviderID:    "openai",
				ContextWindow: 2097152,
				Thinking: config.ThinkingConfig{
					Enabled:         true,
					ReasoningEffort: "max",
				},
			},
		},
	}
}
