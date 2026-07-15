package telemetry_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/telemetry"
)

func TestWithTelemetryContext_RoundTrip(t *testing.T) {
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)
	team, utype := telemetry.TelemetryFromContext(ctx)
	if team != "team-a" {
		t.Errorf("team = %q, want team-a", team)
	}
	if utype != telemetry.UsageChat {
		t.Errorf("usageType = %q, want chat", utype)
	}
}

func TestTelemetryFromContext_Empty(t *testing.T) {
	team, utype := telemetry.TelemetryFromContext(context.Background())
	if team != "" {
		t.Errorf("team = %q, want empty", team)
	}
	if utype != "" {
		t.Errorf("usageType = %q, want empty", utype)
	}
}

func TestNewTelemetryClient(t *testing.T) {
	inner := &agent.FakeLLM{}
	tc := telemetry.NewTelemetryClient(inner, nil)
	if tc == nil {
		t.Fatal("NewTelemetryClient returned nil")
	}
}

func TestTelemetryClient_Chat_PassThrough(t *testing.T) {
	inner := &agent.FakeLLM{Responses: []string{"hello"}}
	tc := telemetry.NewTelemetryClient(inner, nil)
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)

	resp, err := tc.Chat(ctx, agent.LLMRequest{
		Model:      "test-model",
		ProviderID: "test-provider",
		Messages:   []agent.LLMMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("content = %q, want hello", resp.Content)
	}
}

func TestTelemetryClient_Chat_ErrorPropagation(t *testing.T) {
	inner := &agent.FakeLLM{Err: &llm.APIError{StatusCode: 500, Message: "server error"}}
	tc := telemetry.NewTelemetryClient(inner, nil)
	_, err := tc.Chat(context.Background(), agent.LLMRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTelemetryClient_ChatStream_PassThrough(t *testing.T) {
	inner := &agent.FakeLLM{StreamDeltas: [][]string{{"hello", " world"}}}
	tc := telemetry.NewTelemetryClient(inner, nil)
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)

	ch, err := tc.ChatStream(ctx, agent.LLMRequest{Model: "test-model"})
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
	inner := &agent.FakeLLM{Err: &llm.APIError{StatusCode: 429, Message: "rate limit"}}
	tc := telemetry.NewTelemetryClient(inner, nil)
	ch, err := tc.ChatStream(context.Background(), agent.LLMRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected return error: %v", err)
	}
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
	var hookCount atomic.Int32
	inner := &agent.FakeLLM{
		Responses: []string{"ok"},
		Hook: func(req agent.LLMRequest) {
			hookCount.Add(1)
		},
	}
	tc := telemetry.NewTelemetryClient(inner, nil)
	tc.Chat(context.Background(), agent.LLMRequest{Model: "m"})
	if hookCount.Load() != 1 {
		t.Errorf("hook called %d times, want 1", hookCount.Load())
	}
}

func TestTelemetryClient_logUsageAsync_NilDB(t *testing.T) {
	tc := telemetry.NewTelemetryClient(&agent.FakeLLM{}, nil)
	// Call Chat which triggers logUsageAsync with nil db — should not panic.
	_, _ = tc.Chat(context.Background(), agent.LLMRequest{Model: "m"})
}

func TestTelemetryClient_logUsageAsync_WithDB(t *testing.T) {
	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	tc := telemetry.NewTelemetryClient(&agent.FakeLLM{Responses: []string{"ok"}}, db)
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)
	resp, err := tc.Chat(ctx, agent.LLMRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	_ = resp
}

func TestTelemetryClient_logUsageAsync_EmptyUsageType(t *testing.T) {
	db, err := sqlitedb.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	tc := telemetry.NewTelemetryClient(&agent.FakeLLM{Responses: []string{"ok"}}, db)
	// No telemetry context set — should default to "unknown", not panic.
	_, err = tc.Chat(context.Background(), agent.LLMRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}
