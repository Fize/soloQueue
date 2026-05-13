package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Config ──────────────────────────────────────────────────────────────────

// Config 是 DeepSeek client 的构造参数
//
// 所有字段都可零值 —— 有合理默认值。
type Config struct {
	// BaseURL 默认 "https://api.deepseek.com"。内部自动补 /chat/completions。
	BaseURL string

	// APIKey 必填，作为 Authorization: Bearer <key>
	APIKey string

	// Headers 额外 HTTP header（通常用于代理 / 自定义路由）
	Headers map[string]string

	// TimeoutMs HTTP 请求超时（毫秒）；0 默认 600_000 (10min)
	TimeoutMs int

	// Retry 重试策略；零值不重试
	Retry llm.RetryPolicy

	// Log 可选 logger（nil-safe）
	Log *logger.Logger

	// HTTPClient 可选；nil 时用 &http.Client{Timeout}
	//
	// 测试可传入 httptest.Server 的 client 避免真实网络
	HTTPClient *http.Client
}

// Client 实现 agent.LLMClient 接口
type Client struct {
	baseURL string
	apiKey  string
	headers map[string]string
	retry   llm.RetryPolicy
	log     *logger.Logger
	http    *http.Client
	timeout time.Duration // per-request timeout, applied via context (not http.Client.Timeout)
}

// NewClient 构造 DeepSeek client
// APIKey 不做校验，允许用户先进入程序，调用时报错由 API 侧返回。
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

// 确保 *Client 实现 agent.LLMClient
var _ agent.LLMClient = (*Client)(nil)

// Chat 同步调用，内部使用 streaming 拼装完整响应
//
// 策略：所有 HTTP 调用都走 stream=true 路径，Chat 是对 ChatStream 的累积。
// 好处：HTTP 逻辑、retry 逻辑、错误处理只写一次。
func (c *Client) Chat(ctx context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	// 强制 include_usage，方便 Chat 汇总 Usage
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
			// 尝试 drain 剩余 events（通常 Done 之后 channel 立即关闭）
			for range events {
			}
			c.logChatDone(ctx, req, &resp, deltaCount, time.Since(start))
			return &resp, nil
		case llm.EventError:
			c.logChatFailed(ctx, req, ev.Err, time.Since(start))
			return nil, ev.Err
		}
	}
	// channel 关闭但没收到 Done —— 异常
	err = errors.New("deepseek: stream ended without done event")
	c.logChatFailed(ctx, req, err, time.Since(start))
	return nil, err
}

// ChatStream 启动流式请求，返回 Event channel
//
// 行为保证：
//   - 返回的 channel 总会被 close（无泄漏）
//   - 正常流：一系列 Delta → 一个 Done → channel close
//   - 出错：一个 Error（包装原始 err）→ channel close
//   - ctx 取消：一个 Error（ctx.Err()）→ channel close
//
// HTTP 阶段的错误（4xx/5xx/网络）走 retry 后还失败则直接作为 err 从
// ChatStream 返回（不进 channel）；只有 200 之后读 SSE body 时的错误才走 channel。
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
		// Check ctx first（避免在 Scanner 阻塞前就已取消）
		if err := ctx.Err(); err != nil {
			sendErrEvent(ctx, ch, err)
			return
		}

		payload, err := reader.Next()
		if err != nil {
			if errors.Is(err, errSSEDone) {
				return // 正常 DONE
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

// sendErrEvent 尽力发送一个 Error event；ctx 取消时放弃
func sendErrEvent(ctx context.Context, ch chan<- llm.Event, err error) {
	ev := llm.Event{Type: llm.EventError, Err: err}
	// 先尝试非阻塞（buffer 通常有位）
	select {
	case ch <- ev:
		return
	default:
	}
	// 否则阻塞等待或 ctx 取消
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
			// 每次尝试都要新建 request（Body 只能读一次）
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

// parseAPIError 从 error 响应体解析 APIError
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
	// body 不是标准 envelope：用原始文本
	apiErr.Message = truncate(string(body), 500)
	return apiErr
}

// ─── Tool call accumulator ──────────────────────────────────────────────────

// toolCallAccumulator 按 index 累积 streaming tool_call deltas
//
// 使用方式：
//
//	var acc toolCallAccumulator
//	acc.add(delta1)
//	acc.add(delta2)
//	tools := acc.collect() // 按 index 排序的完整 ToolCall 切片
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

// collect 按 index 顺序返回累积的 ToolCall 列表
func (a *toolCallAccumulator) collect() []llm.ToolCall {
	if len(a.slots) == 0 {
		return nil
	}
	// index 可能稀疏；按升序收集
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
		slog.String("model", req.Model),
		slog.Int("messages", len(req.Messages)),
		slog.Int("tools", len(req.Tools)),
		slog.Bool("include_usage", req.IncludeUsage),
	)
}

func (c *Client) logError(ctx context.Context, msg string, err error) {
	if c.log == nil {
		return
	}
	c.log.LogError(ctx, logger.CatLLM, "deepseek: "+msg, err)
}

// logRetry 在 retry 即将发生时触发（backoff 开始前）
func (c *Client) logRetry(ctx context.Context, attempt int, delay time.Duration, err error) {
	if c.log == nil {
		return
	}
	attrs := []any{
		slog.Int("attempt", attempt),
		slog.Int64("delay_ms", delay.Milliseconds()),
		slog.String("err", err.Error()),
	}
	// 如果是结构化 APIError，额外带上 status / type / code
	var apiErr *llm.APIError
	if errors.As(err, &apiErr) {
		attrs = append(attrs,
			slog.Int("status_code", apiErr.StatusCode),
			slog.String("err_type", apiErr.Type),
			slog.String("err_code", apiErr.Code),
		)
	}
	c.log.WarnContext(ctx, logger.CatLLM, "deepseek retry", attrs...)
}

// logChatDone 在 Chat 完整响应拼装完成后触发
func (c *Client) logChatDone(ctx context.Context, req agent.LLMRequest, resp *agent.LLMResponse, deltaCount int, dur time.Duration) {
	if c.log == nil {
		return
	}
	c.log.InfoContext(ctx, logger.CatLLM, "deepseek chat done",
		slog.String("model", req.Model),
		slog.Int("content_len", len(resp.Content)),
		slog.Int("reasoning_len", len(resp.ReasoningContent)),
		slog.Int("tool_calls", len(resp.ToolCalls)),
		slog.Int("delta_events", deltaCount),
		slog.String("finish_reason", string(resp.FinishReason)),
		slog.Int("prompt_tokens", resp.Usage.PromptTokens),
		slog.Int("completion_tokens", resp.Usage.CompletionTokens),
		slog.Int("total_tokens", resp.Usage.TotalTokens),
		slog.Int("reasoning_tokens", resp.Usage.ReasoningTokens),
		slog.Int("cache_hit_tokens", resp.Usage.PromptCacheHitTokens),
		slog.Int64("duration_ms", dur.Milliseconds()),
	)
}

// logChatFailed 在 Chat 失败时触发（区别于 HTTP 阶段的 request failed）
func (c *Client) logChatFailed(ctx context.Context, req agent.LLMRequest, err error, dur time.Duration) {
	if c.log == nil {
		return
	}
	c.log.LogError(ctx, logger.CatLLM, "deepseek chat failed", err,
		slog.String("model", req.Model),
		slog.Int64("duration_ms", dur.Milliseconds()),
	)
}

// ─── small helpers ──────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
