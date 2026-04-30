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

// newMockModelService creates a mock with default models
func newMockModelService() *MockModelService {
	return &MockModelService{
		models: map[string]*config.LLMModel{
			"fast": {
				ID:         "deepseek-v4-flash",
				ProviderID: "deepseek",
			},
			"superior": {
				ID:         "deepseek-v4-pro",
				ProviderID: "deepseek",
			},
			"expert": {
				ID:         "deepseek-v4-pro-max",
				ProviderID: "deepseek",
			},
		},
	}
}
