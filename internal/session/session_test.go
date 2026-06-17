package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/memory"
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

	// goroutine 2: should see Queued
	_, err := s.Ask(context.Background(), "two")
	if !errors.Is(err, ErrQueued) {
		t.Errorf("second Ask err = %v, want ErrQueued", err)
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

func TestSession_AskStream_ResizesContextWindow_WithRouter(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	// Set up router that routes to fast model with 128K context
	s.Router = func(ctx context.Context, prompt string, priorLevel string, history []ctxwin.PayloadMessage) (RouteResult, error) {
		return RouteResult{
			ProviderID:    "test",
			ModelID:       "fast-model",
			Level:         "L0-Conversation",
			ContextWindow: 131072,
		}, nil
	}

	// Verify initial CW state
	_, max, _ := cw.TokenUsage()
	if max != 1048576 {
		t.Fatalf("initial maxTokens = %d, want %d", max, 1048576)
	}

	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	for range ch {
	}

	// Verify CW was resized by the router
	_, max, _ = cw.TokenUsage()
	if max != 131072 {
		t.Errorf("after AskStream maxTokens = %d, want 131072", max)
	}
}

func TestSession_AskStream_ResizesContextWindow_DefaultWithoutRouter(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	// No router set

	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	for range ch {
	}

	// Without router, CW should remain at default (agent's Def.ContextWindow)
	_, max, _ := cw.TokenUsage()
	if max != 1048576 {
		t.Errorf("without router maxTokens = %d, want 1048576", max)
	}
}

func TestSession_AskStream_ResizesAndEvicts_WhenSmallerWindow(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	// Simulate existing history in CW by pushing directly
	cw.Push(ctxwin.RoleSystem, "You are a helpful assistant.")
	for i := 0; i < 15; i++ {
		cw.Push(ctxwin.RoleUser, "This is a test question to fill up the context window with tokens.")
		cw.Push(ctxwin.RoleAssistant, "This is a test answer that adds more tokens to the context window.")
	}

	tokensBefore, _, _ := cw.TokenUsage()
	if tokensBefore < 400 {
		t.Skipf("not enough tokens (%d) to test eviction, try longer messages", tokensBefore)
	}

	// Router returns a much smaller window
	s.Router = func(ctx context.Context, prompt string, priorLevel string, history []ctxwin.PayloadMessage) (RouteResult, error) {
		return RouteResult{
			ProviderID:    "test",
			ModelID:       "tiny-model",
			Level:         "L0-Conversation",
			ContextWindow: 500,
		}, nil
	}

	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	for range ch {
	}

	// Verify CW was resized and eviction ran
	_, max, buffer := cw.TokenUsage()
	if max != 500 {
		t.Errorf("maxTokens = %d, want 500", max)
	}
	current := cw.CurrentTokens()
	capacity := max - buffer
	if current > capacity {
		t.Errorf("currentTokens (%d) exceeds capacity (%d) after Resize+eviction", current, capacity)
	}
	sysMsg, ok := cw.MessageAt(0)
	if !ok || sysMsg.Role != ctxwin.RoleSystem {
		t.Errorf("first message = %+v, want system (never evicted)", sysMsg)
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
	if !errors.Is(err2, ErrQueued) {
		t.Errorf("second AskStream err = %v, want ErrQueued", err2)
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

func TestSession_AskStream_DelegationPendingDoesNotBlock(t *testing.T) {
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

	// Second AskStream should NOT block — it should proceed immediately
	gotSecond := make(chan error, 1)
	go func() {
		_, err := s.AskStream(context.Background(), "two")
		gotSecond <- err
	}()

	// Should succeed quickly (not block)
	select {
	case err := <-gotSecond:
		if err != nil {
			t.Errorf("second AskStream should succeed during delegation, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("second AskStream blocked during delegation — should not block")
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

// ─── Level lock helpers ─────────────────────────────────────────────────

func TestParseLevelLockCommand(t *testing.T) {
	tests := []struct {
		prompt    string
		wantLevel string
		wantLock  bool
	}{
		{"/l0", "L0-Conversation", true},
		{"/l0 tell me something", "L0-Conversation", true},
		{"/l1 fix this bug", "L1-SimpleSingleFile", true},
		{"/l2 refactor the auth module", "L2-MediumMultiFile", true},
		{"/l3 redesign the system", "L3-ComplexRefactoring", true},
		{"/max think hard", "L3-ComplexRefactoring", true},
		{"/expert analyze", "L3-ComplexRefactoring", true},
		{"/chat hello", "L0-Conversation", true},
		{"hello world", "", false},
		{"fix this bug", "", false},
		{"/read main.go", "", false},
		{"/refactor main.go", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			level, locked := parseLevelLockCommand(tt.prompt)
			if locked != tt.wantLock {
				t.Errorf("locked = %v, want %v", locked, tt.wantLock)
			}
			if level != tt.wantLevel {
				t.Errorf("level = %q, want %q", level, tt.wantLevel)
			}
		})
	}
}

func TestIsLevelLockCommand(t *testing.T) {
	if !isLevelLockCommand("/l2 analyze") {
		t.Error("/l2 analyze should be a lock command")
	}
	if isLevelLockCommand("analyze the problem") {
		t.Error("analyze the problem should not be a lock command")
	}
	if isLevelLockCommand("/read main.go") {
		t.Error("/read should not be a lock command")
	}
}

func TestLevelLocked_BlocksRouting(t *testing.T) {
	fake := &agent.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	// Set up a router that would classify everything as L0
	s.Router = func(ctx context.Context, prompt string, priorLevel string, history []ctxwin.PayloadMessage) (RouteResult, error) {
		return RouteResult{
			ProviderID:    "test",
			ModelID:       "test-model",
			Level:         "L0-Conversation",
			ContextWindow: 131072,
		}, nil
	}

	// First message: /l2 locks the level
	// Simulate what AskStream does: parse lock command, then route
	if lvl, locked := parseLevelLockCommand("/l2 do complex work"); locked {
		s.lastLevelMu.Lock()
		s.levelLocked = true
		s.lastLevel = lvl
		s.lastRouteResult = RouteResult{
			ProviderID: "test",
			ModelID:    "test-pro-model",
			Level:      lvl,
		}
		s.lastLevelMu.Unlock()
	}

	// Verify locked state
	s.lastLevelMu.RLock()
	if !s.levelLocked {
		t.Fatal("expected levelLocked=true after /l2")
	}
	if s.lastLevel != "L2-MediumMultiFile" {
		t.Errorf("expected L2-MediumMultiFile, got %q", s.lastLevel)
	}
	s.lastLevelMu.RUnlock()

	// A non-level-lock prompt while locked should use cached result
	if isLevelLockCommand("what does this code do?") {
		t.Error("regular prompt should not be detected as lock command")
	}
}

// ─── Session.FlushMemory ──────────────────────────────────────────────

func TestSession_FlushMemory_PersistsUnrecordedMessages(t *testing.T) {
	fake := &agent.FakeLLM{Responses: []string{"# 2026-01-01\n\n## 2026-01-01 10:00\n- merged\n"}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	// Push some messages with timestamps older than the cursor so they get filtered.
	// We need timestamps strictly after the cursor.
	cw.Push(ctxwin.RoleUser, "hello", ctxwin.WithTimestamp(time.Now()))
	cw.Push(ctxwin.RoleAssistant, "hi there", ctxwin.WithTimestamp(time.Now()))

	memDir := t.TempDir()
	memMgr := memory.NewManager(memDir, fake, "deepseek", "fast", nil)
	s.SetMemoryManager(memMgr)

	var hookCalled bool
	s.SetMemoryHook(func(ctx context.Context, text string, at time.Time) {
		hookCalled = true
		_ = memMgr.RecordAt(ctx, text, at)
	})

	s.FlushMemory(context.Background())

	if !hookCalled {
		t.Error("expected memory hook to be called")
	}

	// Verify memory file was created
	files, _ := memMgr.ListMemoryFiles()
	if len(files) == 0 {
		t.Error("expected at least one memory file after flush")
	}
}

func TestSession_FlushMemory_SkipsWhenNoNewMessages(t *testing.T) {
	fake := &agent.FakeLLM{}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	memDir := t.TempDir()
	memMgr := memory.NewManager(memDir, fake, "deepseek", "fast", nil)
	// Set cursor to now so no messages pass the filter
	memMgr.AdvanceLastRecordedAt(time.Now())
	s.SetMemoryManager(memMgr)

	var hookCalled bool
	s.SetMemoryHook(func(ctx context.Context, text string, at time.Time) {
		hookCalled = true
	})

	s.FlushMemory(context.Background())

	if hookCalled {
		t.Error("memory hook should NOT be called when no new messages exist")
	}
}

func TestSession_FlushMemory_SkipsWhenNoHook(t *testing.T) {
	fake := &agent.FakeLLM{}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)
	cw.Push(ctxwin.RoleUser, "hello", ctxwin.WithTimestamp(time.Now()))

	// No memory hook or manager set — should not panic
	s.FlushMemory(context.Background())
}

// ─── timeUntilNextMidnight ────────────────────────────────────────────

func TestTimeUntilNextMidnight(t *testing.T) {
	d := timeUntilNextMidnight()
	if d <= 0 {
		t.Errorf("expected positive duration, got %v", d)
	}
	if d > 24*time.Hour {
		t.Errorf("duration should be less than 24h, got %v", d)
	}
}

// ─── DailyMemoryFlusher construction ──────────────────────────────────

func TestNewDailyMemoryFlusher(t *testing.T) {
	fake := &agent.FakeLLM{}
	factory := factoryFromFake(t, fake)
	mgr := NewSessionManager(factory, nil)
	flusher := NewDailyMemoryFlusher(mgr, nil, nil)
	if flusher == nil {
		t.Fatal("expected non-nil flusher")
	}
	if flusher.sessionMgr != mgr {
		t.Error("sessionMgr mismatch")
	}
	if flusher.memoryEngine != nil {
		t.Error("memoryEngine should be nil")
	}
}

func TestSession_AskStream_InterceptsCron(t *testing.T) {
	fake := &agent.FakeLLM{}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	var gotExpr, gotInst string
	s.cronHandler = func(ctx context.Context, expression, instruction string) (string, time.Time, error) {
		gotExpr = expression
		gotInst = instruction
		return "test-task-id", time.Now().Add(1 * time.Hour), nil
	}

	ch, err := s.AskStream(context.Background(), "/cron 0 12 * * 1 Check daily emails")
	if err != nil {
		t.Fatalf("AskStream /cron: %v", err)
	}

	var events []iface.AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if gotExpr != "0 12 * * 1" {
		t.Errorf("gotExpr = %q, want '0 12 * * 1'", gotExpr)
	}
	if gotInst != "Check daily emails" {
		t.Errorf("gotInst = %q, want 'Check daily emails'", gotInst)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}

	delta, ok := events[0].(agent.ContentDeltaEvent)
	if !ok || !strings.Contains(delta.Delta, "定时任务已成功创建") {
		t.Errorf("unexpected first event: %+v", events[0])
	}

	done, ok := events[len(events)-1].(agent.DoneEvent)
	if !ok || done.Content != "Cron task created." {
		t.Errorf("unexpected last event: %+v", events[len(events)-1])
	}
}

func TestSession_AskStream_InterceptsSlashCommands(t *testing.T) {
	fake := &agent.FakeLLM{}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	Version = "0.2.0-test"

	t.Run("help", func(t *testing.T) {
		ch, err := s.AskStream(context.Background(), "/help")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var events []iface.AgentEvent
		for ev := range ch {
			events = append(events, ev)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		delta, ok := events[0].(agent.ContentDeltaEvent)
		if !ok || !strings.Contains(delta.Delta, "可用命令：") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
	})

	t.Run("version", func(t *testing.T) {
		ch, err := s.AskStream(context.Background(), "/version")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var events []iface.AgentEvent
		for ev := range ch {
			events = append(events, ev)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		delta, ok := events[0].(agent.ContentDeltaEvent)
		if !ok || !strings.Contains(delta.Delta, "SoloQueue 0.2.0-test") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
	})

	t.Run("clear", func(t *testing.T) {
		ch, err := s.AskStream(context.Background(), "/clear")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var events []iface.AgentEvent
		for ev := range ch {
			events = append(events, ev)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		delta, ok := events[0].(agent.ContentDeltaEvent)
		if !ok || !strings.Contains(delta.Delta, "对话历史已清空") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
	})

	t.Run("cancel when idle", func(t *testing.T) {
		ch, err := s.AskStream(context.Background(), "/cancel")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var events []iface.AgentEvent
		for ev := range ch {
			events = append(events, ev)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		delta, ok := events[0].(agent.ContentDeltaEvent)
		if !ok || !strings.Contains(delta.Delta, "取消失败：") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
	})

	t.Run("cancel when busy", func(t *testing.T) {
		// Mock activeCancel to simulate a running task
		cancelCalled := false
		s.activeCancel = func() {
			cancelCalled = true
		}

		ch, err := s.AskStream(context.Background(), "/cancel")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var events []iface.AgentEvent
		for ev := range ch {
			events = append(events, ev)
		}
		if len(events) < 2 {
			t.Fatalf("expected at least 2 events, got %d", len(events))
		}
		delta, ok := events[0].(agent.ContentDeltaEvent)
		if !ok || !strings.Contains(delta.Delta, "已取消当前任务") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
		if !cancelCalled {
			t.Error("expected activeCancel to be called")
		}

		// Reset activeCancel
		s.activeCancel = nil
	})
}

