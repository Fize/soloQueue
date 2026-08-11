package telemetry_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

type usageLLM struct {
	response *agent.LLMResponse
	err      error
}

func (l *usageLLM) Chat(context.Context, agent.LLMRequest) (*agent.LLMResponse, error) {
	return l.response, l.err
}

func (l *usageLLM) ChatStream(context.Context, agent.LLMRequest) (<-chan llm.Event, error) {
	if l.err != nil {
		return nil, l.err
	}
	ch := make(chan llm.Event, 1)
	ch <- llm.Event{Type: llm.EventDone, FinishReason: l.response.FinishReason, Usage: &l.response.Usage}
	close(ch)
	return ch, nil
}

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

func TestTelemetryMetadata_RoundTrip(t *testing.T) {
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)
	ctx = telemetry.WithTelemetryMetadata(ctx, telemetry.Metadata{
		RequestID: "request-1",
		SessionID: "session-1",
		RunID:     "run-1",
		AgentID:   "agent-1",
		Origin:    telemetry.OriginDesktop,
		TaskType:  "engineering",
	})
	got := telemetry.MetadataFromContext(ctx)
	if got.TeamID != "team-a" || got.UsageType != telemetry.UsageChat {
		t.Fatalf("base metadata = %+v", got)
	}
	if got.RequestID != "request-1" || got.Origin != telemetry.OriginDesktop || got.TaskType != "engineering" {
		t.Fatalf("correlation metadata = %+v", got)
	}
}

func TestNewTelemetryClient(t *testing.T) {
	inner := &agenttest.FakeLLM{}
	tc := telemetry.NewTelemetryClient(inner, nil)
	if tc == nil {
		t.Fatal("NewTelemetryClient returned nil")
	}
}

func TestTelemetryClient_Chat_PassThrough(t *testing.T) {
	inner := &agenttest.FakeLLM{Responses: []string{"hello"}}
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
	inner := &agenttest.FakeLLM{Err: &llm.APIError{StatusCode: 500, Message: "server error"}}
	tc := telemetry.NewTelemetryClient(inner, nil)
	_, err := tc.Chat(context.Background(), agent.LLMRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTelemetryClient_ChatStream_PassThrough(t *testing.T) {
	inner := &agenttest.FakeLLM{StreamDeltas: [][]string{{"hello", " world"}}}
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
	inner := &agenttest.FakeLLM{Err: &llm.APIError{StatusCode: 429, Message: "rate limit"}}
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
	inner := &agenttest.FakeLLM{
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
	tc := telemetry.NewTelemetryClient(&agenttest.FakeLLM{}, nil)
	// Call Chat which triggers logUsageAsync with nil db — should not panic.
	_, _ = tc.Chat(context.Background(), agent.LLMRequest{Model: "m"})
}

func TestTelemetryClient_logUsageAsync_WithDB(t *testing.T) {
	db, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	tc := telemetry.NewTelemetryClient(&agenttest.FakeLLM{Responses: []string{"ok"}}, db)
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)
	resp, err := tc.Chat(ctx, agent.LLMRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	_ = resp
}

func TestTelemetryClient_logUsageAsync_EmptyUsageType(t *testing.T) {
	db, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer db.Close()

	tc := telemetry.NewTelemetryClient(&agenttest.FakeLLM{Responses: []string{"ok"}}, db)
	// No telemetry context set — should default to "unknown", not panic.
	_, err = tc.Chat(context.Background(), agent.LLMRequest{Model: "test-model"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}

func TestTelemetryClient_RecordsSuccessfulCall(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer database.Close()

	inner := &usageLLM{response: &agent.LLMResponse{
		FinishReason: llm.FinishStop,
		Usage: llm.Usage{
			PromptTokens:          100,
			CompletionTokens:      50,
			TotalTokens:           150,
			ReasoningTokens:       20,
			PromptCacheHitTokens:  80,
			PromptCacheMissTokens: 20,
		},
	}}
	client := telemetry.NewTelemetryClient(inner, database)
	ctx := telemetry.WithTelemetryContext(context.Background(), "team-a", telemetry.UsageChat)
	ctx = telemetry.WithTelemetryMetadata(ctx, telemetry.Metadata{
		RequestID: "request-1",
		Origin:    telemetry.OriginDesktop,
		TaskType:  "engineering",
	})
	if _, err := client.Chat(ctx, agent.LLMRequest{ProviderID: "provider-a", Model: "model-a"}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	rows := waitForMetrics(t, database, 1)
	got := rows[0]
	if got.Status != telemetry.StatusSuccess || got.ReasoningTokens != 20 {
		t.Fatalf("metric = %+v", got)
	}
	if got.RequestID != "request-1" || got.Origin != telemetry.OriginDesktop || got.TaskType != "engineering" {
		t.Fatalf("metadata = %+v", got)
	}
	if got.ProviderID != "provider-a" || got.ModelID != "model-a" {
		t.Fatalf("model identity = %+v", got)
	}
}

func TestTelemetryClient_RecordsFailedCall(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	defer database.Close()

	client := telemetry.NewTelemetryClient(&usageLLM{err: errors.New("provider unavailable")}, database)
	_, _ = client.Chat(context.Background(), agent.LLMRequest{ProviderID: "provider-a", Model: "model-a"})

	rows := waitForMetrics(t, database, 1)
	if rows[0].Status != telemetry.StatusError || rows[0].ErrorCode == "" {
		t.Fatalf("failed metric = %+v", rows[0])
	}
}

func waitForMetrics(t *testing.T, database *db.DB, want int) []db.LLMCallMetric {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		rows, err := database.ListLLMCallMetrics(context.Background(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatalf("ListLLMCallMetrics: %v", err)
		}
		if len(rows) >= want {
			return rows
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d metrics", want)
	return nil
}
