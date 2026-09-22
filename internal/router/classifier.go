package router

import (
	"context"
	"errors"

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
		} else {
			fallbackType := input.PreviousTaskType
			fallbackSource := SourcePreviousFallback
			fallbackReason := "previous"
			if !fallbackType.Valid() {
				fallbackType = tasktype.General
				fallbackSource = SourceDefaultFallback
				fallbackReason = "general"
			}
			c.logFallback(ctx, input, history, err, fallbackType, fallbackSource)
			return ClassificationResult{TaskType: fallbackType, Source: fallbackSource, ReasonCode: fallbackReason}
		}
	}
	if input.PreviousTaskType.Valid() {
		return ClassificationResult{TaskType: input.PreviousTaskType, Source: SourcePreviousFallback, ReasonCode: "previous"}
	}
	return ClassificationResult{TaskType: tasktype.General, Source: SourceDefaultFallback, ReasonCode: "general"}
}

func (c *DefaultClassifier) logFallback(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage, err error, fallbackType tasktype.TaskType, fallbackSource ClassificationSource) {
	if c.logger == nil {
		return
	}
	args := []any{
		"err", err.Error(),
		"fallback_task_type", fallbackType,
		"fallback_source", fallbackSource,
		"prompt_len", len(input.Text),
		"history_len", len(history),
		"previous_task_type", input.PreviousTaskType,
	}
	if c.llm != nil {
		c.llm.mu.RLock()
		args = append(args, "provider_id", c.llm.providerID, "model", c.llm.model)
		c.llm.mu.RUnlock()
	}
	var responseErr *classifierResponseError
	if errors.As(err, &responseErr) {
		preview := responseErr.Content
		if len(preview) > 512 {
			preview = preview[:512] + "..."
		}
		args = append(args,
			"response_len", len(responseErr.Content),
			"response_preview", preview,
			"finish_reason", responseErr.FinishReason,
		)
	}
	c.logger.WarnContext(ctx, logger.CatLLM, "task classifier failed; using fallback", args...)
}
