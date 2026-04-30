// Package router implements intelligent task routing and classification.
//
// The Task Router Classifier (TRC) is a dual-channel system that routes user
// input to appropriate processing levels (L0, L1, L2, L3) while minimizing
// latency and token cost. It combines:
//
//   - Fast-track rules (hardcoded local logic) for common cases
//   - LLM-based classification (semantic understanding) for ambiguous cases
//   - Safety checks to prevent dangerous operations
//   - Explicit overrides (slash commands) to bypass classification
package router

// ClassificationLevel represents the routing level for a user task
type ClassificationLevel int

const (
	// LevelUnknown indicates classification failed
	LevelUnknown ClassificationLevel = iota

	// LevelConversation (L0): General discussion, no tools
	// Model: flash (fast, low-cost)
	// Thinking: disabled
	LevelConversation

	// LevelSimpleSingleFile (L1): Single file modification or analysis
	// Model: flash-thinking (fast + reasoning)
	// Thinking: high
	LevelSimpleSingleFile

	// LevelMediumMultiFile (L2): 2-5 files across related modules
	// Model: pro (balanced capability/cost)
	// Thinking: high
	LevelMediumMultiFile

	// LevelComplexRefactoring (L3): 5+ files or architectural changes
	// Model: pro-max (expert capability)
	// Thinking: max
	LevelComplexRefactoring
)

// String returns the human-readable name of the classification level
func (cl ClassificationLevel) String() string {
	switch cl {
	case LevelConversation:
		return "L0-Conversation"
	case LevelSimpleSingleFile:
		return "L1-SimpleSingleFile"
	case LevelMediumMultiFile:
		return "L2-MediumMultiFile"
	case LevelComplexRefactoring:
		return "L3-ComplexRefactoring"
	default:
		return "Unknown"
	}
}

// ModelForLevel returns the recommended model name for a given classification level.
// This is used for informational purposes (ClassificationResult.RecommendedModel).
func ModelForLevel(level ClassificationLevel) string {
	switch level {
	case LevelConversation:
		return "deepseek-v4-flash"
	case LevelSimpleSingleFile:
		return "deepseek-v4-flash-thinking"
	case LevelMediumMultiFile:
		return "deepseek-v4-pro"
	case LevelComplexRefactoring:
		return "deepseek-v4-pro-max"
	default:
		return "deepseek-v4-flash" // fallback
	}
}

// ClassificationResult holds the output of task classification
type ClassificationResult struct {
	// Level is the determined routing level
	Level ClassificationLevel

	// Confidence is a score from 0-100 indicating classification confidence
	Confidence int

	// RecommendedModel is the suggested model for this task
	RecommendedModel string

	// Reason provides a human-readable explanation of the classification
	Reason string

	// DetectedFilePaths are files mentioned in the input (if any)
	DetectedFilePaths []string

	// SlashCommand is non-empty if input starts with a slash command
	SlashCommand string

	// RequiresConfirmation indicates if this task needs user confirmation
	RequiresConfirmation bool

	// ConfirmationMessage provides context for confirmation if needed
	ConfirmationMessage string
}

// ClassifierConfig holds configuration for the task router
type ClassifierConfig struct {
	// EnableFastTrack enables hardcoded fast-track rules
	EnableFastTrack bool

	// EnableLLMClassification enables LLM-based semantic classification
	EnableLLMClassification bool

	// FastTrackConfidenceThreshold is the confidence level (0-100) above which
	// fast-track rules are sufficient. Below this, LLM classification is attempted.
	FastTrackConfidenceThreshold int

	// L0ConfidenceThreshold is the minimum confidence for L0 (conversation) classification
	L0ConfidenceThreshold int

	// L1ConfidenceThreshold is the minimum confidence for L1 (single file) classification
	L1ConfidenceThreshold int

	// L2ConfidenceThreshold is the minimum confidence for L2 (multi-file) classification
	L2ConfidenceThreshold int

	// L3ConfidenceThreshold is the minimum confidence for L3 (complex) classification
	L3ConfidenceThreshold int
}

// DefaultClassifierConfig returns reasonable default configuration
func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{
		EnableFastTrack:              true,
		EnableLLMClassification:      true,
		FastTrackConfidenceThreshold: 85,
		L0ConfidenceThreshold:        95,
		L1ConfidenceThreshold:        75,
		L2ConfidenceThreshold:        70,
		L3ConfidenceThreshold:        60,
	}
}
