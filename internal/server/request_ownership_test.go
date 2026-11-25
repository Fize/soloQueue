package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

type cancelRunRecorder struct {
	runID  string
	reason string
}

type serverFakeClock struct {
	mu  sync.Mutex
	now time.Time
}

type serverBlockingCompactor struct {
	started chan struct{}
}

func TestLogicalRuntimeSessionIDUsesSnapshotKeys(t *testing.T) {
	for target, want := range map[string]string{
		"l1-session":   "l1",
		"l1":           "l1",
		"session-1":    "l2:session-1",
		"l2:session-1": "l2:session-1",
		"l2-bd1049bb-7977-4575-affe-bc7d665fb32a-session": "l2:bd1049bb-7977-4575-affe-bc7d665fb32a",
	} {
		if got := logicalRuntimeSessionID(target); got != want {
			t.Fatalf("logicalRuntimeSessionID(%q) = %q, want %q", target, got, want)
		}
	}
}

func (c *serverBlockingCompactor) Compact(ctx context.Context, _ []ctxwin.Message) (string, error) {
	close(c.started)
	<-ctx.Done()
	return "", context.Cause(ctx)
}

func (c *serverFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *serverFakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (r *cancelRunRecorder) CancelRun(runID, reason string) error {
	r.runID = runID
	r.reason = reason
	return nil
}

func TestCancelRequestTargetsValidatedRequestID(t *testing.T) {
	recorder := &cancelRunRecorder{}
	if err := cancelRequest(recorder, "req-target", "User cancelled"); err != nil {
		t.Fatalf("cancelRequest() error = %v", err)
	}
	if recorder.runID != "req-target" || recorder.reason != "User cancelled" {
		t.Fatalf("CancelRun() received runID=%q reason=%q", recorder.runID, recorder.reason)
	}
}

func TestProjectLiveWatchdogUsesRunWatchSnapshot(t *testing.T) {
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Hour})
	defer watchdog.Close()
	mgr := session.NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		a := agent.NewAgent(agent.Definition{ID: "runtime-agent"}, &agenttest.FakeLLM{}, nil)
		if err := a.Start(context.Background()); err != nil {
			return nil, nil, nil, err
		}
		return a, nil, nil, nil
	}, nil)
	mgr.SetRunWatch(watchdog)
	defer mgr.Shutdown(time.Second)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	hub := NewHub(&Mux{sessionMgr: mgr})
	_, root, err := watchdog.Start(context.Background(), runwatch.Metadata{RunID: "runtime-run"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Complete()
	child, err := root.BeginOperation(runwatch.KindModel, "model-runtime", runwatch.Policy{FirstSemantic: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Complete()
	got := hub.projectLiveWatchdog(ActiveRequest{SessionID: "l1", RequestID: "request", RunID: "runtime-run", WatchdogDueAt: time.Now().Add(-time.Hour)})
	if got.WatchdogDueAt.IsZero() || got.WatchdogDueAt.Before(time.Now()) {
		t.Fatalf("projected watchdog due = %v, want live future deadline", got.WatchdogDueAt)
	}
}

// ─── Request Ownership Validation Tests (Phase 2) ─────────────────────────────

func TestRequestSessionMismatch_CancelValidation(t *testing.T) {
	runtimeMetrics := &RuntimeMetrics{agentStreams: make(map[string]*AgentStreamState)}
	h := NewHub(&Mux{runtimeMetrics: runtimeMetrics, sessionMgr: session.NewSessionManager(nil, nil)})
	client := &Client{
		send: make(chan []byte, 10),
	}

	// Register active request req-active for l1
	_, err := h.requests.Reserve("l1", "req-active", "client-1")
	if err != nil {
		t.Fatalf("Reserve failed: %v", err)
	}
	if err := h.requests.BindCanceller("req-active", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := h.requests.SetRouteMetadata("req-active", "", "", "", "agent-1"); err != nil {
		t.Fatal(err)
	}
	runtimeMetrics.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-active", Delta: "partial"})

	// Attempt cancel with mismatched request ID
	msg := &ClientMessage{
		Type:      "chat_cancel",
		RequestID: "req-unrelated-999",
		SessionID: "l1",
	}

	h.handleChatCancel(client, msg)

	// Since request ID was mismatched, handleChatCancel must drop it without sending another response
	select {
	case data := <-client.send:
		var wsMsg WSMessage
		_ = json.Unmarshal(data, &wsMsg)
		t.Fatalf("unexpected message sent for mismatched cancel: %#v", wsMsg)
	default:
		// Passed — dropped mismatched cancel request
	}

	// Now attempt cancel with correct request ID (req-active)
	msgValid := &ClientMessage{
		Type:      "chat_cancel",
		RequestID: "req-active",
		SessionID: "l1",
	}

	h.handleChatCancel(client, msgValid)

	// The request's end reaches subscribers through the session transcript;
	// the canceller only receives the point-to-point confirmation.
	var confirmed bool
	close(client.send)
	for data := range client.send {
		var wsMsg WSMessage
		if err := json.Unmarshal(data, &wsMsg); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if wsMsg.Type == "chat_cancel_confirmed" && wsMsg.RequestID == "req-active" && wsMsg.SessionID == "l1" {
			confirmed = true
		}
	}

	if !confirmed {
		t.Errorf("valid cancel failed to produce a chat_cancel_confirmed envelope with SessionID")
	}
	stream := runtimeMetrics.AgentStreams()["agent-1\x00req-active"]
	if stream == nil || stream.Processing || stream.Error != "Task cancelled." {
		t.Fatalf("cancelled runtime stream = %#v, want terminal cancellation state", stream)
	}
}

func TestCancelWaitsForStartingRequestToBindAndStop(t *testing.T) {
	reg := NewActiveRequestRegistry()
	if _, err := reg.Reserve("l1", "req-starting", "client"); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	cancelled := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, _, err := reg.CancelAndWait(context.Background(), "l1", "req-starting")
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("starting request cancellation returned before bind: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := reg.BindCanceller("req-starting", func() error {
		close(cancelled)
		<-release
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("bound exact run was not cancelled")
	}
	select {
	case err := <-done:
		t.Fatalf("cancellation returned before exact run stopped: %v", err)
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("CancelAndWait error = %v", err)
	}
}

func TestConcurrentChatCancelConfirmsOnce(t *testing.T) {
	h := NewHub(nil)
	client := &Client{send: make(chan []byte, 8)}
	if _, err := h.requests.Reserve("l1", "req-double-cancel", "client"); err != nil {
		t.Fatal(err)
	}
	cancelStarted := make(chan struct{})
	releaseCancel := make(chan struct{})
	var cancelCalls atomic.Int32
	if err := h.requests.BindCanceller("req-double-cancel", func() error {
		if cancelCalls.Add(1) == 1 {
			close(cancelStarted)
		}
		<-releaseCancel
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	msg := &ClientMessage{Type: "chat_cancel", RequestID: "req-double-cancel", SessionID: "l1"}

	var callers sync.WaitGroup
	callers.Add(2)
	go func() {
		defer callers.Done()
		h.handleChatCancel(client, msg)
	}()
	select {
	case <-cancelStarted:
	case <-time.After(time.Second):
		t.Fatal("first cancellation did not start")
	}
	go func() {
		defer callers.Done()
		h.handleChatCancel(client, msg)
	}()
	close(releaseCancel)
	callers.Wait()

	if got := cancelCalls.Load(); got != 1 {
		t.Fatalf("canceller calls = %d, want 1", got)
	}
	counts := map[string]int{}
	for len(client.send) > 0 {
		var envelope WSMessage
		if err := json.Unmarshal(<-client.send, &envelope); err != nil {
			t.Fatal(err)
		}
		counts[envelope.Type]++
	}
	if got := counts["chat_cancel_confirmed"]; got != 1 {
		t.Fatalf("chat_cancel_confirmed envelopes = %d, want 1", got)
	}
}

func TestChatCancelPinsTerminalBeforeForwarderObservesError(t *testing.T) {
	h := NewHub(nil)
	client := &Client{send: make(chan []byte, 8)}
	if _, err := h.requests.Reserve("l1", "req-pinned-cancel", "client"); err != nil {
		t.Fatal(err)
	}
	// Returning from the exact canceller models Session.unregisterActiveCancel
	// winning before a deliberately blocked event forwarder consumes ErrorEvent.
	if err := h.requests.BindCanceller("req-pinned-cancel", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	h.handleChatCancel(client, &ClientMessage{Type: "chat_cancel", SessionID: "l1", RequestID: "req-pinned-cancel"})
	all := h.requests.GetBySessionAll("l1")
	if len(all) != 1 || all[0].TerminalCode != string(runwatch.CodeCancelledByUser) {
		t.Fatalf("cancel finalized before terminal snapshot was pinned: %+v", all)
	}
}

func TestWatchdogTerminalStateIsPublishedBeforeRequestFinalization(t *testing.T) {
	workDir := t.TempDir()
	log, err := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	a := agent.NewAgent(agent.Definition{Name: "test-agent"}, &agenttest.FakeLLM{Responses: []string{"late"}, Delay: time.Hour}, log, agent.WithAgentWorkDir(workDir))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	factory := func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	clock := &serverFakeClock{now: time.Unix(1_700_000_000, 0)}
	watchdog := runwatch.NewManager(runwatch.Policy{
		ScanInterval: time.Hour, RootIdle: time.Minute,
		FirstSemantic: time.Minute, TransportIdle: time.Minute, SemanticIdle: time.Minute,
	}, runwatch.WithClock(clock))
	defer watchdog.Close()
	mgr.SetRunWatch(watchdog)
	sess, err := mgr.Init(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown(time.Second)
	mux := NewMux(workDir, log, WithSessionManager(mgr), WithRuntimeMetrics(&RuntimeMetrics{}))
	defer mux.Close()
	hub := NewHub(mux)
	client := newClient(hub, nil)
	go hub.Run()
	hub.register <- client
	if readClient(client, 2*time.Second) == nil {
		t.Fatal("initial state was not delivered")
	}
	reqCtx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-terminal"})
	stream, err := sess.AskStream(reqCtx, "stall")
	if err != nil {
		t.Fatal(err)
	}
	active, err := hub.requests.Validate("l1", "req-terminal")
	if err != nil {
		t.Fatal(err)
	}
	go hub.forwardAgentEvents(client, "req-terminal", active.Generation, func() {}, stream, "l1")
	hub.refreshRequestWatchdog("l1", "req-terminal", "")
	liveDeadline := time.Now().Add(2 * time.Second)
	liveObserved := false
	for !liveObserved {
		data := readClient(client, time.Until(liveDeadline))
		if data == nil {
			t.Fatal("observer did not receive live watchdog state update")
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "state" || msg.Runtime == nil {
			continue
		}
		info, ok := msg.Runtime.Sessions["l1:req-terminal"]
		liveObserved = ok && info.RequestID == "req-terminal" && info.TerminalCode == ""
	}
	beforeTerminalRevision := hub.GetSessionRevision("l1")
	clock.Advance(2 * time.Minute)
	watchdog.Scan()

	deadline := time.Now().Add(2 * time.Second)
	observed := false
	for !observed {
		data := readClient(client, time.Until(deadline))
		if data == nil {
			t.Fatal("observer did not receive terminal watchdog state before finalization")
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "state" || msg.Runtime == nil {
			continue
		}
		info, ok := msg.Runtime.Sessions["l1:req-terminal"]
		if ok && info.TerminalCode == string(runwatch.CodeModelTransportStalled) {
			observed = true
		}
	}
	if got := hub.GetSessionRevision("l1"); got <= beforeTerminalRevision {
		t.Fatalf("terminal watchdog update did not increment revision: before=%d after=%d", beforeTerminalRevision, got)
	}
	hub.unregister <- client
	for range client.send {
	}
	hub.Close()
}

func TestTerminalStateSurvivesSaturatedHubAndClientQueues(t *testing.T) {
	workDir := t.TempDir()
	log, err := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	a := agent.NewAgent(agent.Definition{Name: "test-agent"}, &agenttest.FakeLLM{}, log, agent.WithAgentWorkDir(workDir))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	mgr := session.NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}, log)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown(time.Second)
	mux := NewMux(workDir, log, WithSessionManager(mgr), WithRuntimeMetrics(&RuntimeMetrics{}))
	defer mux.Close()
	hub := NewHub(mux)
	slow := newClient(hub, nil)
	slow.send = make(chan []byte, 1)
	slow.send <- []byte(`{"type":"occupied"}`)
	healthy := newClient(hub, nil)
	healthy.send = make(chan []byte, 128)
	hub.clients[slow] = true
	hub.clients[healthy] = true
	for i := 0; i < cap(hub.broadcast); i++ {
		hub.broadcast <- &WSMessage{Type: "notification"}
	}
	if _, err := hub.requests.Reserve("l1", "req-saturated-terminal", "client"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.requests.SetWatchdog("req-saturated-terminal", WatchdogState{
		RunID: "req-saturated-terminal", Phase: "stalled", TerminalCode: string(runwatch.CodeModelTransportStalled),
	}); err != nil {
		t.Fatal(err)
	}
	hub.requests.Finalize("l1", "req-saturated-terminal")
	hub.NextSessionRevision("l1")
	hub.Notify()
	go hub.Run()

	deadline := time.Now().Add(2 * time.Second)
	for {
		data := readClient(healthy, time.Until(deadline))
		if data == nil {
			t.Fatal("terminal state was permanently lost after queue saturation")
		}
		var msg WSMessage
		if json.Unmarshal(data, &msg) != nil || msg.Type != "state" || msg.Runtime == nil {
			continue
		}
		info, ok := msg.Runtime.Sessions["l1:req-saturated-terminal"]
		if ok && info.TerminalCode == string(runwatch.CodeModelTransportStalled) {
			// State is coalesced per client, so a saturated queue neither
			// loses the terminal state nor disconnects the slow client.
			if got := hub.ClientCount(); got != 2 {
				t.Fatalf("connected clients = %d, want both clients", got)
			}
			if state := slow.takeLatestState(); state == nil {
				t.Fatal("slow client has no pending state")
			}
			hub.unregister <- healthy
			hub.unregister <- slow
			for range healthy.send {
			}
			hub.Close()
			return
		}
	}
}

func TestWebCompactRunCanBeCancelledByExactRequestID(t *testing.T) {
	workDir := t.TempDir()
	log, err := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	compactor := &serverBlockingCompactor{started: make(chan struct{})}
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer(), ctxwin.WithCompactor(compactor))
	cw.Push(ctxwin.RoleSystem, "system")
	cw.Push(ctxwin.RoleUser, "old")
	a := agent.NewAgent(agent.Definition{Name: "test-agent"}, &agenttest.FakeLLM{}, log, agent.WithAgentWorkDir(workDir))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	factory := func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	watchdog := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Hour, RootIdle: time.Hour})
	defer watchdog.Close()
	mgr.SetRunWatch(watchdog)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatal(err)
	}
	defer mgr.Shutdown(time.Second)
	mux := NewMux(workDir, log, WithSessionManager(mgr), WithRuntimeMetrics(&RuntimeMetrics{}))
	defer mux.Close()
	hub := NewHub(mux)
	client := newClient(hub, nil)
	mark := transcriptMark(t, mgr.Session())
	hub.handleChatSend(client, &ClientMessage{Type: "chat_send", RequestID: "compact-request", SessionID: "l1", Prompt: "/compact"})
	select {
	case <-compactor.started:
	case <-time.After(time.Second):
		t.Fatal("web compact did not start")
	}
	hub.handleChatCancel(client, &ClientMessage{Type: "chat_cancel", RequestID: "compact-request", SessionID: "l1"})

	counts := map[string]int{}
	for len(client.send) > 0 {
		var msg WSMessage
		if err := json.Unmarshal(<-client.send, &msg); err != nil {
			t.Fatal(err)
		}
		counts[msg.Type]++
	}
	if counts["chat_cancel_confirmed"] != 1 {
		t.Fatalf("compact cancellation envelopes = %+v", counts)
	}
	waitSessionFrame(t, mgr.Session(), mark, transcript.KindRequestEnd, "compact-request", 2*time.Second)
	if _, err := hub.requests.Validate("l1", "compact-request"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("compact request remained active after cancellation: %v", err)
	}
}

// TestBusySessionRejection verifies that a busy single-flight session QUEUES
// a concurrent request instead of rejecting it: the client receives a queued
// chat_ack and the message enters the session's pending queue, where it is
// injected before the agent's next LLM call.
func TestBusySessionRejection(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	// Minimal real session via SessionManager so resolveSession succeeds.
	def := agent.Definition{
		Name: "test-agent",
	}
	// A latch makes the single-flight precondition independent of scheduler speed.
	started := make(chan struct{})
	release := make(chan struct{})
	var first sync.Once
	var releaseOnce sync.Once
	releaseFirst := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseFirst()
	fakeLLM := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"first"}},
		Hook:         func(agent.LLMRequest) { first.Do(func() { close(started); <-release }) },
	}
	a := agent.NewAgent(def, fakeLLM, log, agent.WithAgentWorkDir(workDir))
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatalf("Init manager: %v", err)
	}
	mux := NewMux(workDir, log, WithSessionManager(mgr))
	defer mux.Close()

	h := NewHub(mux)
	client := &Client{
		send:           make(chan []byte, 16),
		activeRequests: make(map[string]*activeRequest),
	}

	const sessionID = "l1"
	sess := mgr.Session()
	if sess == nil {
		t.Fatal("session is nil")
	}
	mark := transcriptMark(t, sess)

	// First request — starts streaming immediately (AskStream is async).
	h.handleChatSend(client, &ClientMessage{
		Type:      "chat_send",
		RequestID: "req-first",
		SessionID: sessionID,
		Prompt:    "first",
	})
	select {
	case data := <-client.send:
		var wsMsg WSMessage
		if err := json.Unmarshal(data, &wsMsg); err != nil {
			t.Fatalf("unmarshal accepted message: %v", err)
		}
		if wsMsg.Type != "chat_ack" || wsMsg.Status != "accepted" || wsMsg.RequestID != "req-first" || wsMsg.SessionID != sessionID {
			t.Fatalf("first response = %#v, want accepted chat_ack", wsMsg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for accepted chat_ack")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first model call did not start")
	}
	// Second request to the same (now busy) session.
	h.handleChatSend(client, &ClientMessage{
		Type:      "chat_send",
		RequestID: "req-second",
		SessionID: sessionID,
		Prompt:    "hello again",
	})

	// Check the pending queue before waiting on websocket delivery; under the
	// race detector, the first request may otherwise finish and consume it.
	if !sess.HasPending() {
		t.Error("expected queued message in session pending queue")
	}

	// The sender is acknowledged before routing and then told the input was
	// queued (accepted, not rejected).
	for _, want := range []string{"accepted", "queued"} {
		select {
		case data := <-client.send:
			var wsMsg WSMessage
			if err := json.Unmarshal(data, &wsMsg); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if wsMsg.Type != "chat_ack" || wsMsg.Status != want || wsMsg.RequestID != "req-second" || wsMsg.SessionID != sessionID {
				t.Fatalf("second response = %#v, want %s chat_ack", wsMsg, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for %s chat_ack", want)
		}
	}
	waitSessionFrame(t, sess, mark, transcript.KindInputQueued, "req-second", 2*time.Second)

	// Release only after queue/envelope assertions, then wait for the actual terminal event.
	releaseFirst()
	waitSessionFrame(t, sess, mark, transcript.KindRequestEnd, "req-first", 2*time.Second)
}

func TestMissingGenerationCaptureCancelsAndDrainsSuccessfulRun(t *testing.T) {
	workDir := t.TempDir()
	log, err := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	// More deltas than Session's 64-event output buffer makes an abandoned
	// successful stream block deterministically unless handleChatSend drains it.
	deltas := make([]string, 128)
	for i := range deltas {
		deltas[i] = "chunk"
	}
	fakeLLM := &agenttest.FakeLLM{StreamDeltas: [][]string{deltas, {"next response"}}}
	a := agent.NewAgent(
		agent.Definition{Name: "test-agent"},
		fakeLLM,
		log,
		agent.WithAgentWorkDir(workDir),
	)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	mgr := session.NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}, log)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	defer mgr.Shutdown(time.Second)

	mux := NewMux(workDir, log, WithSessionManager(mgr))
	defer mux.Close()
	hub := NewHub(mux)
	sess := mgr.Session()
	if sess == nil {
		t.Fatal("session is nil")
	}

	finished := make(chan struct{}, 1)
	// Simulate a lifecycle implementation that accepts the run but violates the
	// generation-capture contract. OnFinish proves the Session forwarder exited.
	sess.SetRequestLifecycleHooks(session.RequestLifecycleHooks{
		OnStart: func(context.Context, string, string) error { return nil },
		OnFinish: func(string, string, string, string) {
			select {
			case finished <- struct{}{}:
			default:
			}
		},
	})
	client := &Client{send: make(chan []byte, 16), activeRequests: make(map[string]*activeRequest)}
	client.addActiveRequest("req-missing-generation", func() {})
	reqCtx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{
		RequestID: "req-missing-generation",
		SessionID: "l1",
		Origin:    telemetry.OriginDesktop,
	})
	ch, err := sess.AskStream(reqCtx, "stream many chunks")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	hub.handleMissingRequestGeneration(client, sess, "req-missing-generation", "l1", ch)

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("run did not terminate after generation capture was missing")
	}
	if !sess.Idle() {
		t.Fatal("session remained busy after missing-generation cleanup")
	}
	client.mu.Lock()
	_, stillActive := client.activeRequests["req-missing-generation"]
	client.mu.Unlock()
	if stillActive {
		t.Fatal("missing-generation request remained active on client")
	}
	select {
	case data := <-client.send:
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Type != "chat_ack" || msg.Status != "rejected" || msg.RequestID != "req-missing-generation" || msg.Error != errRequestGenerationCaptureMissing.Error() {
			t.Fatalf("missing-generation response = %#v", msg)
		}
	default:
		t.Fatal("missing-generation cleanup did not send a rejected chat_ack")
	}

	// A subsequent direct request must be accepted rather than observing the
	// leaked single-flight lock from the abandoned output stream.
	next, err := sess.AskStream(context.Background(), "next request")
	if err != nil {
		t.Fatalf("next AskStream: %v", err)
	}
	for range next {
	}

	naturallyClosed := make(chan iface.AgentEvent)
	close(naturallyClosed)
	if err := cancelAndDrainUnownedRequestStream(sess, "already-finished", naturallyClosed); err != nil {
		t.Fatalf("naturally closed stream with no active run: %v", err)
	}
}

func TestForwardAgentEventsRetainsQueuedReservationUntilRootEnds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		terminal agent.AgentEvent
	}{
		{name: "normal", terminal: agent.DoneEvent{Content: "done"}},
		{name: "error", terminal: agent.ErrorEvent{Err: errors.New("failed")}},
		{name: "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := NewActiveRequestRegistry()
			root, err := registry.Reserve("l1", "req-root", "client")
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.ReserveQueued("l1", "req-queued", root.Generation); err != nil {
				t.Fatal(err)
			}
			client := &Client{send: make(chan []byte, 4), activeRequests: make(map[string]*activeRequest)}
			events := make(chan iface.AgentEvent)
			done := make(chan struct{})
			go func() {
				defer close(done)
				(&Hub{requests: registry}).forwardAgentEvents(client, "req-root", root.Generation, func() {}, events, "l1")
			}()

			events <- agent.PendingInputStartedEvent{RequestIDs: []string{"req-queued"}}
			if err := registry.ReserveQueued("l1", "req-queued", root.Generation); !errors.Is(err, ErrDuplicateRequestID) {
				t.Fatalf("reservation was reusable after boundary: %v", err)
			}
			if tc.terminal != nil {
				events <- tc.terminal
				if err := registry.ReserveQueued("l1", "req-queued", root.Generation); !errors.Is(err, ErrDuplicateRequestID) {
					t.Fatalf("reservation was reusable before forwarder exit: %v", err)
				}
			}
			close(events)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("forwarder did not exit")
			}
			if err := registry.ReserveQueued("l1", "req-queued", root.Generation); err != nil {
				t.Fatalf("reservation was not reusable after root end: %v", err)
			}
		})
	}
}

func TestStaleForwarderReleasesOnlyItsGenerationAfterRequestIDReuse(t *testing.T) {
	registry := NewActiveRequestRegistry()
	hub := &Hub{requests: registry}
	ctx, capture := withRequestGenerationCapture(telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{OwnerID: "client"}))
	if err := hub.reserveLifecycleRequest(ctx, "l1", "req-root"); err != nil {
		t.Fatal(err)
	}
	oldGeneration, ok := takeRequestGeneration(capture)
	if !ok {
		t.Fatal("OnStart did not capture G1")
	}
	if err := registry.ReserveQueued("l1", "req-old-queued", oldGeneration); err != nil {
		t.Fatal(err)
	}
	client := &Client{send: make(chan []byte, 2), activeRequests: make(map[string]*activeRequest)}
	events := make(chan iface.AgentEvent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.forwardAgentEvents(client, "req-root", oldGeneration, func() {}, events, "l1")
	}()

	registry.Finalize("l1", "req-root")
	newRoot, err := registry.Reserve("l1", "req-root", "client")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ReserveQueued("l1", "req-new-queued", newRoot.Generation); err != nil {
		t.Fatal(err)
	}
	close(events)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stale forwarder did not exit")
	}

	if err := registry.ReserveQueued("l1", "req-old-queued", newRoot.Generation); err != nil {
		t.Fatalf("old generation reservation was not released: %v", err)
	}
	if err := registry.ReserveQueued("l1", "req-new-queued", newRoot.Generation); !errors.Is(err, ErrDuplicateRequestID) {
		t.Fatalf("stale forwarder removed new generation reservation: %v", err)
	}
}

func TestSetupFailureCleanupUsesCapturedGenerationOnly(t *testing.T) {
	registry := NewActiveRequestRegistry()
	hub := &Hub{requests: registry}
	ctx, capture := withRequestGenerationCapture(telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{OwnerID: "client"}))
	if err := hub.reserveLifecycleRequest(ctx, "l1", "req-failed"); err != nil {
		t.Fatal(err)
	}
	failedGeneration, ok := takeRequestGeneration(capture)
	if !ok {
		t.Fatal("OnStart did not capture failed generation")
	}
	if err := registry.ReserveQueued("l1", "req-failed-queued", failedGeneration); err != nil {
		t.Fatal(err)
	}
	draining, err := registry.Reserve("l1", "req-draining", "client")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ReserveQueued("l1", "req-draining-queued", draining.Generation); err != nil {
		t.Fatal(err)
	}

	registry.Finalize("l1", "req-failed")
	registry.ReleaseQueuedForGeneration("l1", failedGeneration)
	if err := registry.ReserveQueued("l1", "req-failed-queued", draining.Generation); err != nil {
		t.Fatalf("failed generation reservation was not released: %v", err)
	}
	if err := registry.ReserveQueued("l1", "req-draining-queued", draining.Generation); !errors.Is(err, ErrDuplicateRequestID) {
		t.Fatalf("exact setup cleanup released another draining generation: %v", err)
	}
}

func TestQueuedDeliveryBoundaryKeepsRootRequestCancellable(t *testing.T) {
	registry := NewActiveRequestRegistry()
	root, err := registry.Reserve("l2:session-A", "req-root", "client")
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ReserveQueued("l2:session-A", "req-queued", root.Generation); err != nil {
		t.Fatal(err)
	}
	events := make(chan iface.AgentEvent, 1)
	if err := registry.BindCanceller("req-root", func() error {
		close(events)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	client := &Client{
		ownerID:        "client",
		send:           make(chan []byte, 2),
		activeRequests: make(map[string]*activeRequest),
	}
	hub := &Hub{requests: registry}
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.forwardAgentEvents(client, "req-root", root.Generation, func() {}, events, "l2:session-A")
	}()

	events <- agent.PendingInputStartedEvent{RequestIDs: []string{"req-queued"}}
	req, owner, err := registry.CancelAndWaitOwned(context.Background(), "l2:session-A", "req-root", "client")
	if err != nil || !owner || req.RequestID != "req-root" {
		t.Fatalf("root cancellation = (%#v, %v, %v), want owned req-root", req, owner, err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event forwarder did not stop after root cancellation")
	}
	if err := registry.ReserveQueued("l2:session-A", "req-queued", root.Generation); err != nil {
		t.Fatalf("queued reservation was not released after cancellation: %v", err)
	}
}

type blockingDelegationTarget struct {
	release <-chan struct{}
}

type requestOwnershipEchoTool struct{}

func (requestOwnershipEchoTool) Name() string        { return "echo" }
func (requestOwnershipEchoTool) Description() string { return "returns a deterministic sync result" }
func (requestOwnershipEchoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (requestOwnershipEchoTool) Execute(context.Context, string) (string, error) {
	return "sync follow-up result", nil
}

func (t *blockingDelegationTarget) Ask(ctx context.Context, prompt string) (string, error) {
	select {
	case <-t.release:
		return "delegation result", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (t *blockingDelegationTarget) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	out := make(chan iface.AgentEvent, 2)
	go func() {
		defer close(out)
		result, err := t.Ask(ctx, prompt)
		if err != nil {
			out <- agent.ErrorEvent{Err: err}
			return
		}
		out <- agent.ContentDeltaEvent{Delta: result}
		out <- agent.DoneEvent{Content: result}
	}()
	return out, nil
}

func (t *blockingDelegationTarget) ErrorCount() int32 { return 0 }
func (t *blockingDelegationTarget) LastError() string { return "" }

func TestL1DesktopStartsSecondRequestBeforeDelegationCompletes(t *testing.T) {
	workDir := t.TempDir()
	log, err := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	target := &blockingDelegationTarget{release: release}
	delegate := tools.NewDelegateTool(
		"l1-test",
		5*time.Second,
		func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
			return target, false, nil
		},
		nil,
		log,
		tools.WorkDirExplicitOrInherited,
		tools.WithAlwaysAsyncDelegation(),
	)
	fakeLLM := &agenttest.FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{
			Index:     0,
			ID:        "call-delegate",
			Name:      "delegate",
			Arguments: `{"target":"worker","task":"blocked"}`,
		}}, {{
			Index:     0,
			ID:        "call-follow-up-tool",
			Name:      "echo",
			Arguments: `{}`,
		}}, nil, nil},
		StreamDeltas: [][]string{nil, nil, {"second request response"}, {"first request response"}},
	}
	a := agent.NewAgent(
		agent.Definition{ID: "l1-test", Name: "L1 test"},
		fakeLLM,
		log,
		agent.WithTools(delegate, requestOwnershipEchoTool{}),
		agent.WithPriorityMailbox(),
		agent.WithAgentWorkDir(workDir),
	)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	mux := NewMux(workDir, log, WithSessionManager(mgr))
	t.Cleanup(func() { _ = mux.Close() })

	h := NewHub(mux)
	sess := mgr.Session()
	mark := transcriptMark(t, sess)
	client := &Client{send: make(chan []byte, 32), activeRequests: make(map[string]*activeRequest)}
	h.handleChatSend(client, &ClientMessage{
		Type: "chat_send", RequestID: "req-delegating", SessionID: "l1", Prompt: "delegate this",
	})
	waitSessionFrame(t, sess, mark, transcript.KindDelegationStarted, "req-delegating", 2*time.Second)

	idleDeadline := time.Now().Add(time.Second)
	for !mgr.Session().Idle() && time.Now().Before(idleDeadline) {
		time.Sleep(time.Millisecond)
	}
	if !mgr.Session().Idle() {
		t.Fatal("session did not release inFlight after delegation_start")
	}

	h.handleChatSend(client, &ClientMessage{
		Type: "chat_send", RequestID: "req-follow-up", SessionID: "l1", Prompt: "answer this now",
	})

	secondStarted := time.Now().Add(500 * time.Millisecond)
	for fakeLLM.StreamCallCount() < 2 && time.Now().Before(secondStarted) {
		time.Sleep(time.Millisecond)
	}
	if got := fakeLLM.StreamCallCount(); got < 2 {
		t.Fatalf("L1 stream calls = %d, want at least 2 before delegation completes", got)
	}

	// The follow-up deliberately uses a synchronous tool at its own iter=0.
	// Before the fix, the old request's asyncTurns[0] was mistaken for the
	// follow-up's state, so the stream yielded without ever ending.
	end := waitSessionFrame(t, sess, mark, transcript.KindRequestEnd, "req-follow-up", 2*time.Second)
	if end.TerminalCode != "completed" {
		t.Fatalf("follow-up terminal code = %q, want completed", end.TerminalCode)
	}
	j := sess.Transcript()
	frames, err := j.ReadAfter(j.Epoch(), mark, 0)
	if err != nil {
		t.Fatal(err)
	}
	var content strings.Builder
	for _, f := range frames {
		if f.RequestID != "req-follow-up" {
			continue
		}
		if f.Kind == transcript.KindDelegationStarted {
			t.Fatal("follow-up sync tool was misclassified as async delegation")
		}
		if f.Kind == transcript.KindItemDelta && f.Channel == transcript.ChannelContent {
			content.WriteString(f.Text)
		}
	}
	if content.String() != "second request response" {
		t.Fatalf("follow-up content = %q, want %q", content.String(), "second request response")
	}
}

func TestWebSocketReloadAllowsFollowupAndCancelDuringDelegation(t *testing.T) {
	workDir := t.TempDir()
	log, err := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	target := &blockingDelegationTarget{release: release}
	delegate := tools.NewDelegateTool(
		"l1-test",
		5*time.Second,
		func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
			return target, false, nil
		},
		nil,
		log,
		tools.WorkDirExplicitOrInherited,
		tools.WithAlwaysAsyncDelegation(),
	)
	fakeLLM := &agenttest.FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{
			Index:     0,
			ID:        "call-delegate",
			Name:      "delegate",
			Arguments: `{"target":"worker","task":"blocked"}`,
		}}},
		StreamDeltas: [][]string{nil, {"second request response"}, {"first request response"}},
	}
	a := agent.NewAgent(
		agent.Definition{ID: "l1-test", Name: "L1 test"},
		fakeLLM,
		log,
		agent.WithTools(delegate),
		agent.WithPriorityMailbox(),
		agent.WithAgentWorkDir(workDir),
	)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "old soul")
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}
	mgr := session.NewSessionManager(factory, log)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	mux := NewMux(workDir, log, WithSessionManager(mgr))
	t.Cleanup(func() { _ = mux.Close() })

	h := NewHub(mux)
	mux.SetHub(h)
	go h.Run()
	t.Cleanup(h.Close)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	// Every subscriber receives the conversation through the transcript, so
	// this connection observes its own requests exactly as any other would.
	j := mgr.Session().Transcript()
	if err := conn.WriteJSON(ClientMessage{Type: "subscribe", SessionID: "l1", Epoch: j.Epoch(), AfterSeq: j.Head()}); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	readUntil := func(kind, request string) {
		if seen[kind+":"+request] {
			return
		}
		t.Helper()
		for {
			var msg struct {
				Type      string             `json:"type"`
				RequestID string             `json:"request_id"`
				Frames    []transcript.Frame `json:"frames"`
			}
			if err := conn.ReadJSON(&msg); err != nil {
				t.Fatal(err)
			}
			if msg.Type == "resync_required" {
				t.Fatal("subscription lost its cursor")
			}
			found := false
			if msg.Type == "frames" {
				for _, f := range msg.Frames {
					seen[f.Kind+":"+f.RequestID] = true
					found = found || (f.Kind == kind && (request == "" || f.RequestID == request))
				}
			} else {
				seen[msg.Type+":"+msg.RequestID] = true
				found = msg.Type == kind && (request == "" || msg.RequestID == request)
			}
			if found {
				return
			}
		}
	}
	if err := conn.WriteJSON(ClientMessage{Type: "chat_send", RequestID: "req-delegating", SessionID: "l1", Prompt: "delegate this"}); err != nil {
		t.Fatal(err)
	}
	readUntil(transcript.KindDelegationStarted, "req-delegating")
	deadline := time.Now().Add(time.Second)
	for !mgr.Session().Idle() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := (&session.Builder{}).ReconcileL1TeamCatalog(mgr.Session(), "new soul"); !errors.Is(err, session.ErrSessionBusy) {
		t.Fatalf("reload=%v", err)
	}
	if err := conn.WriteJSON(ClientMessage{Type: "chat_send", RequestID: "req-follow-up", SessionID: "l1", Prompt: "answer now"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(ClientMessage{Type: "chat_cancel", RequestID: "req-delegating", SessionID: "l1"}); err != nil {
		t.Fatal(err)
	}
	readUntil("chat_cancel_confirmed", "req-delegating")
	readUntil(transcript.KindRequestEnd, "req-follow-up")
	if primary, _ := cw.MessageAt(0); primary.Content != "new soul" {
		t.Fatal("follow-up missed reload")
	}
}

func TestL1AllowsConcurrentRequests(t *testing.T) {
	h := NewHub(nil)

	// Reserve first L1 request — must succeed.
	_, err := h.requests.Reserve("l1", "req-1", "c1")
	if err != nil {
		t.Fatalf("first L1 Reserve failed: %v", err)
	}

	// Reserve second L1 request — must succeed (L1 allows concurrent requests).
	_, err = h.requests.Reserve("l1", "req-2", "c2")
	if err != nil {
		t.Fatalf("second L1 Reserve should succeed but got: %v", err)
	}

	// L2 should still reject concurrent requests.
	const l2Session = "l2:test"
	_, err = h.requests.Reserve(l2Session, "req-l2-1", "c1")
	if err != nil {
		t.Fatalf("L2 Reserve failed: %v", err)
	}
	_, err = h.requests.Reserve(l2Session, "req-l2-2", "c2")
	if err != ErrSessionBusy {
		t.Errorf("L2 second Reserve: got %v, want ErrSessionBusy", err)
	}
}

func TestForwardAgentEventsDoesNotOwnRequestFinalization(t *testing.T) {
	h := NewHub(nil)
	const (
		sessionID = "l1"
		requestID = "req-stream"
	)
	root, err := h.requests.Reserve(sessionID, requestID, "client-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	client := &Client{
		send:           make(chan []byte, 10),
		activeRequests: make(map[string]*activeRequest),
	}
	events := make(chan iface.AgentEvent)
	go h.forwardAgentEvents(client, requestID, root.Generation, func() {}, events, sessionID)

	if _, active := h.requests.GetBySession(sessionID); !active {
		t.Fatal("reservation disappeared before stream completion")
	}

	close(events)
	deadline := time.Now().Add(time.Second)
	for {
		if _, active := h.requests.GetBySession(sessionID); !active {
			t.Fatal("forwarder finalized a request owned by the session lifecycle")
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	h.finalizeRequest(sessionID, requestID)
	if _, active := h.requests.GetBySession(sessionID); active {
		t.Fatal("central lifecycle finalization did not remove the request")
	}
}

func TestClientDisconnectDetachesWithoutCancellingRequest(t *testing.T) {
	clientCtx, clientCancel := context.WithCancel(context.Background())
	var requestCancelled atomic.Bool
	client := &Client{
		ctx:    clientCtx,
		cancel: clientCancel,
		activeRequests: map[string]*activeRequest{
			"req-1": {
				RequestID: "req-1",
				Cancel:    func() { requestCancelled.Store(true) },
			},
		},
	}

	client.cancelAllRequests()

	if requestCancelled.Load() {
		t.Fatal("disconnect cancelled a globally owned request")
	}
	if len(client.activeRequests) != 0 {
		t.Fatal("client request attachments were not cleared")
	}
}
