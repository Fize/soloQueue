package router

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/xiaobaitu/soloqueue/internal/config"
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
	logger       *slog.Logger
}

// NewRouter creates a new Router instance
func NewRouter(
	classifier Classifier,
	modelService ModelService,
	logger *slog.Logger,
) *Router {
	if logger == nil {
		logger = slog.Default()
	}

	return &Router{
		classifier:   classifier,
		modelService: modelService,
		logger:       logger,
	}
}

// RouteDecision represents the router's decision about how to handle a task
type RouteDecision struct {
	// Level is the determined routing level
	Level ClassificationLevel

	// ModelID is the recommended model ID for this task (e.g., "deepseek:deepseek-v4-pro")
	ModelID string

	// Classification contains the full classification result
	Classification ClassificationResult

	// Warnings contains any warnings (e.g., dangerous operations requiring confirmation)
	Warnings []string
}

// Route analyzes a user prompt and returns a routing decision
//
// The decision includes:
// - The classification level (L0-L3)
// - The recommended model ID resolved from config
// - Any warnings or special handling notes
func (r *Router) Route(ctx context.Context, prompt string) (RouteDecision, error) {
	decision := RouteDecision{
		Warnings: []string{},
	}

	// Classify the prompt
	classification, err := r.classifier.Classify(ctx, prompt)
	if err != nil {
		return decision, fmt.Errorf("classification failed: %w", err)
	}
	decision.Classification = classification
	decision.Level = classification.Level

	// Resolve model ID from config based on classification level
	modelID := r.resolveModelID(classification.Level)
	decision.ModelID = modelID

	// Collect warnings
	if classification.RequiresConfirmation {
		decision.Warnings = append(decision.Warnings,
			fmt.Sprintf("⚠️  %s", classification.ConfirmationMessage))
	}

	r.logger.DebugContext(ctx, "routing decision made",
		slog.String("level", classification.Level.String()),
		slog.String("model_id", modelID),
		slog.Int("confidence", classification.Confidence),
		slog.Int("warnings", len(decision.Warnings)),
	)

	return decision, nil
}

// resolveModelID determines the model ID to use for a classification level
//
// This maps classification levels to configured model roles, then looks up
// the actual model ID from the model service using the role-based resolution.
func (r *Router) resolveModelID(level ClassificationLevel) string {
	var role string

	switch level {
	case LevelConversation:
		role = "fast" // Conversation uses the fast model (flash)
	case LevelSimpleSingleFile:
		role = "fast" // Single file can use fast model with thinking
	case LevelMediumMultiFile:
		role = "superior" // Multi-file uses pro model
	case LevelComplexRefactoring:
		role = "expert" // Complex uses pro-max model
	default:
		role = "fast" // Safe default
	}

	// Look up the actual model ID from model service
	model := r.modelService.DefaultModelByRole(role)
	if model == nil {
		r.logger.WarnContext(context.Background(), "model not found for role",
			slog.String("role", role),
			slog.String("level", level.String()),
		)
		// Return a safe fallback
		return "deepseek:deepseek-v4-flash"
	}

	// Return provider:id format
	return fmt.Sprintf("%s:%s", model.ProviderID, model.ID)
}

// ModelForClassification returns the recommended model ID for a classification result
// This is a convenience method for direct model lookup without the full routing decision
func (r *Router) ModelForClassification(classification ClassificationResult) string {
	return r.resolveModelID(classification.Level)
}
