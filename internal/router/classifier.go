package router

import (
	"context"
	"log/slog"
)

// Classifier is the main interface for task classification
type Classifier interface {
	// Classify analyzes a user prompt and returns a classification result
	Classify(ctx context.Context, prompt string) (ClassificationResult, error)
}

// DefaultClassifier combines fast-track rules with optional LLM validation
type DefaultClassifier struct {
	config           ClassifierConfig
	fastTrack        *FastTrackClassifier
	logger           *slog.Logger
}

// NewDefaultClassifier creates a new classifier with the given configuration
func NewDefaultClassifier(config ClassifierConfig, logger *slog.Logger) *DefaultClassifier {
	if logger == nil {
		logger = slog.Default()
	}

	return &DefaultClassifier{
		config:    config,
		fastTrack: NewFastTrackClassifier(),
		logger:    logger,
	}
}

// Classify implements the Classifier interface
//
// Classification strategy:
//   1. Run fast-track rules (always, if enabled)
//   2. If confidence >= FastTrackConfidenceThreshold, use the result
//   3. If confidence < threshold and LLM classification enabled, run LLM classification
//   4. Return whichever result has higher confidence
//
// Fast-track is always preferred if confidence is sufficient because it has
// lower latency and token cost.
func (dc *DefaultClassifier) Classify(ctx context.Context, prompt string) (ClassificationResult, error) {
	if !dc.config.EnableFastTrack {
		// LLM-only mode (not typical)
		return ClassificationResult{
			Level:    LevelSimpleSingleFile, // safe default
			Reason:   "Fast-track disabled",
			Confidence: 0,
		}, nil
	}

	// Step 1: Run fast-track classifier
	ftResult := dc.fastTrack.Classify(prompt)

	dc.logger.DebugContext(ctx, "fast-track classification",
		slog.String("level", ftResult.Level.String()),
		slog.Int("confidence", ftResult.Confidence),
	)

	// Step 2: Check if confidence is sufficient
	if ftResult.Confidence >= dc.config.FastTrackConfidenceThreshold {
		dc.logger.DebugContext(ctx, "classification complete (fast-track)",
			slog.String("level", ftResult.Level.String()),
			slog.Int("confidence", ftResult.Confidence),
		)
		return ftResult, nil
	}

	// Step 3: LLM classification as fallback (not implemented yet)
	// For now, we return the fast-track result even if confidence is low
	if !dc.config.EnableLLMClassification {
		dc.logger.DebugContext(ctx, "classification complete (low confidence, LLM disabled)",
			slog.String("level", ftResult.Level.String()),
			slog.Int("confidence", ftResult.Confidence),
		)
		return ftResult, nil
	}

	// TODO: Implement LLM-based classification here
	// This would call an LLM to semantically understand the task and provide
	// better classification for ambiguous cases.

	dc.logger.DebugContext(ctx, "classification complete (fast-track with low confidence)",
		slog.String("level", ftResult.Level.String()),
		slog.Int("confidence", ftResult.Confidence),
	)

	return ftResult, nil
}
