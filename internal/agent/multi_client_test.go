package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func TestRoutingClient_Chat(t *testing.T) {
	fake1 := &FakeLLM{Responses: []string{"response from provider 1"}}
	fake2 := &FakeLLM{Responses: []string{"response from provider 2"}}

	clients := map[string]LLMClient{
		"p1": fake1,
		"p2": fake2,
	}

	rc := NewRoutingClient(clients)

	// Route to p1
	req1 := LLMRequest{
		ProviderID: "p1",
		Model:      "model-1",
	}
	resp1, err := rc.Chat(context.Background(), req1)
	if err != nil {
		t.Fatalf("Chat p1 failed: %v", err)
	}
	if resp1.Content != "response from provider 1" {
		t.Errorf("expected response from provider 1, got %q", resp1.Content)
	}

	// Route to p2
	req2 := LLMRequest{
		ProviderID: "p2",
		Model:      "model-2",
	}
	resp2, err := rc.Chat(context.Background(), req2)
	if err != nil {
		t.Fatalf("Chat p2 failed: %v", err)
	}
	if resp2.Content != "response from provider 2" {
		t.Errorf("expected response from provider 2, got %q", resp2.Content)
	}

	// Empty providerID should return error
	reqEmpty := LLMRequest{
		ProviderID: "",
		Model:      "model-3",
	}
	_, err = rc.Chat(context.Background(), reqEmpty)
	if err == nil {
		t.Error("expected error for empty provider ID, got nil")
	} else if !strings.Contains(err.Error(), "provider ID is empty") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Unknown providerID should return error
	reqUnknown := LLMRequest{
		ProviderID: "unknown",
		Model:      "model-4",
	}
	_, err = rc.Chat(context.Background(), reqUnknown)
	if err == nil {
		t.Error("expected error for unknown provider ID, got nil")
	} else if !strings.Contains(err.Error(), "provider \"unknown\" not initialized or not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRoutingClient_ChatStream(t *testing.T) {
	fake1 := &FakeLLM{Responses: []string{"stream delta 1"}}
	clients := map[string]LLMClient{
		"p1": fake1,
	}

	rc := NewRoutingClient(clients)

	req := LLMRequest{
		ProviderID: "p1",
		Model:      "model-1",
	}
	ch, err := rc.ChatStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var events []llm.Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (Delta + Done), got %d", len(events))
	}
	if events[0].ContentDelta != "stream delta 1" {
		t.Errorf("expected ContentDelta 'stream delta 1', got %q", events[0].ContentDelta)
	}
}

func TestRoutingClient_UpdateClients(t *testing.T) {
	fake1 := &FakeLLM{Responses: []string{"p1 response"}}
	fake2 := &FakeLLM{Responses: []string{"p2 response"}}

	rc := NewRoutingClient(map[string]LLMClient{
		"p1": fake1,
	})

	// Before update: p2 is unknown
	req := LLMRequest{
		ProviderID: "p2",
		Model:      "model-2",
	}
	_, err := rc.Chat(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for p2 before update, got nil")
	}

	// Update clients
	rc.UpdateClients(map[string]LLMClient{
		"p1": fake1,
		"p2": fake2,
	})

	// After update: p2 is successfully routed
	resp, err := rc.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat p2 failed after update: %v", err)
	}
	if resp.Content != "p2 response" {
		t.Errorf("expected 'p2 response', got %q", resp.Content)
	}
}
