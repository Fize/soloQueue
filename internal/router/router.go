package router

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type ModelService interface {
	DefaultModelForTask(tasktype.TaskType) *config.LLMModel
}

type RouteDecision struct {
	Classification ClassificationResult
	TaskType       tasktype.TaskType
	ProviderID     string
	ModelID        string // API model sent to the provider
	ModelName      string
	ThinkingEnabled bool
	ReasoningEffort string
	ThinkingType    string
	ContextWindow   int
	Vision          bool // copied model capability; never used to route
}

type Router struct {
	classifier Classifier
	modelService ModelService
	logger *logger.Logger
}

func NewRouter(classifier Classifier, modelService ModelService, l *logger.Logger) *Router {
	return &Router{classifier: classifier, modelService: modelService, logger: l}
}

func (r *Router) UpdateClassifierModel(provider, model string) {
	if c, ok := r.classifier.(*DefaultClassifier); ok { c.SetModelAndProvider(provider, model) }
}

func (r *Router) Route(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) (RouteDecision, error) {
	if r.classifier == nil { return RouteDecision{}, fmt.Errorf("task classifier is not configured") }
	classification := r.classifier.Classify(ctx, input, history)
	if !classification.TaskType.Valid() { return RouteDecision{}, fmt.Errorf("classifier returned invalid task type %q", classification.TaskType) }
	model := r.modelService.DefaultModelForTask(classification.TaskType)
	if model == nil { return RouteDecision{}, fmt.Errorf("no enabled model for task type %s", classification.TaskType) }
	apiModel := model.APIModel
	if apiModel == "" { apiModel = model.ID }
	return RouteDecision{
		Classification: classification, TaskType: classification.TaskType,
		ProviderID: model.ProviderID, ModelID: apiModel, ModelName: model.Name,
		ThinkingEnabled: model.Thinking.Enabled, ReasoningEffort: model.Thinking.ReasoningEffort,
		ThinkingType: model.Thinking.ThinkingType, ContextWindow: model.ContextWindow,
		Vision: model.Vision,
	}, nil
}
