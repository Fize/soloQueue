package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/conversation"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

type sessionFakeClock struct {
	mu  sync.Mutex
	now time.Time
}

type blockingSessionCompactor struct {
	started chan struct{}
}

func (c *blockingSessionCompactor) Compact(ctx context.Context, _ []ctxwin.Message) (string, error) {
	close(c.started)
	<-ctx.Done()
	return "", context.Cause(ctx)
}

func (c *sessionFakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *sessionFakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestSessionOwnsDispatchManagerAndClearRetainsArtifacts(t *testing.T) {
	root := t.TempDir()
	tw, err := timeline.NewWriter(root, "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tw.Close() })
	a := agent.NewAgent(agent.Definition{ID: "test-agent"}, &agenttest.FakeLLM{}, nil, agent.WithTools(syncEchoTool{}))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	s := NewSession("session-1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
	if s.dispatchManager == nil || !a.HasTool("inspect_delegation") {
		t.Fatal("session must create its dispatch manager and inspection tool")
	}
	created, err := s.dispatchManager.Begin(dispatch.BeginInput{TaskName: "retain", Task: "Retain this.", Requester: "L1", Executor: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(root, "delegations", created.Record.ID, "meta.json")
	if _, err := os.Stat(metaPath); err != nil {
		t.Fatalf("soft clear removed dispatch artifact: %v", err)
	}
}

func TestSessionRejectsAskWhenDispatchManagerInitializationFails(t *testing.T) {
	root := t.TempDir()
	tw, err := timeline.NewWriter(root, "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tw.Close() })
	if err := os.WriteFile(filepath.Join(root, "delegations"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent(agent.Definition{ID: "test-agent"}, &agenttest.FakeLLM{Responses: []string{"must not run"}}, nil, agent.WithTools(syncEchoTool{}))
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	s := NewSession("session-1", "default", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), tw, nil)
	if _, err := s.AskStream(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "dispatch manager") {
		t.Fatalf("AskStream error=%v", err)
	}
}

// ─── Test helpers ──────────────────────────────────────────────────────

// startAgent builds + starts an agent with the given FakeLLM and returns it.
// t.Cleanup stops the agent.
func startAgent(t *testing.T, fake *agenttest.FakeLLM) *agent.Agent {
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

func TestSendMediaViaChannelUsesOnlyConfiguredChannel(t *testing.T) {
	calledQQ := 0
	calledWechat := 0
	sess := NewSession("media", "team", &agent.Agent{Def: agent.Definition{NotifyChannel: "qq"}}, nil, nil, nil)
	sess.channelMediaSenders = map[string]func(context.Context, []channel.OutboundMedia) error{
		"qq":     func(context.Context, []channel.OutboundMedia) error { calledQQ++; return nil },
		"wechat": func(context.Context, []channel.OutboundMedia) error { calledWechat++; return nil },
	}
	if err := sess.SendMediaViaChannel(context.Background(), []channel.OutboundMedia{{Kind: channel.MediaFile, Path: "/export/a"}}); err != nil {
		t.Fatal(err)
	}
	if calledQQ != 1 || calledWechat != 0 {
		t.Fatalf("qq=%d wechat=%d", calledQQ, calledWechat)
	}
}

// factoryFromFake returns a factory that produces fresh started agents each
// time from the given FakeLLM (sharing the same LLM across sessions).
func factoryFromFake(t *testing.T, fake *agenttest.FakeLLM) AgentFactory {
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

// ─── Session.AskStream ─────────────────────────────────────────────────

func TestSession_AskStream_AppendsHistoryOnDone(t *testing.T) {
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"hel", "lo"}}}
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

func TestSession_AskStream_PreservesLongPrompt(t *testing.T) {
	prompt := "this self-contained prompt must remain unchanged"
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"done"}}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	ch, err := s.AskStream(context.Background(), prompt)
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	for range ch {
	}

	history := s.History()
	if len(history) < 1 {
		t.Fatal("expected user prompt in history")
	}
	if history[0].Content != prompt {
		t.Fatalf("user prompt = %q, want %q", history[0].Content, prompt)
	}
	if strings.Contains(history[0].Content, "<recalled_memories>") {
		t.Fatal("user prompt must not receive recalled memory injection")
	}
}

func TestSession_AskStream_ErrorNoHistoryAppend(t *testing.T) {
	fake := &agenttest.FakeLLM{Err: errors.New("bad")}
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
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
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
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
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
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"ok"}}}
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
	fake := &agenttest.FakeLLM{
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

func TestSession_AskStream_RejectBusyDoesNotQueue(t *testing.T) {
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"x"}},
		Delay:        200 * time.Millisecond,
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	ch, err := s.AskStream(context.Background(), "one")
	if err != nil {
		t.Fatalf("first AskStream: %v", err)
	}
	_, err = s.AskStream(WithRejectBusyQueue(context.Background()), "must not queue")
	if !errors.Is(err, ErrQueued) {
		t.Fatalf("second AskStream err = %v, want ErrQueued", err)
	}
	if s.pending.Len() != 0 {
		t.Fatalf("desktop busy request entered pending queue: len=%d", s.pending.Len())
	}
	for range ch {
	}
}

func TestSession_CancelCurrent_KeepsSessionReusable(t *testing.T) {
	started := make(chan struct{})
	var startedOnce sync.Once
	fake := &agenttest.FakeLLM{
		Responses: []string{"cancelled response", "response after cancel"},
		Delay:     200 * time.Millisecond,
		Hook: func(agent.LLMRequest) {
			startedOnce.Do(func() { close(started) })
		},
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)

	first, err := s.AskStream(context.Background(), "cancel me")
	if err != nil {
		t.Fatalf("first AskStream: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first LLM request did not start")
	}

	if err := s.CancelCurrent("test stop"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	var cancellationErrors []error
	for event := range first {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			cancellationErrors = append(cancellationErrors, errorEvent.Err)
		}
	}
	if len(cancellationErrors) != 0 {
		t.Fatalf("explicit cancellation emitted terminal errors: %v", cancellationErrors)
	}

	deadline := time.Now().Add(time.Second)
	for a.State() == agent.StateProcessing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if a.State() == agent.StateStopped || a.State() == agent.StateStopping {
		t.Fatalf("cancel stopped reusable agent: state=%s", a.State())
	}

	second, err := s.AskStream(context.Background(), "still usable")
	if err != nil {
		t.Fatalf("AskStream after cancel: %v", err)
	}
	gotDone := false
	for ev := range second {
		if _, ok := ev.(agent.DoneEvent); ok {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("AskStream after cancel completed without DoneEvent")
	}
}

func TestSession_CancelRunTargetsOnlyRequestedTurn(t *testing.T) {
	log, err := logger.System(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("logger.System() error = %v", err)
	}
	s := &Session{activeCancels: make(map[string]activeTurnCancel), logger: log}
	ctxA, cancelA := context.WithCancelCause(context.Background())
	ctxB, cancelB := context.WithCancelCause(context.Background())
	s.registerActiveCancel("req-a", cancelA)
	s.registerActiveCancel("req-b", cancelB)

	cancelDone := make(chan error, 1)
	go func() { cancelDone <- s.CancelRun("req-a", "user cancelled") }()
	select {
	case <-ctxA.Done():
	case <-time.After(time.Second):
		t.Fatal("req-a was not cancelled")
	}
	select {
	case err := <-cancelDone:
		t.Fatalf("CancelRun returned before exact run cleanup: %v", err)
	default:
	}
	if !errors.Is(context.Cause(ctxA), context.Canceled) {
		t.Fatalf("req-a cause = %v, want context.Canceled", context.Cause(ctxA))
	}
	if got := runwatch.CodeOf(context.Cause(ctxA)); got != runwatch.CodeCancelledByUser {
		t.Fatalf("req-a code = %q, want %q", got, runwatch.CodeCancelledByUser)
	}
	if err := context.Cause(ctxB); err != nil {
		t.Fatalf("req-b was cancelled: %v", err)
	}

	s.unregisterActiveCancel("req-a")
	if err := <-cancelDone; err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	s.unregisterActiveCancel("req-b")
}

func TestEffectiveRunIDFallsBackToRFC4122UUID(t *testing.T) {
	runID := effectiveRunID(context.Background())
	if len(runID) != 36 || runID[8] != '-' || runID[13] != '-' || runID[18] != '-' || runID[23] != '-' {
		t.Fatalf("effectiveRunID() = %q, want RFC 4122 UUID form", runID)
	}
}

func TestSession_AskStreamProgressKeepsRunAliveBeyondLegacyLimit(t *testing.T) {
	clock := &sessionFakeClock{now: time.Unix(1_700_000_000, 0)}
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: 15 * time.Minute}, runwatch.WithClock(clock))
	defer watchdog.Close()
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetRunWatch(watchdog)
	source := make(chan agent.AgentEvent, 4)
	s.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		return source, nil
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-long"})
	stream, err := s.AskStream(ctx, "long task")
	if err != nil {
		t.Fatalf("AskStream() error = %v", err)
	}

	source <- agent.ContentDeltaEvent{Delta: "first"}
	clock.Advance(10 * time.Minute)
	source <- agent.ContentDeltaEvent{Delta: "second"}
	clock.Advance(11 * time.Minute)
	watchdog.Scan()
	if _, ok := s.WatchdogSnapshot("req-long"); !ok {
		t.Fatal("Session did not register request with RunWatch")
	}

	source <- agent.DoneEvent{Content: "firstsecond"}
	close(source)
	for range stream {
	}
	if _, ok := watchdog.Snapshot("req-long"); ok {
		t.Fatal("completed Session run remains registered")
	}
}

func TestSession_CompactRegistersExactCancelableRun(t *testing.T) {
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Hour})
	defer watchdog.Close()
	compactor := &blockingSessionCompactor{started: make(chan struct{})}
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer(), ctxwin.WithCompactor(compactor))
	cw.Push(ctxwin.RoleSystem, "system")
	cw.Push(ctxwin.RoleUser, "old message")
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, cw, nil, nil)
	s.SetRunWatch(watchdog)
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-compact"})
	stream, err := s.AskStream(ctx, "/compact")
	if err != nil {
		t.Fatalf("AskStream(/compact): %v", err)
	}
	select {
	case <-compactor.started:
	case <-time.After(time.Second):
		t.Fatal("compactor did not start")
	}
	if _, ok := s.WatchdogSnapshot("req-compact"); !ok {
		t.Fatal("/compact did not register watchdog run")
	}
	if err := s.CancelRun("req-compact", "test exact cancel"); err != nil {
		t.Fatalf("CancelRun(/compact): %v", err)
	}
	var terminal runwatch.Code
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			terminal = runwatch.CodeOf(errorEvent.Err)
		}
	}
	if terminal != runwatch.CodeCancelledByUser {
		t.Fatalf("compact terminal code = %q, want %q", terminal, runwatch.CodeCancelledByUser)
	}
	if _, ok := s.WatchdogSnapshot("req-compact"); ok {
		t.Fatal("cancelled /compact leaked watchdog run")
	}
}

func TestSession_AskStreamSetupFailureReleasesWatchdogRootForRetry(t *testing.T) {
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute})
	defer watchdog.Close()
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetRunWatch(watchdog)
	setupErr := errors.New("history setup failed")
	attempt := 0
	s.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		attempt++
		if attempt == 1 {
			return nil, setupErr
		}
		source := make(chan agent.AgentEvent, 1)
		source <- agent.DoneEvent{Content: "retried"}
		close(source)
		return source, nil
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-retry"})

	if _, err := s.AskStream(ctx, "first attempt"); !errors.Is(err, setupErr) {
		t.Fatalf("first AskStream() error = %v, want %v", err, setupErr)
	}
	if _, ok := watchdog.Snapshot("req-retry"); ok {
		t.Fatal("failed AskStream setup leaked watchdog root")
	}

	stream, err := s.AskStream(ctx, "retry")
	if err != nil {
		t.Fatalf("retry AskStream() error = %v", err)
	}
	for range stream {
	}
	if _, ok := watchdog.Snapshot("req-retry"); ok {
		t.Fatal("retried AskStream leaked watchdog root after completion")
	}
}

func TestSession_AskIsolatedOwnsWatchdogRootUntilStreamCloses(t *testing.T) {
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute})
	defer watchdog.Close()
	a := startAgent(t, &agenttest.FakeLLM{Responses: []string{"done"}, Delay: 50 * time.Millisecond})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetRunWatch(watchdog)
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RunID: "cron-run"})
	stream, err := s.AskIsolated(ctx, "scheduled work")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := watchdog.Snapshot("cron-run"); !ok {
		t.Fatal("AskIsolated did not register watchdog root")
	}
	for range stream {
	}
	if _, ok := watchdog.Snapshot("cron-run"); ok {
		t.Fatal("AskIsolated watchdog root survived stream completion")
	}
}

func TestSessionAskIsolatedParticipatesInSingleFlight(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"scheduled"}, Delay: 75 * time.Millisecond}
	a := startAgent(t, fake)
	s := NewSession("isolated-single-flight", "team", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	first, err := s.AskIsolated(context.Background(), "scheduled work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AskIsolated(context.Background(), "overlap"); !errors.Is(err, ErrSessionBusy) {
		t.Fatalf("overlapping isolated ask error = %v, want ErrSessionBusy", err)
	}
	for range first {
	}
	if !s.Idle() {
		t.Fatal("session remained busy after isolated stream closed")
	}
}

func TestReleasedDelegationCannotClearLaterRequestOwnership(t *testing.T) {
	s := NewSession("flight-owner", "team", nil, nil, nil, nil)
	first, ok := s.acquireFlight()
	if !ok {
		t.Fatal("first flight was not acquired")
	}
	s.releaseFlight(first) // delegation yielded and made the session schedulable
	second, ok := s.acquireFlight()
	if !ok {
		t.Fatal("second flight was not acquired")
	}
	s.releaseFlight(first) // old forwarder finishes after the second ask started
	if s.Idle() {
		t.Fatal("old delegation completion cleared the later request's ownership")
	}
	s.releaseFlight(second)
	if !s.Idle() {
		t.Fatal("current request did not release its own flight")
	}
}

func TestRequestRouteCaptureStaysExactAcrossAsyncYield(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "leader", ModelID: "default", ProviderID: "default-provider"}, &agenttest.FakeLLM{}, nil)
	s := NewSession("route-yield", "team", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.Router = func(_ context.Context, prompt, _ string, _ []ctxwin.PayloadMessage) (RouteResult, error) {
		return RouteResult{Level: "engineering", ModelID: "model-" + prompt, ProviderID: "provider-" + prompt}, nil
	}
	var sourcesMu sync.Mutex
	var sources []chan agent.AgentEvent
	s.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		ch := make(chan agent.AgentEvent, 2)
		sourcesMu.Lock()
		sources = append(sources, ch)
		sourcesMu.Unlock()
		return ch, nil
	}

	firstCtx, firstRouteCh := WithRequestRouteCapture(context.Background())
	first, err := s.AskStream(firstCtx, "first")
	if err != nil {
		t.Fatal(err)
	}
	firstRoute := <-firstRouteCh
	sourcesMu.Lock()
	firstSource := sources[0]
	sourcesMu.Unlock()
	firstSource <- agent.DelegationStartedEvent{}
	deadline := time.Now().Add(time.Second)
	for !s.Idle() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.Idle() {
		t.Fatal("async yield did not release Session flight")
	}

	secondCtx, secondRouteCh := WithRequestRouteCapture(context.Background())
	second, err := s.AskStream(secondCtx, "second")
	if err != nil {
		t.Fatal(err)
	}
	secondRoute := <-secondRouteCh
	if firstRoute.ModelID != "model-first" || secondRoute.ModelID != "model-second" {
		t.Fatalf("routes crossed async yield: first=%+v second=%+v", firstRoute, secondRoute)
	}
	sourcesMu.Lock()
	secondSource := sources[1]
	sourcesMu.Unlock()
	firstSource <- agent.DoneEvent{Content: "first"}
	close(firstSource)
	secondSource <- agent.DoneEvent{Content: "second"}
	close(secondSource)
	for range first {
	}
	for range second {
	}
}

func TestGenerationRebuildKeepsFreshGenerationWhenOldRecoveryRetries(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "old"}, &agenttest.FakeLLM{}, nil)
	fresh := agent.NewAgent(agent.Definition{ID: "fresh"}, &agenttest.FakeLLM{}, nil)
	s := NewSession("generation-cas", "team", old, nil, nil, nil)
	var builds atomic.Int32
	s.SetGenerationRebuilder(func(context.Context) (*agent.Agent, *agent.Supervisor, error) {
		builds.Add(1)
		return fresh, nil, nil
	})
	if err := s.rebuildQuarantinedAgent(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if err := s.rebuildQuarantinedAgent(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if builds.Load() != 1 {
		t.Fatalf("generation factory called %d times, want 1", builds.Load())
	}
	if s.CurrentAgent() != fresh {
		t.Fatal("late old-generation recovery replaced the fresh generation")
	}
}

func TestGenerationRebuildPublishesPendingAgentAndSupervisorOnlyAfterSwap(t *testing.T) {
	registry := agent.NewRegistry(nil)
	old := agent.NewAgent(agent.Definition{ID: "leader"}, &agenttest.FakeLLM{}, nil)
	fresh := agent.NewAgent(agent.Definition{ID: "leader"}, &agenttest.FakeLLM{}, nil, agent.WithSchedulingPending())
	for _, a := range []*agent.Agent{old, fresh} {
		if err := a.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := registry.Register(a); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = old.Stop(time.Second)
		_ = fresh.Stop(time.Second)
	})
	freshSupervisor := agent.NewSupervisor(fresh, nil, nil)
	s := NewSession("pending-publish", "team", old, nil, nil, nil)
	s.SetAgentRegistry(registry)
	published := make(chan *agent.Supervisor, 1)
	s.SetSupervisorPublisher(func(sv *agent.Supervisor) { published <- sv })
	factoryStarted := make(chan struct{})
	releaseFactory := make(chan struct{})
	s.SetGenerationRebuilder(func(context.Context) (*agent.Agent, *agent.Supervisor, error) {
		close(factoryStarted)
		<-releaseFactory
		return fresh, freshSupervisor, nil
	})
	rebuilt := make(chan error, 1)
	go func() { rebuilt <- s.rebuildQuarantinedAgent(context.Background(), old) }()
	<-factoryStarted
	loc, ok := registry.Locate("leader")
	if !ok || loc.(*agent.LocatableAdapter).Agent != old {
		t.Fatal("pending replacement displaced the current schedulable generation")
	}
	select {
	case <-published:
		t.Fatal("fresh Supervisor published before Session generation swap")
	default:
	}
	close(releaseFactory)
	if err := <-rebuilt; err != nil {
		t.Fatal(err)
	}
	loc, ok = registry.Locate("leader")
	if !ok || loc.(*agent.LocatableAdapter).Agent != fresh {
		t.Fatal("fresh generation was not the sole schedulable Agent after swap")
	}
	select {
	case got := <-published:
		if got != freshSupervisor {
			t.Fatal("published wrong Supervisor")
		}
	default:
		t.Fatal("fresh Supervisor was not published after Session generation swap")
	}
}

func TestGenerationRebuildRejectedByConcurrentCloseCleansFreshDomain(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "leader"}, &agenttest.FakeLLM{}, nil)
	fresh := agent.NewAgent(agent.Definition{ID: "leader"}, &agenttest.FakeLLM{}, nil, agent.WithSchedulingPending())
	if err := old.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Stop(time.Second) })
	if err := fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	registry := agent.NewRegistry(nil)
	if err := registry.Register(old); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(fresh); err != nil {
		t.Fatal(err)
	}
	s := NewSession("close-race", "team", old, nil, nil, nil)
	s.SetAgentRegistry(registry)
	freshSupervisor := agent.NewSupervisor(fresh, nil, nil)
	removed := make(chan *agent.Supervisor, 1)
	s.SetSupervisor(nil, func(sv *agent.Supervisor) { removed <- sv })
	factoryStarted := make(chan struct{})
	releaseFactory := make(chan struct{})
	s.SetGenerationRebuilder(func(context.Context) (*agent.Agent, *agent.Supervisor, error) {
		close(factoryStarted)
		<-releaseFactory
		return fresh, freshSupervisor, nil
	})
	rebuilt := make(chan error, 1)
	go func() { rebuilt <- s.rebuildQuarantinedAgent(context.Background(), old) }()
	<-factoryStarted
	loc, ok := registry.Locate("leader")
	if !ok || loc.(*agent.LocatableAdapter).Agent != old {
		t.Fatal("pending replacement became locatable before close rejection")
	}
	closed := make(chan struct{})
	go func() {
		s.Close()
		close(closed)
	}()
	for !s.closed.Load() {
		time.Sleep(time.Millisecond)
	}
	close(releaseFactory)
	if err := <-rebuilt; !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("rebuild error = %v, want ErrSessionClosed", err)
	}
	<-closed
	if s.CurrentAgent() != old {
		t.Fatal("fresh generation was published after Session.Close")
	}
	if _, ok := registry.Get(fresh.InstanceID); ok {
		t.Fatal("rejected fresh generation leaked in AgentRegistry")
	}
	select {
	case got := <-removed:
		if got != freshSupervisor {
			t.Fatal("removed wrong fresh supervisor")
		}
	default:
		t.Fatal("rejected fresh supervisor leaked in runtime registry")
	}
	if fresh.State() != agent.StateStopped {
		t.Fatalf("fresh agent state = %s, want stopped", fresh.State())
	}
}

func TestRoutedModelSurvivesQuarantinedGenerationRebuild(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "old", ModelID: "default"}, &agenttest.FakeLLM{}, nil)
	if err := old.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Quarantine(errors.New("stuck"))
	var gotModel, gotProvider string
	fresh := agent.NewAgent(agent.Definition{ID: "fresh", ModelID: "default"}, &agenttest.FakeLLM{
		Responses: []string{"ok"},
		Hook: func(req agent.LLMRequest) {
			gotModel, gotProvider = req.Model, req.ProviderID
		},
	}, nil)
	if err := fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Stop(time.Second) })
	s := NewSession("route-rebuild", "team", old, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.Router = func(context.Context, string, string, []ctxwin.PayloadMessage) (RouteResult, error) {
		return RouteResult{Level: "engineering", ProviderID: "routed-provider", ModelID: "routed-model"}, nil
	}
	s.SetAgentRebuilder(func(context.Context) (*agent.Agent, error) { return fresh, nil })
	stream, err := s.AskStream(context.Background(), "repair")
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if gotModel != "routed-model" || gotProvider != "routed-provider" {
		t.Fatalf("rebuilt generation route = %q/%q, want routed-provider/routed-model", gotProvider, gotModel)
	}
}

func TestGenerationRebuildReapsOldSupervisorDomain(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "old"}, &agenttest.FakeLLM{}, nil)
	child := agent.NewAgent(agent.Definition{ID: "child"}, &agenttest.FakeLLM{}, nil)
	for _, a := range []*agent.Agent{old, child} {
		if err := a.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	oldSupervisor := agent.NewSupervisor(old, nil, nil)
	oldSupervisor.AdoptChild(child)
	fresh := agent.NewAgent(agent.Definition{ID: "fresh"}, &agenttest.FakeLLM{}, nil)
	if err := fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	newSupervisor := agent.NewSupervisor(fresh, nil, nil)
	s := NewSession("l2-generation", "team", old, nil, nil, nil)
	removed := make(chan *agent.Supervisor, 1)
	s.SetSupervisor(oldSupervisor, func(sv *agent.Supervisor) { removed <- sv })
	s.SetGenerationRebuilder(func(context.Context) (*agent.Agent, *agent.Supervisor, error) {
		return fresh, newSupervisor, nil
	})
	if err := s.rebuildQuarantinedAgent(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if s.CurrentSupervisor() != newSupervisor {
		t.Fatal("fresh Supervisor was not published with fresh Agent")
	}
	select {
	case sv := <-removed:
		if sv != oldSupervisor {
			t.Fatal("removed wrong supervisor")
		}
	default:
		t.Fatal("old supervisor was not removed from runtime ownership")
	}
	if child.State() != agent.StateStopped {
		t.Fatalf("old L3 child state = %s, want stopped", child.State())
	}
	_ = fresh.Stop(time.Second)
}

func TestSession_AskStreamOwnsWatchdogRoot(t *testing.T) {
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute})
	defer watchdog.Close()
	a := startAgent(t, &agenttest.FakeLLM{Responses: []string{"done"}, Delay: 50 * time.Millisecond})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetRunWatch(watchdog)
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "sync-run"})
	done := make(chan error, 1)
	go func() {
		stream, err := s.AskStream(ctx, "sync work")
		if err != nil {
			done <- err
			return
		}
		for range stream {
		}
		done <- nil
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if _, ok := watchdog.Snapshot("sync-run"); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("AskStream did not register watchdog root")
		}
		time.Sleep(time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, ok := watchdog.Snapshot("sync-run"); ok {
		t.Fatal("AskStream watchdog root survived completion")
	}
}

func TestSessionAskStreamNilAgentReleasesWatchdogAndCancelOwnership(t *testing.T) {
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute})
	defer watchdog.Close()
	s := NewSession("nil-agent", "team", nil, ctxwin.NewContextWindow(8192, 1024, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetRunWatch(watchdog)
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "nil-agent-run"})
	if _, err := s.AskStream(ctx, "hello"); err == nil {
		t.Fatal("AskStream with no current Agent succeeded")
	}
	if _, ok := watchdog.Snapshot("nil-agent-run"); ok {
		t.Fatal("nil-Agent setup failure leaked watchdog root")
	}
	if err := s.CancelRun("nil-agent-run", "late cancel"); !errors.Is(err, ErrNoActiveTask) {
		t.Fatalf("CancelRun after setup failure = %v, want ErrNoActiveTask", err)
	}
}

func TestSession_WatchdogCancellationEmitsTypedTerminalError(t *testing.T) {
	clock := &sessionFakeClock{now: time.Unix(1_700_000_000, 0)}
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute}, runwatch.WithClock(clock))
	defer watchdog.Close()
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.SetRunWatch(watchdog)
	source := make(chan agent.AgentEvent)
	s.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		return source, nil
	}
	ctx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: "req-stalled"})
	stream, err := s.AskStream(ctx, "stalled task")
	if err != nil {
		t.Fatalf("AskStream() error = %v", err)
	}
	clock.Advance(2 * time.Minute)
	watchdog.Scan()
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			if got := runwatch.CodeOf(errorEvent.Err); got != runwatch.CodeRootOrphaned {
				t.Fatalf("terminal code = %q, want %q", got, runwatch.CodeRootOrphaned)
			}
			return
		}
	}
	t.Fatal("watchdog cancellation completed without a typed terminal error")
}

func TestSession_WatchdogEventProjectionDoesNotDuplicateConfirmationPause(t *testing.T) {
	clock := &sessionFakeClock{now: time.Unix(1_700_000_000, 0)}
	watchdog := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute}, runwatch.WithClock(clock))
	defer watchdog.Close()
	_, root, err := watchdog.Start(context.Background(), runwatch.Metadata{RunID: "confirm-projection"})
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{}
	s.applyWatchdogEvent(root, agent.ToolNeedsConfirmEvent{CallID: "call-1"})
	snapshot, _ := watchdog.Snapshot("confirm-projection")
	if snapshot.PausedReason != "" {
		t.Fatalf("Session duplicated lower-layer confirmation pause: %q", snapshot.PausedReason)
	}
	clock.Advance(10 * time.Second)
	s.applyWatchdogEvent(root, agent.ContentDeltaEvent{Delta: "progress"})
	snapshot, _ = watchdog.Snapshot("confirm-projection")
	if want := clock.Now(); !snapshot.LastProgressAt.Equal(want) {
		t.Fatalf("LastProgressAt = %v, want event time %v", snapshot.LastProgressAt, want)
	}
}

func TestSession_AskStream_DeadlineEmitsOneTerminalError(t *testing.T) {
	fake := &agenttest.FakeLLM{
		Responses: []string{"late response"},
		Delay:     200 * time.Millisecond,
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.requestTimeout = 10 * time.Millisecond

	stream, err := s.AskStream(context.Background(), "time out")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var terminalErrors []error
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			terminalErrors = append(terminalErrors, errorEvent.Err)
		}
	}

	if len(terminalErrors) != 1 {
		t.Fatalf("terminal error count = %d, want 1", len(terminalErrors))
	}
	if got := terminalErrors[0].Error(); got != "Session request timed out after 10ms" {
		t.Fatalf("terminal error = %q", got)
	}
}

func TestSession_AskStream_DeadlineReleasesWithSaturatedOutput(t *testing.T) {
	deltas := make([]string, 256)
	for i := range deltas {
		deltas[i] = "x"
	}
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{deltas}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.requestTimeout = 100 * time.Millisecond

	stream, err := s.AskStream(context.Background(), "fill the output buffer")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	fillDeadline := time.Now().Add(time.Second)
	for len(stream) < cap(stream) && time.Now().Before(fillDeadline) {
		time.Sleep(time.Millisecond)
	}
	if len(stream) != cap(stream) {
		t.Fatalf("output buffer did not saturate: len=%d cap=%d", len(stream), cap(stream))
	}

	releaseDeadline := time.Now().Add(500 * time.Millisecond)
	for s.inFlight.Load() != 0 && time.Now().Before(releaseDeadline) {
		time.Sleep(time.Millisecond)
	}
	releasedBeforeDrain := s.inFlight.Load() == 0

	var terminalErrors []error
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			terminalErrors = append(terminalErrors, errorEvent.Err)
		}
	}
	if !releasedBeforeDrain {
		t.Fatal("deadline cleanup waited for the saturated output consumer")
	}
	if len(terminalErrors) != 1 {
		t.Fatalf("terminal error count = %d, want 1", len(terminalErrors))
	}
	if got := terminalErrors[0].Error(); got != "Session request timed out after 100ms" {
		t.Fatalf("terminal error = %q", got)
	}
}

func TestSession_AskStream_DeadlineSourceErrorsAndCloseEmitOnce(t *testing.T) {
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	s.requestTimeout = 10 * time.Millisecond
	s.askStreamHistory = func(ctx context.Context, _ *ctxwin.ContextWindow, _ string) (<-chan agent.AgentEvent, error) {
		source := make(chan agent.AgentEvent, 2)
		go func() {
			<-ctx.Done()
			source <- agent.ErrorEvent{Err: context.DeadlineExceeded}
			source <- agent.ErrorEvent{Err: context.DeadlineExceeded}
			close(source)
		}()
		return source, nil
	}

	stream, err := s.AskStream(context.Background(), "source deadline")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var terminalErrors []error
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			terminalErrors = append(terminalErrors, errorEvent.Err)
		}
	}
	if len(terminalErrors) != 1 {
		t.Fatalf("terminal error count = %d, want 1", len(terminalErrors))
	}
	if got := terminalErrors[0].Error(); got != "Session request timed out after 10ms" {
		t.Fatalf("terminal error = %q", got)
	}
}

func TestSession_AskStream_LiveContextForwardsUpstreamDeadline(t *testing.T) {
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	upstreamErr := fmt.Errorf("provider timeout: %w", context.DeadlineExceeded)
	s.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		source := make(chan agent.AgentEvent, 1)
		source <- agent.ErrorEvent{Err: upstreamErr}
		close(source)
		return source, nil
	}

	stream, err := s.AskStream(context.Background(), "upstream deadline")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var errorsSeen []error
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			errorsSeen = append(errorsSeen, errorEvent.Err)
		}
	}
	if len(errorsSeen) != 1 {
		t.Fatalf("error count = %d, want 1", len(errorsSeen))
	}
	if errorsSeen[0].Error() != upstreamErr.Error() {
		t.Fatalf("error = %q, want %q", errorsSeen[0], upstreamErr)
	}
}

func TestSession_AskStream_LiveContextForwardsUpstreamCancel(t *testing.T) {
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	upstreamErr := fmt.Errorf("provider canceled: %w", context.Canceled)
	s.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		source := make(chan agent.AgentEvent, 1)
		source <- agent.ErrorEvent{Err: upstreamErr}
		close(source)
		return source, nil
	}

	stream, err := s.AskStream(context.Background(), "upstream cancel")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var errorsSeen []error
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			errorsSeen = append(errorsSeen, errorEvent.Err)
		}
	}
	if len(errorsSeen) != 1 {
		t.Fatalf("error count = %d, want 1", len(errorsSeen))
	}
	if errorsSeen[0].Error() != upstreamErr.Error() {
		t.Fatalf("error = %q, want %q", errorsSeen[0], upstreamErr)
	}
}

func TestSession_CancelCurrent_SourceCancelErrorIsSilent(t *testing.T) {
	a := startAgent(t, &agenttest.FakeLLM{})
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	started := make(chan struct{})
	s.askStreamHistory = func(ctx context.Context, _ *ctxwin.ContextWindow, _ string) (<-chan agent.AgentEvent, error) {
		source := make(chan agent.AgentEvent, 2)
		close(started)
		go func() {
			<-ctx.Done()
			source <- agent.ErrorEvent{Err: context.Canceled}
			source <- agent.ErrorEvent{Err: context.Canceled}
			close(source)
		}()
		return source, nil
	}

	stream, err := s.AskStream(context.Background(), "cancel controlled source")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	<-started
	if err := s.CancelCurrent("test controlled cancel"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	for event := range stream {
		if errorEvent, ok := event.(agent.ErrorEvent); ok {
			t.Fatalf("explicit cancellation emitted terminal error: %v", errorEvent.Err)
		}
	}
}

func TestSession_CancelCurrent_CancelsRouterRequest(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"response after router cancel"}}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	routerStarted := make(chan struct{})
	routerCalls := 0
	s.Router = func(ctx context.Context, _ string, _ string, _ []ctxwin.PayloadMessage) (RouteResult, error) {
		routerCalls++
		if routerCalls == 1 {
			close(routerStarted)
			<-ctx.Done()
			return RouteResult{}, ctx.Err()
		}
		return RouteResult{Level: "L2"}, nil
	}

	type streamResult struct {
		ch  <-chan iface.AgentEvent
		err error
	}
	firstResult := make(chan streamResult, 1)
	go func() {
		ch, err := s.AskStream(context.Background(), "cancel during routing")
		firstResult <- streamResult{ch: ch, err: err}
	}()

	select {
	case <-routerStarted:
	case <-time.After(time.Second):
		t.Fatal("router request did not start")
	}
	if err := s.CancelCurrent("stop router"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	first := <-firstResult
	if first.ch != nil {
		for range first.ch {
		}
	}

	second, err := s.AskStream(context.Background(), "route again")
	if err != nil {
		t.Fatalf("AskStream after router cancel: %v", err)
	}
	gotDone := false
	for ev := range second {
		if _, ok := ev.(agent.DoneEvent); ok {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("AskStream after router cancel completed without DoneEvent")
	}
}

// ─── SessionManager ────────────────────────────────────────────────────

func TestSessionManager_Init(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"r"}}
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

func TestSessionRebuildsQuarantinedAgentGeneration(t *testing.T) {
	old := agent.NewAgent(agent.Definition{ID: "old"}, &agenttest.FakeLLM{}, nil)
	s := NewSession("s-rebuild", "team", old, nil, nil, nil)
	registry := agent.NewRegistry(nil)
	if err := registry.Register(old); err != nil {
		t.Fatal(err)
	}
	s.SetAgentRegistry(registry)
	fresh := agent.NewAgent(agent.Definition{ID: "fresh"}, &agenttest.FakeLLM{}, nil, agent.WithSchedulingPending())
	if err := registry.Register(fresh); err != nil {
		t.Fatal(err)
	}
	s.SetAgentRebuilder(func(context.Context) (*agent.Agent, error) { return fresh, nil })
	if err := old.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	old.Quarantine(errors.New("stuck"))
	if err := s.rebuildQuarantinedAgent(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.CurrentAgent() != fresh {
		t.Fatalf("session agent = %p, want fresh %p", s.CurrentAgent(), fresh)
	}
	if old.State() != agent.StateQuarantined {
		t.Fatalf("old agent state = %s, want quarantined", old.State())
	}
	if registry.Len() != 1 {
		t.Fatalf("registry generation count = %d, want only fresh", registry.Len())
	}
	if got, ok := registry.Get(fresh.InstanceID); !ok || got != fresh {
		t.Fatal("registry did not retain fresh generation")
	}
	_ = fresh.Stop(time.Second)
}

func TestWatchdogTimerDoesNotQuarantineLaterAgentJob(t *testing.T) {
	oldGrace := quarantineGraceNanos.Load()
	quarantineGraceNanos.Store(int64(5 * time.Millisecond))
	t.Cleanup(func() { quarantineGraceNanos.Store(oldGrace) })
	fake := &agenttest.FakeLLM{Responses: []string{"first", "second"}, Delay: 50 * time.Millisecond}
	a := startAgent(t, fake)
	s := NewSession("s-watchdog-identity", "team", a, ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer()), nil, nil)
	first, firstJob, err := a.AskStreamWithHistoryTracked(context.Background(), s.cw, "first")
	if err != nil {
		t.Fatal(err)
	}
	for range first {
	}
	second, secondJob, err := a.AskStreamWithHistoryTracked(context.Background(), s.cw, "second")
	if err != nil {
		t.Fatal(err)
	}
	s.quarantineAgentAfterWatchdog(&runwatch.Cause{Code: runwatch.CodeModelSemanticStalled}, a, firstJob)
	time.Sleep(20 * time.Millisecond)
	if a.State() == agent.StateQuarantined {
		t.Fatal("watchdog timer quarantined a later request")
	}
	if !secondJob.Active() {
		t.Fatal("second request was not active during quarantine check")
	}
	for range second {
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
	fake := &agenttest.FakeLLM{Responses: []string{"r"}}
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

	// AskStream on shutdown session returns ErrSessionClosed
	_, err = s.AskStream(context.Background(), "hi")
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("AskStream after Shutdown err = %v, want ErrSessionClosed", err)
	}
}

func TestSessionManagerConsecutiveRebuildsCloseOwnedResourcesExactlyOnce(t *testing.T) {
	var sequence atomic.Int32
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		a := agent.NewAgent(agent.Definition{ID: fmt.Sprintf("agent-%d", sequence.Add(1))}, &agenttest.FakeLLM{}, nil)
		if err := a.Start(ctx); err != nil {
			return nil, nil, nil, err
		}
		return a, ctxwin.NewContextWindow(8192, 1024, 0, ctxwin.NewTokenizer()), nil, nil
	}
	mgr := NewSessionManager(factory, nil)
	s, err := mgr.Init(context.Background(), "team")
	if err != nil {
		t.Fatal(err)
	}
	var closes atomic.Int32
	s.resourceCloser = func() error {
		closes.Add(1)
		return nil
	}
	for range 2 {
		old := s.CurrentAgent()
		old.Quarantine(errors.New("stuck"))
		if err := s.rebuildQuarantinedAgent(context.Background(), old); err != nil {
			t.Fatal(err)
		}
	}
	mgr.Shutdown(time.Second)
	mgr.Shutdown(time.Second)
	if got := closes.Load(); got != 1 {
		t.Fatalf("Session-owned resource closes = %d, want 1", got)
	}
}

// ─── Delegation-aware inFlight ─────────────────────────────────────────

func TestSession_AskStream_DelegationReleasesInFlight(t *testing.T) {
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"hello"}}}
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
	fake := &agenttest.FakeLLM{
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

// startTimelineSession builds a session whose CW push hook writes to a real
// timeline (mirroring the production builder wiring), returning the session
// plus a function to read back the persisted assistant rows.
func startTimelineSession(t *testing.T, fake *agenttest.FakeLLM, tools ...tools.Tool) (*Session, func() []string) {
	t.Helper()
	dir := t.TempDir()
	tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("timeline.NewWriter: %v", err)
	}
	t.Cleanup(func() { _ = tl.Close() })

	opts := []agent.Option{}
	if len(tools) > 0 {
		opts = append(opts, agent.WithTools(tools...))
	}
	a := agent.NewAgent(agent.Definition{ID: "tl-agent"}, fake, nil, opts...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("agent Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })

	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer(),
		ctxwin.WithPushHook(func(msg ctxwin.Message) {
			if msg.Role != ctxwin.RoleAssistant {
				return
			}
			var tcs []timeline.ToolCallRec
			for _, tc := range msg.ToolCalls {
				tcs = append(tcs, timeline.ToolCallRec{
					ID:        tc.ID,
					Type:      tc.Type,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}
			_ = tl.AppendMessage(&timeline.MessagePayload{
				Role:             string(msg.Role),
				Content:          msg.Content,
				ReasoningContent: msg.ReasoningContent,
				ToolCalls:        tcs,
				AgentID:          "tl-agent",
			})
		}),
	)
	cw.SetReplayMode(true)
	cw.Push(ctxwin.RoleSystem, "sys")
	cw.SetReplayMode(false)

	s := NewSession("s1", "t1", a, cw, tl, nil)

	readAssistants := func() []string {
		var out []string
		events, err := timeline.ReadFileEvents(timelineLatestFile(dir))
		if err != nil {
			return out
		}
		for _, ev := range events {
			if ev.EventType != timeline.EventMessage || ev.Message == nil {
				continue
			}
			if ev.Message.Role == "assistant" && ev.Message.Content != "" {
				out = append(out, ev.Message.Content)
			}
		}
		return out
	}
	return s, readAssistants
}

// timelineLatestFile returns the newest timeline-*.jsonl in dir.
func timelineLatestFile(dir string) string {
	files, _ := timeline.ListTimelineFiles(dir, "timeline")
	if len(files) == 0 {
		return ""
	}
	return files[len(files)-1]
}

// syncEchoTool is a synchronous tool used to trigger postIteration CW pushes
// (assistant content with tool_calls) inside the agent tool loop.
type syncEchoTool struct{}

func (syncEchoTool) Name() string                { return "echo" }
func (syncEchoTool) Description() string         { return "echoes its argument" }
func (syncEchoTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (syncEchoTool) Execute(ctx context.Context, args string) (string, error) {
	return "echoed", nil
}

// TestSession_AskStream_CancelAfterToolCall_NoDuplicateTimeline is a regression
// test for duplicate timeline rows: when a turn is cancelled after the agent's
// postIteration has already persisted assistant content (tool loop iteration),
// the partial flush must NOT write the same content a second time.
func TestSession_AskStream_CancelAfterToolCall_NoDuplicateTimeline(t *testing.T) {
	// Turn 0: content + tool_call (postIteration → pushHook → timeline).
	// Turn 1: delayed content — cancellation lands here, after content was
	// already persisted, reproducing the production duplicate-row bug.
	var llmCalls atomic.Int32
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"first", ""}, {"second"}},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				{Index: 0, ID: "call_1", Name: "echo", Arguments: `{}`},
			},
			nil,
		},
		FinishByTurn: []llm.FinishReason{llm.FinishToolCalls, llm.FinishStop},
		Delay:        300 * time.Millisecond,
		Hook: func(agent.LLMRequest) {
			llmCalls.Add(1)
		},
	}
	s, readAssistants := startTimelineSession(t, fake, syncEchoTool{})

	ch, err := s.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	// Wait until the second LLM call (turn 1) is in flight — by then the
	// first iteration's content is already persisted via postIteration.
	// Cancelling here exits the forwarder with gotDone=false while accContent
	// still holds "first", reproducing the production duplicate-row bug.
	deadline := time.Now().Add(3 * time.Second)
	for llmCalls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if llmCalls.Load() < 2 {
		t.Fatal("agent did not reach second LLM call")
	}

	if err := s.CancelCurrent("test cancel after tool call"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	for range ch {
	}

	// The timeline must contain exactly one row with content "first".
	var contents []string
	for _, c := range readAssistants() {
		if strings.Contains(c, "first") {
			contents = append(contents, c)
		}
	}
	if len(contents) != 1 {
		t.Errorf("timeline contains %d rows with content %q, want exactly 1 (duplicate partial flush)", len(contents), contents)
	}
}

// TestPartialFlushRemainder covers the TrimPrefix semantics deterministically:
// the guard must strip only the already-persisted prefix and keep the
// increment (never swallow it, never duplicate it).
func TestPartialFlushRemainder(t *testing.T) {
	cases := []struct {
		name      string
		pending   string
		persisted string
		want      string
	}{
		{"increment preserved", "firstsecond", "first", "second"},
		{"pure duplicate -> empty", "first", "first", ""},
		{"nothing persisted -> full buffer", "first", "", "first"},
		{"no prefix match -> full buffer", "first", "xyz", "first"},
		{"prefix overlap -> increment", "abc", "a", "bc"},
		{"multibyte prefix", "中文回复后续", "中文回复", "后续"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := partialFlushRemainder(tc.pending, tc.persisted)
			if got != tc.want {
				t.Errorf("partialFlushRemainder(%q, %q) = %q, want %q", tc.pending, tc.persisted, got, tc.want)
			}
		})
	}
}

// TestSession_AskStream_CancelNoDuplicate_AfterSamePrevTurn is a regression
// test for the assistantDump scoping fix: the persisted-content scan must only
// cover THIS turn's assistant rows. When a previous turn's reply shares text
// with this turn's output, the guard must still dedupe correctly.
func TestSession_AskStream_CancelNoDuplicate_AfterSamePrevTurn(t *testing.T) {
	// Regression for the assistantDump scoping fix: the persisted-content
	// scan must only cover THIS turn's assistant rows. Scenario: the CW
	// already holds a previous turn whose assistant reply shares the exact
	// same text ("same") as this turn's output. turn 0 emits "same" +
	// tool_call → postIteration persists "same"; cancellation then triggers
	// the partial flush with pending="same". If assistantDump wrongly
	// included the previous turn's "same" (rows before cwLenBeforeTurn),
	// the prefix check would fail and a duplicate row would be written.
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"same", ""}},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				{Index: 0, ID: "call_1", Name: "echo", Arguments: `{}`},
			},
		},
		FinishByTurn: []llm.FinishReason{llm.FinishToolCalls},
		Delay:        300 * time.Millisecond,
	}

	dir := t.TempDir()
	tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatalf("timeline.NewWriter: %v", err)
	}
	t.Cleanup(func() { _ = tl.Close() })
	a := agent.NewAgent(agent.Definition{ID: "tl-agent2"}, fake, nil, agent.WithTools(syncEchoTool{}))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("agent Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })
	cw2 := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer(),
		ctxwin.WithPushHook(func(msg ctxwin.Message) {
			if msg.Role != ctxwin.RoleAssistant {
				return
			}
			_ = tl.AppendMessage(&timeline.MessagePayload{
				Role:    string(msg.Role),
				Content: msg.Content,
				AgentID: "tl-agent2",
			})
		}),
	)
	cw2.SetReplayMode(true)
	cw2.Push(ctxwin.RoleSystem, "sys")
	// Pre-populate the previous turn: user prompt + assistant reply "same".
	cw2.Push(ctxwin.RoleUser, "first")
	cw2.Push(ctxwin.RoleAssistant, "same")
	cw2.SetReplayMode(false)
	s2 := NewSession("s2", "t1", a, cw2, tl, nil)

	ch2, err := s2.AskStream(context.Background(), "second")
	if err != nil {
		t.Fatalf("second AskStream: %v", err)
	}
	// Wait until postIteration has persisted "same" (turn 0 completed).
	deadline := time.Now().Add(5 * time.Second)
	for {
		events, _ := timeline.ReadFileEvents(timelineLatestFile(dir))
		found := false
		for _, ev := range events {
			if ev.EventType == timeline.EventMessage && ev.Message != nil &&
				ev.Message.Role == "assistant" && ev.Message.Content == "same" {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for postIteration to persist 'same'")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := s2.CancelCurrent("test cancel"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	for range ch2 {
	}

	// Expect exactly 1 "same" row in THIS session's timeline: the turn-0
	// postIteration row. The partial flush must be skipped — no duplicate.
	var contents []string
	events, err := timeline.ReadFileEvents(timelineLatestFile(dir))
	if err == nil {
		for _, ev := range events {
			if ev.EventType == timeline.EventMessage && ev.Message != nil &&
				ev.Message.Role == "assistant" && ev.Message.Content == "same" {
				contents = append(contents, ev.Message.Content)
			}
		}
	}
	if len(contents) != 1 {
		t.Errorf("content %q persisted %d times, want 1; contents=%q", "same", len(contents), contents)
	}
}

// ─── Session.FlushMemory ──────────────────────────────────────────────

func TestSession_FlushMemory_PersistsUnrecordedMessages(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"# 2026-01-01\n\n## 2026-01-01 10:00\n- merged\n"}}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	// Push some messages with timestamps older than the cursor so they get filtered.
	// We need timestamps strictly after the cursor.
	cw.Push(ctxwin.RoleUser, "hello", ctxwin.WithTimestamp(time.Now()))
	cw.Push(ctxwin.RoleAssistant, "hi there", ctxwin.WithTimestamp(time.Now()))

	memDir := t.TempDir()
	memMgr := conversation.NewManager(memDir, fake, "deepseek", "fast", nil)
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
	fake := &agenttest.FakeLLM{}
	a := startAgent(t, fake)
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	s := NewSession("s1", "t1", a, cw, nil, nil)

	memDir := t.TempDir()
	memMgr := conversation.NewManager(memDir, fake, "deepseek", "fast", nil)
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
	fake := &agenttest.FakeLLM{}
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
	fake := &agenttest.FakeLLM{}
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

func TestSessionAskStreamDoesNotInterceptCronSlashText(t *testing.T) {
	var sawPrompt bool
	fake := &agenttest.FakeLLM{
		Responses: []string{"handled by agent"},
		Hook: func(req agent.LLMRequest) {
			for _, msg := range req.Messages {
				if msg.Role == "user" && strings.Contains(msg.Content, "/cron daily legacy task") {
					sawPrompt = true
				}
			}
		},
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	ch, err := s.AskStream(context.Background(), "/cron daily legacy task")
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if !sawPrompt {
		t.Fatal("/cron text was intercepted instead of being handled as a normal user prompt")
	}
}

func TestSessionAskIsolatedWithModelUsesAndClearsOverride(t *testing.T) {
	var gotModel, gotProvider string
	fake := &agenttest.FakeLLM{
		Responses: []string{"done"},
		Hook: func(req agent.LLMRequest) {
			gotModel = req.Model
			gotProvider = req.ProviderID
		},
	}
	a := startAgent(t, fake)
	s := NewSession("s1", "t1", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	ch, err := s.AskIsolatedWithModel(context.Background(), "run task", &iface.ModelOverrideParams{
		ProviderID: "scheduled-provider",
		ModelID:    "scheduled-model",
		TaskType:   "engineering",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if gotModel != "scheduled-model" || gotProvider != "scheduled-provider" {
		t.Fatalf("unexpected request model: provider=%q model=%q", gotProvider, gotModel)
	}
	if a.ModelOverride() != nil {
		t.Fatal("scheduled model override leaked after isolated execution")
	}
}

func TestSessionAskStreamWithModelUsesAndClearsOverride(t *testing.T) {
	var gotModel, gotProvider string
	fake := &agenttest.FakeLLM{
		Responses: []string{"done"},
		Hook: func(req agent.LLMRequest) {
			gotModel = req.Model
			gotProvider = req.ProviderID
		},
	}
	a := startAgent(t, fake)
	s := NewSession("s2", "t2", a, ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer()), nil, nil)
	ch, err := s.AskStreamWithModel(context.Background(), "run task", &iface.ModelOverrideParams{
		ProviderID: "stream-provider",
		ModelID:    "stream-model",
		TaskType:   "general",
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if gotModel != "stream-model" || gotProvider != "stream-provider" {
		t.Fatalf("unexpected request model: provider=%q model=%q", gotProvider, gotModel)
	}
	if a.ModelOverride() != nil {
		t.Fatal("stream model override leaked after execution")
	}
}

func TestSession_AskStream_InterceptsSlashCommands(t *testing.T) {
	fake := &agenttest.FakeLLM{}
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
		if !ok || !strings.Contains(delta.Delta, "Available commands:") {
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
		if !ok || !strings.Contains(delta.Delta, "Dialogue history cleared") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
	})

	t.Run("compact", func(t *testing.T) {
		ch, err := s.AskStream(context.Background(), "/compact")
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
		if !ok || !strings.Contains(delta.Delta, "compacted") {
			t.Errorf("unexpected event content: %+v", events[0])
		}
	})
}

func TestStripRecalledMemories(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal text",
			input:    "normal text",
			expected: "normal text",
		},
		{
			name:     "with recalled memories",
			input:    "<recalled_memories>\n1. memory\n</recalled_memories>\n\nreal message",
			expected: "real message",
		},
		{
			name:     "with spaces after tag",
			input:    "<recalled_memories>\n1. memory\n</recalled_memories>\n  \nreal message",
			expected: "real message",
		},
		{
			name:     "only tag",
			input:    "<recalled_memories>\n1. memory\n</recalled_memories>",
			expected: "<recalled_memories>\n1. memory\n</recalled_memories>",
		},
		{
			name:     "with design directive",
			input:    "real message\n\n[CRITICAL DIRECTIVE: Design preview mode is active. You MUST save any previewable HTML, CSS, JS, asset files, or drawings directly to the designated design directory: `/Users/xiaobaitu/github.com/kumquat/.soloqueue/design`. Storing them in any other directory is a STRICT PROTOCOL VIOLATION and will break the user's real-time interface rendering. Ensure your files are generated or modified exactly in this location.]",
			expected: "real message",
		},
		{
			name:     "with both memories and design directive",
			input:    "<recalled_memories>\n1. memory\n</recalled_memories>\n\nreal message\n\n[CRITICAL DIRECTIVE: Design preview mode is active. You MUST save any previewable HTML, CSS, JS, asset files, or drawings directly to the designated design directory: `/Users/xiaobaitu/github.com/kumquat/.soloqueue/design`. Storing them in any other directory is a STRICT PROTOCOL VIOLATION and will break the user's real-time interface rendering. Ensure your files are generated or modified exactly in this location.]",
			expected: "real message",
		},
		{
			name:     "with drawings block",
			input:    "real message\n\n[USER DRAWINGS/ANNOTATIONS DETECTED: The user has drawn visual markings/annotations on the HTML preview for file `ui-spec.html`. The drawing coordinates/strokes are saved directly in `<script id=\"sketch-data\" type=\"application/json\">` inside that HTML file. You MUST read this file and pay close attention to where the user circled, pointed, or highlighted to correctly address the request.]",
			expected: "real message",
		},
		{
			name:     "with all annotations combined",
			input:    "<recalled_memories>\n1. memory\n</recalled_memories>\n\nreal message\n\n[SELECTED DOM ELEMENT:\n- Selector: `button`]\n\n[USER DRAWINGS/ANNOTATIONS DETECTED: ...]\n\n[CRITICAL DIRECTIVE: ...]",
			expected: "real message",
		},
		{
			name:     "with legacy ws upload block",
			input:    "real message\n\n[Uploaded files:\n- image.png: /Users/xiaobaitu/.soloqueue/downloads/image.png (image, recognized by visual model)\n]",
			expected: "real message",
		},
		{
			name:     "with legacy ws upload block and vision transcription",
			input:    "real message\n\n[Uploaded files:\n- image.png: /x/y.png (image, recognized by visual model)\n]\n\n[System: The user included 1 image(s). ...]",
			expected: "real message",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripRecalledMemories(tc.input)
			if got != tc.expected {
				t.Errorf("StripRecalledMemories(%q) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}
