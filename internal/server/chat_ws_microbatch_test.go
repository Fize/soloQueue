package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestBuildChatRouteMessageUsesEffectivePerAskRoute(t *testing.T) {
	a := agent.NewAgent(agent.Definition{
		ID:         "leader",
		ModelID:    "fallback-model",
		ProviderID: "fallback-provider",
	}, &agenttest.FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	sess := session.NewSession("l2:s1", "team", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	sess.Router = func(context.Context, string, string, []ctxwin.PayloadMessage) (session.RouteResult, error) {
		return session.RouteResult{Level: "engineering", ModelID: "routed-model", ProviderID: "routed-provider"}, nil
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-1", Origin: telemetry.OriginDesktop})
	ctx, routeCh := session.WithRequestRouteCapture(ctx)
	stream, err := sess.AskStream(ctx, "route me")
	if err != nil {
		t.Fatal(err)
	}

	route := <-routeCh
	msg := buildChatRouteMessage(sess, &route, "req-1", "l2:s1")
	if msg.Type != "chat_route" || msg.RequestID != "req-1" || msg.SessionID != "l2:s1" {
		t.Fatalf("unexpected envelope: %#v", msg)
	}
	if msg.TaskType != "engineering" || msg.ModelID != "routed-model" || msg.ProviderID != "routed-provider" {
		t.Fatalf("unexpected route metadata: %#v", msg)
	}
	if msg.AgentInstanceID != a.InstanceID {
		t.Fatalf("agent_instance_id = %q, want %q", msg.AgentInstanceID, a.InstanceID)
	}
	for range stream {
	}
}

func TestBuildChatRouteMessageFallsBackToDefinitionModel(t *testing.T) {
	a := agent.NewAgent(agent.Definition{
		ID:         "leader",
		ModelID:    "fallback-model",
		ProviderID: "fallback-provider",
	}, &agenttest.FakeLLM{}, nil)

	msg := buildChatRouteMessage(session.NewSession("l1", "team", a, nil, nil, nil), nil, "req-1", "l1")
	if msg.ModelID != "fallback-model" || msg.ProviderID != "fallback-provider" {
		t.Fatalf("unexpected fallback route metadata: %#v", msg)
	}
}

func TestBuildChatRouteMessageUsesRequestRouteBeforeQueuedActorStarts(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "leader", ModelID: "fallback"}, &agenttest.FakeLLM{Responses: []string{"ok"}}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	started := make(chan struct{})
	release := make(chan struct{})
	if err := a.Submit(context.Background(), func(context.Context) error {
		close(started)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	<-started
	sess := session.NewSession("l2:queued", "team", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	sess.Router = func(context.Context, string, string, []ctxwin.PayloadMessage) (session.RouteResult, error) {
		return session.RouteResult{Level: "research", ModelID: "queued-route", ProviderID: "queued-provider"}, nil
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-queued", Origin: telemetry.OriginDesktop})
	ctx, routeCh := session.WithRequestRouteCapture(ctx)
	stream, err := sess.AskStream(ctx, "queued")
	if err != nil {
		t.Fatal(err)
	}
	route := <-routeCh
	msg := buildChatRouteMessage(sess, &route, "req-queued", "l2:queued")
	if msg.ModelID != "queued-route" || msg.ProviderID != "queued-provider" || msg.TaskType != "research" {
		t.Fatalf("route before actor start = %#v", msg)
	}
	close(release)
	for range stream {
	}
}

func TestBuildChatRouteMessageUsesFreshGenerationOnRebuild(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "old", ModelID: "old-default"}, &agenttest.FakeLLM{}, nil)
	if err := old.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Quarantine(errors.New("stuck"))
	fresh := agent.NewAgent(agent.Definition{ID: "fresh", ModelID: "fresh-default"}, &agenttest.FakeLLM{Responses: []string{"ok"}}, nil)
	if err := fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Stop(time.Second) })
	sess := session.NewSession("l2:rebuild", "team", old, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	sess.SetAgentRebuilder(func(context.Context) (*agent.Agent, error) { return fresh, nil })
	sess.Router = func(context.Context, string, string, []ctxwin.PayloadMessage) (session.RouteResult, error) {
		return session.RouteResult{Level: "engineering", ModelID: "rebuilt-route", ProviderID: "rebuilt-provider"}, nil
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-rebuild", Origin: telemetry.OriginDesktop})
	ctx, routeCh := session.WithRequestRouteCapture(ctx)
	stream, err := sess.AskStream(ctx, "rebuild")
	if err != nil {
		t.Fatal(err)
	}
	route := <-routeCh
	msg := buildChatRouteMessage(sess, &route, "req-rebuild", "l2:rebuild")
	if msg.AgentInstanceID != fresh.InstanceID || msg.ModelID != "rebuilt-route" {
		t.Fatalf("rebuilt route = %#v", msg)
	}
	for range stream {
	}
}

// TestForwardAgentEvents_BatchesContentDeltas verifies that a burst of
// ContentDeltaEvents arriving within streamBatchInterval is collapsed into a
// single chat_chunk WS frame, dramatically reducing commit frequency on the
// client.
func TestForwardAgentEvents_BatchesContentDeltas(t *testing.T) {
	client := &Client{
		send:           make(chan []byte, 64),
		ctx:            context.Background(),
		activeRequests: make(map[string]*activeRequest),
	}
	reqCancel := func() {}
	client.addActiveRequest("req-test", reqCancel)

	ch := make(chan iface.AgentEvent, 32)
	go func() {
		defer close(ch)
		// 5 deltas within ~5ms — well inside streamBatchInterval (30ms).
		for i := 0; i < 5; i++ {
			ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "a"}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan struct{})
	h := &Hub{}
	go func() {
		h.forwardAgentEvents(client, "req-test", reqCancel, ch, "l1", "")
		close(done)
	}()

	chunkCount := 0
	var chunkBodies []string
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
loop:
	for {
		select {
		case data, ok := <-client.send:
			if !ok {
				break loop
			}
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if msg.Type == "chat_chunk" {
				chunkCount++
				chunkBodies = append(chunkBodies, msg.Delta)
			}
		case <-done:
			// Drain remaining buffered frames.
			for {
				select {
				case data, ok := <-client.send:
					if !ok {
						break loop
					}
					var msg WSMessage
					_ = json.Unmarshal(data, &msg)
					if msg.Type == "chat_chunk" {
						chunkCount++
						chunkBodies = append(chunkBodies, msg.Delta)
					}
				default:
					break loop
				}
			}
		case <-deadline.C:
			break loop
		}
	}

	if chunkCount != 1 {
		t.Errorf("expected exactly 1 batched chat_chunk frame, got %d (bodies=%q)", chunkCount, chunkBodies)
	}
	if len(chunkBodies) == 1 && chunkBodies[0] != "aaaaa" {
		t.Errorf("expected concatenated delta %q, got %q", "aaaaa", chunkBodies[0])
	}
}

func TestForwardAgentEventsKeepsDelegationStartOutOfAssistantContent(t *testing.T) {
	client := &Client{
		send:           make(chan []byte, 8),
		ctx:            context.Background(),
		activeRequests: make(map[string]*activeRequest),
	}
	reqCancel := func() {}
	client.addActiveRequest("req-delegation", reqCancel)

	ch := make(chan iface.AgentEvent, 1)
	ch <- agent.DelegationStartedEvent{Iter: 0, NumTasks: 1}
	close(ch)

	(&Hub{requests: NewActiveRequestRegistry()}).forwardAgentEvents(client, "req-delegation", reqCancel, ch, "l1", "")

	var msgs []WSMessage
	for {
		select {
		case data := <-client.send:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			msgs = append(msgs, msg)
		default:
			goto done
		}
	}
done:
	if len(msgs) != 1 || msgs[0].Type != "delegation_start" {
		t.Fatalf("delegation frames = %#v, want one delegation_start and no chat_chunk", msgs)
	}
}

// TestForwardAgentEvents_FlushesOnStructuralEvent verifies that when a
// structural event (e.g. ToolExecStartEvent) arrives while content deltas are
// pending, the pending deltas are flushed BEFORE the structural frame so the
// client always sees consistent ordering.
func TestForwardAgentEvents_FlushesOnStructuralEvent(t *testing.T) {
	client := &Client{
		send:           make(chan []byte, 64),
		ctx:            context.Background(),
		activeRequests: make(map[string]*activeRequest),
	}
	reqCancel := func() {}
	client.addActiveRequest("req-test", reqCancel)

	ch := make(chan iface.AgentEvent, 16)
	go func() {
		defer close(ch)
		ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "hello "}
		time.Sleep(5 * time.Millisecond)
		ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "world"}
		// Structural event — must trigger a flush of pending text first.
		ch <- agent.ToolExecStartEvent{Iter: 0, CallID: "c1", Name: "Read", Args: "{}"}
	}()

	h := &Hub{}
	h.forwardAgentEvents(client, "req-test", reqCancel, ch, "l1", "")
	_ = time.Millisecond

	var types []string
	for {
		select {
		case data, ok := <-client.send:
			if !ok {
				goto done
			}
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			types = append(types, msg.Type)
		default:
			goto done
		}
	}
done:
	if len(types) < 2 {
		t.Fatalf("expected at least 2 frames (chat_chunk + tool_start), got %d: %v", len(types), types)
	}
	if types[0] != "chat_chunk" {
		t.Errorf("expected first frame to be chat_chunk (flushed before tool_start), got %s", types[0])
	}
	if types[1] != "tool_start" {
		t.Errorf("expected second frame to be tool_start, got %s", types[1])
	}
}

func TestForwardAgentEvents_PreservesReasoningContentOrderAcrossMicrobatch(t *testing.T) {
	client := &Client{
		send:           make(chan []byte, 64),
		ctx:            context.Background(),
		activeRequests: make(map[string]*activeRequest),
	}
	reqCancel := func() {}
	client.addActiveRequest("req-test", reqCancel)

	ch := make(chan iface.AgentEvent, 16)
	ch <- agent.ReasoningDeltaEvent{Iter: 0, Delta: "Invest"}
	ch <- agent.ReasoningDeltaEvent{Iter: 0, Delta: "Lab."}
	ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "好了"}
	ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "，我对 SoloQueue 有了解。"}
	ch <- agent.ToolExecStartEvent{Iter: 0, CallID: "c1", Name: "delegate_team", Args: "{}"}
	close(ch)

	(&Hub{}).forwardAgentEvents(client, "req-test", reqCancel, ch, "l1", "")

	var msgs []WSMessage
	for {
		select {
		case data := <-client.send:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			msgs = append(msgs, msg)
		default:
			goto done
		}
	}
done:
	if len(msgs) != 3 {
		t.Fatalf("got %d frames, want 3: %#v", len(msgs), msgs)
	}
	if msgs[0].Type != "reasoning_chunk" || msgs[0].Delta != "InvestLab." {
		t.Errorf("first frame = %#v, want combined reasoning chunk", msgs[0])
	}
	if msgs[1].Type != "chat_chunk" || msgs[1].Delta != "好了，我对 SoloQueue 有了解。" {
		t.Errorf("second frame = %#v, want combined content chunk", msgs[1])
	}
	if msgs[2].Type != "tool_start" {
		t.Errorf("third frame = %#v, want tool_start", msgs[2])
	}
}
