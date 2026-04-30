package router

import (
	"context"
	"log/slog"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// Classifier is the main interface for task classification
type Classifier interface {
	// Classify analyzes a user prompt and returns a classification result
	Classify(ctx context.Context, prompt string) (ClassificationResult, error)
}

// DefaultClassifier combines fast-track rules with optional LLM validation
type DefaultClassifier struct {
	config    ClassifierConfig
	fastTrack *FastTrackClassifier
	llm       *LLMClassifier // nil when LLM classification is disabled
	logger    *slog.Logger
}

// NewDefaultClassifier creates a new classifier with the given configuration.
//
// Parameters:
//   - config: classifier behavior settings (thresholds, feature flags)
//   - llmClient: shared LLM client for semantic fallback (nil = disable LLM)
//   - model: API model name for the LLM classifier (used only if llmClient != nil)
//   - logger: optional logger (nil = slog.Default())
func NewDefaultClassifier(config ClassifierConfig, llmClient agent.LLMClient, model string, logger *slog.Logger) *DefaultClassifier {
	if logger == nil {
		logger = slog.Default()
	}

	var lc *LLMClassifier
	if config.EnableLLMClassification && llmClient != nil && model != "" {
		lc = NewLLMClassifier(llmClient, model, logger)
	}

	return &DefaultClassifier{
		config:    config,
		fastTrack: NewFastTrackClassifier(),
		llm:       lc,
		logger:    logger,
	}
}

// Classify implements the Classifier interface
//
// Classification strategy (dual-channel):
//  1. Run fast-track rules (always, if enabled)
//  2. If confidence >= FastTrackConfidenceThreshold → use the result (fast path)
//  3. If confidence < threshold AND LLM classification enabled → run LLM
//  4. Return whichever result has higher confidence
//
// Fast-track is always preferred when confident because it has
// zero latency and zero token cost. LLM is the fallback for ambiguous cases.
func (dc *DefaultClassifier) Classify(ctx context.Context, prompt string) (ClassificationResult, error) {
	if !dc.config.EnableFastTrack {
		// LLM-only mode (not typical)
		if dc.llm != nil {
			return dc.llm.Classify(ctx, prompt)
		}
		return ClassificationResult{
			Level:      LevelSimpleSingleFile,
			Reason:     "Fast-track disabled, no LLM available",
			Confidence: 0,
		}, nil
	}

	// Step 1: Run fast-track classifier
	ftResult := dc.fastTrack.Classify(prompt)

	dc.logger.DebugContext(ctx, "fast-track classification",
		slog.String("level", ftResult.Level.String()),
		slog.Int("confidence", ftResult.Confidence),
		slog.String("reason", ftResult.Reason),
	)

	// Step 2: Check if confidence is sufficient
	if ftResult.Confidence >= dc.config.FastTrackConfidenceThreshold {
		dc.logger.DebugContext(ctx, "classification complete (fast-track sufficient)",
			slog.String("level", ftResult.Level.String()),
			slog.Int("confidence", ftResult.Confidence),
		)
		return ftResult, nil
	}

	// Step 3: LLM classification as fallback (only when fast-track is uncertain)
	if !dc.config.EnableLLMClassification || dc.llm == nil {
		dc.logger.DebugContext(ctx, "classification complete (low confidence, LLM unavailable)",
			slog.String("level", ftResult.Level.String()),
			slog.Int("confidence", ftResult.Confidence),
		)
		return ftResult, nil
	}

	dc.logger.DebugContext(ctx, "fast-track uncertain, invoking LLM fallback",
		slog.Int("ft_confidence", ftResult.Confidence),
		slog.Int("threshold", dc.config.FastTrackConfidenceThreshold),
	)

	llmResult, err := dc.llm.Classify(ctx, prompt)
	if err != nil {
		// LLM error: use fast-track result regardless of confidence
		dc.logger.DebugContext(ctx, "LLM classifier error, using fast-track",
			slog.String("err", err.Error()),
		)
		return ftResult, nil
	}

	// Step 4: Use whichever result has higher confidence
	if llmResult.Confidence > ftResult.Confidence {
		dc.logger.DebugContext(ctx, "classification complete (LLM override)",
			slog.String("level", llmResult.Level.String()),
			slog.Int("confidence", llmResult.Confidence),
			slog.String("reason", llmResult.Reason),
		)
		// Preserve RequiresConfirmation from fast-track (safety check)
		if ftResult.RequiresConfirmation {
			llmResult.RequiresConfirmation = true
			llmResult.ConfirmationMessage = ftResult.ConfirmationMessage
		}
		return llmResult, nil
	}

	dc.logger.DebugContext(ctx, "classification complete (fast-track preferred over LLM)",
		slog.String("level", ftResult.Level.String()),
		slog.Int("ft_confidence", ftResult.Confidence),
		slog.Int("llm_confidence", llmResult.Confidence),
	)
	return ftResult, nil
}
