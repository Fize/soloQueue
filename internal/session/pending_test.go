package session

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

type pendingCancelTool struct {
	started chan struct{}
}

func (t *pendingCancelTool) Name() string                { return "pending_cancel" }
func (t *pendingCancelTool) Description() string         { return "blocks until cancelled" }
func (t *pendingCancelTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (t *pendingCancelTool) Execute(ctx context.Context, _ string) (string, error) {
	close(t.started)
	<-ctx.Done()
	return "", ctx.Err()
}

func TestPendingQueueDrainsOneAggregateTurn(t *testing.T) {
	q := &PendingQueue{}
	q.EnqueueMessage(PendingMessage{Prompt: "first", RequestID: "req-first"})
	q.EnqueueMessage(PendingMessage{Prompt: "second", RequestID: "req-second"})

	drained := q.Drain()
	if drained.Content != "first\n\nsecond" || len(drained.Parts) != 2 {
		t.Fatalf("drained = %#v, want one aggregate turn with two retained parts", drained)
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", q.Len())
	}
	if len(drained.RequestIDs) != 2 || drained.RequestIDs[0] != "req-first" || drained.RequestIDs[1] != "req-second" {
		t.Fatalf("request IDs = %#v, want queued identities in order", drained.RequestIDs)
	}
}

func TestPendingQueuePreservesMixedTemporalEligibility(t *testing.T) {
	firstAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	q := &PendingQueue{}
	q.EnqueueMessage(PendingMessage{Prompt: "channel", ReceivedAt: firstAt, ExposeTimestamp: true})
	q.EnqueueMessage(PendingMessage{Prompt: "web"})

	drained := q.Drain()
	if len(drained.Parts) != 2 || drained.Parts[0].Prompt != "channel" || !drained.Parts[0].ExposeTimestamp ||
		!drained.Parts[0].ReceivedAt.Equal(firstAt) || drained.Parts[1].Prompt != "web" || drained.Parts[1].ExposeTimestamp {
		t.Fatalf("drained parts = %#v, want ordered mixed eligibility", drained.Parts)
	}
}

func TestSessionPendingMessagesRemainOneAggregateModelTurn(t *testing.T) {
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	sess := NewSession("aggregate", "default", &agent.Agent{}, cw, nil, newTestLog(t))
	channelAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	sess.pending.EnqueueMessage(PendingMessage{Prompt: "channel", ReceivedAt: channelAt, ExposeTimestamp: true})
	sess.pending.EnqueueMessage(PendingMessage{Prompt: "web"})
	cw.DrainPending()

	payload := cw.BuildPayload()
	if len(payload) != 1 || sess.pending.Len() != 0 {
		t.Fatalf("payload=%#v pending=%d, want one turn and empty queue", payload, sess.pending.Len())
	}
	if payload[0].Content != "channel\n\nweb" || len(payload[0].TemporalParts) != 2 ||
		!payload[0].TemporalParts[0].ExposeTimestamp || payload[0].TemporalParts[1].ExposeTimestamp {
		t.Fatalf("payload = %#v, want one raw aggregate with mixed temporal parts", payload)
	}
}

func TestSessionPendingDrainReturnsOneProcessingBoundaryIdentity(t *testing.T) {
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	sess := NewSession("aggregate-boundary", "default", &agent.Agent{}, cw, nil, newTestLog(t))
	sess.pending.EnqueueMessage(PendingMessage{Prompt: "first", RequestID: "req-first"})
	sess.pending.EnqueueMessage(PendingMessage{Prompt: "second", RequestID: "req-second"})

	requestIDs := cw.DrainPending()
	if len(requestIDs) != 2 || requestIDs[0] != "req-first" || requestIDs[1] != "req-second" {
		t.Fatalf("request IDs = %#v, want both queued requests in order", requestIDs)
	}
}

func TestBusyAskRetainsTemporalMetadataWithoutChangingQueueOwnership(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"reply"}}
	a := startAgent(t, fake)
	sess := NewSession("busy", "default", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, newTestLog(t))
	sess.inFlight.Store(1)
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	ctx := withTemporalExposure(context.Background(), receivedAt)
	if _, err := sess.AskStream(ctx, "queued"); !errors.Is(err, ErrQueued) {
		t.Fatalf("AskStream error = %v, want ErrQueued", err)
	}
	drained := sess.pending.Drain()
	if len(drained.Parts) != 1 || drained.Parts[0].Prompt != "queued" || !drained.Parts[0].ExposeTimestamp || !drained.Parts[0].ReceivedAt.Equal(receivedAt) {
		t.Fatalf("pending = %#v, want original temporal metadata", drained)
	}
}

func TestQueueMessageRetainsDesktopRequestIdentity(t *testing.T) {
	sess := NewSession("queued-request", "default", &agent.Agent{}, nil, nil, newTestLog(t))
	sess.QueueMessage("queued", "req-queued")
	drained := sess.pending.Drain()
	if len(drained.RequestIDs) != 1 || drained.RequestIDs[0] != "req-queued" {
		t.Fatalf("request IDs = %#v, want desktop request identity", drained.RequestIDs)
	}
}

func TestQueuedBoundaryPersistsSubsequentAssistantUnderDeliveryRequest(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"queued reply"}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	sess := NewSession("queued-persistence", "default", a, cw, nil, newTestLog(t))
	sess.QueueMessage("queued", "req-queued")
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-root"})

	out, err := sess.AskStream(ctx, "root")
	if err != nil {
		t.Fatal(err)
	}
	for range out {
	}

	var queuedUser, queuedAssistant bool
	for i := 0; i < cw.Len(); i++ {
		msg, _ := cw.MessageAt(i)
		switch {
		case msg.Role == ctxwin.RoleUser && msg.Content == "queued":
			queuedUser = msg.RequestID == "req-queued"
		case msg.Role == ctxwin.RoleAssistant && msg.Content == "queued reply":
			queuedAssistant = msg.RequestID == "req-queued"
		}
	}
	if !queuedUser || !queuedAssistant {
		t.Fatalf("queued ownership: user=%v assistant=%v", queuedUser, queuedAssistant)
	}
}

func TestQueuedBoundaryPartialFlushUsesDeliveryRequest(t *testing.T) {
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"queued partial"}},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{
			{Index: 0, ID: "call-1", Name: "pending_cancel", Arguments: `{}`},
		}},
		FinishByTurn: []llm.FinishReason{llm.FinishToolCalls},
	}
	blocking := &pendingCancelTool{started: make(chan struct{})}
	a := agent.NewAgent(agent.Definition{ID: "queued-partial"}, fake, nil, agent.WithTools(blocking))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	dir := t.TempDir()
	tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tl.Close() })
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer(), ctxwin.WithPushHook(func(msg ctxwin.Message) {
		_ = tl.AppendMessage(&timeline.MessagePayload{
			Role:      string(msg.Role),
			Content:   msg.Content,
			RequestID: msg.RequestID,
		})
	}))
	sess := NewSession("queued-partial", "default", a, cw, tl, newTestLog(t))
	sess.QueueMessage("queued", "req-queued")
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-root"})
	out, err := sess.AskStream(ctx, "root")
	if err != nil {
		t.Fatal(err)
	}
	seenPartial := false
	for !seenPartial {
		select {
		case event, ok := <-out:
			if !ok {
				t.Fatal("stream closed before partial content was forwarded")
			}
			if delta, ok := event.(agent.ContentDeltaEvent); ok && delta.Delta == "queued partial" {
				seenPartial = true
			}
		case <-time.After(time.Second):
			t.Fatal("partial content was not forwarded")
		}
	}
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	if err := sess.CancelCurrent("test cancellation"); err != nil {
		t.Fatal(err)
	}
	for range out {
	}
	if err := tl.Close(); err != nil {
		t.Fatal(err)
	}

	events, err := timeline.ReadFileEvents(timelineLatestFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Message != nil && event.Message.Role == "assistant" && event.Message.Content == "queued partial" {
			if event.Message.RequestID != "req-queued" {
				t.Fatalf("partial assistant request ID = %q, want req-queued", event.Message.RequestID)
			}
			return
		}
	}
	t.Fatal("queued partial assistant row was not persisted")
}

func TestNonL1InputsRemainTemporallyUnmarked(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"direct": context.Background(),
		"web":    context.Background(),
		"l2":     withChannelTelemetry(context.Background()),
	} {
		t.Run(name, func(t *testing.T) {
			if opts := inputPushOptions(ctx); len(opts) != 0 {
				t.Fatalf("input options = %#v, want no temporal eligibility", opts)
			}
		})
	}
}
