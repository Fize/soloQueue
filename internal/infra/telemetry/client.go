package telemetry

import (
	"context"
	"strings"

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
	resp, err := c.inner.Chat(ctx, req)
	if err == nil && resp != nil {
		c.logUsageAsync(ctx, req, resp.Usage)
	}
	return resp, err
}

// ChatStream calls the underlying client's ChatStream method and intercepts the EventDone
// or EventError to extract and log the token usage.
func (c *TelemetryClient) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	innerChan, err := c.inner.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}

	outChan := make(chan llm.Event)

	go func() {
		defer close(outChan)
		for event := range innerChan {
			outChan <- event
			if event.Type == llm.EventDone && event.Usage != nil {
				c.logUsageAsync(ctx, req, *event.Usage)
			}
		}
	}()

	return outChan, nil
}

func (c *TelemetryClient) logUsageAsync(ctx context.Context, req agent.LLMRequest, usage llm.Usage) {
	if c.db == nil {
		return
	}

	teamID, usageType := TelemetryFromContext(ctx)
	if usageType == "" {
		usageType = "unknown"
	}

	modelName := req.Model
	if req.ProviderID != "" && !strings.HasPrefix(req.Model, req.ProviderID+"/") {
		modelName = req.ProviderID + "/" + req.Model
	}

	bgCtx := context.Background()
	go func() {
		_ = c.db.InsertTokenUsage(
			bgCtx,
			usageType,
			teamID,
			modelName,
			usage.PromptTokens,
			usage.CompletionTokens,
			usage.TotalTokens,
			usage.PromptCacheHitTokens,
			usage.PromptCacheMissTokens,
		)
	}()
}
