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
		// ── L0: Conversation / Q&A ──
		{
			name:          "L0: Explain concept",
			prompt:        "Explain how closures work in JavaScript",
			expectedLevel: LevelConversation,
			minConfidence: 60,
		},
		{
			name:          "L0: What is question",
			prompt:        "What is the difference between goroutines and threads?",
			expectedLevel: LevelConversation,
			minConfidence: 60,
		},
		{
			name:          "L0: Chinese explanation",
			prompt:        "解释一下什么是依赖注入",
			expectedLevel: LevelConversation,
			minConfidence: 60,
		},
		{
			name:          "L0: Help me understand",
			prompt:        "Help me understand how JWT tokens work",
			expectedLevel: LevelConversation,
			minConfidence: 60,
		},
		{
			name:          "L0: Chinese question pattern",
			prompt:        "微服务和单体架构有什么区别",
			expectedLevel: LevelConversation,
			minConfidence: 60,
		},
		// ── L1: Simple single-step tasks ──
		{
			name:          "L1: Fix single file",
			prompt:        "Fix the null pointer bug on line 42 of main.go",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 60,
		},
		{
			name:          "L1: Add something simple",
			prompt:        "Add a type annotation to the function in service.ts",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 60,
		},
		{
			name:          "L1: Chinese simple task",
			prompt:        "修复 auth.go 里的空指针问题",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 60,
		},
		{
			name:          "L1: Typo fix",
			prompt:        "Fix the typo in README.md",
			expectedLevel: LevelSimpleSingleFile,
			minConfidence: 60,
		},
		// ── L2: Multi-step / coordination ──
		{
			name:          "L2: Refactor keyword",
			prompt:        "Refactor the data layer to use repository pattern",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 60,
		},
		{
			name:          "L2: Multiple files mentioned",
			prompt:        "Update auth.go, middleware.go, and handler.go to add user authentication",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 50, // file count boost + multi-file signals
		},
		{
			name:          "L2: Chinese refactor",
			prompt:        "重构数据层，实现功能模块化",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 60,
		},
		{
			name:          "L2: Implement feature",
			prompt:        "Implement a new caching workflow with Redis integration",
			expectedLevel: LevelMediumMultiFile,
			minConfidence: 60,
		},
		// ── L3: Complex / deep-reasoning ──
		{
			name:          "L3: Rewrite from scratch",
			prompt:        "Rewrite the entire authentication system from scratch",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 60,
		},
		{
			name:          "L3: Architecture redesign",
			prompt:        "Redesign the system architecture for high availability",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 60,
		},
		{
			name:          "L3: Chinese complex",
			prompt:        "从零开始重新设计整个系统的分布式架构",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 60,
		},
		{
			name:          "L3: Root cause investigation",
			prompt:        "Investigate the root cause of the race condition in the distributed lock",
			expectedLevel: LevelComplexRefactoring,
			minConfidence: 60,
		},
		// ── Dangerous operations ──
		{
			name:              "Dangerous: rm -rf",
			prompt:            "Run rm -rf on the old backup directory",
			expectedLevel:     LevelSimpleSingleFile,
			minConfidence:     0,
			shouldRequireConf: true,
		},
		{
			name:              "Dangerous: drop table",
			prompt:            "Drop table users from the database",
			shouldRequireConf: true,
			expectedLevel:     LevelMediumMultiFile, // "database" triggers L2 signal
			minConfidence:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ftc.Classify(tt.prompt)

			if result.Level != tt.expectedLevel {
				t.Errorf("Level: got %v, want %v (confidence=%d, reason=%q)",
					result.Level, tt.expectedLevel, result.Confidence, result.Reason)
			}

			if result.Confidence < tt.minConfidence {
				t.Errorf("Confidence: got %d, want >= %d",
					result.Confidence, tt.minConfidence)
			}

			if tt.shouldRequireConf && !result.RequiresConfirmation {
				t.Errorf("expected RequiresConfirmation=true, got false")
			}
		})
	}
}

func TestFastTrackClassifier_SlashCommandOverrides(t *testing.T) {
	ftc := NewFastTrackClassifier()

	tests := []struct {
		name          string
		prompt        string
		expectedLevel ClassificationLevel
		expectedConf  int
	}{
		{
			name:          "/l0 force conversation",
			prompt:        "/l0 tell me about Go generics",
			expectedLevel: LevelConversation,
			expectedConf:  100,
		},
		{
			name:          "/chat force conversation",
			prompt:        "/chat hey how are you",
			expectedLevel: LevelConversation,
			expectedConf:  100,
		},
		{
			name:          "/l1 force simple",
			prompt:        "/l1 fix the bug in auth.go",
			expectedLevel: LevelSimpleSingleFile,
			expectedConf:  100,
		},
		{
			name:          "/l2 force multi-step",
			prompt:        "/l2 refactor the authentication module",
			expectedLevel: LevelMediumMultiFile,
			expectedConf:  100,
		},
		{
			name:          "/l3 force complex",
			prompt:        "/l3 redesign the entire system",
			expectedLevel: LevelComplexRefactoring,
			expectedConf:  100,
		},
		{
			name:          "/max force expert",
			prompt:        "/max investigate the performance issue",
			expectedLevel: LevelComplexRefactoring,
			expectedConf:  100,
		},
		{
			name:          "/expert force expert",
			prompt:        "/expert analyze race conditions",
			expectedLevel: LevelComplexRefactoring,
			expectedConf:  100,
		},
		{
			name:          "/fast force fast",
			prompt:        "/fast what is a pointer",
			expectedLevel: LevelConversation,
			expectedConf:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ftc.Classify(tt.prompt)

			if result.Level != tt.expectedLevel {
				t.Errorf("Level: got %v, want %v", result.Level, tt.expectedLevel)
			}
			if result.Confidence != tt.expectedConf {
				t.Errorf("Confidence: got %d, want %d", result.Confidence, tt.expectedConf)
			}
		})
	}
}

func TestFastTrackClassifier_Escalation(t *testing.T) {
	ftc := NewFastTrackClassifier()

	tests := []struct {
		name          string
		prompt        string
		expectedLevel ClassificationLevel
	}{
		{
			name:          "Escalation: Chinese 仔细想 bumps L1→L2",
			prompt:        "仔细想一下怎么修复 auth.go 里的 bug",
			expectedLevel: LevelMediumMultiFile, // L1 + 1 = L2
		},
		{
			name:          "Escalation: think carefully bumps up",
			prompt:        "Think carefully about how to fix the login bug in auth.go",
			expectedLevel: LevelMediumMultiFile, // L1 + 1 (think carefully) = L2
		},
		{
			name:          "Escalation: deeply + in depth = +2",
			prompt:        "Do a deeply in depth review of the error handling",
			expectedLevel: LevelComplexRefactoring, // bumped +2
		},
		{
			name:          "De-escalation: simple/quick lowers",
			prompt:        "Just quickly fix the typo",
			expectedLevel: LevelConversation, // L1 - 1 = L0 (clamped)
		},
		{
			name:          "De-escalation: Chinese 简单",
			prompt:        "简单改改这个文件的格式",
			expectedLevel: LevelConversation, // L1 - 1 = L0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ftc.Classify(tt.prompt)

			if result.Level != tt.expectedLevel {
				t.Errorf("Level: got %v, want %v (confidence=%d, reason=%q)",
					result.Level, tt.expectedLevel, result.Confidence, result.Reason)
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

func TestFastTrackClassifier_EmptyInput(t *testing.T) {
	ftc := NewFastTrackClassifier()

	result := ftc.Classify("")
	if result.Level != LevelConversation {
		t.Errorf("empty input should be L0, got %v", result.Level)
	}
	if result.Confidence < 90 {
		t.Errorf("empty input should have high confidence, got %d", result.Confidence)
	}
}
