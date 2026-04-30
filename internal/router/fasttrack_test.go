package router

import (
	"testing"
)

func TestFastTrackClassifier_Classify(t *testing.T) {
	ftc := NewFastTrackClassifier()

	tests := []struct {
		name              string
		prompt            string
		expectedLevel     ClassificationLevel
		minConfidence     int
		shouldRequireConf bool
	}{
		{
			name:          "L0: Explain JavaScript closures",
			prompt:        "Explain how closures work in JavaScript",
			expectedLevel: LevelConversation,
			minConfidence: 80,
		},
		{
			name:          "L0: Design question",
			prompt:        "What's the best design pattern for handling errors in Go?",
			expectedLevel: LevelConversation,
			minConfidence: 80,
		},
		{
			name:          "L0: Concept review",
			prompt:        "Review this architectural concept: should we use microservices?",
			expectedLevel: LevelConversation,
			minConfidence: 75,
		},
		{
			name:          "L1: Fix single file",
			prompt:        "Fix the null pointer bug on line 42 of main.go",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 85,
		},
		{
			name:          "L1: Add to single file",
			prompt:        "Add type annotation to the function in service.ts",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 75,
		},
		{
			name:          "L1: /read command",
			prompt:        "/read /path/to/auth.js",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 90,
		},
		{
			name:          "L1: /write command single file",
			prompt:        "/write main.go Fix the null pointer",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 85,
		},
		{
			name:          "L2: Multiple files with paths",
			prompt:        "Update auth.go, middleware.go, and create login.tsx to add user authentication",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 75,
		},
		{
			name:          "L2: Refactor mentioned",
			prompt:        "Refactor the data layer across models.go, dal.go, and service.go",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 75,
		},
		{
			name:          "L2: /refactor command",
			prompt:        "/refactor models.go dal.go service.go",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 80,
		},
		{
			name:          "L3: Many files mentioned",
			prompt:        "Refactor error handling across server.go handler.go middleware.go auth.go db.go utils.go logging.go",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 75,
		},
		{
			name:          "L3: /refactor with many files",
			prompt:        "/refactor api.go handler.go service.go repo.go model.go dto.go config.go main.go",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 85,
		},
		{
			name:          "L3: Architecture changes",
			prompt:        "Redesign the entire authentication system from scratch",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 60,
		},
		{
			name:              "Dangerous operation confirmation",
			prompt:            "Delete all files in the database folder",
			expectedLevel:     LevelSimpleSingleFile,
			minConfidence:     40,
			shouldRequireConf: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ftc.Classify(tt.prompt)

			if result.Level != tt.expectedLevel {
				t.Errorf("expected level %v, got %v (confidence: %d)",
					tt.expectedLevel, result.Level, result.Confidence)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("expected confidence >= %d, got %d",
					tt.minConfidence, result.Confidence)
			}

			if tt.shouldRequireConf && !result.RequiresConfirmation {
				t.Errorf("expected confirmation required, but got none")
			}
		})
	}
}

func TestFastTrackClassifier_ExtractFilePaths(t *testing.T) {
	ftc := NewFastTrackClassifier()

	tests := []struct {
		name      string
		prompt    string
		wantCount int
	}{
		{
			name:      "No paths",
			prompt:    "Explain how closures work",
			wantCount: 0,
		},
		{
			name:      "Single path",
			prompt:    "Fix the bug in main.go",
			wantCount: 1,
		},
		{
			name:      "Multiple paths",
			prompt:    "Update auth.go, middleware.go, and service.go",
			wantCount: 3,
		},
		{
			name:      "Absolute paths",
			prompt:    "Fix /src/main.go and /src/utils/helper.go",
			wantCount: 2,
		},
		{
			name:      "Relative paths",
			prompt:    "Update ./package.json and ../config/settings.toml",
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := ftc.extractFilePaths(tt.prompt)
			if len(paths) != tt.wantCount {
				t.Errorf("expected %d paths, got %d: %v",
					tt.wantCount, len(paths), paths)
			}
		})
	}
}

func TestFastTrackClassifier_SlashCommands(t *testing.T) {
	ftc := NewFastTrackClassifier()

	tests := []struct {
		name              string
		prompt            string
		expectedCommand   string
		expectedLevel     ClassificationLevel
		minConfidence     int
	}{
		{
			name:            "/read command",
			prompt:          "/read main.go",
			expectedCommand: "read",
			expectedLevel:   LevelSimpleSingleFile,
			minConfidence:   90,
		},
		{
			name:            "/write command single file",
			prompt:          "/write main.go Fix the type error",
			expectedCommand: "write",
			expectedLevel:   LevelSimpleSingleFile,
			minConfidence:   85,
		},
		{
			name:            "/refactor command",
			prompt:          "/refactor api.go handler.go service.go",
			expectedCommand: "refactor",
			expectedLevel:   LevelMediumMultiFile,
			minConfidence:   80,
		},
		{
			name:            "/implement feature",
			prompt:          "/implement feature authentication system",
			expectedCommand: "implement",
			expectedLevel:   LevelComplexRefactoring,
			minConfidence:   70,
		},
		{
			name:            "/test with multiple files",
			prompt:          "/test auth_test.go main_test.go handler_test.go",
			expectedCommand: "test",
			expectedLevel:   LevelMediumMultiFile,
			minConfidence:   75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ftc.Classify(tt.prompt)

			if result.SlashCommand != tt.expectedCommand {
				t.Errorf("expected command %q, got %q",
					tt.expectedCommand, result.SlashCommand)
			}

			if result.Level != tt.expectedLevel {
				t.Errorf("expected level %v, got %v",
					tt.expectedLevel, result.Level)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("expected confidence >= %d, got %d",
					tt.minConfidence, result.Confidence)
			}
		})
	}
}

func TestModelForLevel(t *testing.T) {
	tests := []struct {
		level         ClassificationLevel
		expectedModel string
	}{
		{LevelConversation, "deepseek:deepseek-v4-flash"},
		{LevelSimpleSingleFile, "deepseek:deepseek-v4-flash-thinking"},
		{LevelMediumMultiFile, "deepseek:deepseek-v4-pro"},
		{LevelComplexRefactoring, "deepseek:deepseek-v4-pro-max"},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			model := ModelForLevel(tt.level)
			if model != tt.expectedModel {
				t.Errorf("expected model %q, got %q",
					tt.expectedModel, model)
			}
		})
	}
}
