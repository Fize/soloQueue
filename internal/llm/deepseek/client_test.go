package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Test server helpers ─────────────────────────────────────────────────────

// script 是对单个 HTTP 请求的响应控制
type script struct {
	Status    int
	Headers   map[string]string
	Body      string        // 整段 body（非 SSE）
	SSE       []string      // 若非空，按 SSE 格式输出
	DelayBody time.Duration // 开始写 body 前的延迟
}

// recorder 记录服务端收到的请求
type recorder struct {
	mu       sync.Mutex
	requests []recordedReq
}

type recordedReq struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
}

func (r *recorder) add(req recordedReq) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
}

func (r *recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

func (r *recorder) Get(i int) recordedReq {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests[i]
}

// newFakeDeepSeek 按顺序返回 scripts 里的响应；第 i 次请求用 scripts[i]
// 若请求数超过 scripts 长度，返回 500
func newFakeDeepSeek(t *testing.T, scripts ...script) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	var idx atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.add(recordedReq{
			Method: r.Method, Path: r.URL.Path,
			Headers: r.Header.Clone(), Body: body,
		})

		i := int(idx.Add(1)) - 1
		if i >= len(scripts) {
			http.Error(w, "out of scripts", http.StatusInternalServerError)
			return
		}
		s := scripts[i]
		if s.DelayBody > 0 {
			select {
			case <-time.After(s.DelayBody):
			case <-r.Context().Done():
				return
			}
		}
		for k, v := range s.Headers {
			w.Header().Set(k, v)
		}
		status := s.Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)

		if len(s.SSE) > 0 {
			flusher, _ := w.(http.Flusher)
			for _, line := range s.SSE {
				if _, err := fmt.Fprint(w, line); err != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			return
		}
		if s.Body != "" {
			_, _ = io.WriteString(w, s.Body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// newClient 是测试构造 helper
func newClient(t *testing.T, srv *httptest.Server, opts ...func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// sseChunk 构造一行 SSE payload
func sseChunk(v any) string {
	data, _ := json.Marshal(v)
	return "data: " + string(data) + "\n\n"
}

// ─── NewClient ───────────────────────────────────────────────────────────────

func TestNewClient_DefaultBaseURL(t *testing.T) {
	c, err := NewClient(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.baseURL != "https://api.deepseek.com" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestNewClient_BaseURLTrimmed(t *testing.T) {
	c, _ := NewClient(Config{APIKey: "k", BaseURL: "https://api.deepseek.com/"})
	if c.baseURL != "https://api.deepseek.com" {
		t.Errorf("baseURL should trim trailing /, got %q", c.baseURL)
	}
}

// ─── ChatStream: happy path ─────────────────────────────────────────────────

func TestChatStream_Happy_ContentDeltas(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "Hello "}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "world"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	ch, err := c.ChatStream(context.Background(), agent.LLMRequest{
		Model: "deepseek-chat",
		Messages: []agent.LLMMessage{
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var events []llm.Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("events = %d (%+v)", len(events), events)
	}
	if events[0].ContentDelta != "Hello " {
		t.Errorf("[0].ContentDelta = %q", events[0].ContentDelta)
	}
	if events[1].ContentDelta != "world" {
		t.Errorf("[1].ContentDelta = %q", events[1].ContentDelta)
	}
	if events[2].Type != llm.EventDone || events[2].FinishReason != llm.FinishStop {
		t.Errorf("[2] = %+v", events[2])
	}
}

func TestChatStream_Happy_WithUsage(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "ok"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 10, "total_tokens": 15},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	ch, _ := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m", IncludeUsage: true})
	var done llm.Event
	for ev := range ch {
		if ev.Type == llm.EventDone {
			done = ev
		}
	}
	if done.Usage == nil || done.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v", done.Usage)
	}
}

func TestChatStream_KeepAlivesSkipped(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			": keep-alive\n\n",
			"\n\n",
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "x"}}},
			}),
			": ping\n\n",
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	ch, _ := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	count := 0
	var content string
	for ev := range ch {
		count++
		if ev.Type == llm.EventDelta {
			content += ev.ContentDelta
		}
	}
	if content != "x" {
		t.Errorf("content = %q", content)
	}
	if count != 2 { // 1 delta + 1 done
		t.Errorf("event count = %d, want 2", count)
	}
}

func TestChatStream_MalformedChunk_ProducesErrorEvent(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			"data: not-json\n\n",
		},
	})
	c := newClient(t, srv)

	ch, _ := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	var events []llm.Event
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 1 || events[0].Type != llm.EventError {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Err == nil {
		t.Error("Err nil")
	}
}

func TestChatStream_CtxCancel_DuringStream(t *testing.T) {
	// 构造一个每 chunk 间 sleep 的流，caller 中途取消
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		for i := 0; i < 100; i++ {
			if r.Context().Err() != nil {
				return
			}
			_, _ = fmt.Fprint(w, sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "x"}}},
			}))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, _ := c.ChatStream(ctx, agent.LLMRequest{Model: "m"})
	sawErr := false
	count := 0
	for ev := range ch {
		count++
		if ev.Type == llm.EventError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Error("expected EventError after ctx cancel")
	}
	if count == 0 {
		t.Error("should have received some events before cancel")
	}
}

// ─── APIError paths ─────────────────────────────────────────────────────────

func TestChatStream_APIError_401_NotRetried(t *testing.T) {
	srv, rec := newFakeDeepSeek(t, script{
		Status: 401,
		Body:   `{"error":{"message":"Invalid API key","type":"authentication_error","code":"invalid_api_key"}}`,
	})
	c := newClient(t, srv, func(cfg *Config) {
		cfg.Retry = llm.RetryPolicy{MaxRetries: 3, InitialDelay: 10 * time.Millisecond}
	})

	_, err := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err is not *APIError: %v", err)
	}
	if apiErr.StatusCode != 401 || apiErr.Code != "invalid_api_key" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	// 不 retry：只一次请求
	if rec.Len() != 1 {
		t.Errorf("HTTP calls = %d, want 1 (no retry)", rec.Len())
	}
}

func TestChatStream_APIError_429_Retried(t *testing.T) {
	// 前 2 次 429，第 3 次成功
	srv, rec := newFakeDeepSeek(t,
		script{Status: 429, Body: `{"error":{"message":"rate limit","type":"rate_limit_reached"}}`},
		script{Status: 429, Body: `{"error":{"message":"rate limit","type":"rate_limit_reached"}}`},
		script{SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "ok"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		}},
	)
	c := newClient(t, srv, func(cfg *Config) {
		cfg.Retry = llm.RetryPolicy{MaxRetries: 3, InitialDelay: 5 * time.Millisecond, Multiplier: 2}
	})

	ch, err := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	for range ch {
	} // drain
	if rec.Len() != 3 {
		t.Errorf("HTTP calls = %d, want 3", rec.Len())
	}
}

func TestChatStream_APIError_500_RetriedThenGivesUp(t *testing.T) {
	srv, rec := newFakeDeepSeek(t,
		script{Status: 500, Body: `{"error":{"message":"server"}}`},
		script{Status: 500, Body: `{"error":{"message":"server"}}`},
		script{Status: 500, Body: `{"error":{"message":"server"}}`},
	)
	c := newClient(t, srv, func(cfg *Config) {
		cfg.Retry = llm.RetryPolicy{MaxRetries: 2, InitialDelay: 1 * time.Millisecond}
	})

	_, err := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 500 {
		t.Errorf("err = %v", err)
	}
	if rec.Len() != 3 { // 1 original + 2 retries
		t.Errorf("HTTP calls = %d, want 3", rec.Len())
	}
}

func TestChatStream_RawErrorBody_NonEnvelope(t *testing.T) {
	// 非标准 envelope，Message 应填原始文本
	srv, _ := newFakeDeepSeek(t, script{
		Status: 502,
		Body:   "Bad Gateway",
	})
	c := newClient(t, srv)
	_, err := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	var apiErr *llm.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(apiErr.Message, "Bad Gateway") {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

// ─── Chat (synchronous wrapper) ─────────────────────────────────────────────

func TestChat_AccumulatesContent(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "Hello "}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "world"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
				"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	resp, err := c.Chat(context.Background(), agent.LLMRequest{
		Model:    "deepseek-chat",
		Messages: []agent.LLMMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.FinishReason != llm.FinishStop {
		t.Errorf("FinishReason = %q", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestChat_AccumulatesReasoningContent(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": "thinking "}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning_content": "hard"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "answer"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	resp, err := c.Chat(context.Background(), agent.LLMRequest{Model: "deepseek-reasoner"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.ReasoningContent != "thinking hard" {
		t.Errorf("ReasoningContent = %q", resp.ReasoningContent)
	}
	if resp.Content != "answer" {
		t.Errorf("Content = %q", resp.Content)
	}
}

func TestChat_AccumulatesToolCalls(t *testing.T) {
	// 跨 3 个 chunk 拼 tool_call arguments
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "id": "call_123", "type": "function",
						"function": map[string]any{"name": "get_weather", "arguments": ""},
					}},
				}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "function": map[string]any{"arguments": `{"loc":`},
					}},
				}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0, "function": map[string]any{"arguments": `"SF"}`},
					}},
				}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	resp, err := c.Chat(context.Background(), agent.LLMRequest{Model: "m"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.FinishReason != llm.FinishToolCalls {
		t.Errorf("FinishReason = %q", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ID = %q", tc.ID)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("Name = %q", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"loc":"SF"}` {
		t.Errorf("Arguments = %q", tc.Function.Arguments)
	}
}

func TestChat_StreamEndedWithoutDone(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "incomplete"}}},
			}),
			// 没有 finish_reason 也没有 [DONE]
		},
	})
	c := newClient(t, srv)
	_, err := c.Chat(context.Background(), agent.LLMRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error when stream ends without Done")
	}
	if !strings.Contains(err.Error(), "without done") {
		t.Errorf("err = %v", err)
	}
}

// ─── Request body assertions ────────────────────────────────────────────────

func TestChatStream_RequestBody_StreamAlwaysTrue(t *testing.T) {
	srv, rec := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	ch, _ := c.ChatStream(context.Background(), agent.LLMRequest{
		Model: "deepseek-chat",
		Messages: []agent.LLMMessage{
			{Role: "system", Content: "s"},
			{Role: "user", Content: "u"},
		},
		MaxTokens:    1024,
		IncludeUsage: true,
	})
	for range ch {
	}

	if rec.Len() != 1 {
		t.Fatalf("calls = %d", rec.Len())
	}
	req := rec.Get(0)
	if req.Method != "POST" {
		t.Errorf("method = %s", req.Method)
	}
	if req.Path != "/chat/completions" {
		t.Errorf("path = %s", req.Path)
	}
	if got := req.Headers.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	// 断言 body 包含 stream=true + stream_options.include_usage=true
	var body map[string]any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		t.Fatalf("body unmarshal: %v (raw: %s)", err, req.Body)
	}
	if body["stream"] != true {
		t.Errorf("stream = %v", body["stream"])
	}
	so, ok := body["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("stream_options = %+v", body["stream_options"])
	}
	if body["model"] != "deepseek-chat" {
		t.Errorf("model = %v", body["model"])
	}
	if body["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
}

func TestChatStream_CustomHeaders(t *testing.T) {
	srv, rec := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv, func(cfg *Config) {
		cfg.Headers = map[string]string{"X-Custom": "value"}
	})

	ch, _ := c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	for range ch {
	}

	if got := rec.Get(0).Headers.Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom = %q", got)
	}
}

// ─── Implements agent.LLMClient ──────────────────────────────────────────────

func TestClient_ImplementsLLMClient(t *testing.T) {
	srv, _ := newFakeDeepSeek(t, script{
		SSE: []string{
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": "hi"}}},
			}),
			sseChunk(map[string]any{
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			}),
			"data: [DONE]\n\n",
		},
	})
	c := newClient(t, srv)

	// 通过 agent.LLMClient 接口调用
	var client agent.LLMClient = c
	resp, err := client.Chat(context.Background(), agent.LLMRequest{
		Model:    "m",
		Messages: []agent.LLMMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat via interface: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q", resp.Content)
	}
}

// ─── Coverage fill-in ────────────────────────────────────────────────────────

func TestParseAPIError_ReadBodyFail(t *testing.T) {
	// 响应 Body 读失败：构造一个会在读时出错的 reader
	// 通过 httptest 很难模拟，这里直接传一个构造好的 response
	r := &http.Response{
		StatusCode: 500,
		Body:       errReader{},
	}
	apiErr := parseAPIError(r)
	if apiErr == nil || apiErr.StatusCode != 500 {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if !strings.Contains(apiErr.Message, "read body") {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

// errReader 每次 Read 都返回 error
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) { return 0, errors.New("read fail") }
func (errReader) Close() error               { return nil }

func TestTruncate(t *testing.T) {
	long := strings.Repeat("x", 100)
	got := truncate(long, 20)
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncate did not add marker: %q", got)
	}
	if len(got) <= 20 {
		t.Errorf("truncated string length = %d", len(got))
	}

	// short：不截断
	short := "hello"
	if got := truncate(short, 20); got != "hello" {
		t.Errorf("short string should not be truncated: %q", got)
	}
}

func TestClient_WithTimeoutMs(t *testing.T) {
	// 构造时传 TimeoutMs 走的是 "no HTTPClient" 分支（Timeout 生效）
	c, err := NewClient(Config{APIKey: "k", TimeoutMs: 5000})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.http.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v", c.http.Timeout)
	}
}

func TestChatStream_WithLogger(t *testing.T) {
	// 覆盖 logger 非 nil 的路径（logStart + logError）
	dir := t.TempDir()
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	defer log.Close()

	srv, _ := newFakeDeepSeek(t, script{
		Status: 500,
		Body:   `{"error":{"message":"oops"}}`,
	})

	c := newClient(t, srv, func(cfg *Config) {
		cfg.Log = log
	})

	_, err = c.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	if err == nil {
		t.Error("expected error")
	}
}

// ─── SSE tests ───────────────────────────────────────────────────────────────

func TestSSE_SimpleDataLine(t *testing.T) {
	input := "data: {\"foo\":1}\n\n"
	r := newSSEReader(strings.NewReader(input))

	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != `{"foo":1}` {
		t.Errorf("payload = %q", p)
	}

	// 后续应 EOF
	_, err = r.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("want EOF, got %v", err)
	}
}

func TestSSE_MultipleLines(t *testing.T) {
	input := "data: a\n\ndata: b\n\ndata: c\n\n"
	r := newSSEReader(strings.NewReader(input))

	for _, want := range []string{"a", "b", "c"} {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("Next(%s): %v", want, err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	_, err := r.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("after all lines: want EOF, got %v", err)
	}
}

func TestSSE_CommentLinesSkipped(t *testing.T) {
	input := ": keep-alive\n\n: ping\n\ndata: real\n\n"
	r := newSSEReader(strings.NewReader(input))

	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "real" {
		t.Errorf("payload = %q, want real", p)
	}
}

func TestSSE_BlankLinesSkipped(t *testing.T) {
	// 连续空行（SSE event boundary）不报错
	input := "\n\n\n\ndata: x\n\n"
	r := newSSEReader(strings.NewReader(input))
	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "x" {
		t.Errorf("payload = %q", p)
	}
}

func TestSSE_DoneMarker(t *testing.T) {
	input := "data: first\n\ndata: [DONE]\n\n"
	r := newSSEReader(strings.NewReader(input))

	p, err := r.Next()
	if err != nil {
		t.Fatalf("first Next: %v", err)
	}
	if p != "first" {
		t.Errorf("payload = %q", p)
	}

	_, err = r.Next()
	if !errors.Is(err, errSSEDone) {
		t.Errorf("second Next: want errSSEDone, got %v", err)
	}
}

func TestSSE_DoneMarker_ErrorString(t *testing.T) {
	// 顺便校验 sentinel 的字符串表示
	got := errSSEDone.Error()
	if !strings.Contains(got, "[DONE]") {
		t.Errorf("errSSEDone.Error = %q", got)
	}
}

func TestSSE_NonDataFieldsIgnored(t *testing.T) {
	// SSE 规范允许 event: id: retry: 字段，我们都忽略
	input := "event: message\nid: 123\nretry: 1000\ndata: real\n\n"
	r := newSSEReader(strings.NewReader(input))
	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "real" {
		t.Errorf("payload = %q", p)
	}
}

func TestSSE_DataWithoutSpace(t *testing.T) {
	// "data:xxx" 没有空格也合法
	input := "data:hello\n\n"
	r := newSSEReader(strings.NewReader(input))
	p, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if p != "hello" {
		t.Errorf("payload = %q", p)
	}
}

func TestSSE_EmptyReader(t *testing.T) {
	r := newSSEReader(strings.NewReader(""))
	_, err := r.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("empty reader: want EOF, got %v", err)
	}
}
