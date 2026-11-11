package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

func TestPendingContinuesAfterFinalModelCall(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var calls atomic.Int32
	fake := &agenttest.FakeLLM{Responses: []string{"first reply", "queued reply"}, Hook: func(req agent.LLMRequest) {
		calls.Add(1)
		once.Do(func() { close(started); <-release })
	}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	s := NewSession("pending-final", "default", a, cw, nil, newTestLog(t))
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "root"})
	out, err := s.AskStream(ctx, "first")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	for _, id := range []string{"second", "third"} {
		queuedCtx := withTemporalExposure(telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: id}), time.Now())
		if _, err := s.AskStream(queuedCtx, id); !errors.Is(err, ErrQueued) {
			t.Fatalf("enqueue: %v", err)
		}
	}
	unblock()
	var replies []string
	var boundary agent.PendingInputStartedEvent
	for ev := range out {
		switch e := ev.(type) {
		case agent.DoneEvent:
			replies = append(replies, e.Content)
		case agent.PendingInputStartedEvent:
			boundary = e
		case agent.ErrorEvent:
			t.Fatal(e.Err)
		}
	}
	if calls.Load() != 2 || strings.Join(replies, "|") != "first reply|queued reply" || s.HasPending() {
		t.Fatalf("calls=%d replies=%v pending=%v", calls.Load(), replies, s.HasPending())
	}
	if boundary.RequestID != "third" || len(boundary.RequestIDs) != 2 {
		t.Fatalf("boundary: %#v", boundary)
	}
	msgs := cw.BuildPayload()
	if len(msgs) != 4 || msgs[0].Content != "first" || msgs[1].Content != "first reply" || msgs[2].Content != "second\n\nthird" || msgs[3].Content != "queued reply" {
		t.Fatalf("duplicate or missing history: %#v", msgs)
	}
	last, _ := cw.MessageAt(3)
	if last.RequestID != "third" || len(msgs[2].TemporalParts) != 2 {
		t.Fatalf("lost queued metadata: %#v", msgs)
	}
}

func TestPendingArrivalRacesFinalCompletion(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	fake := &agenttest.FakeLLM{Responses: []string{"reply"}, Hook: func(agent.LLMRequest) { once.Do(func() { close(started); <-release }) }}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	s := NewSession("pending-race", "default", a, cw, nil, newTestLog(t))
	var consumers sync.WaitGroup
	consume := func(out <-chan iface.AgentEvent) {
		defer consumers.Done()
		for ev := range out {
			if e, ok := ev.(agent.ErrorEvent); ok {
				t.Error(e.Err)
			}
		}
	}
	out, err := s.AskStream(context.Background(), "root")
	if err != nil {
		t.Fatal(err)
	}
	consumers.Add(1)
	go consume(out)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	const count = 32
	var arrivals sync.WaitGroup
	for i := 0; i < count; i++ {
		arrivals.Add(1)
		go func(i int) {
			defer arrivals.Done()
			<-release
			out, err := s.AskStream(context.Background(), fmt.Sprintf("input-%d", i))
			if errors.Is(err, ErrQueued) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			consumers.Add(1)
			go consume(out)
		}(i)
	}
	unblock()
	arrivals.Wait()
	consumers.Wait()
	seen := map[string]int{}
	for _, msg := range cw.BuildPayload() {
		if msg.Role == string(ctxwin.RoleUser) {
			for _, input := range strings.Split(msg.Content, "\n\n") {
				seen[input]++
			}
		}
	}
	if s.HasPending() || len(seen) != count+1 {
		t.Fatalf("pending=%v inputs=%v", s.HasPending(), seen)
	}
	for input, n := range seen {
		if n != 1 {
			t.Errorf("%s consumed %d times", input, n)
		}
	}
}

func TestPendingContinuationStopsOnCancelOrClose(t *testing.T) {
	for _, action := range []string{"cancel", "close"} {
		t.Run(action, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			var calls atomic.Int32
			fake := &agenttest.FakeLLM{Responses: []string{"reply"}, Hook: func(agent.LLMRequest) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
				}
			}}
			a := startAgent(t, fake)
			s := NewSession("pending-stop", "default", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, newTestLog(t))
			out, err := s.AskStream(context.Background(), "first")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("model did not start")
			}
			if _, err := s.AskStream(context.Background(), "queued"); !errors.Is(err, ErrQueued) {
				t.Fatal(err)
			}
			if action == "close" {
				s.Close()
			} else {
				cancelled := make(chan struct{})
				s.cancelMu.Lock()
				for id, turn := range s.activeCancels {
					cancel := turn.cancel
					turn.cancel = func(cause error) { cancel(cause); close(cancelled) }
					s.activeCancels[id] = turn
				}
				s.cancelMu.Unlock()
				cancelDone := make(chan error, 1)
				go func() { cancelDone <- s.CancelCurrentByUser("stop") }()
				select {
				case <-cancelled:
				case <-time.After(time.Second):
					t.Fatal("cancel did not reach root")
				}
				unblock()
				if err := <-cancelDone; err != nil {
					t.Fatal(err)
				}
			}
			unblock()
			for range out {
			}
			if calls.Load() != 1 {
				t.Fatalf("stopped queue made %d model calls", calls.Load())
			}
			out, err = s.AskStream(context.Background(), "fresh")
			if action == "close" {
				if !errors.Is(err, ErrSessionClosed) {
					t.Fatalf("closed session: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				for range out {
				}
				if calls.Load() != 2 || s.HasPending() {
					t.Fatal("session did not recover cleanly")
				}
			}
		})
	}
}

func TestPendingContinuationKeepsWechatReplyStream(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var once sync.Once
	fake := &agenttest.FakeLLM{Responses: []string{"first reply;", "queued reply;", "other reply;"}, Hook: func(agent.LLMRequest) {
		once.Do(func() { close(started); <-release })
	}}
	a := startAgent(t, fake)
	log := newTestLog(t)
	s := NewSession("wechat-pending", "default", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, log)
	base := &channelAdapterBase{log: log}
	ctx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"})
	ctx, _ = base.withChannelRunID(withChannelTelemetry(ctx))
	out, releaseRoute, err := base.askChannelStream(ctx, s, "first")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRoute()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	if _, _, err := base.askChannelStream(ctx, s, "queued"); !errors.Is(err, ErrQueued) {
		t.Fatalf("enqueue: %v", err)
	}
	unblock()
	var delivered []string
	result, err := base.consumeAskStreamEvents(ctx, s, out, func(_ context.Context, text string) { delivered = append(delivered, text) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(delivered, "") != "first reply;" || result.Content != "queued reply;" || s.HasPending() {
		t.Fatalf("reply=%#v pending=%v", result, s.HasPending())
	}
	releaseRoute()
	other := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{Channel: "wechat", AccountID: "bot", ConversationID: "other", UserID: "other"})
	other, cancel := context.WithTimeout(other, time.Second)
	defer cancel()
	out, releaseOther, err := base.askChannelStream(other, s, "other")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseOther()
	result, err = base.consumeAskStreamEvents(other, s, out, nil)
	if err != nil || result.Content != "other reply;" {
		t.Fatalf("other route: %#v, %v", result, err)
	}
}

func TestPendingContinuationFailureAllowsNextRequest(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var calls atomic.Int32
	failure := errors.New("provider failed on continuation")
	fake := &agenttest.FakeLLM{Responses: []string{"reply"}}
	fake.Hook = func(agent.LLMRequest) {
		switch calls.Add(1) {
		case 1:
			close(started)
			<-release
		case 2:
			fake.Err = failure
		default:
			fake.Err = nil
		}
	}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	s := NewSession("pending-failure", "default", a, cw, nil, newTestLog(t))
	out, err := s.AskStream(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("model did not start")
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "queued"})
	if _, err := s.AskStream(ctx, "queued"); !errors.Is(err, ErrQueued) {
		t.Fatal(err)
	}
	unblock()
	var delivered []string
	base := &channelAdapterBase{log: newTestLog(t)}
	_, streamErr := base.consumeAskStreamEvents(context.Background(), s, out, func(_ context.Context, text string) { delivered = append(delivered, text) })
	// The channel consumer returns on Error; wait for session cleanup too.
	for range out {
	}
	seenError := errors.Is(streamErr, failure)
	if strings.Join(delivered, "|") != "reply" {
		t.Fatalf("completed reply lost or repeated: %v", delivered)
	}
	if !seenError || (s.inFlight.Load() != 0) || s.HasPending() || calls.Load() != 2 {
		t.Fatalf("error=%v calls=%d busy=%v pending=%v", seenError, calls.Load(), (s.inFlight.Load() != 0), s.HasPending())
	}
	msgs := cw.BuildPayload()
	queued, _ := cw.MessageAt(2)
	if len(msgs) != 3 || msgs[1].Content != "reply" || queued.Content != "queued" {
		t.Fatalf("failure lost history: %#v", msgs)
	}
	out, err = s.AskStream(context.Background(), "fresh")
	if err != nil {
		t.Fatal(err)
	}
	for ev := range out {
		if e, ok := ev.(agent.ErrorEvent); ok {
			t.Fatal(e.Err)
		}
	}
	if calls.Load() != 3 || (s.inFlight.Load() != 0) {
		t.Fatal("failed continuation blocked next request")
	}
}

func TestPendingControlCommandDoesNotRepeatCompletedTurn(t *testing.T) {
	for _, command := range []string{"/compact", "/clear"} {
		t.Run(command, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			var calls atomic.Int32
			fake := &agenttest.FakeLLM{Responses: []string{"reply"}, Hook: func(agent.LLMRequest) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
				}
			}}
			a := startAgent(t, fake)
			s := NewSession("pending-command", "default", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, newTestLog(t))
			out, err := s.AskStream(context.Background(), "first")
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("model did not start")
			}
			if _, err := s.AskStream(context.Background(), command); !errors.Is(err, ErrQueued) {
				t.Fatalf("queued command: %v", err)
			}
			unblock()
			for range out {
			}
			if calls.Load() != 1 || s.HasPending() {
				t.Fatalf("command caused extra execution: calls=%d pending=%v", calls.Load(), s.HasPending())
			}
			out, err = s.AskStream(context.Background(), "fresh")
			if err != nil {
				t.Fatal(err)
			}
			for range out {
			}
			if calls.Load() != 2 {
				t.Fatal("next request failed to run exactly once")
			}
		})
	}
}
