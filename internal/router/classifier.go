package router

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Classifier is the main interface for task classification
type Classifier interface {
	// Classify analyzes a user prompt and returns a classification result.
	// priorLevel is the session's current task level (LevelUnknown if none).
	Classify(ctx context.Context, prompt string, priorLevel ClassificationLevel) (ClassificationResult, error)
}

// DefaultClassifier combines fast-track rules with optional LLM validation
type DefaultClassifier struct {
	config    ClassifierConfig
	fastTrack *FastTrackClassifier
	llm       *LLMClassifier // nil when LLM classification is disabled
	logger    *logger.Logger
}

// NewDefaultClassifier creates a new classifier with the given configuration.
//
// Parameters:
//   - config: classifier behavior settings (thresholds, feature flags)
//   - llmClient: shared LLM client for semantic fallback (nil = disable LLM)
//   - model: API model name for the LLM classifier (used only if llmClient != nil)
//   - logger: optional logger (nil = default System-layer logger)
func NewDefaultClassifier(config ClassifierConfig, llmClient agent.LLMClient, model string, l *logger.Logger) *DefaultClassifier {
	if l == nil {
		// Create a minimal system-layer logger for classification
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}

	var lc *LLMClassifier
	if config.EnableLLMClassification && llmClient != nil && model != "" {
		lc = NewLLMClassifier(llmClient, model, l)
	}

	return &DefaultClassifier{
		config:    config,
		fastTrack: NewFastTrackClassifier(),
		llm:       lc,
		logger:    l,
	}
}

// Classify implements the Classifier interface
//
// Classification strategy (dual-channel + session sticky):
//  1. Run fast-track rules (always, if enabled)
//  2. If confidence >= FastTrackConfidenceThreshold → use the result (fast path)
//  3. If confidence < threshold AND LLM classification enabled → run LLM
//  4. Return whichever result has higher confidence
//  5. Apply hybrid sticky logic: if priorLevel is set and confidence is uncertain,
//     inherit or merge with the prior level to prevent level oscillation.
//
// Fast-track is always preferred when confident because it has
// zero latency and zero token cost. LLM is the fallback for ambiguous cases.
func (dc *DefaultClassifier) Classify(ctx context.Context, prompt string, priorLevel ClassificationLevel) (ClassificationResult, error) {
	if !dc.config.EnableFastTrack {
		// LLM-only mode (not typical)
		if dc.llm != nil {
			result, err := dc.llm.Classify(ctx, prompt, priorLevel)
			if err == nil {
				result = dc.applyHybrid(result, priorLevel)
			}
			return result, err
		}
		return ClassificationResult{
			Level:      LevelSimpleSingleFile,
			Reason:     "Fast-track disabled, no LLM available",
			Confidence: 0,
		}, nil
	}

	// Step 1: Run fast-track classifier
	ftResult := dc.fastTrack.Classify(prompt)

	dc.logger.DebugContext(ctx, logger.CatApp, "fast-track classification",
		"level", ftResult.Level.String(),
		"confidence", ftResult.Confidence,
		"reason", ftResult.Reason,
	)

	// Step 2: Check if confidence is sufficient
	if ftResult.Confidence >= dc.config.FastTrackConfidenceThreshold {
		dc.logger.DebugContext(ctx, logger.CatApp, "classification complete (fast-track sufficient)",
			"level", ftResult.Level.String(),
			"confidence", ftResult.Confidence,
		)
		return dc.applyHybrid(ftResult, priorLevel), nil
	}

	// Step 3: LLM classification as fallback (only when fast-track is uncertain)
	if !dc.config.EnableLLMClassification || dc.llm == nil {
		dc.logger.DebugContext(ctx, logger.CatApp, "classification complete (low confidence, LLM unavailable)",
			"level", ftResult.Level.String(),
			"confidence", ftResult.Confidence,
		)
		return dc.applyHybrid(ftResult, priorLevel), nil
	}

	dc.logger.DebugContext(ctx, logger.CatApp, "fast-track uncertain, invoking LLM fallback",
		"ft_confidence", ftResult.Confidence,
		"threshold", dc.config.FastTrackConfidenceThreshold,
	)

	llmResult, err := dc.llm.Classify(ctx, prompt, priorLevel)
	if err != nil {
		// LLM error: use fast-track result regardless of confidence
		dc.logger.DebugContext(ctx, logger.CatApp, "LLM classifier error, using fast-track",
			"err", err.Error(),
		)
		return dc.applyHybrid(ftResult, priorLevel), nil
	}

	// Step 4: Use whichever result has higher confidence
	var finalResult ClassificationResult
	if llmResult.Confidence > ftResult.Confidence {
		dc.logger.DebugContext(ctx, logger.CatApp, "classification complete (LLM override)",
			"level", llmResult.Level.String(),
			"confidence", llmResult.Confidence,
			"reason", llmResult.Reason,
		)
		// Preserve RequiresConfirmation from fast-track (safety check)
		if ftResult.RequiresConfirmation {
			llmResult.RequiresConfirmation = true
			llmResult.ConfirmationMessage = ftResult.ConfirmationMessage
		}
		finalResult = llmResult
	} else {
		dc.logger.DebugContext(ctx, logger.CatApp, "classification complete (fast-track preferred over LLM)",
			"level", ftResult.Level.String(),
			"ft_confidence", ftResult.Confidence,
			"llm_confidence", llmResult.Confidence,
		)
		finalResult = ftResult
	}

	// Step 5: Apply hybrid sticky logic
	return dc.applyHybrid(finalResult, priorLevel), nil
}

// applyHybrid is a convenience wrapper that applies hybrid logic only when prior is set.
func (dc *DefaultClassifier) applyHybrid(result ClassificationResult, priorLevel ClassificationLevel) ClassificationResult {
	if priorLevel != LevelUnknown {
		return dc.applyHybridLogic(result, priorLevel)
	}
	return result
}

// applyHybridLogic adjusts the classification result based on the session's
// prior task level to prevent level oscillation for short follow-up messages.
//
// Rules:
//   - Confidence >= threshold (default 85): use new result (clear signal)
//   - Complex task protection: if priorLevel >= L2 and new is L0, require
//     confidence >= 96 to downgrade (prevents follow-up questions from
//     accidentally resetting a complex task session)
//   - Confidence >= 50: use max(new, prior) — stay at the higher level
//   - Confidence < 50: inherit prior level (unclear signal → keep context)
func (dc *DefaultClassifier) applyHybridLogic(result ClassificationResult, priorLevel ClassificationLevel) ClassificationResult {
	threshold := dc.config.FastTrackConfidenceThreshold
	if threshold <= 0 {
		threshold = 85
	}

	// Complex task session continuity: prevent accidental downgrade to L0
	// when the user asks follow-up questions about an ongoing complex task.
	// Follow-up questions naturally contain L0 keywords (解释,为什么,etc.)
	// that score high enough to falsely trigger L0. Require a very strong
	// conversation signal (confidence >= 96) to override a complex session.
	if priorLevel >= LevelMediumMultiFile && result.Level <= LevelConversation {
		if result.Confidence < 96 {
			result.Level = LevelSimpleSingleFile
			result.Reason += "; complex session: L0 prevented, min L1 maintained"
			result.Confidence = min(result.Confidence, 45)
			return result
		}
	}

	if result.Confidence >= threshold {
		// High confidence: new classification always wins (clear signal)
		return result
	}

	if result.Confidence >= 50 {
		// Medium confidence: take the higher level to avoid accidental downgrade
		if priorLevel > result.Level {
			result.Level = priorLevel
			result.Reason += "; inherited session level (medium confidence)"
		}
		return result
	}

	// Low confidence: inherit prior level entirely
	result.Level = priorLevel
	result.Reason += "; inherited session level (low confidence)"
	return result
}
