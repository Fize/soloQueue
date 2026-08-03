package server

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

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

	// Second request to the same (now busy) session.
	h.handleChatSend(client, &ClientMessage{
		Type:      "chat_send",
		RequestID: "req-second",
		SessionID: sessionID,
		Prompt:    "hello again",
	})

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

	// The message must have been queued on the session, ready for injection
	// before the agent's next LLM call.
	sess := mgr.Session()
	if sess == nil {
		t.Fatal("session is nil")
	}
	if !sess.HasPending() {
		t.Error("expected queued message in session pending queue")
	}

	// Let the first request finish cleanly.
	time.Sleep(400 * time.Millisecond)
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
