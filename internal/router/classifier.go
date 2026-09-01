package router

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type Classifier interface {
	Classify(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) ClassificationResult
}

type DefaultClassifier struct {
	config ClassifierConfig
	local  *LocalClassifier
	llm    *LLMClassifier
	logger *logger.Logger
}

func NewDefaultClassifier(config ClassifierConfig, client agent.LLMClient, providerID, model string, l *logger.Logger) *DefaultClassifier {
	var semantic *LLMClassifier
	if config.EnableLLM && client != nil && model != "" {
		semantic = NewLLMClassifier(client, providerID, model)
	}
	return &DefaultClassifier{
		config: config,
		local:  NewLocalClassifier(),
		llm:    semantic,
		logger: l,
	}
}

func (c *DefaultClassifier) SetModelAndProvider(providerID, model string) {
	if c.llm != nil {
		c.llm.SetModelAndProvider(providerID, model)
	}
}

func (c *DefaultClassifier) Classify(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) ClassificationResult {
	if c.config.EnableLocal {
		if result := c.local.Classify(input.Text); result.Matched {
			return ClassificationResult{TaskType: result.TaskType, Source: SourceLocal, ReasonCode: result.ReasonCode}
		}
	}
	if c.config.EnableLLM && c.llm != nil {
		if t, err := c.llm.Classify(ctx, input, history); err == nil {
			return ClassificationResult{TaskType: t, Source: SourceLLM, ReasonCode: "llm"}
		}
	}
	if input.PreviousTaskType.Valid() {
		return ClassificationResult{TaskType: input.PreviousTaskType, Source: SourcePreviousFallback, ReasonCode: "previous"}
	}
	return ClassificationResult{TaskType: tasktype.General, Source: SourceDefaultFallback, ReasonCode: "general"}
}
