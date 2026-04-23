//go:build integration

// integration_test.go — 使用真实 DeepSeek API 的集成测试
//
// 运行方式（需要 API Key）：
//
//	DEEPSEEK_API_KEY=sk-xxx \
//	  go test -v -tags integration -timeout 120s ./internal/llm/deepseek/
//
// 或加载 .env 后运行：
//
//	set -a && source ../../.env && set +a && \
//	  go test -v -tags integration -timeout 120s ./internal/llm/deepseek/
package deepseek_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
)

// resolveKey 读取 DEEPSEEK_API_KEY
func resolveKey(t *testing.T) string {
	t.Helper()
	v := os.Getenv("DEEPSEEK_API_KEY")
	if v == "" {
		t.Skip("no API key found: set DEEPSEEK_API_KEY")
	}
	t.Logf("using API key from env DEEPSEEK_API_KEY")
	return v
}

func resolveBaseURL() string {
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" {
		return v
	}
	return "https://api.deepseek.com/v1"
}

func newTestClient(t *testing.T, model string) *deepseek.Client {
	t.Helper()
	apiKey := resolveKey(t)
	baseURL := resolveBaseURL()
	t.Logf("base_url=%s model=%s", baseURL, model)

	c, err := deepseek.NewClient(deepseek.Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		TimeoutMs: 90_000,
		Retry:     llm.RetryPolicy{MaxRetries: 0},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// ─── Test: simple Chat (non-streaming accumulation) ───────────────────────────

func TestIntegration_Chat_SimpleQuestion(t *testing.T) {
	c := newTestClient(t, "deepseek-chat")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := agent.LLMRequest{
		Model: "deepseek-chat",
		Messages: []agent.LLMMessage{
			{Role: "user", Content: "Reply with exactly the text: hello world"},
		},
		MaxTokens:   64,
		Temperature: 0,
	}

	resp, err := c.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	t.Logf("content=%q finish=%s tokens=%+v", resp.Content, resp.FinishReason, resp.Usage)

	if resp.Content == "" {
		t.Error("expected non-empty content")
	}
	if !strings.Contains(strings.ToLower(resp.Content), "hello") {
		t.Errorf("expected 'hello' in response, got: %q", resp.Content)
	}
}

// ─── Test: streaming ChatStream ────────────────────────────────────────────────

func TestIntegration_ChatStream_Streaming(t *testing.T) {
	c := newTestClient(t, "deepseek-chat")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req := agent.LLMRequest{
		Model: "deepseek-chat",
		Messages: []agent.LLMMessage{
			{Role: "user", Content: "Count from 1 to 5, one number per line."},
		},
		MaxTokens:   128,
		Temperature: 0,
	}

	evCh, err := c.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var sb strings.Builder
	var gotDone bool
	var deltaCount int

	for ev := range evCh {
		switch ev.Type {
		case llm.EventDelta:
			deltaCount++
			sb.WriteString(ev.ContentDelta)
		case llm.EventDone:
			gotDone = true
			t.Logf("finish=%s tokens=%+v", ev.FinishReason, ev.Usage)
		case llm.EventError:
			t.Fatalf("stream error: %v", ev.Err)
		}
	}

	full := sb.String()
	t.Logf("deltas=%d full=%q", deltaCount, full)

	if deltaCount == 0 {
		t.Error("expected at least one delta event")
	}
	if !gotDone {
		t.Error("expected a Done event")
	}
	if full == "" {
		t.Error("expected non-empty streamed content")
	}
	// 简单检查是否包含数字
	for _, digit := range []string{"1", "2", "3"} {
		if !strings.Contains(full, digit) {
			t.Errorf("expected %q in response, got: %q", digit, full)
		}
	}
}

// ─── Test: reasoning model (deepseek-reasoner) ────────────────────────────────

func TestIntegration_ChatStream_Reasoner(t *testing.T) {
	c := newTestClient(t, "deepseek-reasoner")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	req := agent.LLMRequest{
		Model: "deepseek-reasoner",
		Messages: []agent.LLMMessage{
			{Role: "user", Content: "What is 7 * 8? Answer with just the number."},
		},
		MaxTokens:   512,
		Temperature: 0,
	}

	evCh, err := c.ChatStream(ctx, req)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var content, reasoning strings.Builder
	for ev := range evCh {
		switch ev.Type {
		case llm.EventDelta:
			content.WriteString(ev.ContentDelta)
			reasoning.WriteString(ev.ReasoningContentDelta)
		case llm.EventError:
			t.Fatalf("stream error: %v", ev.Err)
		case llm.EventDone:
			t.Logf("finish=%s tokens=%+v", ev.FinishReason, ev.Usage)
		}
	}

	t.Logf("reasoning=%q", reasoning.String())
	t.Logf("content=%q", content.String())

	if !strings.Contains(content.String(), "56") {
		t.Errorf("expected '56' in content, got: %q", content.String())
	}
}

// ─── Test: multi-turn conversation ────────────────────────────────────────────

func TestIntegration_Chat_MultiTurn(t *testing.T) {
	c := newTestClient(t, "deepseek-chat")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := []agent.LLMMessage{
		{Role: "user", Content: "My favorite color is blue. Remember this."},
		{Role: "assistant", Content: "Got it! Your favorite color is blue."},
		{Role: "user", Content: "What is my favorite color?"},
	}

	resp, err := c.Chat(ctx, agent.LLMRequest{
		Model:       "deepseek-chat",
		Messages:    messages,
		MaxTokens:   64,
		Temperature: 0,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	t.Logf("response: %q", resp.Content)

	if !strings.Contains(strings.ToLower(resp.Content), "blue") {
		t.Errorf("expected 'blue' in response, got: %q", resp.Content)
	}
}

// ─── Test: context cancellation ────────────────────────────────────────────────

func TestIntegration_ChatStream_Cancellation(t *testing.T) {
	c := newTestClient(t, "deepseek-chat")
	outerCtx, outerCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer outerCancel()

	// 用一个可提前取消的子 ctx 来中断流
	streamCtx, cancelStream := context.WithCancel(outerCtx)

	req := agent.LLMRequest{
		Model: "deepseek-chat",
		Messages: []agent.LLMMessage{
			{Role: "user", Content: "Write a very long essay about the history of computing, at least 2000 words."},
		},
		MaxTokens:   2048,
		Temperature: 0.7,
	}

	evCh, err := c.ChatStream(streamCtx, req)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var deltaCount int
	for ev := range evCh {
		if ev.Type == llm.EventDelta && ev.ContentDelta != "" {
			deltaCount++
			if deltaCount >= 3 {
				cancelStream() // 收到 3 个 delta 后立即取消
			}
		}
		if ev.Type == llm.EventError {
			// context.Canceled 是预期错误
			break
		}
	}

	// 排空 channel
	for range evCh {
	}

	if deltaCount == 0 {
		t.Error("expected at least one delta before cancel")
	}
	t.Logf("received %d deltas before cancel", deltaCount)
}
