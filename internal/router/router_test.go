package router

import (
	"context"
	"testing"
)

func TestRouter_Route(t *testing.T) {
	classifierCfg := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(classifierCfg, nil, "deepseek", "", nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	tests := []struct {
		name          string
		prompt        string
		expectedLevel ClassificationLevel
		expectedModel string
	}{
		{
			name:          "L0 → basic (flash)",
			prompt:        "Explain how closures work",
			expectedLevel: LevelConversation,
			expectedModel: "deepseek-v4-flash",
		},
		{
			name:          "L1 → universal (flash)",
			prompt:        "Fix the bug in main.go",
			expectedLevel: LevelSimpleSingleFile,
			expectedModel: "deepseek-v4-flash",
		},
		{
			name:          "L2 → superior (pro)",
			prompt:        "Refactor auth.go, middleware.go, and service.go",
			expectedLevel: LevelMediumMultiFile,
			expectedModel: "deepseek-v4-pro",
		},
		{
			name:          "L3 → expert (pro-max)",
			prompt:        "/l3 redesign the entire authentication system",
			expectedLevel: LevelComplexRefactoring,
			expectedModel: "deepseek-v4-pro",
		},
		{
			name:          "L4 → apex (gpt)",
			prompt:        "/l4 redesign zero-downtime production migration",
			expectedLevel: LevelDeepReasoning,
			expectedModel: "gpt-5.6",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			decision, err := router.Route(ctx, tt.prompt, LevelUnknown, nil)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if decision.Level != tt.expectedLevel {
				t.Errorf("Level: got %v, want %v (confidence=%d)",
					decision.Level, tt.expectedLevel, decision.Classification.Confidence)
			}

			if decision.ModelID != tt.expectedModel {
				t.Errorf("ModelID: got %q, want %q",
					decision.ModelID, tt.expectedModel)
			}

			if decision.ContextWindow <= 0 {
				t.Errorf("ContextWindow: got %d, want > 0", decision.ContextWindow)
			}
		})
	}
}

func TestRouter_Warnings(t *testing.T) {
	classifierCfg := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(classifierCfg, nil, "deepseek", "", nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	ctx := context.Background()
	decision, _ := router.Route(ctx, "Run rm -rf on the old backup", LevelUnknown, nil)

	if len(decision.Warnings) == 0 {
		t.Errorf("expected warnings for dangerous operation, got none")
	}

	if decision.Warnings[0] == "" {
		t.Errorf("warning message should not be empty")
	}
}

func TestRouter_ModelForClassification(t *testing.T) {
	classifier := NewDefaultClassifier(DefaultClassifierConfig(), nil, "deepseek", "", nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	classification := ClassificationResult{
		Level: LevelComplexRefactoring,
	}

	model := router.ModelForClassification(classification)
	// expert role → APIModel "deepseek-v4-pro" → "deepseek-v4-pro"
	if model != "deepseek-v4-pro" {
		t.Errorf("expected deepseek:deepseek-v4-pro, got %q", model)
	}
}
