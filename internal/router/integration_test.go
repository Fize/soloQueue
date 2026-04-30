package router

import (
	"context"
	"testing"
)

// TestIntegration_FullRoutingFlow tests a complete routing flow from classification to model selection
func TestIntegration_FullRoutingFlow(t *testing.T) {
	tests := []struct {
		name        string
		prompt      string
		expectLevel ClassificationLevel
		expectModel string
	}{
		{
			name:        "Simple hello - conversation level",
			prompt:      "hello, how are you?",
			expectLevel: LevelConversation,
			expectModel: "deepseek:deepseek-v4-flash",
		},
		{
			name:        "Single file read - single file level",
			prompt:      "/read main.go",
			expectLevel: LevelSimpleSingleFile,
			expectModel: "deepseek:deepseek-v4-flash",
		},
		{
			name:        "Multiple files - multi-file level",
			prompt:      "/refactor main.go config.go utils.go to improve performance",
			expectLevel: LevelMediumMultiFile,
			expectModel: "deepseek:deepseek-v4-pro",
		},
		{
			name:        "Complex refactoring - complex level",
			prompt:      "/implement new authentication system with OAuth2 support across models.go, auth.go, middleware.go, handlers.go",
			expectLevel: LevelComplexRefactoring,
			expectModel: "deepseek:deepseek-v4-pro-max",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create classifier and router
			config := ClassifierConfig{
				EnableFastTrack:              true,
				EnableLLMClassification:     false,
				FastTrackConfidenceThreshold: 75,
			}
			classifier := NewDefaultClassifier(config, nil)
			router := NewRouter(classifier, NewMockModelService(), nil)

			// Route the prompt
			ctx := context.Background()
			decision, err := router.Route(ctx, tt.prompt)
			if err != nil {
				t.Fatalf("Route() failed: %v", err)
			}

			// Verify classification level
			if decision.Level != tt.expectLevel {
				t.Errorf("Level: got %v, want %v", decision.Level, tt.expectLevel)
			}

			// Verify model ID
			if decision.ModelID != tt.expectModel {
				t.Errorf("ModelID: got %s, want %s", decision.ModelID, tt.expectModel)
			}

			// Verify classification has data
			if decision.Classification.Confidence == 0 {
				t.Error("Classification confidence should not be 0")
			}
		})
	}
}

// TestIntegration_ConfidenceThreshold tests that confidence threshold is respected
func TestIntegration_ConfidenceThreshold(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		prompt       string
		threshold    int
		expectBypass bool // whether we bypass LLM classification due to high confidence
	}{
		{
			name:         "High confidence with high threshold",
			prompt:       "/refactor main.go config.go utils.go parser.go handler.go",
			threshold:    75,
			expectBypass: true,
		},
		{
			name:         "Ambiguous prompt with low threshold",
			prompt:       "help me with something",
			threshold:    50,
			expectBypass: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := ClassifierConfig{
				EnableFastTrack:              true,
				EnableLLMClassification:     false,
				FastTrackConfidenceThreshold: tt.threshold,
			}
			classifier := NewDefaultClassifier(config, nil)
			router := NewRouter(classifier, NewMockModelService(), nil)

			decision, err := router.Route(ctx, tt.prompt)
			if err != nil {
				t.Fatalf("Route() failed: %v", err)
			}

			// If fast-track should bypass, confidence should be present
			if tt.expectBypass && decision.Classification.Confidence == 0 {
				t.Error("Expected fast-track bypass but confidence is 0")
			}
		})
	}
}

// TestIntegration_ModelMappingAccuracy tests that each classification level maps to correct model
func TestIntegration_ModelMappingAccuracy(t *testing.T) {
	levels := map[ClassificationLevel]string{
		LevelConversation:         "deepseek:deepseek-v4-flash",
		LevelSimpleSingleFile:     "deepseek:deepseek-v4-flash",
		LevelMediumMultiFile:      "deepseek:deepseek-v4-pro",
		LevelComplexRefactoring:   "deepseek:deepseek-v4-pro-max",
	}

	mockService := NewMockModelService()
	router := NewRouter(nil, mockService, nil)

	for level, expectedModel := range levels {
		modelID := router.resolveModelID(level)
		if modelID != expectedModel {
			t.Errorf("Level %v: got model %s, want %s", level, modelID, expectedModel)
		}
	}
}
