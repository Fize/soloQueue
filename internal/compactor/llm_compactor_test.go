package compactor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
)

// ─── MockChatClient ─────────────────────────────────────────────────────────

type mockChatClient struct {
	chatFn func(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	called int
}

func (m *mockChatClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	m.called++
	if m.chatFn != nil {
		return m.chatFn(ctx, req)
	}
	return &ChatResponse{Content: "This is a compressed summary."}, nil
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestLLMCompactorCompact(t *testing.T) {
	mc := &mockChatClient{}
	c := NewLLMCompactor(mc, "deepseek", "test-model")

	msgs := []ctxwin.Message{
		{Role: ctxwin.RoleSystem, Content: "You are a helpful assistant."},
		{Role: ctxwin.RoleUser, Content: "Hello"},
		{Role: ctxwin.RoleAssistant, Content: "Hi there!"},
	}

	summary, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if summary == "" {
		t.Error("Compact returned empty summary")
	}
	if mc.called != 1 {
		t.Errorf("ChatClient.Chat called %d times, want 1", mc.called)
	}
}

func TestLLMCompactorCompactWithReasoning(t *testing.T) {
	var sawReasoningInInput bool
	mc := &mockChatClient{
		chatFn: func(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
			// Reasoning must NOT leak into the compactor's input — internal
			// chain-of-thought belongs to the agent, not the summary.
			for _, m := range req.Messages {
				if m.Role == "assistant" && strings.Contains(m.Content, "[Reasoning]") {
					sawReasoningInInput = true
				}
			}
			return &ChatResponse{Content: "Compressed summary."}, nil
		},
	}
	c := NewLLMCompactor(mc, "deepseek", "test-model")

	msgs := []ctxwin.Message{
		{Role: ctxwin.RoleUser, Content: "Explain recursion"},
		{Role: ctxwin.RoleAssistant, Content: "Recursion is...", ReasoningContent: "Let me think about this step by step"},
	}

	summary, err := c.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if summary != "Compressed summary." {
		t.Errorf("Unexpected summary: %q", summary)
	}
	if sawReasoningInInput {
		t.Error("Reasoning content must not leak into the compactor's input messages")
	}
}

func TestLLMCompactorStripsReasoningFromOutput(t *testing.T) {
	mc := &mockChatClient{
		chatFn: func(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
			return &ChatResponse{Content: "Header summary.\n\n[Reasoning]: This is internal chain of thought that must not leak.\n\nTrailing summary."}, nil
		},
	}
	c := NewLLMCompactor(mc, "deepseek", "test-model")

	summary, err := c.Compact(context.Background(), []ctxwin.Message{
		{Role: ctxwin.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if strings.Contains(summary, "[Reasoning]") {
		t.Errorf("Summary must not contain [Reasoning] block, got: %q", summary)
	}
	if !strings.Contains(summary, "Header summary.") {
		t.Errorf("Summary must keep non-reasoning content, got: %q", summary)
	}
	if !strings.Contains(summary, "Trailing summary.") {
		t.Errorf("Summary must keep trailing content, got: %q", summary)
	}
}

func TestLLMCompactorCompactEmpty(t *testing.T) {
	mc := &mockChatClient{}
	c := NewLLMCompactor(mc, "deepseek", "test-model")

	summary, err := c.Compact(context.Background(), nil)
	if err != nil {
		t.Fatalf("Compact with nil should not error: %v", err)
	}
	if summary != "" {
		t.Errorf("Compact with nil should return empty, got %q", summary)
	}
	if mc.called != 0 {
		t.Errorf("ChatClient should not be called for empty messages, called %d", mc.called)
	}
}

func TestLLMCompactorError(t *testing.T) {
	mc := &mockChatClient{
		chatFn: func(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
			return nil, fmt.Errorf("API error")
		},
	}
	c := NewLLMCompactor(mc, "deepseek", "test-model")

	msgs := []ctxwin.Message{
		{Role: ctxwin.RoleUser, Content: "Hello"},
	}

	_, err := c.Compact(context.Background(), msgs)
	if err == nil {
		t.Error("Expected error from Compact when ChatClient fails")
	}
}

func TestLLMCompactorUsesCorrectModel(t *testing.T) {
	var gotModel string
	mc := &mockChatClient{
		chatFn: func(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
			gotModel = req.Model
			return &ChatResponse{Content: "Summary"}, nil
		},
	}
	c := NewLLMCompactor(mc, "deepseek", "deepseek-v4-flash")

	msgs := []ctxwin.Message{
		{Role: ctxwin.RoleUser, Content: "Test"},
	}
	_, _ = c.Compact(context.Background(), msgs)

	if gotModel != "deepseek-v4-flash" {
		t.Errorf("Model = %q, want %q", gotModel, "deepseek-v4-flash")
	}
}

func TestCompactorPromptHasContinuationStateSchema(t *testing.T) {
	for _, heading := range []string{
		"## Current goal", "## Completed", "## Current state",
		"## Key decisions", "## Changed files", "## Remaining work",
		"## Blockers", "## User preferences",
	} {
		if !strings.Contains(compactSystemPrompt, heading) {
			t.Fatalf("compactor prompt missing continuation heading %q", heading)
		}
	}
	if !strings.Contains(compactSystemPrompt, "untrusted data") {
		t.Fatal("compactor prompt must define a trust boundary")
	}
}
