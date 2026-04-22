package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
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
	return func(ctx context.Context, teamID string) (*agent.Agent, error) {
		a := agent.NewAgent(
			agent.Definition{ID: "agent-" + teamID},
			fake,
			nil,
		)
		if err := a.Start(ctx); err != nil {
			return nil, err
		}
		return a, nil
	}
}

// ─── Session.Ask ──────────────────────────────────────────────────────

func TestSession_Ask_UpdatesHistory(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"hi there"}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a)

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
	s := NewSession("s1", "t1", a)

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
	s := NewSession("s1", "t1", a)

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
	s := NewSession("s1", "t1", a)
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
	s := NewSession("s1", "t1", a)

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
	s := NewSession("s1", "t1", a)
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
	s := NewSession("s1", "t1", a)

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

func TestSessionManager_CreateGetDelete(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), 0)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	s, err := mgr.Create(context.Background(), "team1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got, ok := mgr.Get(s.ID); !ok || got != s {
		t.Errorf("Get returned different session")
	}
	if mgr.Count() != 1 {
		t.Errorf("count = %d, want 1", mgr.Count())
	}
	if err := mgr.Delete(s.ID, time.Second); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := mgr.Get(s.ID); ok {
		t.Error("session should be gone after Delete")
	}
}

func TestSessionManager_DeleteNotFound(t *testing.T) {
	mgr := NewSessionManager(factoryFromFake(t, &agent.FakeLLM{}), 0)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	err := mgr.Delete("ghost", time.Second)
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("err = %v, want ErrSessionNotFound", err)
	}
}

func TestSessionManager_FactoryError(t *testing.T) {
	factory := func(ctx context.Context, teamID string) (*agent.Agent, error) {
		return nil, fmt.Errorf("factory kaboom")
	}
	mgr := NewSessionManager(factory, 0)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	_, err := mgr.Create(context.Background(), "t1")
	if err == nil {
		t.Fatal("expected factory error")
	}
}

func TestSessionManager_Shutdown(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), 0)

	var ids []string
	for i := 0; i < 3; i++ {
		s, err := mgr.Create(context.Background(), "t")
		if err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
		ids = append(ids, s.ID)
	}
	mgr.Shutdown(time.Second)
	if mgr.Count() != 0 {
		t.Errorf("count after Shutdown = %d", mgr.Count())
	}
	// Create after Shutdown fails
	if _, err := mgr.Create(context.Background(), "t"); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Create after Shutdown err = %v, want ErrSessionClosed", err)
	}
}

func TestSessionManager_ReapIdle(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), 50*time.Millisecond)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	s1, _ := mgr.Create(context.Background(), "t1")
	_, _ = mgr.Create(context.Background(), "t2")

	// touch s1 so it stays fresh
	_, _ = s1.Ask(context.Background(), "keep alive")

	// wait past TTL
	time.Sleep(120 * time.Millisecond)

	n := mgr.ReapIdle(time.Second)
	if n == 0 {
		t.Error("expected at least 1 session reaped")
	}
	// s1 was freshly touched but its lastActive is still past cutoff after sleep → it may also be reaped
	// we don't strictly assert which survives; just verify behavior
}

func TestSessionManager_ReapLoop(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), 30*time.Millisecond)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	_, _ = mgr.Create(context.Background(), "t1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mgr.ReapLoop(ctx, 20*time.Millisecond, time.Second)

	// wait for reap to happen
	deadline := time.After(1 * time.Second)
	for {
		if mgr.Count() == 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("ReapLoop did not clean up; count = %d", mgr.Count())
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// ─── Concurrency / race ─────────────────────────────────────────────────

func TestSession_ConcurrentCreateDelete_Race(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), 0)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	const N = 20
	var wg sync.WaitGroup
	var created atomic.Int32

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := mgr.Create(context.Background(), "t")
			if err != nil {
				return
			}
			created.Add(1)
			_ = mgr.Delete(s.ID, time.Second)
		}()
	}
	wg.Wait()
	if mgr.Count() != 0 {
		t.Errorf("leaked sessions: %d", mgr.Count())
	}
	if created.Load() == 0 {
		t.Error("no sessions created")
	}
}
