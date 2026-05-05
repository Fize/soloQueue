package router

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ModelService provides model lookups by role
type ModelService interface {
	// DefaultModelByRole returns the model for a given role (expert, superior, universal, fast)
	DefaultModelByRole(role string) *config.LLMModel
}

// Router coordinates task classification with model selection
//
// It takes a user prompt, classifies it, and returns:
// - The classification level (for routing/delegation decisions)
// - The recommended model ID (for agent selection)
// - Any metadata (detected files, warnings, etc.)
type Router struct {
	classifier   Classifier
	modelService ModelService
	logger       *logger.Logger
}

// NewRouter creates a new Router instance
func NewRouter(
	classifier Classifier,
	modelService ModelService,
	l *logger.Logger,
) *Router {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}

	return &Router{
		classifier:   classifier,
		modelService: modelService,
		logger:       l,
	}
}

// RouteDecision represents the router's decision about how to handle a task
type RouteDecision struct {
	// Level is the determined routing level
	Level ClassificationLevel

	// ProviderID identifies which LLM provider to use (e.g., "deepseek")
	ProviderID string

	// ModelID is the recommended API model name (e.g., "deepseek-v4-pro")
	ModelID string

	// ThinkingEnabled indicates whether the model should use thinking/reasoning mode
	ThinkingEnabled bool

	// ReasoningEffort specifies the reasoning depth: "high", "max", or "" (disabled)
	ReasoningEffort string

	// Classification contains the full classification result
	Classification ClassificationResult

	// Warnings contains any warnings (e.g., dangerous operations requiring confirmation)
	Warnings []string
}

// Route analyzes a user prompt and returns a routing decision.
//
// priorLevel is the session's current task level (LevelUnknown if none).
// When set, the classifier applies hybrid sticky logic to prevent level
// oscillation for short follow-up messages within a larger task context.
//
// The decision includes:
// - The classification level (L0-L3)
// - The recommended model ID resolved from config
// - Thinking configuration (enabled + effort level)
// - Any warnings or special handling notes
func (r *Router) Route(ctx context.Context, prompt string, priorLevel ClassificationLevel) (RouteDecision, error) {
	decision := RouteDecision{
		Warnings: []string{},
	}

	// Classify the prompt
	classification, err := r.classifier.Classify(ctx, prompt, priorLevel)
	if err != nil {
		return decision, fmt.Errorf("classification failed: %w", err)
	}
	decision.Classification = classification
	decision.Level = classification.Level

	// Resolve full model parameters from config based on classification level
	providerID, modelID, thinking, effort := r.resolveModelParams(classification.Level)
	decision.ProviderID = providerID
	decision.ModelID = modelID
	decision.ThinkingEnabled = thinking
	decision.ReasoningEffort = effort

	// Fill RecommendedModel in classification (single source of truth from config)
	decision.Classification.RecommendedModel = modelID

	// Collect warnings
	if classification.RequiresConfirmation {
		decision.Warnings = append(decision.Warnings,
			fmt.Sprintf("⚠️  %s", classification.ConfirmationMessage))
	}

	r.logger.DebugContext(ctx, logger.CatApp, "routing decision made",
		"level", classification.Level.String(),
		"model_id", modelID,
		"thinking", thinking,
		"effort", effort,
		"confidence", classification.Confidence,
		"warnings", len(decision.Warnings),
	)

	return decision, nil
}

// resolveModelParams determines the full model configuration for a classification level.
//
// Mapping:
//
//	L0 → fast     (flash, no thinking)
//	L1 → universal (flash-thinking, high)
//	L2 → superior (pro, high)
//	L3 → expert   (pro-max, max)
func (r *Router) resolveModelParams(level ClassificationLevel) (providerID, modelID string, thinking bool, effort string) {
	var role string

	switch level {
	case LevelConversation:
		role = "fast"
		thinking, effort = false, ""
	case LevelSimpleSingleFile:
		role = "universal"
		thinking, effort = true, "high"
	case LevelMediumMultiFile:
		role = "superior"
		thinking, effort = true, "high"
	case LevelComplexRefactoring:
		role = "expert"
		thinking, effort = true, "max"
	default:
		role = "fast"
		thinking, effort = false, ""
	}

	// Look up the actual model ID from model service
	model := r.modelService.DefaultModelByRole(role)
	if model == nil {
		r.logger.WarnContext(context.Background(), logger.CatApp, "model not found for role",
			"role", role,
			"level", level.String(),
		)
		// Return a safe fallback
		return "deepseek", "deepseek-v4-flash", false, ""
	}

	// Use APIModel for the actual API call (may differ from the config ID)
	// Return ONLY the model name (not "provider:model"), because this value
	// is sent directly to the LLM API as the "model" field.
	apiModel := model.APIModel
	if apiModel == "" {
		apiModel = model.ID
	}

	return model.ProviderID, apiModel, thinking, effort
}

// ModelForClassification returns the recommended model ID for a classification result.
// This is a convenience method for direct model lookup without the full routing decision.
func (r *Router) ModelForClassification(classification ClassificationResult) string {
	_, modelID, _, _ := r.resolveModelParams(classification.Level)
	return modelID
}
