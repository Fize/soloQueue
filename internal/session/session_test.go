package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// ─── Test helpers ──────────────────────────────────────────────────────

// startAgent builds + starts an agent with the given FakeLLM and returns it.
// t.Cleanup stops the agent.
func startAgent(t *testing.T, fake *agent.FakeLLM) *agent.Agent {
	t.Helper()
	a := agent.NewAgent(
		agent.Definition{ID: "test-agent"},
		fake,
		nil,
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("agent Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })
	return a
}

// factoryFromFake returns a factory that produces fresh started agents each
// time from the given FakeLLM (sharing the same LLM across sessions).
func factoryFromFake(t *testing.T, fake *agent.FakeLLM) AgentFactory {
	return func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		a := agent.NewAgent(
			agent.Definition{ID: "agent-" + teamID},
			fake,
			nil,
		)
		if err := a.Start(ctx); err != nil {
			return nil, nil, nil, err
		}
		cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
		return a, cw, nil, nil
	}
}

// ─── Session.Ask ──────────────────────────────────────────────────────

func TestSession_Ask_UpdatesHistory(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"hi there"}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	reply, err := s.Ask(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "hi there" {
		t.Errorf("reply = %q", reply)
	}
	h := s.History()
	if len(h) != 2 {
		t.Fatalf("history len = %d, want 2", len(h))
	}
	if h[0].Role != "user" || h[0].Content != "hello" {
		t.Errorf("h[0] = %+v", h[0])
	}
	if h[1].Role != "assistant" || h[1].Content != "hi there" {
		t.Errorf("h[1] = %+v", h[1])
	}
}

func TestSession_Ask_ErrorDoesNotAppendHistory(t *testing.T) {
	myErr := errors.New("kaboom")
	fake := &agent.FakeLLM{Err: myErr}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	_, err := s.Ask(context.Background(), "hi")
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
	if len(s.History()) != 0 {
		t.Errorf("history len = %d, want 0", len(s.History()))
	}
}

func TestSession_Ask_BusyReturnsErr(t *testing.T) {
	fake := &agent.FakeLLM{
		Responses: []string{"slow"},
		Delay:     300 * time.Millisecond,
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	// goroutine 1 starts a slow Ask
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.Ask(context.Background(), "one")
	}()
	// wait for it to enter
	time.Sleep(30 * time.Millisecond)

	// goroutine 2: should see Busy
	_, err := s.Ask(context.Background(), "two")
	if !errors.Is(err, ErrSessionBusy) {
		t.Errorf("second Ask err = %v, want ErrSessionBusy", err)
	}

	<-done
	// after completion, Ask should work again
	if _, err := s.Ask(context.Background(), "three"); err != nil {
		t.Errorf("Ask after busy release: %v", err)
	}
}

func TestSession_Ask_ClosedReturnsErr(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.Close()
	_, err := s.Ask(context.Background(), "hi")
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("err = %v, want ErrSessionClosed", err)
	}
}

// ─── Session.AskStream ─────────────────────────────────────────────────

func TestSession_AskStream_AppendsHistoryOnDone(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"hel", "lo"}}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	gotDone := false
	for ev := range ch {
		if _, ok := ev.(agent.DoneEvent); ok {
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected DoneEvent")
	}
	// history should be appended
	h := s.History()
	if len(h) != 2 {
		t.Fatalf("history len = %d", len(h))
	}
	if h[1].Content != "hello" {
		t.Errorf("final = %q, want 'hello'", h[1].Content)
	}
}

func TestSession_AskStream_ErrorNoHistoryAppend(t *testing.T) {
	fake := &agent.FakeLLM{Err: errors.New("bad")}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	for range ch {
	}
	if len(s.History()) != 0 {
		t.Errorf("history len = %d, want 0", len(s.History()))
	}
}

func TestSession_AskStream_ConcurrentRejected(t *testing.T) {
	fake := &agent.FakeLLM{
		StreamDeltas: [][]string{{"x"}, {"y"}},
		Delay:        200 * time.Millisecond,
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	ch1, err := s.AskStream(context.Background(), "one")
	if err != nil {
		t.Fatalf("first AskStream: %v", err)
	}
	// before first completes, try a second
	_, err2 := s.AskStream(context.Background(), "two")
	if !errors.Is(err2, ErrSessionBusy) {
		t.Errorf("second AskStream err = %v, want ErrSessionBusy", err2)
	}
	for range ch1 {
	}
}

// ─── SessionManager ────────────────────────────────────────────────────

func TestSessionManager_Init(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), nil)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	s, err := mgr.Init(context.Background(), "team1")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if s == nil {
		t.Fatal("Init returned nil session")
	}
	if got := mgr.Session(); got != s {
		t.Error("Session() returned different session")
	}

	// Second Init should return the same session
	s2, err := mgr.Init(context.Background(), "team2")
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if s2 != s {
		t.Error("second Init should return the same session")
	}
}

func TestSessionManager_FactoryError(t *testing.T) {
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return nil, nil, nil, fmt.Errorf("factory kaboom")
	}
	mgr := NewSessionManager(factory, nil)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	_, err := mgr.Init(context.Background(), "t1")
	if err == nil {
		t.Fatal("expected factory error")
	}
}

func TestSessionManager_Shutdown(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), nil)

	s, err := mgr.Init(context.Background(), "t")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	mgr.Shutdown(time.Second)

	// Session should be nil after Shutdown
	if mgr.Session() != nil {
		t.Error("Session() should be nil after Shutdown")
	}

	// Init after Shutdown fails
	_, err = mgr.Init(context.Background(), "t")
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Init after Shutdown err = %v, want ErrSessionClosed", err)
	}

	// Ask on shutdown session returns ErrSessionClosed
	_, err = s.Ask(context.Background(), "hi")
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Ask after Shutdown err = %v, want ErrSessionClosed", err)
	}
}

// ─── Delegation-aware inFlight ─────────────────────────────────────────

func TestSession_AskStream_DelegationReleasesInFlight(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"hello"}}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	// inFlight should be 0 initially
	if s.inFlight.Load() != 0 {
		t.Fatalf("initial inFlight = %d, want 0", s.inFlight.Load())
	}

	// Start a stream
	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	// inFlight should be 1
	if s.inFlight.Load() != 1 {
		t.Fatalf("inFlight after AskStream = %d, want 1", s.inFlight.Load())
	}

	// Simulate DelegationStartedEvent using session's helper methods
	s.newTurnDone()
	s.inFlight.Store(0)

	// Now a second AskStream should be allowed (inFlight is 0)
	ch2, err := s.AskStream(context.Background(), "second")
	if err != nil {
		t.Fatalf("second AskStream during delegation: %v", err)
	}

	// Close turnDone to unblock the second stream's CW push
	s.closeTurnDone()

	// Drain both streams
	for range ch {
	}
	for range ch2 {
	}
}

func TestSession_AskStream_DelegationPendingBlocksCWPush(t *testing.T) {
	fake := &agent.FakeLLM{
		StreamDeltas: [][]string{{"first"}, {"second"}},
		Delay:        500 * time.Millisecond, // slow LLM so forwarder doesn't finish first
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	// Start first stream
	ch1, err := s.AskStream(context.Background(), "one")
	if err != nil {
		t.Fatalf("first AskStream: %v", err)
	}

	// Set delegation pending state using session's helper
	s.newTurnDone()
	s.inFlight.Store(0)

	// Second AskStream should block on turnDone but eventually succeed
	gotSecond := make(chan error, 1)
	go func() {
		_, err := s.AskStream(context.Background(), "two")
		gotSecond <- err
	}()

	// Give it time to reach the turnDone wait
	time.Sleep(50 * time.Millisecond)

	// Before closing turnDone, second stream should still be waiting
	select {
	case err := <-gotSecond:
		t.Fatalf("second AskStream should have blocked, but got: %v", err)
	default:
		// Expected: still blocked
	}

	// Close turnDone — should unblock
	s.closeTurnDone()

	// Now second AskStream should succeed
	select {
	case err := <-gotSecond:
		if err != nil {
			t.Errorf("second AskStream after unblock: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("second AskStream timed out after unblock")
	}

	// Drain first stream
	for range ch1 {
	}
}

func TestSession_AskStream_CloseTurnDoneIdempotent(t *testing.T) {
	s := NewSession("s1", "t1", nil, nil, nil, nil)

	// Calling closeTurnDone when no turn is active should be safe
	s.closeTurnDone()

	// Create a turn and close it
	s.newTurnDone()
	s.closeTurnDone()

	// Close again — should be idempotent, no panic
	s.closeTurnDone()

	if s.delegationPending.Load() {
		t.Error("delegationPending should be false after closeTurnDone")
	}
}
