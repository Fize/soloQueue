package router

import (
	"context"
	"testing"
)

func TestRouter_Route(t *testing.T) {
	classifierCfg := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(classifierCfg, nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	tests := []struct {
		name          string
		prompt        string
		expectedLevel ClassificationLevel
		expectedModel string
	}{
		{
			name:          "Conversation routes to flash",
			prompt:        "Explain how closures work",
			expectedLevel: LevelConversation,
			expectedModel: "deepseek:deepseek-v4-flash",
		},
		{
			name:          "SingleFile routes to flash",
			prompt:        "Fix the bug in main.go",
			expectedLevel: LevelSimpleSingleFile,
			expectedModel: "deepseek:deepseek-v4-flash",
		},
		{
			name:          "MultiFile routes to pro",
			prompt:        "Refactor auth.go, middleware.go, and service.go",
			expectedLevel: LevelMediumMultiFile,
			expectedModel: "deepseek:deepseek-v4-pro",
		},
		{
			name:          "Complex routes to pro-max",
			prompt:        "Redesign the entire authentication system with 8 files",
			expectedLevel: LevelComplexRefactoring,
			expectedModel: "deepseek:deepseek-v4-pro-max",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			decision, err := router.Route(ctx, tt.prompt)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if decision.Level != tt.expectedLevel {
				t.Errorf("expected level %v, got %v",
					tt.expectedLevel, decision.Level)
			}

			if decision.ModelID != tt.expectedModel {
				t.Errorf("expected model %q, got %q",
					tt.expectedModel, decision.ModelID)
			}
		})
	}
}

func TestRouter_Warnings(t *testing.T) {
	classifierCfg := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(classifierCfg, nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	ctx := context.Background()
	decision, _ := router.Route(ctx, "Delete all files in the database")

	if len(decision.Warnings) == 0 {
		t.Errorf("expected warnings for dangerous operation, got none")
	}

	if decision.Warnings[0] == "" {
		t.Errorf("warning message should not be empty")
	}
}

func TestRouter_ModelForClassification(t *testing.T) {
	classifier := NewDefaultClassifier(DefaultClassifierConfig(), nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	classification := ClassificationResult{
		Level: LevelComplexRefactoring,
	}

	model := router.ModelForClassification(classification)
	if model != "deepseek:deepseek-v4-pro-max" {
		t.Errorf("expected pro-max model, got %q", model)
	}
}
