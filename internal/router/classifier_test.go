package router

import (
	"context"
	
	"testing"
)

func TestDefaultClassifier_Classify(t *testing.T) {
	config := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(config, nil, "", nil)

	tests := []struct {
		name             string
		prompt           string
		expectedLevel    ClassificationLevel
		minConfidence    int
		requiresLLMCheck bool // whether LLM classification is needed (confidence < threshold)
	}{
		{
			name:          "Conversation: pure explanation",
			prompt:        "Explain how the circuit breaker pattern works",
			expectedLevel: LevelConversation,
			minConfidence: 80,
		},
		{
			name:          "SingleFile: fix and file mentioned",
			prompt:        "Fix the null pointer bug in main.go line 42",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 85,
		},
		{
			name:          "MultiFile: multiple files with refactor",
			prompt:        "Refactor auth.go, middleware.go, and handler.go to use dependency injection",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 75,
		},
		{
			name:          "Complex: many files",
			prompt:        "Redesign the entire API layer: api.go, handler.go, service.go, repo.go, model.go, dto.go, config.go",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 75,
		},
		{
			name:          "SlashCommand: /read",
			prompt:        "/read src/main.go",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 90,
		},
		{
			name:          "SlashCommand: /refactor multiple",
			prompt:        "/refactor service.go repo.go handler.go",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := classifier.Classify(ctx, tt.prompt)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Level != tt.expectedLevel {
				t.Errorf("expected level %v, got %v (confidence: %d, reason: %s)",
					tt.expectedLevel, result.Level, result.Confidence, result.Reason)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("expected confidence >= %d, got %d",
					tt.minConfidence, result.Confidence)
			}
		})
	}
}

func TestDefaultClassifier_WithDisabledFastTrack(t *testing.T) {
	config := DefaultClassifierConfig()
	config.EnableFastTrack = false
	classifier := NewDefaultClassifier(config, nil, "", nil)

	ctx := context.Background()
	result, err := classifier.Classify(ctx, "Explain closures")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return a default result when fast-track is disabled
	if result.Level != LevelSimpleSingleFile {
		t.Errorf("expected default level L1, got %v", result.Level)
	}
}

func TestClassificationResultDetails(t *testing.T) {
	config := DefaultClassifierConfig()
	classifier := NewDefaultClassifier(config, nil, "", nil)

	ctx := context.Background()

	// Test that we capture file paths
	result, _ := classifier.Classify(ctx, "Fix bugs in auth.go and service.go")
	if len(result.DetectedFilePaths) < 2 {
		t.Errorf("expected to detect 2 files, got %d: %v",
			len(result.DetectedFilePaths), result.DetectedFilePaths)
	}

	// Test that we capture slash commands
	result, _ = classifier.Classify(ctx, "/read main.go read the main entry point")
	if result.SlashCommand != "read" {
		t.Errorf("expected slash command 'read', got %q", result.SlashCommand)
	}

	// Test that reason is always provided
	result, _ = classifier.Classify(ctx, "Some task")
	if result.Reason == "" {
		t.Errorf("expected reason to be provided")
	}

	// RecommendedModel is now filled by Router.Route(), not by the classifier directly.
	// Classifier output leaves it empty; Router populates it from config.
}

func TestConfidenceThresholds(t *testing.T) {
	config := ClassifierConfig{
		EnableFastTrack:              true,
		EnableLLMClassification:      false,
		FastTrackConfidenceThreshold: 85,
		L0ConfidenceThreshold:        95,
		L1ConfidenceThreshold:        75,
		L2ConfidenceThreshold:        70,
		L3ConfidenceThreshold:        60,
	}
	classifier := NewDefaultClassifier(config, nil, "", nil)

	ctx := context.Background()

	// This should have high confidence from fast-track
	result, _ := classifier.Classify(ctx, "Explain how closures work in JavaScript")
	if result.Confidence < config.FastTrackConfidenceThreshold {
		t.Logf("Note: low confidence result %d may trigger LLM classification if enabled",
			result.Confidence)
	}
}
