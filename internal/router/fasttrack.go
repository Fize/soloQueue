package router

import (
	"regexp"
	"strings"
)

// FastTrackClassifier implements rule-based classification without LLM
type FastTrackClassifier struct {
	// compiled regexes for performance
	filePathRegex        *regexp.Regexp
	slashCommandRegex    *regexp.Regexp
	conversationKeywords map[string]bool
	singleFileKeywords   map[string]bool
	multiFileKeywords    map[string]bool
	complexKeywords      map[string]bool
	dangerousPatterns    map[string]bool
}

// NewFastTrackClassifier creates and initializes a FastTrackClassifier
func NewFastTrackClassifier() *FastTrackClassifier {
	return &FastTrackClassifier{
		// Match common file paths: /path/to/file, ./file, ../file, file.ext
		// More restrictive: must be at word boundary or start of line
		filePathRegex: regexp.MustCompile(`(?:^|\s)((?:\.{0,2}/[a-zA-Z0-9_.\-/]+(?:\.[a-zA-Z0-9]+)?|[a-zA-Z0-9_][a-zA-Z0-9_.\-/]*\.[a-zA-Z0-9]+))`),
		// Match slash commands: /read, /write, /refactor, etc.
		slashCommandRegex: regexp.MustCompile(`^/([a-z-]+)(?:\s|$)`),
		// L0: Conversation keywords (explanation, design, discussion)
		conversationKeywords: map[string]bool{
			"explain":    true,
			"how":        true,
			"what":       true,
			"why":        true,
			"design":     true,
			"discuss":    true,
			"understand": true,
			"concept":    true,
			"question":   true,
			"clarify":    true,
			"review":     true,
		},
		// L1: Single-file keywords (fix, add, update)
		singleFileKeywords: map[string]bool{
			"fix":    true,
			"add":    true,
			"update": true,
			"change": true,
			"modify": true,
			"write":  true,
			"create": true,
			"delete": true,
			"remove": true,
		},
		// L2: Multi-file / refactoring keywords
		multiFileKeywords: map[string]bool{
			"refactor":   true,
			"implement":  true,
			"migrate":    true,
			"feature":    true,
			"integrate":  true,
			"coordinate": true,
			"across":     true,
			"throughout": true,
			"everywhere": true,
		},
		// L3: Complex / architectural keywords
		complexKeywords: map[string]bool{
			"redesign":      true,
			"architecture":  true,
			"reorganize":    true,
			"rewrite":       true,
			"complete":      true,
			"overhaul":      true,
			"performance":   true,
			"optimization":  true,
			"entire system": true,
			"entire app":    true,
		},
		// Dangerous patterns that require confirmation
		dangerousPatterns: map[string]bool{
			"rm -rf":   true,
			"delete":   true,
			"drop":     true,
			"truncate":  true,
			"destroy":   true,
			"uninstall": true,
			"format":    true,
			"mkfs":      true,
		},
	}
}

// Classify performs fast-track classification on a user prompt
func (ftc *FastTrackClassifier) Classify(prompt string) ClassificationResult {
	result := ClassificationResult{
		Level:                LevelSimpleSingleFile, // default level
		Confidence:           0,
		RequiresConfirmation: false,
	}

	normalized := strings.ToLower(strings.TrimSpace(prompt))

	// Check for slash commands first (highest priority)
	if slashMatch := ftc.slashCommandRegex.FindStringSubmatch(normalized); len(slashMatch) > 1 {
		result.SlashCommand = slashMatch[1]
		return ftc.classifySlashCommand(slashMatch[1], prompt, result)
	}

	// Check for file paths
	filePaths := ftc.extractFilePaths(prompt)
	result.DetectedFilePaths = filePaths
	fileCount := len(filePaths)

	// Analyze keywords and confidence
	result = ftc.analyzeKeywords(normalized, fileCount, result)

	// Check for dangerous patterns
	if ftc.containsDangerousPattern(normalized) {
		result.RequiresConfirmation = true
		result.ConfirmationMessage = "This command appears to involve file deletion or destructive operations. Proceed?"
	}

	result.RecommendedModel = ModelForLevel(result.Level)
	return result
}

// classifySlashCommand handles slash command routing
func (ftc *FastTrackClassifier) classifySlashCommand(cmd string, fullPrompt string, result ClassificationResult) ClassificationResult {
	switch cmd {
	case "read", "cat", "view":
		// Reading a file - single file task
		result.Level = LevelSimpleSingleFile
		result.Confidence = 95
		result.Reason = "Slash command /read indicates single-file read operation"
		result.RecommendedModel = ModelForLevel(result.Level)

	case "write", "edit", "modify":
		// Writing/editing - could be single or multi-file
		// Extract only files mentioned immediately after the command
		filePaths := ftc.extractFilePathsImmediate(fullPrompt, len(cmd)+1)
		if len(filePaths) <= 1 {
			result.Level = LevelSimpleSingleFile
			result.Confidence = 90
			result.Reason = "Slash command /write for single file modification"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 85
			result.Reason = "Slash command /write for multiple files"
		}
		result.RecommendedModel = ModelForLevel(result.Level)

	case "refactor":
		// Refactoring - multi-file or complex depending on scope
		filePaths := ftc.extractFilePaths(fullPrompt)
		if len(filePaths) >= 5 {
			result.Level = LevelComplexRefactoring
			result.Confidence = 90
			result.Reason = "Slash command /refactor with many files indicates complex refactoring"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 85
			result.Reason = "Slash command /refactor indicates multi-file changes"
		}
		result.RecommendedModel = ModelForLevel(result.Level)

	case "implement":
		// Implementation - likely L2 or L3 depending on scope
		if strings.Contains(fullPrompt, "feature") || strings.Contains(fullPrompt, "system") {
			result.Level = LevelComplexRefactoring
			result.Confidence = 80
			result.Reason = "Slash command /implement for feature/system implementation"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 75
			result.Reason = "Slash command /implement indicates multi-part implementation"
		}
		result.RecommendedModel = ModelForLevel(result.Level)

	case "test", "debug":
		// Testing/debugging - typically L1 or L2
		filePaths := ftc.extractFilePaths(fullPrompt)
		if len(filePaths) <= 2 {
			result.Level = LevelSimpleSingleFile
			result.Confidence = 85
			result.Reason = "Slash command /test or /debug for limited scope"
		} else {
			result.Level = LevelMediumMultiFile
			result.Confidence = 80
			result.Reason = "Slash command /test or /debug across multiple files"
		}
		result.RecommendedModel = ModelForLevel(result.Level)

	default:
		// Unknown slash command - default to L1
		result.Level = LevelSimpleSingleFile
		result.Confidence = 50
		result.Reason = "Unknown slash command; defaulting to L1 with low confidence"
		result.RecommendedModel = ModelForLevel(result.Level)
	}

	return result
}

// analyzeKeywords examines the prompt for keywords to determine classification
func (ftc *FastTrackClassifier) analyzeKeywords(normalized string, fileCount int, result ClassificationResult) ClassificationResult {
	// Count keyword occurrences
	conversationScore := ftc.countKeywords(normalized, ftc.conversationKeywords)
	singleFileScore := ftc.countKeywords(normalized, ftc.singleFileKeywords)
	multiFileScore := ftc.countKeywords(normalized, ftc.multiFileKeywords)
	complexScore := ftc.countKeywords(normalized, ftc.complexKeywords)

	// Rules based on file count and keywords
	if conversationScore > 0 && fileCount == 0 && singleFileScore == 0 {
		// Pure conversation: explain/how/what without file operations
		result.Level = LevelConversation
		result.Confidence = 85 + conversationScore*5 // boost with more conversation keywords
		if result.Confidence > 100 {
			result.Confidence = 100
		}
		result.Reason = "Conversation-focused query with explanation keywords and no file operations"
	} else if fileCount == 0 && complexScore == 0 && multiFileScore == 0 {
		// No files, no complex operations - default to L0/L1 conversation
		if conversationScore > 0 {
			result.Level = LevelConversation
			result.Confidence = 80
			result.Reason = "No file operations mentioned; conversation-focused"
		} else {
			result.Level = LevelSimpleSingleFile
			result.Confidence = 50
			result.Reason = "Generic task without explicit file operations; assuming single file"
		}
	} else if fileCount == 1 {
		// Single file explicitly mentioned
		if complexScore > 0 {
			result.Level = LevelMediumMultiFile
			result.Confidence = 70
			result.Reason = "Single file with complex operation keywords; assuming multi-file scope"
		} else {
			result.Level = LevelSimpleSingleFile
			result.Confidence = 90
			result.Reason = "Single file path explicitly mentioned"
		}
	} else if fileCount >= 2 && fileCount <= 5 {
		// 2-5 files: L2 territory
		result.Level = LevelMediumMultiFile
		result.Confidence = 80 + (fileCount-2)*2 // boost confidence with more files
		result.Reason = "Multiple files mentioned; multi-file task"
	} else if fileCount > 5 {
		// 5+ files or vague large-scale operations: L3
		result.Level = LevelComplexRefactoring
		result.Confidence = 85
		result.Reason = "Large number of files mentioned; complex refactoring"
	}

	// Boost confidence if multi-file keywords present
	if multiFileScore > 0 && result.Level < LevelMediumMultiFile {
		result.Level = LevelMediumMultiFile
		result.Confidence = 75
		result.Reason = "Multi-file operation keywords detected"
	}

	// Boost confidence if complex keywords present
	if complexScore > 0 && result.Level < LevelComplexRefactoring {
		result.Level = LevelComplexRefactoring
		result.Confidence = 70
		result.Reason = "Complex operation keywords detected"
	}

	return result
}

// extractFilePaths finds all file paths in the prompt
func (ftc *FastTrackClassifier) extractFilePaths(prompt string) []string {
	matches := ftc.filePathRegex.FindAllStringSubmatch(prompt, -1)
	if len(matches) == 0 {
		return []string{}
	}

	// Deduplicate and clean up paths (matches[i][1] is the captured group)
	seen := make(map[string]bool)
	var paths []string
	for _, m := range matches {
		if len(m) > 1 {
			clean := strings.TrimSpace(m[1])
			if !seen[clean] && clean != "" {
				paths = append(paths, clean)
				seen[clean] = true
			}
		}
	}
	return paths
}

// extractFilePathsImmediate extracts file paths from the start of a position (used for slash commands)
// This looks for files mentioned right after the command, before descriptive text
func (ftc *FastTrackClassifier) extractFilePathsImmediate(prompt string, startPos int) []string {
	if startPos >= len(prompt) {
		return []string{}
	}

	// Extract just the first line or first sentence after the command
	remainder := prompt[startPos:]
	// Find the first sentence (up to period, newline, or reasonable length)
	firstSentence := remainder
	for i, r := range remainder {
		if r == '.' || r == '\n' || r == '?' || i > 100 {
			firstSentence = remainder[:i]
			break
		}
	}

	return ftc.extractFilePaths(firstSentence)
}

// countKeywords counts how many keywords from a set appear in text
func (ftc *FastTrackClassifier) countKeywords(text string, keywords map[string]bool) int {
	count := 0
	for keyword := range keywords {
		// Count word boundaries to avoid partial matches
		if strings.Contains(text, keyword) {
			// Simple heuristic: count occurrences
			count += strings.Count(text, keyword)
		}
	}
	return count
}

// containsDangerousPattern checks if the prompt contains dangerous operations
func (ftc *FastTrackClassifier) containsDangerousPattern(normalized string) bool {
	for pattern := range ftc.dangerousPatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}
