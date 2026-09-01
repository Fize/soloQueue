package server

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	h := NewHub(nil)
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

	// Attempt cancel with mismatched request ID
	msg := &ClientMessage{
		Type:      "chat_cancel",
		RequestID: "req-unrelated-999",
		SessionID: "l1",
	}

	h.handleChatCancel(client, msg)

	// Since request ID was mismatched, handleChatCancel must drop it without sending confirmation
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

	var confirmed, done bool
	close(client.send)
	for data := range client.send {
		var wsMsg WSMessage
		if err := json.Unmarshal(data, &wsMsg); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if wsMsg.Type == "chat_cancel_confirmed" && wsMsg.RequestID == "req-active" && wsMsg.SessionID == "l1" {
			confirmed = true
		}
		if wsMsg.Type == "chat_done" && wsMsg.RequestID == "req-active" && wsMsg.SessionID == "l1" {
			done = true
		}
	}

	if !confirmed || !done {
		t.Errorf("valid cancel failed to produce chat_cancel_confirmed and chat_done envelopes with SessionID")
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

func TestConcurrentChatCancelEmitsOneTerminalCompletion(t *testing.T) {
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
	if got := counts["chat_done"]; got != 1 {
		t.Fatalf("chat_done envelopes = %d, want 1", got)
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
	<-client.send // initial state
	if _, err := hub.requests.Reserve("l1", "req-terminal", "client"); err != nil {
		t.Fatal(err)
	}
	reqCtx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-terminal"})
	stream, err := sess.AskStream(reqCtx, "stall")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.requests.BindCanceller("req-terminal", func() error {
		return sess.CancelRun("req-terminal", "test")
	}); err != nil {
		t.Fatal(err)
	}
	go hub.forwardAgentEvents(client, "req-terminal", func() {}, stream, "l1", "stall")
	hub.refreshRequestWatchdog("l1", "req-terminal", "")
	liveDeadline := time.After(2 * time.Second)
	liveObserved := false
	for !liveObserved {
		select {
		case data := <-client.send:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "state" || msg.Runtime == nil {
				continue
			}
			info, ok := msg.Runtime.Sessions["l1:req-terminal"]
			liveObserved = ok && info.RequestID == "req-terminal" && info.TerminalCode == ""
		case <-liveDeadline:
			t.Fatal("observer did not receive live watchdog state update")
		}
	}
	beforeTerminalRevision := hub.GetSessionRevision("l1")
	clock.Advance(2 * time.Minute)
	watchdog.Scan()

	deadline := time.After(2 * time.Second)
	observed := false
	for !observed {
		select {
		case data := <-client.send:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "state" || msg.Runtime == nil {
				continue
			}
			info, ok := msg.Runtime.Sessions["l1:req-terminal"]
			if ok && info.TerminalCode == string(runwatch.CodeModelTransportStalled) {
				observed = true
			}
		case <-deadline:
			t.Fatal("observer did not receive terminal watchdog state before finalization")
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

	deadline := time.After(2 * time.Second)
	for {
		select {
		case data := <-healthy.send:
			var msg WSMessage
			if json.Unmarshal(data, &msg) != nil || msg.Type != "state" || msg.Runtime == nil {
				continue
			}
			info, ok := msg.Runtime.Sessions["l1:req-saturated-terminal"]
			if ok && info.TerminalCode == string(runwatch.CodeModelTransportStalled) {
				if got := hub.ClientCount(); got != 1 {
					t.Fatalf("connected clients = %d, want only healthy client", got)
				}
				hub.unregister <- healthy
				for range healthy.send {
				}
				hub.Close()
				return
			}
		case <-deadline:
			t.Fatal("terminal state was permanently lost after queue saturation")
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
	if counts["chat_cancel_confirmed"] != 1 || counts["chat_done"] == 0 {
		t.Fatalf("compact cancellation envelopes = %+v", counts)
	}
	if _, err := hub.requests.Validate("l1", "compact-request"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("compact request remained active after cancellation: %v", err)
	}
}

// TestBusySessionRejection verifies that a busy single-flight session QUEUES
// a concurrent request instead of rejecting it: the client receives
// chat_queued and the message enters the session's pending queue, where it is
// injected before the agent's next LLM call.
func TestBusySessionRejection(t *testing.T) {
	workDir := t.TempDir()
	log, _ := logger.System(workDir, logger.WithConsole(false), logger.WithFile(false))

	// Minimal real session via SessionManager so resolveSession succeeds.
	def := agent.Definition{
		Name: "test-agent",
	}
	// Delay keeps the first request in-flight so the second one queues.
	fakeLLM := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"first"}},
		Delay:        300 * time.Millisecond,
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
		if wsMsg.Type != "chat_accepted" || wsMsg.RequestID != "req-first" || wsMsg.SessionID != sessionID {
			t.Fatalf("first response = %#v, want chat_accepted", wsMsg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for chat_accepted message")
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
	sess := mgr.Session()
	if sess == nil {
		t.Fatal("session is nil")
	}
	if !sess.HasPending() {
		t.Error("expected queued message in session pending queue")
	}

	// Client should receive chat_queued (message accepted, not rejected).
	gotQueued := false
	for i := 0; i < 4; i++ {
		select {
		case data := <-client.send:
			var wsMsg WSMessage
			if err := json.Unmarshal(data, &wsMsg); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if wsMsg.Type == "chat_queued" {
				if wsMsg.RequestID != "req-second" || wsMsg.SessionID != sessionID {
					t.Errorf("unexpected queued envelope: %#v", wsMsg)
				}
				gotQueued = true
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for chat_queued message")
		}
		if gotQueued {
			break
		}
	}
	if !gotQueued {
		t.Fatal("expected chat_queued message")
	}

	// Let the first request finish cleanly.
	time.Sleep(400 * time.Millisecond)
}

type blockingDelegationTarget struct {
	release <-chan struct{}
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

func (t *blockingDelegationTarget) Confirm(callID, choice string) error { return nil }
func (t *blockingDelegationTarget) ErrorCount() int32                   { return 0 }
func (t *blockingDelegationTarget) LastError() string                   { return "" }

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
	client := &Client{send: make(chan []byte, 32), activeRequests: make(map[string]*activeRequest)}
	h.handleChatSend(client, &ClientMessage{
		Type: "chat_send", RequestID: "req-delegating", SessionID: "l1", Prompt: "delegate this",
	})

	deadline := time.After(2 * time.Second)
	for {
		select {
		case data := <-client.send:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal websocket message: %v", err)
			}
			if msg.Type == "delegation_start" && msg.RequestID == "req-delegating" {
				goto delegationStarted
			}
		case <-deadline:
			t.Fatal("timeout waiting for delegation_start")
		}
	}

delegationStarted:
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

func TestForwardAgentEventsOwnsReservationUntilStreamCloses(t *testing.T) {
	h := NewHub(nil)
	const (
		sessionID = "l1"
		requestID = "req-stream"
	)
	if _, err := h.requests.Reserve(sessionID, requestID, "client-1"); err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	client := &Client{
		send:           make(chan []byte, 10),
		activeRequests: make(map[string]*activeRequest),
	}
	events := make(chan iface.AgentEvent)
	go h.forwardAgentEvents(client, requestID, func() {}, events, sessionID, "prompt")

	if _, active := h.requests.GetBySession(sessionID); !active {
		t.Fatal("reservation disappeared before stream completion")
	}

	close(events)
	deadline := time.Now().Add(time.Second)
	for {
		if _, active := h.requests.GetBySession(sessionID); !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reservation was not finalized after stream completion")
		}
		time.Sleep(time.Millisecond)
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
