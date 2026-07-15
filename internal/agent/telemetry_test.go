package agent

import (
	"context"
	"sync/atomic"
	"testing"

	llm "github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

func TestWithTelemetryContext_RoundTrip(t *testing.T) {
	ctx := WithTelemetryContext(context.Background(), "team-a", UsageChat)
	team, utype := TelemetryFromContext(ctx)
	if team != "team-a" {
		t.Errorf("team = %q, want team-a", team)
	}
	if utype != UsageChat {
		t.Errorf("usageType = %q, want chat", utype)
	}
}

func TestTelemetryFromContext_Empty(t *testing.T) {
	team, utype := TelemetryFromContext(context.Background())
	if team != "" {
		t.Errorf("team = %q, want empty", team)
	}
	if utype != "" {
		t.Errorf("usageType = %q, want empty", utype)
	}
}

func TestUsageTypeConstants(t *testing.T) {
	constants := []string{UsageChat, UsageRouter, UsageCompactor, UsageMemory, UsageSimulation}
	for _, c := range constants {
		if c == "" {
			t.Error("usage type constant should not be empty")
		}
	}
}

func TestNewTelemetryClient(t *testing.T) {
	inner := &FakeLLM{}
	tc := NewTelemetryClient(inner, nil)
	if tc == nil {
		t.Fatal("NewTelemetryClient returned nil")
	}
	if tc.db != nil {
		t.Error("db should be nil when passed nil")
	}
}

func TestTelemetryClient_Chat_PassThrough(t *testing.T) {
	inner := &FakeLLM{
		Responses: []string{"hello"},
	}
	tc := NewTelemetryClient(inner, nil)
	ctx := WithTelemetryContext(context.Background(), "team-a", UsageChat)

	resp, err := tc.Chat(ctx, LLMRequest{
		Model:      "test-model",
		ProviderID: "test-provider",
		Messages:   []LLMMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("content = %q, want hello", resp.Content)
	}
}

func TestTelemetryClient_Chat_ErrorPropagation(t *testing.T) {
	inner := &FakeLLM{Err: &llm.APIError{StatusCode: 500, Message: "server error"}}
	tc := NewTelemetryClient(inner, nil)
	_, err := tc.Chat(context.Background(), LLMRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTelemetryClient_ChatStream_PassThrough(t *testing.T) {
	inner := &FakeLLM{
		StreamDeltas:        [][]string{{"hello", " world"}},
	}
	tc := NewTelemetryClient(inner, nil)
	ctx := WithTelemetryContext(context.Background(), "team-a", UsageChat)

	ch, err := tc.ChatStream(ctx, LLMRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	var gotDone bool
	for ev := range ch {
		if ev.Type == llm.EventDone {
			gotDone = true
		} else {
			content += ev.ContentDelta
		}
	}
	if content != "hello world" {
		t.Errorf("content = %q, want 'hello world'", content)
	}
	if !gotDone {
		t.Error("expected EventDone")
	}
}

func TestTelemetryClient_ChatStream_ErrorPropagation(t *testing.T) {
	inner := &FakeLLM{Err: &llm.APIError{StatusCode: 429, Message: "rate limit"}}
	tc := NewTelemetryClient(inner, nil)
	ch, err := tc.ChatStream(context.Background(), LLMRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected return error: %v", err)
	}
	// FakeLLM sends errors as EventError in the stream, not as return error.
	var gotError bool
	for ev := range ch {
		if ev.Type == llm.EventError {
			gotError = true
		}
	}
	if !gotError {
		t.Error("expected EventError in stream")
	}
}

func TestTelemetryClient_UsageTrackingHook(t *testing.T) {
	// Verify that TelemetryClient triggers the inner LLM and the hook fires.
	var hookCount atomic.Int32
	inner := &FakeLLM{
		Responses: []string{"ok"},
		Hook: func(req LLMRequest) {
			hookCount.Add(1)
		},
	}
	tc := NewTelemetryClient(inner, nil)
	tc.Chat(context.Background(), LLMRequest{Model: "m"})
	if hookCount.Load() != 1 {
		t.Errorf("hook called %d times, want 1", hookCount.Load())
	}
}

func TestTelemetryClient_logUsageAsync_NilDB(t *testing.T) {
	tc := NewTelemetryClient(&FakeLLM{}, nil)
	tc.logUsageAsync(context.Background(), "m", llm.Usage{TotalTokens: 100})
	// No panic = pass.
}

func TestTelemetryClient_logUsageAsync_WithDB(t *testing.T) {
	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	var panicCaught atomic.Bool
	defer func() {
		if r := recover(); r != nil {
			panicCaught.Store(true)
		}
	}()

	tc := NewTelemetryClient(&FakeLLM{}, db)
	ctx := WithTelemetryContext(context.Background(), "team-a", UsageChat)
	tc.logUsageAsync(ctx, "test-model", llm.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	})
	if panicCaught.Load() {
		t.Error("logUsageAsync panicked with in-memory db")
	}
}

func TestTelemetryClient_logUsageAsync_EmptyUsageType(t *testing.T) {
	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	tc := NewTelemetryClient(&FakeLLM{}, db)
	// No telemetry context set → usage type defaults to "unknown", should not panic
	tc.logUsageAsync(context.Background(), "test-model", llm.Usage{TotalTokens: 100})
}
