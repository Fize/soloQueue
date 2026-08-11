package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// TelemetryClient wraps an underlying agent.LLMClient to automatically capture
// and log token usage statistics to the provided database.
type TelemetryClient struct {
	inner agent.LLMClient
	db    *db.DB
}

// NewTelemetryClient creates a new telemetry client wrapper.
func NewTelemetryClient(inner agent.LLMClient, db *db.DB) *TelemetryClient {
	return &TelemetryClient{
		inner: inner,
		db:    db,
	}
}

// Chat calls the underlying client's Chat method and logs the final usage upon completion.
func (c *TelemetryClient) Chat(ctx context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	startedAt := time.Now().UTC()
	resp, err := c.inner.Chat(ctx, req)
	if err != nil {
		c.logCallAsync(ctx, req, startedAt, nil, "", err)
	} else if resp != nil {
		c.logCallAsync(ctx, req, startedAt, &resp.Usage, string(resp.FinishReason), nil)
	}
	return resp, err
}

// ChatStream calls the underlying client's ChatStream method and intercepts the EventDone
// or EventError to extract and log the token usage.
func (c *TelemetryClient) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	startedAt := time.Now().UTC()
	innerChan, err := c.inner.ChatStream(ctx, req)
	if err != nil {
		c.logCallAsync(ctx, req, startedAt, nil, "", err)
		return nil, err
	}

	outChan := make(chan llm.Event)

	go func() {
		defer close(outChan)
		terminalLogged := false
		for event := range innerChan {
			outChan <- event
			if event.Type == llm.EventDone {
				c.logCallAsync(ctx, req, startedAt, event.Usage, string(event.FinishReason), nil)
				terminalLogged = true
			} else if event.Type == llm.EventError {
				eventErr := event.Err
				if eventErr == nil {
					eventErr = errors.New("stream returned an error event")
				}
				c.logCallAsync(ctx, req, startedAt, event.Usage, string(event.FinishReason), eventErr)
				terminalLogged = true
			}
		}
		if !terminalLogged {
			c.logCallAsync(ctx, req, startedAt, nil, "", errors.New("stream ended without terminal event"))
		}
	}()

	return outChan, nil
}

func (c *TelemetryClient) logCallAsync(
	ctx context.Context,
	req agent.LLMRequest,
	startedAt time.Time,
	usage *llm.Usage,
	finishReason string,
	callErr error,
) {
	if c.db == nil {
		return
	}

	metadata := MetadataFromContext(ctx)
	finishedAt := time.Now().UTC()
	status, errorCode := classifyCallError(callErr)
	metric := db.LLMCallMetric{
		CallID:       uuid.NewString(),
		RequestID:    metadata.RequestID,
		SessionID:    metadata.SessionID,
		RunID:        metadata.RunID,
		AgentID:      metadata.AgentID,
		TeamID:       metadata.TeamID,
		Origin:       valueOrUnknown(metadata.Origin),
		UsageType:    valueOrUnknown(metadata.UsageType),
		TaskType:     valueOrUnknown(metadata.TaskType),
		ProviderID:   req.ProviderID,
		ModelID:      req.Model,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		Status:       status,
		FinishReason: finishReason,
		ErrorCode:    errorCode,
		DurationMS:   finishedAt.Sub(startedAt).Milliseconds(),
	}
	if strings.HasPrefix(metric.ModelID, metric.ProviderID+"/") && metric.ProviderID != "" {
		metric.ModelID = strings.TrimPrefix(metric.ModelID, metric.ProviderID+"/")
	}
	if usage != nil {
		metric.PromptTokens = usage.PromptTokens
		metric.CompletionTokens = usage.CompletionTokens
		metric.ReasoningTokens = usage.ReasoningTokens
		metric.TotalTokens = usage.TotalTokens
		metric.CacheHitTokens = usage.PromptCacheHitTokens
		metric.CacheMissTokens = usage.PromptCacheMissTokens
	}

	bgCtx := context.Background()
	go func() {
		_ = c.db.InsertLLMCallMetric(bgCtx, metric)
	}()
}

func classifyCallError(err error) (string, string) {
	if err == nil {
		return StatusSuccess, ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return StatusTimeout, "context_deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return StatusCancelled, "context_cancelled"
	}
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		return StatusError, fmt.Sprintf("provider_http_%d", apiErr.StatusCode)
	}
	return StatusError, "llm_call_failed"
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
