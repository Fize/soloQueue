package agent

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// TelemetryClient wraps an underlying LLMClient to automatically capture and log
// token usage statistics to the provided database.
type TelemetryClient struct {
	inner LLMClient
	db    *sqlitedb.DB
}

// NewTelemetryClient creates a new telemetry client wrapper.
func NewTelemetryClient(inner LLMClient, db *sqlitedb.DB) *TelemetryClient {
	return &TelemetryClient{
		inner: inner,
		db:    db,
	}
}

// Chat calls the underlying client's Chat method and logs the final usage upon completion.
func (c *TelemetryClient) Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	resp, err := c.inner.Chat(ctx, req)
	
	if err == nil && resp != nil {
		c.logUsageAsync(ctx, req.Model, resp.Usage)
	}
	
	return resp, err
}

// ChatStream calls the underlying client's ChatStream method and intercepts the EventDone
// or EventError to extract and log the token usage.
func (c *TelemetryClient) ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error) {
	innerChan, err := c.inner.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}

	outChan := make(chan llm.Event)

	go func() {
		defer close(outChan)
		
		for event := range innerChan {
			// Pass event downstream
			outChan <- event
			
			// Intercept usage when stream ends
			if event.Type == llm.EventDone && event.Usage != nil {
				c.logUsageAsync(ctx, req.Model, *event.Usage)
			}
		}
	}()

	return outChan, nil
}

func (c *TelemetryClient) logUsageAsync(ctx context.Context, modelName string, usage llm.Usage) {
	if c.db == nil {
		return
	}
	
	teamID, usageType := TelemetryFromContext(ctx)
	// If no usage context was provided, default to unknown to still capture the tokens.
	if usageType == "" {
		usageType = "unknown"
	}
	
	// Create a new background context because the incoming ctx might be cancelled 
	// shortly after Chat completes, interrupting our DB insert.
	bgCtx := context.Background()

	// Launch async DB insert
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
