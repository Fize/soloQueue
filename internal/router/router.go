package router

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

type Router struct {
	classifier Classifier
	logger     *logger.Logger
}

func NewRouter(classifier Classifier, l *logger.Logger) *Router {
	return &Router{classifier: classifier, logger: l}
}

func (r *Router) UpdateClassifierModel(provider, model string) {
	if c, ok := r.classifier.(*DefaultClassifier); ok {
		c.SetModelAndProvider(provider, model)
	}
}

// UpdateClassifierEngine changes the engine used by future routing requests.
func (r *Router) UpdateClassifierEngine(engine Engine) {
	if c, ok := r.classifier.(*DefaultClassifier); ok {
		c.SetEngine(engine)
	}
}

// Route classifies the task input. It never resolves a model: task type is a
// label for observability, not a routing key.
func (r *Router) Route(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) (ClassificationResult, error) {
	if r.classifier == nil {
		return ClassificationResult{}, fmt.Errorf("task classifier is not configured")
	}

	classification := r.classifier.Classify(ctx, input, history)
	if !classification.TaskType.Valid() {
		return ClassificationResult{}, fmt.Errorf("classifier returned invalid task type %q", classification.TaskType)
	}

	return classification, nil
}
