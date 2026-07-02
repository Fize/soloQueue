package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Config ──────────────────────────────────────────────────────────────────

// Config are the construction parameters for the DeepSeek client.
//
// All fields can be zero-valued — they have reasonable default values.
type Config struct {
	// BaseURL defaults to "https://api.deepseek.com". Internally, "/chat/completions" is automatically appended.
	BaseURL string

	// APIKey is required, used as Authorization: Bearer <key>
	APIKey string

	// Headers for additional HTTP headers (typically for proxy / custom routing)
	Headers map[string]string

	// TimeoutMs HTTP request timeout (milliseconds); 0 defaults to 600_000 (10min)
	TimeoutMs int

	// Retry policy; zero value means no retries
	Retry llm.RetryPolicy

	// Log optional logger (nil-safe)
	Log *logger.Logger

	// HTTPClient optional; if nil, &http.Client{Timeout} is used
	//
	// For testing, an httptest.Server client can be passed to avoid real network calls.
	HTTPClient *http.Client
}

// Client implements the agent.LLMClient interface
type Client struct {
	baseURL string
	apiKey  string
	headers map[string]string
	retry   llm.RetryPolicy
	log     *logger.Logger
	http    *http.Client
	timeout time.Duration // per-request timeout, applied via context (not http.Client.Timeout)
}

// NewClient constructs a DeepSeek client.
// APIKey is not validated here; allows the user to proceed into the program, with errors returned by the API during calls.
func NewClient(cfg Config) (*Client, error) {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		// Use context deadline instead of http.Client.Timeout so streamLoop
		// can detect the timeout via ctx.Err(). http.Client.Timeout cancels
		// the request context internally without touching the caller's ctx.
		httpClient = &http.Client{}
	}
	timeoutDur := 10 * time.Minute
	if cfg.TimeoutMs > 0 {
		timeoutDur = time.Duration(cfg.TimeoutMs) * time.Millisecond
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		headers: cfg.Headers,
		retry:   cfg.Retry,
		log:     cfg.Log,
		http:    httpClient,
		timeout: timeoutDur,
	}, nil
}

// ─── Interface ───────────────────────────────────────────────────────────────

// Ensure *Client implements agent.LLMClient
var _ agent.LLMClient = (*Client)(nil)

// Chat makes a synchronous call, internally assembling the full response using streaming.
//
// Strategy: All HTTP calls follow the stream=true path; Chat is an accumulation of ChatStream.
// Benefit: HTTP logic, retry logic, and error handling are written only once.
func (c *Client) Chat(ctx context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	// Force include_usage to facilitate Usage aggregation in Chat
	req.IncludeUsage = true

	start := time.Now()
	events, err := c.ChatStream(ctx, req)
	if err != nil {
		return nil, err
	}

	var resp agent.LLMResponse
	var acc toolCallAccumulator
	var deltaCount int

	for ev := range events {
		switch ev.Type {
		case llm.EventDelta:
			deltaCount++
			resp.Content += ev.ContentDelta
			resp.ReasoningContent += ev.ReasoningContentDelta
			if ev.ToolCallDelta != nil {
				acc.add(*ev.ToolCallDelta)
			}
		case llm.EventDone:
			resp.FinishReason = ev.FinishReason
			if ev.Usage != nil {
				resp.Usage = *ev.Usage
			}
			resp.ToolCalls = acc.collect()
			// Attempt to drain remaining events (channel usually closes immediately after Done)
			for range events {
			}
			c.logChatDone(ctx, req, &resp, deltaCount, time.Since(start))
			return &resp, nil
		case llm.EventError:
			c.logChatFailed(ctx, req, ev.Err, time.Since(start))
			return nil, ev.Err
		}
	}
	// Channel closed but no Done received — abnormal.
	err = errors.New("deepseek: stream ended without done event")
	c.logChatFailed(ctx, req, err, time.Since(start))
	return nil, err
}

// ChatStream initiates a streaming request and returns an Event channel.
//
// Guaranteed behavior:
//   - The returned channel will always be closed (no leaks).
//   - Normal flow: a series of Delta events → one Done event → channel close.
//   - Error: one Error event (wrapping the original err) → channel close.
//   - ctx cancellation: one Error event (ctx.Err()) → channel close.
//
// Errors during the HTTP phase (4xx/5xx/network) that fail even after retries are returned directly as `err` from
// ChatStream (not sent to the channel); only errors encountered while reading the SSE body after a 200 OK response are sent to the channel.
func (c *Client) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	body, err := json.Marshal(buildWireRequest(req, true, req.IncludeUsage))
	if err != nil {
		return nil, fmt.Errorf("deepseek: marshal request: %w", err)
	}

	c.logStart(ctx, req)

	httpCtx := ctx
	var cancelTimeout context.CancelFunc
	if c.timeout > 0 {
		httpCtx, cancelTimeout = context.WithTimeout(ctx, c.timeout)
	}

	httpResp, err := c.doWithRetry(httpCtx, body)
	if err != nil {
		if cancelTimeout != nil {
			cancelTimeout()
		}
		c.logError(ctx, "request failed", err)
		return nil, err
	}

	ch := make(chan llm.Event, 16)
	go func() {
		defer func() {
			if cancelTimeout != nil {
				cancelTimeout()
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				c.logError(ctx, "streamLoop panic recovered", fmt.Errorf("panic: %v", r))
			}
		}()
		c.streamLoop(httpCtx, httpResp, ch)
	}()
	return ch, nil
}

// ─── Streaming loop ──────────────────────────────────────────────────────────

func (c *Client) streamLoop(ctx context.Context, resp *http.Response, ch chan<- llm.Event) {
	defer close(ch)
	defer resp.Body.Close()

	reader := newSSEReader(resp.Body)

	for {
		// Check ctx first (to avoid blocking on Scanner if already canceled)
		if err := ctx.Err(); err != nil {
			sendErrEvent(ctx, ch, err)
			return
		}

		payload, err := reader.Next()
		if err != nil {
			if errors.Is(err, errSSEDone) {
				return // Normal DONE
			}
			if errors.Is(err, io.EOF) {
				// EOF without [DONE] is abnormal unless the server sent it
				// as a clean shutdown. If ctx is done, the EOF is a timeout
				// or cancellation — report it as an error.
				if ctxErr := ctx.Err(); ctxErr != nil {
					sendErrEvent(ctx, ch, fmt.Errorf("deepseek: sse stream interrupted: %w", ctxErr))
					return
				}
				return
			}
			sendErrEvent(ctx, ch, fmt.Errorf("deepseek: sse read: %w", err))
			return
		}

		var chunk wireChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			sendErrEvent(ctx, ch, fmt.Errorf("deepseek: parse chunk: %w (payload=%s)", err, truncate(payload, 200)))
			return
		}

		for _, ev := range chunkToEvents(chunk) {
			select {
			case ch <- ev:
			case <-ctx.Done():
				sendErrEvent(ctx, ch, ctx.Err())
				return
			}
		}
	}
}

// sendErrEvent attempts to send an Error event; gives up if ctx is canceled.
func sendErrEvent(ctx context.Context, ch chan<- llm.Event, err error) {
	ev := llm.Event{Type: llm.EventError, Err: err}
	// First, try non-blocking (buffer usually has capacity)
	select {
	case ch <- ev:
		return
	default:
	}
	// Otherwise, block or give up if ctx is canceled.
	select {
	case ch <- ev:
	case <-ctx.Done():
	}
}

// ─── HTTP doWithRetry ────────────────────────────────────────────────────────

func (c *Client) doWithRetry(ctx context.Context, body []byte) (*http.Response, error) {
	var resp *http.Response

	onRetry := func(attempt int, delay time.Duration, err error) {
		c.logRetry(ctx, attempt, delay, err)
	}

	err := llm.RunWithRetryHooks(ctx, c.retry, llm.IsRetryableErr, onRetry,
		func(ctx context.Context) error {
			// A new request must be created for each attempt (Body can only be read once).
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				c.baseURL+"/chat/completions",
				bytes.NewReader(body))
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}

			r, err := c.http.Do(req)
			if err != nil {
				return err
			}

			if r.StatusCode >= 400 {
				defer r.Body.Close()
				return parseAPIError(r)
			}
			resp = r
			return nil
		})

	return resp, err
}

// parseAPIError parses APIError from the error response body.
func parseAPIError(r *http.Response) *llm.APIError {
	apiErr := &llm.APIError{StatusCode: r.StatusCode}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		apiErr.Message = fmt.Sprintf("read body: %v", err)
		return apiErr
	}
	var env wireErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		apiErr.Type = env.Error.Type
		apiErr.Code = env.Error.Code
		apiErr.Message = env.Error.Message
		apiErr.Param = env.Error.Param
		return apiErr
	}
	// Body is not a standard envelope: use raw text.
	apiErr.Message = truncate(string(body), 500)
	return apiErr
}

// ─── Tool call accumulator ──────────────────────────────────────────────────

// toolCallAccumulator accumulates streaming tool_call deltas by index.
//
// Usage:
//
//	var acc toolCallAccumulator
//	acc.add(delta1)
//	acc.add(delta2)
//	tools := acc.collect() // A slice of complete ToolCalls, sorted by index.
type toolCallAccumulator struct {
	slots map[int]*llm.ToolCall // key = delta.Index
}

func (a *toolCallAccumulator) add(d llm.ToolCallDelta) {
	if a.slots == nil {
		a.slots = make(map[int]*llm.ToolCall)
	}
	tc, ok := a.slots[d.Index]
	if !ok {
		tc = &llm.ToolCall{Type: "function"}
		a.slots[d.Index] = tc
	}
	if d.ID != "" {
		tc.ID = d.ID
	}
	if d.Name != "" {
		tc.Function.Name = d.Name
	}
	tc.Function.Arguments += d.Arguments
}

// collect returns the accumulated ToolCall list in index order.
func (a *toolCallAccumulator) collect() []llm.ToolCall {
	if len(a.slots) == 0 {
		return nil
	}
	// Indices might be sparse; collect in ascending order.
	maxIdx := -1
	for i := range a.slots {
		if i > maxIdx {
			maxIdx = i
		}
	}
	out := make([]llm.ToolCall, 0, len(a.slots))
	for i := 0; i <= maxIdx; i++ {
		if tc, ok := a.slots[i]; ok {
			out = append(out, *tc)
		}
	}
	return out
}

// ─── Logging ─────────────────────────────────────────────────────────────────

func (c *Client) logStart(ctx context.Context, req agent.LLMRequest) {
	if c.log == nil {
		return
	}
	c.log.InfoContext(ctx, logger.CatLLM, "deepseek chat start",
		"model", req.Model,
		"messages", len(req.Messages),
		"tools", len(req.Tools),
		"include_usage", req.IncludeUsage,
	)
}

func (c *Client) logError(ctx context.Context, msg string, err error) {
	if c.log == nil {
		return
	}
	c.log.LogError(ctx, logger.CatLLM, "deepseek: "+msg, err)
}

// logRetry is triggered when a retry is about to occur (before backoff begins).
func (c *Client) logRetry(ctx context.Context, attempt int, delay time.Duration, err error) {
	if c.log == nil {
		return
	}
	attrs := []any{
		"attempt", attempt,
		"delay_ms", delay.Milliseconds(),
		"err", err.Error(),
	}
	// If it's a structured APIError, include status / type / code as extra attributes.
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		attrs = append(attrs,
			"status_code", apiErr.StatusCode,
			"err_type", apiErr.Type,
			"err_code", apiErr.Code,
		)
	}
	c.log.WarnContext(ctx, logger.CatLLM, "deepseek retry", attrs...)
}

// logChatDone is triggered after the full Chat response has been assembled.
func (c *Client) logChatDone(ctx context.Context, req agent.LLMRequest, resp *agent.LLMResponse, deltaCount int, dur time.Duration) {
	if c.log == nil {
		return
	}
	c.log.InfoContext(ctx, logger.CatLLM, "deepseek chat done",
		"model", req.Model,
		"content_len", len(resp.Content),
		"reasoning_len", len(resp.ReasoningContent),
		"tool_calls", len(resp.ToolCalls),
		"delta_events", deltaCount,
		"finish_reason", string(resp.FinishReason),
		"prompt_tokens", resp.Usage.PromptTokens,
		"completion_tokens", resp.Usage.CompletionTokens,
		"total_tokens", resp.Usage.TotalTokens,
		"reasoning_tokens", resp.Usage.ReasoningTokens,
		"cache_hit_tokens", resp.Usage.PromptCacheHitTokens,
		"duration_ms", dur.Milliseconds(),
	)
}

// logChatFailed is triggered when Chat fails (distinct from "request failed" during the HTTP phase).
func (c *Client) logChatFailed(ctx context.Context, req agent.LLMRequest, err error, dur time.Duration) {
	if c.log == nil {
		return
	}
	c.log.LogError(ctx, logger.CatLLM, "deepseek chat failed", err,
		"model", req.Model,
		"duration_ms", dur.Milliseconds(),
	)
}

// ─── small helpers ──────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}