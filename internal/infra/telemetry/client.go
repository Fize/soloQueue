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
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/observability"
)

// TelemetryClient wraps an underlying agent.LLMClient to automatically capture
// and log token usage statistics to the provided database.
type TelemetryClient struct {
	inner agent.LLMClient
	db    *db.DB
	store *observability.Store
}

// NewTelemetryClient creates a new telemetry client wrapper.
func NewTelemetryClient(inner agent.LLMClient, db *db.DB, stores ...*observability.Store) *TelemetryClient {
	var store *observability.Store
	if len(stores) > 0 {
		store = stores[0]
	}
	return &TelemetryClient{
		inner: inner,
		db:    db,
		store: store,
	}
}

// Chat calls the underlying client's Chat method and logs the final usage upon completion.
func (c *TelemetryClient) Chat(ctx context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	startedAt := time.Now().UTC()
	ctx, record := c.begin(ctx, req, startedAt)
	resp, err := c.inner.Chat(ctx, req)
	if resp != nil {
		record.appendContent(resp.Content)
		record.appendReasoning(resp.ReasoningContent)
	}
	if err != nil {
		c.logCallAsync(ctx, req, startedAt, nil, "", err)
	} else if resp != nil {
		c.logCallAsync(ctx, req, startedAt, &resp.Usage, string(resp.FinishReason), nil)
	} else {
		c.logCallAsync(ctx, req, startedAt, nil, "", errors.New("provider returned no response"))
	}
	return resp, err
}

// ChatStream calls the underlying client's ChatStream method and intercepts the EventDone
// or EventError to extract and log the token usage.
func (c *TelemetryClient) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	startedAt := time.Now().UTC()
	ctx, record := c.begin(ctx, req, startedAt)
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
			if event.Type == llm.EventDelta {
				record.appendContent(event.ContentDelta)
				record.appendReasoning(event.ReasoningContentDelta)
			}
			// A terminal event is also the consumer's completion boundary. Persist the
			// optional call snapshot before exposing that boundary; SQL aggregation remains asynchronous.

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
			select {
			case outChan <- event:
			case <-ctx.Done():
				if !terminalLogged {
					c.logCallAsync(ctx, req, startedAt, nil, "", ctx.Err())
				}
				return
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
	record, _ := ctx.Value(callRecordKey{}).(*callRecord)
	callID := uuid.NewString()
	if record != nil {
		callID = record.call.CallID
	}

	metadata := MetadataFromContext(ctx)
	if metadata.Origin == "" {
		metadata.Origin = OriginSystem
	}
	finishedAt := time.Now().UTC()
	status, errorCode := classifyCallError(callErr)
	metric := db.LLMCallMetric{
		CallID:       callID,
		RequestID:    metadata.RequestID,
		SessionID:    metadata.SessionID,
		SegmentID:    metadata.SegmentID,
		RunID:        metadata.RunID,
		AgentID:      metadata.AgentID,
		TeamID:       metadata.TeamID,
		Origin:       metadata.Origin,
		UsageType:    metadata.UsageType,
		TaskType:     metadata.TaskType,
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
		metric.ReasoningDetailsReported = usage.ReasoningDetailsReported
		metric.CacheDetailsReported = usage.CacheDetailsReported
	}

	if record != nil {
		if !record.finish() {
			return
		}
		n, reported := record.observer.Attempts()
		metric.RetryCount = max(0, n-1)
		metric.AttemptCount = n
		metric.AttemptsReported = reported
		record.call.Status = status
		record.call.FinishedAt = &finishedAt
		record.call.DurationMS = metric.DurationMS
		record.call.PromptTokens = metric.PromptTokens
		record.call.CompletionTokens = metric.CompletionTokens
		record.call.TotalTokens = metric.TotalTokens
		record.call.AttemptCount = n
		record.call.RetryCount = metric.RetryCount
		record.call.AttemptsReported = reported
		record.call.Content = record.contentBuffer.String()
		record.call.Reasoning = record.reasoningBuffer.String()
		if callErr != nil {
			record.call.Error = errorCode
		}
		if record.truncated {
			record.call.RecordingStatus = "partial"
		}
		if record.store != nil {
			if err := record.store.SaveCall(record.call, nil); err != nil {
				record.call.RecordingStatus = "partial"
			}
		}
	}
	if c.db == nil {
		return
	}
	bgCtx := context.Background()
	go func() {
		if err := c.db.InsertLLMCallMetric(bgCtx, metric); err == nil && metadata.OnUsage != nil && usage != nil {
			metadata.OnUsage(telemetryctx.Usage{
				InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
				TotalTokens: usage.TotalTokens, CacheHitTokens: usage.PromptCacheHitTokens,
				CacheMissTokens: usage.PromptCacheMissTokens, CacheDetailsReported: usage.CacheDetailsReported,
			})
		}
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
