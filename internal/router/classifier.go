package router

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type Classifier interface {
	Classify(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) ClassificationResult
}

type DefaultClassifier struct {
	mu      sync.RWMutex
	config  ClassifierConfig
	local   *LocalClassifier
	llm     *LLMClassifier
	decider Decider
	logger  *logger.Logger
}

// NewDefaultClassifier builds the configured engine. decider is required for EngineLaya and ignored
// otherwise; a nil decider makes the local engine degrade to the previous/default fallback.
func NewDefaultClassifier(config ClassifierConfig, client agent.LLMClient, providerID, model string, decider Decider, l *logger.Logger) *DefaultClassifier {
	var semantic *LLMClassifier
	if config.EnableLLM && client != nil && model != "" {
		semantic = NewLLMClassifier(client, providerID, model)
	}
	c := &DefaultClassifier{
		config:  config,
		local:   NewLocalClassifier(),
		llm:     semantic,
		decider: decider,
		logger:  l,
	}
	c.SetEngine(config.Engine)
	return c
}

// SetEngine changes the engine for subsequent classifications. In-flight requests keep their snapshot.
func (c *DefaultClassifier) SetEngine(engine Engine) {
	if engine == "" {
		engine = EngineLLM
	}
	c.mu.Lock()
	c.config.Engine = engine
	c.mu.Unlock()
}

func (c *DefaultClassifier) SetModelAndProvider(providerID, model string) {
	if c.llm != nil {
		c.llm.SetModelAndProvider(providerID, model)
	}
}

func (c *DefaultClassifier) Classify(ctx context.Context, input ClassifyInput, history []ctxwin.PayloadMessage) ClassificationResult {
	c.mu.RLock()
	cfg := c.config
	c.mu.RUnlock()
	if cfg.EnableLocal {
		if result := c.local.Classify(input.Text); result.Matched {
			return ClassificationResult{TaskType: result.TaskType, Source: SourceLocal, ReasonCode: result.ReasonCode}
		}
	}
	if cfg.Engine == EngineLaya {
		if c.decider != nil {
			messages := classifierMessages(input, history)
			wire := make([]struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}, 0, len(messages))
			for _, msg := range messages {
				wire = append(wire, struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}{msg.Role, msg.Content})
			}
			data, _ := json.Marshal(wire)
			if t, err := c.decider.Decide(ctx, string(data)); err == nil && t.Valid() {
				return ClassificationResult{TaskType: t, Source: SourceLaya, ReasonCode: "laya"}
			}
		}
		// The local engine is the configured alternative to the LLM, so a failure degrades to the
		// existing fallback instead of a network call.
		if c.logger != nil {
			c.logger.WarnContext(ctx, logger.CatApp, "local task classifier unavailable; using fallback",
				"prompt_len", len(input.Text), "previous_task_type", input.PreviousTaskType)
		}
		return fallbackResult(input, SourceLayaUnavailable)
	}
	if cfg.EnableLLM && c.llm != nil {
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
	return fallbackResult(input, "")
}

// fallbackResult returns the previous task type, or general when there is none. source overrides the
// reported source so the caller can see which engine gave up; empty keeps the historical values.
func fallbackResult(input ClassifyInput, source ClassificationSource) ClassificationResult {
	if input.PreviousTaskType.Valid() {
		reported := SourcePreviousFallback
		if source != "" {
			reported = source
		}
		return ClassificationResult{TaskType: input.PreviousTaskType, Source: reported, ReasonCode: "previous"}
	}
	reported := SourceDefaultFallback
	if source != "" {
		reported = source
	}
	return ClassificationResult{TaskType: tasktype.General, Source: reported, ReasonCode: "general"}
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
