package telemetry

import (
	"context"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"testing"
	"time"
)

type observationFake struct{}

func (observationFake) Chat(ctx context.Context, r agent.LLMRequest) (*agent.LLMResponse, error) {
	llm.ReportAttempt(ctx)
	llm.ReportAttempt(ctx)
	llm.ReportInstructions(ctx, "provider-transformed")
	return &agent.LLMResponse{Content: "answer", Usage: llm.Usage{TotalTokens: 12}}, nil
}
func (observationFake) ChatStream(ctx context.Context, r agent.LLMRequest) (<-chan llm.Event, error) {
	llm.ReportAttempt(ctx)
	ch := make(chan llm.Event, 3)
	ch <- llm.Event{Type: llm.EventDelta, ContentDelta: "intermediate"}
	ch <- llm.Event{Type: llm.EventDone, Usage: &llm.Usage{TotalTokens: 5}}
	close(ch)
	return ch, nil
}
func TestCallObservationStableIdentityAndProviderAttempts(t *testing.T) {
	s := observability.ForWorkDir(t.TempDir())
	c := NewTelemetryClient(observationFake{}, nil, s)
	ctx := WithTelemetryMetadata(context.Background(), Metadata{SessionID: "l1", RequestID: "r", AgentID: "instance", UsageType: UsageRouter})
	_, e := c.Chat(ctx, agent.LLMRequest{Model: "m", Messages: []agent.LLMMessage{{Role: "system", Content: "template"}}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Request("l1", "r")
	if e != nil || len(r.Calls) != 1 {
		t.Fatalf("%#v %v", r, e)
	}
	call := r.Calls[0]
	if call.AttemptCount != 2 || call.RetryCount != 1 || !call.AttemptsReported || call.TotalTokens != 12 || call.Content != "answer" {
		t.Fatalf("%#v", call)
	}
	p, e := s.CallPrompt("l1", "r", call.CallID)
	if e != nil || p.Instructions != "provider-transformed" {
		t.Fatalf("%#v %v", p, e)
	}
}
func TestStreamingObservationPersistsCompleteChunk(t *testing.T) {
	s := observability.ForWorkDir(t.TempDir())
	c := NewTelemetryClient(observationFake{}, nil, s)
	ctx := WithTelemetryMetadata(context.Background(), Metadata{SessionID: "l1", RequestID: "r"})
	ch, e := c.ChatStream(ctx, agent.LLMRequest{Model: "m"})
	if e != nil {
		t.Fatal(e)
	}
	for range ch {
	}
	deadline := time.Now().Add(time.Second)
	for {
		r, e := s.Request("l1", "r")
		if e != nil {
			t.Fatal(e)
		}
		if len(r.Calls) == 1 && r.Calls[0].Status == StatusSuccess {
			if r.Calls[0].Content != "intermediate" {
				t.Fatal(r)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("call never completed")
		}
	}
}

type terminalGateFake struct{ gate <-chan struct{} }

func (f terminalGateFake) Chat(context.Context, agent.LLMRequest) (*agent.LLMResponse, error) {
	return nil, nil
}
func (f terminalGateFake) ChatStream(ctx context.Context, _ agent.LLMRequest) (<-chan llm.Event, error) {
	ch := make(chan llm.Event)
	go func() {
		defer close(ch)
		select {
		case ch <- llm.Event{Type: llm.EventDelta, ContentDelta: "done"}:
		case <-ctx.Done():
			return
		}
		select {
		case <-f.gate:
		case <-ctx.Done():
			return
		}
		select {
		case ch <- llm.Event{Type: llm.EventDone}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}
func TestTerminalObservationPrecedesTerminalDelivery(t *testing.T) {
	store := observability.ForWorkDir(t.TempDir())
	gate := make(chan struct{})
	ctx, cancel := context.WithCancel(WithTelemetryMetadata(context.Background(), Metadata{SessionID: "l1", RequestID: "r"}))
	defer cancel()
	client := NewTelemetryClient(terminalGateFake{gate}, nil, store)
	stream, e := client.ChatStream(ctx, agent.LLMRequest{Model: "m"})
	if e != nil {
		t.Fatal(e)
	}
	<-stream
	locked := make(chan struct{})
	release := make(chan struct{})
	guardDone := make(chan struct{})
	go func() {
		defer close(guardDone)
		_ = store.GuardOptional("l1", "r", 0, func(_ observability.Policy, _ bool) error { close(locked); <-release; return nil })
	}()
	<-locked
	close(gate)
	select {
	case event := <-stream:
		t.Errorf("terminal event %v escaped before final observation could be written", event.Type)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	for range stream {
	}
	<-guardDone
	r, e := store.Request("l1", "r")
	if e != nil || len(r.Calls) != 1 || r.Calls[0].Status != "success" {
		t.Fatalf("final observation not ready %#v %v", r, e)
	}
}
