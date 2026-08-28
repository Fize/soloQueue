package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

func TestPendingQueueDrainsOneAggregateTurn(t *testing.T) {
	q := &PendingQueue{}
	q.EnqueueMessage(PendingMessage{Prompt: "first"})
	q.EnqueueMessage(PendingMessage{Prompt: "second"})

	drained := q.Drain()
	if drained.Content != "first\n\nsecond" || len(drained.Parts) != 2 {
		t.Fatalf("drained = %#v, want one aggregate turn with two retained parts", drained)
	}
	if q.Len() != 0 {
		t.Fatalf("queue length = %d, want 0", q.Len())
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

func TestBusyAskRetainsTemporalMetadataWithoutChangingQueueOwnership(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"reply"}}
	a := startAgent(t, fake)
	sess := NewSession("busy", "default", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, newTestLog(t))
	sess.inFlight.Store(1)
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	ctx := withTemporalExposure(context.Background(), receivedAt)
	if _, err := sess.Ask(ctx, "queued"); !errors.Is(err, ErrQueued) {
		t.Fatalf("Ask error = %v, want ErrQueued", err)
	}
	drained := sess.pending.Drain()
	if len(drained.Parts) != 1 || drained.Parts[0].Prompt != "queued" || !drained.Parts[0].ExposeTimestamp || !drained.Parts[0].ReceivedAt.Equal(receivedAt) {
		t.Fatalf("pending = %#v, want original temporal metadata", drained)
	}
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
