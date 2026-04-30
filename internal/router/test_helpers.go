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
// default configuration (fast/universal/superior/expert roles)
func NewMockModelService() *MockModelService {
	return &MockModelService{
		models: map[string]*config.LLMModel{
			"fast": {
				ID:         "deepseek-v4-flash",
				ProviderID: "deepseek",
				// APIModel empty = use ID
			},
			"universal": {
				ID:         "deepseek-v4-flash-thinking",
				ProviderID: "deepseek",
				APIModel:   "deepseek-v4-flash",
			},
			"superior": {
				ID:         "deepseek-v4-pro",
				ProviderID: "deepseek",
				// APIModel empty = use ID
			},
			"expert": {
				ID:         "deepseek-v4-pro-max",
				ProviderID: "deepseek",
				APIModel:   "deepseek-v4-pro",
			},
		},
	}
}
