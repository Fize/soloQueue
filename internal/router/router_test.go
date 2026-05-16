package router

import (
	"context"
	"testing"
)

func TestRouter_Route(t *testing.T) {
	classifierCfg := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(classifierCfg, nil, "", nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	tests := []struct {
		name           string
		prompt         string
		expectedLevel  ClassificationLevel
		expectedModel  string
		expectedThink  bool
		expectedEffort string
	}{
		{
			name:           "Conversation routes to flash (no thinking)",
			prompt:         "Explain how closures work",
			expectedLevel:  LevelConversation,
			expectedModel:  "deepseek-v4-flash",
			expectedThink:  false,
			expectedEffort: "",
		},
		{
			name:           "SingleFile routes to universal (flash + thinking high)",
			prompt:         "Fix the bug in main.go",
			expectedLevel:  LevelSimpleSingleFile,
			expectedModel:  "deepseek-v4-flash",
			expectedThink:  true,
			expectedEffort: "high",
		},
		{
			name:           "MultiFile routes to superior (pro + thinking high)",
			prompt:         "Refactor auth.go, middleware.go, and service.go",
			expectedLevel:  LevelMediumMultiFile,
			expectedModel:  "deepseek-v4-pro",
			expectedThink:  true,
			expectedEffort: "high",
		},
		{
			name:           "Complex routes to expert (pro + thinking max)",
			prompt:         "/l3 redesign the entire authentication system",
			expectedLevel:  LevelComplexRefactoring,
			expectedModel:  "deepseek-v4-pro",
			expectedThink:  true,
			expectedEffort: "max",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			decision, err := router.Route(ctx, tt.prompt, LevelUnknown)

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

			if decision.ThinkingEnabled != tt.expectedThink {
				t.Errorf("ThinkingEnabled: got %v, want %v",
					decision.ThinkingEnabled, tt.expectedThink)
			}

			if decision.ReasoningEffort != tt.expectedEffort {
				t.Errorf("ReasoningEffort: got %q, want %q",
					decision.ReasoningEffort, tt.expectedEffort)
			}

			if decision.ContextWindow <= 0 {
				t.Errorf("ContextWindow: got %d, want > 0", decision.ContextWindow)
			}
		})
	}
}

func TestRouter_Warnings(t *testing.T) {
	classifierCfg := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(classifierCfg, nil, "", nil)
	modelService := NewMockModelService()
	router := NewRouter(classifier, modelService, nil)

	ctx := context.Background()
	decision, _ := router.Route(ctx, "Run rm -rf on the old backup", LevelUnknown)

	if len(decision.Warnings) == 0 {
		t.Errorf("expected warnings for dangerous operation, got none")
	}

	if decision.Warnings[0] == "" {
		t.Errorf("warning message should not be empty")
	}
}

func TestRouter_ModelForClassification(t *testing.T) {
	classifier := NewDefaultClassifier(DefaultClassifierConfig(), nil, "", nil)
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
