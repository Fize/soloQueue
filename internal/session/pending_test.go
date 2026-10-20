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
	ctx := withTemporalExposure(telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-qq-queued"}), receivedAt)
	if _, err := sess.AskStream(ctx, "queued"); !errors.Is(err, ErrQueued) {
		t.Fatalf("AskStream error = %v, want ErrQueued", err)
	}
	drained := sess.pending.Drain()
	if len(drained.RequestIDs) != 1 || drained.RequestIDs[0] != "req-qq-queued" {
		t.Fatalf("queued request IDs = %#v, want channel input identity for the reply boundary", drained.RequestIDs)
	}
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
	var boundarySeen bool
	for event := range out {
		if boundary, ok := event.(agent.PendingInputStartedEvent); ok {
			boundarySeen = true
			if boundary.Prompt != "queued" || boundary.ReceivedAt.IsZero() {
				t.Fatalf("boundary input metadata = %#v", boundary)
			}
		}
	}
	if !boundarySeen {
		t.Fatal("queued input did not emit a reply boundary")
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

func TestQueuedBoundaryStartsFreshAssistantOutput(t *testing.T) {
	for _, complete := range []bool{true, false} {
		name := "partial"
		if complete {
			name = "complete"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tl.Close() })
			cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer(), ctxwin.WithPushHook(func(msg ctxwin.Message) {
				_ = tl.AppendMessage(&timeline.MessagePayload{Role: string(msg.Role), Content: msg.Content, ReasoningContent: msg.ReasoningContent, Timestamp: msg.Timestamp.Format(time.RFC3339Nano), RequestID: msg.RequestID})
			}))
			a := startAgent(t, &agenttest.FakeLLM{})
			sess := NewSession("queued-output", "default", a, cw, tl, newTestLog(t))
			source := make(chan agent.AgentEvent)
			sess.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
				return source, nil
			}
			ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-root"})
			out, err := sess.AskStream(ctx, "root")
			if err != nil {
				t.Fatal(err)
			}
			receive := func() {
				t.Helper()
				select {
				case _, ok := <-out:
					if !ok {
						t.Fatal("output ended before boundary")
					}
				case <-time.After(time.Second):
					t.Fatal("output timed out")
				}
			}
			source <- agent.ReasoningDeltaEvent{RequestID: "req-root", Delta: "old reasoning"}
			receive()
			source <- agent.ContentDeltaEvent{RequestID: "req-root", Delta: "old output"}
			receive()
			// Simulate the agent persisting a completed iteration before draining input.
			cw.Push(ctxwin.RoleAssistant, "old output", ctxwin.WithRequestID("req-root"))
			cw.Push(ctxwin.RoleUser, "queued", ctxwin.WithRequestID("req-queued"))
			queued, _ := cw.MessageAt(cw.Len() - 1)
			source <- agent.PendingInputStartedEvent{RequestID: "req-queued", PreviousRequestID: "req-root", RequestIDs: []string{"req-queued"}}
			receive()
			source <- agent.ReasoningDeltaEvent{RequestID: "req-queued", Delta: "new reasoning"}
			receive()
			source <- agent.ContentDeltaEvent{RequestID: "req-queued", Delta: "new output"}
			receive()
			if complete {
				source <- agent.DoneEvent{RequestID: "req-queued", Content: "new output", ReasoningContent: "new reasoning"}
			}
			close(source)
			for range out {
			}
			if err := tl.Close(); err != nil {
				t.Fatal(err)
			}
			events, err := timeline.ReadFileEvents(timelineLatestFile(dir))
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, event := range events {
				msg := event.Message
				if msg == nil || msg.Role != "assistant" || msg.RequestID != "req-queued" {
					continue
				}
				found = true
				if msg.Content != "new output" || msg.ReasoningContent != "new reasoning" {
					t.Fatalf("queued output mixed with previous reply: %#v", msg)
				}
				stamp, err := time.Parse(time.RFC3339Nano, msg.Timestamp)
				if err != nil || stamp.Before(queued.Timestamp) {
					t.Fatalf("new reply timestamp=%q precedes queued input %s (parse error: %v)", msg.Timestamp, queued.Timestamp, err)
				}
			}
			if !found {
				t.Fatal("queued assistant reply was not persisted")
			}
		})
	}
}
