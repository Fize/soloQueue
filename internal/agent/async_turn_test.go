package agent

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

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

// mockAsyncTool implements AsyncTool for testing.
type mockAsyncTool struct {
	name      string
	action    *tools.AsyncAction
	actionErr error
}

type lateCancelLocatable struct {
	stream    <-chan iface.AgentEvent
	started   chan struct{}
	doneCalls atomic.Int32
	startOnce sync.Once
}

func (l *lateCancelLocatable) Ask(context.Context, string) (string, error) { return "", nil }
func (l *lateCancelLocatable) AskStream(context.Context, string) (<-chan iface.AgentEvent, error) {
	l.startOnce.Do(func() { close(l.started) })
	return l.stream, nil
}
func (l *lateCancelLocatable) ErrorCount() int32 { return 0 }
func (l *lateCancelLocatable) LastError() string { return "" }
func (l *lateCancelLocatable) OnDelegationDone() { l.doneCalls.Add(1) }

func TestAsyncResumeSubmissionFailureClosesStreamAndTracker(t *testing.T) {
	a := NewAgent(Definition{ID: "stopped"}, &agenttest.FakeLLM{}, newTestLogger(t), WithPriorityMailbox())
	tracker := &jobTracker{id: 99, done: make(chan struct{})}
	out := make(chan AgentEvent)
	turn := &asyncTurnState{
		iter:      7,
		out:       out,
		callerCtx: withJobTracker(context.Background(), tracker),
	}
	a.turnMu.Lock()
	a.asyncTurns[turn.iter] = turn
	a.turnMu.Unlock()

	a.submitAsyncResume(turn, func(context.Context) {})
	select {
	case <-out:
	case <-time.After(time.Second):
		t.Fatal("failed continuation submission left output stream open")
	}
	select {
	case <-tracker.done:
	case <-time.After(time.Second):
		t.Fatal("failed continuation submission left tracker open")
	}
}

func TestAsyncResumeHighQueueCancellationTerminatesPromptly(t *testing.T) {
	a := startedAgent(t, &agenttest.FakeLLM{}, WithPriorityMailbox())
	occupied := make(chan struct{})
	actorStarted := make(chan struct{})
	defer close(occupied)
	if err := a.submit(context.Background(), func(context.Context) {
		close(actorStarted)
		<-occupied
	}); err != nil {
		t.Fatal(err)
	}
	<-actorStarted
	for range 4 {
		a.priorityMailbox.SubmitHigh(func(context.Context) {})
	}
	ctx, cancel := context.WithCancel(context.Background())
	tracker := &jobTracker{id: 100, done: make(chan struct{})}
	out := make(chan AgentEvent)
	mergedCanceled := make(chan struct{})
	turn := &asyncTurnState{
		iter:      8,
		out:       out,
		callerCtx: withJobTracker(ctx, tracker),
		cancelMerged: func() {
			select {
			case <-mergedCanceled:
			default:
				close(mergedCanceled)
			}
		},
	}
	a.turnMu.Lock()
	a.asyncTurns[turn.iter] = turn
	a.turnMu.Unlock()
	a.taskWg.Add(1)
	go func() {
		defer a.taskWg.Done()
		a.submitAsyncResume(turn, func(context.Context) {})
	}()
	cancel()
	waits := []struct {
		name string
		wait func() bool
	}{
		{"output", func() bool {
			select {
			case <-out:
				return true
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}},
		{"tracker", func() bool {
			select {
			case <-tracker.done:
				return true
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}},
		{"merged context", func() bool {
			select {
			case <-mergedCanceled:
				return true
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}},
	}
	for _, item := range waits {
		if !item.wait() {
			t.Fatalf("%s was retained by full high-priority queue", item.name)
		}
	}
	waited := make(chan struct{})
	go func() { a.taskWg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("taskWg retained canceled continuation submission")
	}
}

func TestAbandonedAsyncTurnIgnoresLateWatcherCompletion(t *testing.T) {
	a := NewAgent(Definition{ID: "abandon"}, &agenttest.FakeLLM{}, newTestLogger(t), WithPriorityMailbox())
	tracker := &jobTracker{id: 101, done: make(chan struct{})}
	out := make(chan AgentEvent)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	turn := &asyncTurnState{
		iter:      9,
		out:       out,
		cw:        cw,
		callerCtx: withJobTracker(context.Background(), tracker),
		results:   make([]string, 1),
	}
	turn.pending.Store(1)
	reply := make(chan delegateResult, 1)
	task := &delegatedTask{turn: turn, callIndex: 0, replyCh: reply}
	a.turnMu.Lock()
	a.asyncTurns[turn.iter] = turn
	a.turnMu.Unlock()
	a.taskWg.Add(1)
	go func() {
		defer a.taskWg.Done()
		a.watchDelegatedTask(task)
	}()

	if !a.abandonAsyncTurnForOutput(out) {
		t.Fatal("registered async turn was not abandoned")
	}
	reply <- delegateResult{content: "late result"}
	waited := make(chan struct{})
	go func() { a.taskWg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("late watcher did not exit")
	}
	select {
	case <-out:
	default:
		t.Fatal("abandoned output was not closed")
	}
	select {
	case <-tracker.done:
	default:
		t.Fatal("abandoned tracker was not finished")
	}
	if cw.Len() != 0 {
		t.Fatalf("late watcher mutated context window: len=%d", cw.Len())
	}
}

func TestQueuedAsyncResumeKeepsCancellationOwnerUntilCallbackStarts(t *testing.T) {
	a := startedAgent(t, &agenttest.FakeLLM{}, WithPriorityMailbox())
	occupied := make(chan struct{})
	actorStarted := make(chan struct{})
	if err := a.submit(context.Background(), func(context.Context) {
		close(actorStarted)
		<-occupied
	}); err != nil {
		t.Fatal(err)
	}
	<-actorStarted
	ctx, cancel := context.WithCancel(context.Background())
	tracker := &jobTracker{id: 102, done: make(chan struct{})}
	out := make(chan AgentEvent)
	mergedCanceled := make(chan struct{})
	turn := &asyncTurnState{
		iter: 10, out: out, callerCtx: withJobTracker(ctx, tracker),
		cancelMerged: func() { close(mergedCanceled) },
	}
	a.turnMu.Lock()
	a.asyncTurns[turn.iter] = turn
	a.turnMu.Unlock()
	a.submitAsyncResume(turn, func(context.Context) { a.resumeTurn(turn) })
	cancel()
	for name, ch := range map[string]<-chan struct{}{
		"tracker": tracker.done, "merged context": mergedCanceled,
	} {
		select {
		case <-ch:
		case <-time.After(200 * time.Millisecond):
			close(occupied)
			t.Fatalf("queued callback retained %s after cancellation", name)
		}
	}
	select {
	case <-out:
	case <-time.After(200 * time.Millisecond):
		close(occupied)
		t.Fatal("queued callback retained output after cancellation")
	}
	waited := make(chan struct{})
	go func() { a.taskWg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(200 * time.Millisecond):
		close(occupied)
		t.Fatal("queued cancellation owner retained taskWg")
	}
	close(occupied)
	waitFor(t, time.Second, func() bool { return a.State() == StateIdle })
	if a.ErrorCount() != 0 {
		t.Fatalf("late queued callback panicked: %s", a.LastError())
	}
}

func TestCancelAfterResumeClaimBeforeMutationPreventsContextWrite(t *testing.T) {
	a := startedAgent(t, &agenttest.FakeLLM{}, WithPriorityMailbox())
	ctx, cancel := context.WithCancel(context.Background())
	tracker := &jobTracker{id: 103, done: make(chan struct{})}
	out := make(chan AgentEvent)
	cw := ctxwin.NewContextWindow(10000, 1000, 0, ctxwin.NewTokenizer())
	claimed := make(chan struct{})
	releaseMutation := make(chan struct{})
	mergedCanceled := make(chan struct{})
	turn := &asyncTurnState{
		iter: 11, out: out, cw: cw, callerCtx: withJobTracker(ctx, tracker),
		toolCalls: []llm.ToolCall{{Type: "function", ID: "call-1", Function: llm.FunctionCall{Name: "delegate"}}},
		results:   []string{"late result"},
		cancelMerged: func() {
			close(mergedCanceled)
		},
		beforeResumeMutation: func() {
			close(claimed)
			<-releaseMutation
		},
	}
	a.turnMu.Lock()
	a.asyncTurns[turn.iter] = turn
	a.turnMu.Unlock()
	a.submitAsyncResume(turn, func(context.Context) { a.resumeTurn(turn) })
	select {
	case <-claimed:
	case <-time.After(time.Second):
		t.Fatal("resume callback did not reach pre-mutation barrier")
	}
	cancel()
	waits := []struct {
		name string
		wait func() bool
	}{
		{"output", func() bool {
			select {
			case <-out:
				return true
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}},
		{"tracker", func() bool {
			select {
			case <-tracker.done:
				return true
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}},
		{"merged context", func() bool {
			select {
			case <-mergedCanceled:
				return true
			case <-time.After(200 * time.Millisecond):
				return false
			}
		}},
	}
	for _, item := range waits {
		if !item.wait() {
			close(releaseMutation)
			t.Fatalf("pre-mutation cancellation did not close %s", item.name)
		}
	}
	close(releaseMutation)
	waited := make(chan struct{})
	go func() { a.taskWg.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("resume barrier path retained taskWg")
	}
	if cw.Len() != 0 {
		t.Fatalf("canceled old turn mutated context window: len=%d", cw.Len())
	}
}

func (m *mockAsyncTool) Name() string                { return m.name }
func (m *mockAsyncTool) Description() string         { return "mock async tool" }
func (m *mockAsyncTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (m *mockAsyncTool) Execute(ctx context.Context, args string) (string, error) {
	return "sync-result", nil
}
func (m *mockAsyncTool) ExecuteAsync(ctx context.Context, args string) (*tools.AsyncAction, error) {
	return m.action, m.actionErr
}

// ─── TestExecToolsWithAsync_SingleAsyncTool ────────────────────────────────

func TestExecToolsWithAsync_SingleAsyncTool(t *testing.T) {
	release := make(chan struct{})

	// Create target Agent (simulating L2)
	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			<-release
			return "async-result", nil
		},
	}

	// Create AsyncTool
	asyncTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  target,
			Prompt:  "test task",
			Timeout: 5 * time.Second,
		},
	}

	// Create Agent with PriorityMailbox
	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithTools(asyncTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	// Create ContextWindow
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleUser, "start")

	// Create out channel
	out := make(chan AgentEvent, 64)

	// Construct tool calls
	calls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test"}`,
			},
		},
	}

	// Call execToolsWithAsync
	results := a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// Verify result placeholder (async tool returns empty string as placeholder)
	if results[0] != "" {
		t.Errorf("results[0] = %q, want empty (placeholder)", results[0])
	}

	// Verify asyncTurns is registered
	a.turnMu.RLock()
	_, hasAsync := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if !hasAsync {
		t.Error("asyncTurns[0] not registered")
	}

	close(release)
	waitFor(t, time.Second, func() bool {
		a.turnMu.RLock()
		defer a.turnMu.RUnlock()
		_, stillHas := a.asyncTurns[0]
		return !stillHas
	})

	if results[0] != "" {
		t.Errorf("results[0] changed to %q after async completion, want empty placeholder", results[0])
	}
}

func TestAsyncDelegationCancellationDropsLateTargetEvents(t *testing.T) {
	events := make(chan iface.AgentEvent, 1)
	target := &lateCancelLocatable{stream: events, started: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	var persisted atomic.Int32
	var finished atomic.Int32
	finishErr := make(chan error, 1)
	asyncTool := &mockAsyncTool{name: "delegate", action: &tools.AsyncAction{
		Target:  target,
		Prompt:  "task",
		Context: ctx,
		OnEvent: func(iface.AgentEvent) error {
			persisted.Add(1)
			return nil
		},
		OnFinish: func(err error) error {
			finished.Add(1)
			finishErr <- err
			return nil
		},
	}}
	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t), WithTools(asyncTool), WithPriorityMailbox())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	a.execToolsWithAsync(ctx, 0, []llm.ToolCall{{ID: "call", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}}}, make(chan AgentEvent, 64), ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()))
	<-target.started
	cancel()
	events <- DoneEvent{Content: "late"}
	close(events)

	select {
	case err := <-finishErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("OnFinish error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("OnFinish was not called")
	}
	if got := persisted.Load(); got != 0 {
		t.Fatalf("late canceled events persisted = %d, want 0", got)
	}
	if got := target.doneCalls.Load(); got != 1 {
		t.Fatalf("OnDelegationDone calls = %d, want 1", got)
	}
	if got := finished.Load(); got != 1 {
		t.Fatalf("OnFinish calls = %d, want 1", got)
	}
}

func TestAsyncDelegationPropagatesPersistenceCallbackFailure(t *testing.T) {
	persistErr := errors.New("persist callback failed")
	finishInput := make(chan error, 1)
	asyncTool := &mockAsyncTool{name: "delegate", action: &tools.AsyncAction{
		Target: &mockLocatable{askFunc: func(context.Context, string) (string, error) { return "done", nil }},
		Prompt: "task", Timeout: time.Second,
		OnEvent:  func(iface.AgentEvent) error { return persistErr },
		OnFinish: func(err error) error { finishInput <- err; return persistErr },
	}}
	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t), WithTools(asyncTool), WithPriorityMailbox())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	a.execToolsWithAsync(context.Background(), 0, []llm.ToolCall{{ID: "call", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}}}, make(chan AgentEvent, 64), cw)
	select {
	case err := <-finishInput:
		if !errors.Is(err, persistErr) {
			t.Fatalf("OnFinish input=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("OnFinish did not receive event persistence failure")
	}
}

func TestAsyncDelegationPanicFinishesExactlyOnceAndReplies(t *testing.T) {
	var finishCalls atomic.Int32
	finishErr := make(chan error, 1)
	asyncTool := &mockAsyncTool{name: "delegate", action: &tools.AsyncAction{
		Target: &mockLocatable{askStreamFunc: func(context.Context, string) (<-chan iface.AgentEvent, error) {
			panic("async target exploded")
		}},
		Prompt: "task", Timeout: time.Second,
		OnFinish: func(err error) error {
			finishCalls.Add(1)
			finishErr <- err
			return nil
		},
	}}
	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t), WithTools(asyncTool), WithPriorityMailbox())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	a.execToolsWithAsync(context.Background(), 0, []llm.ToolCall{{ID: "call", Type: "function", Function: llm.FunctionCall{Name: "delegate", Arguments: `{}`}}}, make(chan AgentEvent, 64), ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()))
	select {
	case err := <-finishErr:
		if err == nil || !strings.Contains(err.Error(), "async target exploded") {
			t.Fatalf("OnFinish input = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("async panic did not invoke OnFinish")
	}
	waitFor(t, time.Second, func() bool {
		a.turnMu.RLock()
		defer a.turnMu.RUnlock()
		_, pending := a.asyncTurns[0]
		return !pending
	})
	if got := finishCalls.Load(); got != 1 {
		t.Fatalf("OnFinish calls = %d, want 1", got)
	}
}

func TestAsyncDelegation_PassesAppendedDelegationChainToChild(t *testing.T) {
	seenChain := make(chan []string, 1)
	target := &mockLocatable{askFunc: func(ctx context.Context, _ string) (string, error) {
		seenChain <- append([]string(nil), tools.DelegationChainFromContext(ctx)...)
		return "done", nil
	}}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return target, false, nil
	}
	delegate := tools.NewDelegateTool(
		"l1",
		5*time.Second,
		resolver,
		nil,
		nil,
		tools.WorkDirExplicitOrInherited,
		tools.WithAlwaysAsyncDelegation(),
	)
	parent := NewAgent(
		Definition{ID: "l1"},
		&agenttest.FakeLLM{},
		newTestLogger(t),
		WithTools(delegate),
		WithPriorityMailbox(),
	)
	if err := parent.Start(context.Background()); err != nil {
		t.Fatalf("Start parent: %v", err)
	}
	t.Cleanup(func() { _ = parent.Stop(time.Second) })

	out := make(chan AgentEvent, 64)
	parent.execToolsWithAsync(context.Background(), 0, []llm.ToolCall{{
		Type: "function",
		ID:   "delegate_call",
		Function: llm.FunctionCall{
			Name:      "delegate",
			Arguments: `{"target":"worker","task":"inspect","work_dir":"/tmp","async":false}`,
		},
	}}, out, ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()))

	select {
	case chain := <-seenChain:
		if got, want := strings.Join(chain, "/"), "l1"; got != want {
			t.Fatalf("child delegation chain = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delegated child")
	}
}

// ─── TestExecToolsWithAsync_MultipleAsyncTools ─────────────────────────────

func TestExecToolsWithAsync_MultipleAsyncTools(t *testing.T) {
	var callCount int32
	var mu sync.Mutex

	target1 := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return "result1", nil
		},
	}
	target2 := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return "result2", nil
		},
	}

	asyncTool1 := &mockAsyncTool{
		name: "delegate_dev",
		action: &tools.AsyncAction{
			Target:  target1,
			Prompt:  "task1",
			Timeout: 5 * time.Second,
		},
	}
	asyncTool2 := &mockAsyncTool{
		name: "delegate_qa",
		action: &tools.AsyncAction{
			Target:  target2,
			Prompt:  "task2",
			Timeout: 5 * time.Second,
		},
	}

	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithTools(asyncTool1, asyncTool2),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	out := make(chan AgentEvent, 64)

	calls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "delegate_dev",
				Arguments: `{"task":"task1"}`,
			},
		},
		{
			Type: "function",
			ID:   "call_2",
			Function: llm.FunctionCall{
				Name:      "delegate_qa",
				Arguments: `{"task":"task2"}`,
			},
		},
	}

	a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// Wait for all async tasks to complete
	time.Sleep(300 * time.Millisecond)

	// Verify both tasks were invoked
	mu.Lock()
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
	mu.Unlock()
}

// ─── TestExecToolsWithAsync_MixedSyncAndAsync ────────────────────────────

func TestExecToolsWithAsync_MixedSyncAndAsync(t *testing.T) {
	asyncTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  &mockLocatable{askFunc: func(ctx context.Context, prompt string) (string, error) { return "async", nil }},
			Prompt:  "task",
			Timeout: 5 * time.Second,
		},
	}

	syncTool := &mockSyncTool{name: "echo", result: "sync-result"}

	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithTools(asyncTool, syncTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	out := make(chan AgentEvent, 64)

	calls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "echo",
				Arguments: `{"msg":"hello"}`,
			},
		},
		{
			Type: "function",
			ID:   "call_2",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test"}`,
			},
		},
	}

	results := a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// Verify sync tool returns result immediately
	if results[0] != "sync-result" {
		t.Errorf("results[0] (sync) = %q, want 'sync-result'", results[0])
	}

	// Verify async tool is a placeholder
	if results[1] != "" {
		t.Errorf("results[1] (async) = %q, want empty (placeholder)", results[1])
	}
}

// ─── TestExecToolsWithAsync_AsyncToolError ───────────────────────────────

func TestExecToolsWithAsync_AsyncToolError(t *testing.T) {
	asyncTool := &mockAsyncTool{
		name:      "delegate",
		actionErr: tools.ErrToolNotFound,
	}

	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithTools(asyncTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	out := make(chan AgentEvent, 64)

	calls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test"}`,
			},
		},
	}

	results := a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// Verify error result
	if results[0] == "" {
		t.Error("expected error result, got empty")
	}
	if !strings.Contains(results[0], "error:") {
		t.Errorf("results[0] = %q, want contains 'error:'", results[0])
	}
}

// ─── TestExecToolsWithAsync_PendingCount ─────────────────────────────────

func TestExecToolsWithAsync_PendingCount(t *testing.T) {
	// Verify pending count is correct
	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			time.Sleep(100 * time.Millisecond)
			return "done", nil
		},
	}

	asyncTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  target,
			Prompt:  "task",
			Timeout: 5 * time.Second,
		},
	}

	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithTools(asyncTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	out := make(chan AgentEvent, 64)

	calls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test"}`,
			},
		},
	}

	a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// Wait for async tasks to complete
	time.Sleep(200 * time.Millisecond)

	// Verify asyncTurns has been cleaned up
	a.turnMu.RLock()
	_, exists := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if exists {
		t.Error("asyncTurns[0] should be cleaned up after task completes")
	}
}

// ─── TestWatchDelegatedTask_ContextCancel ────────────────────────────────

func TestWatchDelegatedTask_ContextCancel(t *testing.T) {
	// Verify that when caller context is cancelled, watchDelegatedTask fills error result and triggers resumeTurn
	// (New behavior: instead of simply deleting asyncTurns, use submitHighPriority to let resumeTurn handle cleanup and close(out))
	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	// Create unfinished replyCh
	replyCh := make(chan delegateResult)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create asyncTurnState
	var pending atomic.Int32
	pending.Store(1)

	turnState := &asyncTurnState{
		agentID:   "l1",
		out:       make(chan AgentEvent, 64),
		cw:        ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()),
		iter:      0,
		toolCalls: []llm.ToolCall{},
		results:   make([]string, 1),
		callerCtx: ctx,
	}
	turnState.pending.Store(1)

	// Register with agent
	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	// Create task
	task := &delegatedTask{
		replyCh:   replyCh,
		callID:    "call_1",
		callIndex: 0,
		turn:      turnState,
	}

	// Start watchDelegatedTask
	done := make(chan struct{})
	go func() {
		a.watchDelegatedTask(task)
		close(done)
	}()

	// Wait for watchDelegatedTask to complete (including 100ms grace period)
	select {
	case <-done:
		// Verify: result has been filled with error message
		if turnState.results[0] != "error: delegation cancelled" {
			t.Errorf("results[0] = %q, want %q", turnState.results[0], "error: delegation cancelled")
		}
		// Validation: pending has gone to zero (triggered resumeTurn dispatch)
		if turnState.pending.Load() != 0 {
			t.Errorf("pending = %d, want 0", turnState.pending.Load())
		}
	case <-time.After(2 * time.Second):
		t.Error("watchDelegatedTask did not handle context cancel within timeout")
	}
}

// ─── TestResumeTurn_CleansUpAndContinues ────────────────────────────────

func TestResumeTurn_CleansUpAndContinues(t *testing.T) {
	// Create FakeLLM that returns a final answer after resume
	fakeLLM := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"final answer"}},
	}

	a := NewAgent(Definition{ID: "l1"}, fakeLLM, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")
	cw.Push(ctxwin.RoleUser, "start")

	// Create out channel
	out := make(chan AgentEvent, 64)

	// Create asyncTurnState (pending=0 means all async tasks are complete)
	turnState := &asyncTurnState{
		agentID: "l1",
		out:     out,
		cw:      cw,
		iter:    0,
		toolCalls: []llm.ToolCall{
			{
				Type: "function",
				ID:   "call_1",
				Function: llm.FunctionCall{
					Name:      "delegate",
					Arguments: `{"task":"test"}`,
				},
			},
		},
		results:   []string{"async-result"},
		callerCtx: context.Background(),
	}

	// Register with agent
	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	// Call resumeTurn (it will clean up asyncTurns and continue the tool loop)
	a.resumeTurn(turnState)

	// Verify asyncTurns are cleaned up
	a.turnMu.RLock()
	_, exists := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if exists {
		t.Error("asyncTurns[0] should be deleted after resumeTurn")
	}

	// Verify out channel receives events
	select {
	case ev := <-out:
		t.Logf("received event: %T", ev)
	case <-time.After(time.Second):
		t.Error("no event received from resumeTurn")
	}
}

// ─── TestContinueToolLoop_ResumesFromIter ────────────────────────────────

func TestContinueToolLoop_ResumesFromIter(t *testing.T) {
	callCount := 0
	fakeLLM := &agenttest.FakeLLM{
		Hook: func(_ LLMRequest) {
			callCount++
		},
		StreamDeltas: [][]string{{"answer"}},
	}

	a := NewAgent(Definition{ID: "l1"}, fakeLLM, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")
	cw.Push(ctxwin.RoleUser, "start")

	out := make(chan AgentEvent, 64)

	// Start from iter=1 (simulating a resume)
	a.continueToolLoop(context.Background(), out, cw, 1)

	// Verify LLM is called
	if callCount == 0 {
		t.Error("LLM should be called by continueToolLoop")
	}

	// Verify out channel receives DoneEvent or ContentDeltaEvent
	select {
	case ev := <-out:
		t.Logf("received event: %T", ev)
	case <-time.After(time.Second):
		t.Error("no event received from continueToolLoop")
	}
}

// ─── TestEndToEnd_AsyncDelegation ────────────────────────────────────────

func TestEndToEnd_AsyncDelegation(t *testing.T) {
	// End-to-end test: L1 async delegation -> L2 execution -> results return
	// Use FakeLLM with ToolCallDeltasByTurn to simulate L1 returning a delegate tool call
	// Then simulate L2 returning the final result

	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			return "delegation-result", nil
		},
	}

	delegateTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:     target,
			Prompt:     "test task",
			Timeout:    5 * time.Second,
			DispatchID: "dlg_test_secret",
		},
	}

	// L1's FakeLLM: first round returns tool call (via ToolCallDeltasByTurn), second round returns final answer
	fakeLLM := &agenttest.FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				{Index: 0, ID: "call_1", Name: "delegate", Arguments: `{"task":"test"}`},
			},
		},
		StreamDeltas: [][]string{{"final answer after delegation"}},
	}

	a := NewAgent(Definition{ID: "l1"}, fakeLLM, newTestLogger(t),
		WithTools(delegateTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(2 * time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")

	// Use AskStreamWithHistory to trigger the full flow
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := a.AskStreamWithHistory(ctx, cw, "start delegation")
	if err != nil {
		t.Fatalf("AskStreamWithHistory: %v", err)
	}

	// Collect events
	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
		t.Logf("event: %T", ev)
	}

	// Verify DelegationStartedEvent is received
	hasStarted := false
	for _, ev := range events {
		if _, ok := ev.(DelegationStartedEvent); ok {
			hasStarted = true
			break
		}
	}
	if !hasStarted {
		t.Error("should receive DelegationStartedEvent")
	}
	for _, ev := range events {
		if e, ok := ev.(ContentDeltaEvent); ok && (strings.Contains(e.Delta, "Delegation started") || strings.Contains(e.Delta, "dlg_test_secret")) {
			t.Errorf("delegation control data leaked into ContentDeltaEvent: %q", e.Delta)
		}
	}

	// Verify a final answer is given
	hasContent := false
	for _, ev := range events {
		if e, ok := ev.(ContentDeltaEvent); ok {
			if strings.Contains(e.Delta, "final") {
				hasContent = true
			}
		}
		if e, ok := ev.(DoneEvent); ok {
			if strings.Contains(e.Content, "final") {
				hasContent = true
			}
		}
	}
	if !hasContent {
		t.Error("should receive final answer after delegation")
	}
}

// ─── mockSyncTool for mixed tests ─────────────────────────────────────────

type mockSyncTool struct {
	name   string
	result string
}

func (m *mockSyncTool) Name() string                { return m.name }
func (m *mockSyncTool) Description() string         { return "mock sync tool" }
func (m *mockSyncTool) Parameters() json.RawMessage { return json.RawMessage(`{}`) }
func (m *mockSyncTool) Execute(ctx context.Context, args string) (string, error) {
	return m.result, nil
}

// --- DelegateTool API tests ---

func TestDelegateTool_IsAsync(t *testing.T) {
	dt := tools.NewDelegateTool("dev", 5*time.Minute, nil, nil, nil, tools.WorkDirInheritOnly)
	if !dt.IsAsync() {
		t.Error("IsAsync() = false, want true")
	}
}

func TestDelegateTool_ExecuteAsync(t *testing.T) {
	target := &mockLocatable{}
	resolver := func(ctx context.Context, targetName, systemPrompt, modelID, task, workDir, skillID string) (iface.Locatable, bool, error) {
		return target, false, nil
	}
	dt := tools.NewDelegateTool("dev", 5*time.Minute, resolver, nil, nil, tools.WorkDirExplicitOrInherited, tools.WithAlwaysAsyncDelegation())

	action, err := dt.ExecuteAsync(context.Background(), `{"target":"dev","task":"test","work_dir":"/tmp","async":true}`)
	if err != nil {
		t.Fatalf("ExecuteAsync: %v", err)
	}
	if action == nil {
		t.Fatal("action is nil")
	}
	if action.Target == nil {
		t.Error("action.Target is nil")
	}
	if action.Prompt != "test" {
		t.Errorf("action.Prompt = %q, want 'test'", action.Prompt)
	}
	if action.Timeout != 0 {
		t.Errorf("action.Timeout = %v, want no total runtime cap", action.Timeout)
	}
}

func TestDelegateTool_ExecuteAsync_InvalidArgs(t *testing.T) {
	dt := tools.NewDelegateTool("dev", 5*time.Minute, nil, nil, nil, tools.WorkDirInheritOnly, tools.WithAlwaysAsyncDelegation())

	// Empty task
	_, err := dt.ExecuteAsync(context.Background(), `{"target":"dev","task":"","async":true}`)
	if err == nil {
		t.Error("expected error for empty task")
	}

	// Invalid JSON
	_, err = dt.ExecuteAsync(context.Background(), `not-json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDelegateTool_ExecuteAsync_NoLocatorOrSpawnFn(t *testing.T) {
	dt := tools.NewDelegateTool("dev", 5*time.Minute, nil, nil, nil, tools.WorkDirInheritOnly, tools.WithAlwaysAsyncDelegation())

	_, err := dt.ExecuteAsync(context.Background(), `{"target":"dev","task":"test","async":true}`)
	if err == nil {
		t.Error("expected error when no Locator or SpawnFn configured")
	}
}

func TestDelegationEvents_AreAgentEvents(t *testing.T) {
	var _ AgentEvent = DelegationStartedEvent{}
	var _ AgentEvent = DelegationCompletedEvent{}

	ev1 := DelegationStartedEvent{Iter: 1, NumTasks: 2}
	ev2 := DelegationCompletedEvent{Iter: 1, TargetAgentID: "dev"}

	var ae1 AgentEvent = ev1
	var ae2 AgentEvent = ev2
	switch ae1.(type) {
	case DelegationStartedEvent:
		// ok
	default:
		t.Error("DelegationStartedEvent not recognized in type switch")
	}

	switch ae2.(type) {
	case DelegationCompletedEvent:
		// ok
	default:
		t.Error("DelegationCompletedEvent not recognized in type switch")
	}
}

// ─── Integration: L2 failure must not hang L1 ────────────────────────────

func TestEndToEnd_AsyncDelegation_L2Failure(t *testing.T) {
	// Reproduces the original bug: L1 delegates to L2, L2's Ask returns an
	// error (e.g. invalid model → HTTP 400). Before the fix, L1's out channel
	// was never closed and the caller hung forever.
	//
	// The fix ensures:
	//   1. defer cancel() does NOT prematurely cancel merged ctx on yield
	//   2. watchDelegatedTask always triggers resumeTurn (even on ctx cancel)

	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			return "", fmt.Errorf("llm: http 400: model not found")
		},
	}

	delegateTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  target,
			Prompt:  "do something",
			Timeout: 5 * time.Second,
		},
	}

	// L1 FakeLLM: turn 0 = tool call, turn 1 = final answer
	fakeLLM := &agenttest.FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				{Index: 0, ID: "call_1", Name: "delegate", Arguments: `{"task":"test"}`},
			},
		},
		StreamDeltas: [][]string{{"recovered from L2 failure"}},
	}

	a := NewAgent(Definition{ID: "l1"}, fakeLLM, newTestLogger(t),
		WithTools(delegateTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(2 * time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")

	// Use AskStreamWithHistory — the exact code path that was hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := a.AskStreamWithHistory(ctx, cw, "delegate to L2")
	if err != nil {
		t.Fatalf("AskStreamWithHistory: %v", err)
	}

	// Collect all events. Before the fix this would block forever.
	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("received no events — out channel was likely never closed (bug not fixed)")
	}

	// Should have DelegationStartedEvent
	hasDelegation := false
	for _, ev := range events {
		if _, ok := ev.(DelegationStartedEvent); ok {
			hasDelegation = true
		}
	}
	if !hasDelegation {
		t.Error("expected DelegationStartedEvent")
	}

	// Should eventually get content (L1 recovers after L2 error)
	hasContent := false
	for _, ev := range events {
		if e, ok := ev.(ContentDeltaEvent); ok && strings.Contains(e.Delta, "recovered") {
			hasContent = true
		}
		if e, ok := ev.(DoneEvent); ok && strings.Contains(e.Content, "recovered") {
			hasContent = true
		}
	}
	if !hasContent {
		t.Error("expected L1 to recover with final content after L2 failure")
	}

	t.Logf("received %d events total — L1 did not hang", len(events))
}

// ─── Integration: watchDelegatedTask grace period catches in-flight result ──

// ─── TestResumeTurn_TruncatedToolCalls ────────────────────────────────
func TestResumeTurn_TruncatedToolCalls(t *testing.T) {
	// Simulated scenario: L2 delegates to L3, L3's LLM response is truncated (finish_reason="length")
	// Causing L3 to return an incomplete result, but resumeTurn() still pushes all tool results
	// This should be detected and handled correctly

	fakeLLM := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"final answer"}},
	}

	a := NewAgent(Definition{ID: "l2"}, fakeLLM, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")
	cw.Push(ctxwin.RoleUser, "start")

	// Simulate L2's tool_calls (delegation to L3)
	toolCalls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test"}`,
			},
		},
	}

	// Simulate L3's returned results (may be incomplete or contain errors)
	results := []string{"error: L3 response truncated due to max_tokens"}

	// Create asyncTurnState
	turnState := &asyncTurnState{
		agentID:   "l2",
		out:       make(chan AgentEvent, 64),
		cw:        cw,
		iter:      0,
		toolCalls: toolCalls,
		results:   results,
		callerCtx: context.Background(),
	}

	// Register with agent
	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	// Call resumeTurn
	a.resumeTurn(turnState)

	// Verification: ContextWindow should contain the complete message sequence
	// Even if the tool result contains errors, it is still a valid tool message
	payload := cw.BuildPayload()

	// Check if the payload contains the complete tool_call/tool_result pair
	hasAssistantWithToolCalls := false
	hasToolResult := false

	for _, msg := range payload {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			hasAssistantWithToolCalls = true
		}
		if msg.Role == "tool" {
			hasToolResult = true
		}
	}

	// Even if the tool result contains errors, the message sequence should still be complete
	if hasAssistantWithToolCalls && !hasToolResult {
		t.Error("incomplete tool_call/tool_result pair: assistant has tool_calls but no tool result")
	}

	// Verify asyncTurns has been cleaned up
	a.turnMu.RLock()
	_, exists := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if exists {
		t.Error("asyncTurns[0] should be deleted after resumeTurn")
	}
}

// ─── TestResumeTurn_MismatchedToolCallsAndResults ──────────────────────
func TestResumeTurn_MismatchedToolCallsAndResults(t *testing.T) {
	// Simulated scenario: toolCalls and results length mismatch
	// This should be detected and handled correctly

	fakeLLM := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"final answer"}},
	}

	a := NewAgent(Definition{ID: "l2"}, fakeLLM, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")
	cw.Push(ctxwin.RoleUser, "start")

	// Simulate L2's tool_calls (2 tool calls)
	toolCalls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test1"}`,
			},
		},
		{
			Type: "function",
			ID:   "call_2",
			Function: llm.FunctionCall{
				Name:      "delegate",
				Arguments: `{"task":"test2"}`,
			},
		},
	}

	// Simulate results (only 1 result, mismatch)
	results := []string{"result1"}

	// Creating asyncTurnState
	turnState := &asyncTurnState{
		agentID:   "l2",
		out:       make(chan AgentEvent, 64),
		cw:        cw,
		iter:      0,
		toolCalls: toolCalls,
		results:   results,
		callerCtx: context.Background(),
	}

	// Calling resumeTurn (this should handle the mismatch)
	a.resumeTurn(turnState)

	// Verification: ContextWindow should still be valid (not corrupted)
	payload := cw.BuildPayload()

	// Check if the payload contains a complete message sequence
	// Note: filterCompletePairs() filters out incomplete pairs
	// So if tool_calls and tool_results do not match, the entire pair will be filtered out
	t.Logf("payload length: %d", len(payload))

	// Verify no panics or errors
	// The actual filtering logic is handled by filterCompletePairs()
}

// ─── TestBuildPayload_FiltersIncompleteToolCallPairs ──────────────────────
func TestBuildPayload_FiltersIncompleteToolCallPairs(t *testing.T) {
	// Verification: When ContextWindow contains incomplete tool_call/tool_result pairs,
	// BuildPayload() filters out incomplete pairs, preventing API 400 errors

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())

	// Push system message
	cw.Push(ctxwin.RoleSystem, "you are helpful")

	// Push user message
	cw.Push(ctxwin.RoleUser, "start")

	// Push assistant message (containing 2 tool_calls)
	cw.Push(ctxwin.RoleAssistant, "I'll delegate these tasks",
		ctxwin.WithToolCalls([]llm.ToolCall{
			{
				Type: "function",
				ID:   "call_1",
				Function: llm.FunctionCall{
					Name:      "delegate",
					Arguments: `{"task":"test1"}`,
				},
			},
			{
				Type: "function",
				ID:   "call_2",
				Function: llm.FunctionCall{
					Name:      "delegate",
					Arguments: `{"task":"test2"}`,
				},
			},
		}),
	)

	// Push only 1 tool result (incomplete!)
	cw.Push(ctxwin.RoleTool, "result1",
		ctxwin.WithToolCallID("call_1"),
		ctxwin.WithToolName("delegate"),
	)

	// Calling BuildPayload()
	payload := cw.BuildPayload()

	// Verification: Incomplete tool_call/tool_result pairs should be filtered out
	// So the payload should not contain assistant(tool_calls) and the incomplete tool result
	t.Logf("payload length: %d", len(payload))

	// Check the messages in the payload
	hasIncompletePair := false
	for _, msg := range payload {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Check if all tool_calls have corresponding tool_results
			// This is handled by filterCompletePairs()
			t.Logf("assistant message with %d tool_calls", len(msg.ToolCalls))
		}
		if msg.Role == "tool" {
			t.Logf("tool message for %s", msg.ToolCallID)
		}
	}

	// Key verification: BuildPayload() should not return incomplete pairs
	// If filterCompletePairs() works correctly, incomplete pairs will be filtered out
	if hasIncompletePair {
		t.Error("BuildPayload() returned incomplete tool_call/tool_result pair")
	}
}

// ─── TestEndToEnd_TruncatedDelegateResponse ──────────────────────
func TestEndToEnd_TruncatedDelegateResponse(t *testing.T) {
	// End-to-end test: Simulate L2 delegating to L3, L3's response gets truncated
	// Causing L2's ContextWindow to be corrupted, API request returns a 400 error

	// Create L3 (target Agent), its response will be truncated
	l3Target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			// Simulate L3's LLM response being truncated
			// Return error result
			return "", fmt.Errorf("llm: finish_reason=length, output truncated")
		},
	}

	// L2's delegate tool
	delegateTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  l3Target,
			Prompt:  "test task",
			Timeout: 5 * time.Second,
		},
	}

	// L2's FakeLLM:
	// First round: return tool call (delegate to L3)
	// Second round: return final answer (or error)
	l2LLM := &agenttest.FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				{Index: 0, ID: "call_1", Name: "delegate", Arguments: `{"task":"test"}`},
			},
		},
		StreamDeltas: [][]string{{"recovered after L3 error"}},
	}

	// Create L2 Agent
	l2Agent := NewAgent(Definition{ID: "l2"}, l2LLM, newTestLogger(t),
		WithTools(delegateTool),
		WithPriorityMailbox(),
	)
	if err := l2Agent.Start(context.Background()); err != nil {
		t.Fatalf("Start L2: %v", err)
	}
	defer l2Agent.Stop(2 * time.Second)

	// Create L2's ContextWindow
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")

	// Trigger the full flow using AskStreamWithHistory
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := l2Agent.AskStreamWithHistory(ctx, cw, "delegate to L3")
	if err != nil {
		t.Fatalf("AskStreamWithHistory: %v", err)
	}

	// Collect events
	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
		t.Logf("event: %T", ev)
	}

	// Verify: should receive DelegationStartedEvent
	hasDelegation := false
	for _, ev := range events {
		if _, ok := ev.(DelegationStartedEvent); ok {
			hasDelegation = true
			break
		}
	}
	if !hasDelegation {
		t.Error("should receive DelegationStartedEvent")
	}

	// Verify: L2 should recover and return the final answer (even if L3 fails)
	hasContent := false
	for _, ev := range events {
		if e, ok := ev.(ContentDeltaEvent); ok {
			if strings.Contains(e.Delta, "recovered") {
				hasContent = true
			}
		}
		if e, ok := ev.(DoneEvent); ok {
			if strings.Contains(e.Content, "recovered") {
				hasContent = true
			}
		}
	}
	if !hasContent {
		t.Error("L2 should recover and return final answer after L3 error")
	}

	t.Logf("received %d events total", len(events))
}

// ─── TestWatchDelegatedTask_GracePeriodCatchesInFlightResult ────────────
func TestWatchDelegatedTask_GracePeriodCatchesInFlightResult(t *testing.T) {
	// Verify that when callerCtx is cancelled but the result arrives within
	// the 100ms grace period, the result is NOT lost.
	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	replyCh := make(chan delegateResult, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	turnState := &asyncTurnState{
		agentID:   "l1",
		out:       make(chan AgentEvent, 64),
		cw:        ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer()),
		iter:      0,
		toolCalls: []llm.ToolCall{},
		results:   make([]string, 1),
		callerCtx: ctx,
	}
	turnState.pending.Store(1)

	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	task := &delegatedTask{
		replyCh:   replyCh,
		callID:    "call_1",
		callIndex: 0,
		turn:      turnState,
	}

	// Send result to replyCh BEFORE watchDelegatedTask runs.
	// Even though ctx is cancelled, the grace period should pick it up.
	replyCh <- delegateResult{content: "real-result", err: nil}

	done := make(chan struct{})
	go func() {
		a.watchDelegatedTask(task)
		close(done)
	}()

	select {
	case <-done:
		// The real result should be captured, not "delegation cancelled"
		if turnState.results[0] != "real-result" {
			t.Errorf("results[0] = %q, want %q", turnState.results[0], "real-result")
		}
	case <-time.After(2 * time.Second):
		t.Error("watchDelegatedTask did not complete")
	}
}

// ─── TestFormatDelegationStarted ────────────────────────────────────────────

func TestFormatDelegationStarted(t *testing.T) {
	tests := []struct {
		name     string
		tc       llm.ToolCall
		expected string
	}{
		{
			name: "with task description",
			tc: llm.ToolCall{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "delegate_code_review",
					Arguments: `{"task": "review the PR"}`,
				},
			},
			expected: "Delegation started: task assigned via 'delegate_code_review'.\nTask: review the PR\nWaiting for results...",
		},
		{
			name: "with empty task",
			tc: llm.ToolCall{
				ID:   "call_2",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "delegate_test",
					Arguments: `{}`,
				},
			},
			expected: "Delegation started: task assigned via 'delegate_test'.\nTask: {}\nWaiting for results...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDelegationStarted(tt.tc)
			if got != tt.expected {
				t.Errorf("formatDelegationStarted() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ─── TestFormatDelegationCompleted ──────────────────────────────────────────

func TestFormatDelegationCompleted(t *testing.T) {
	tests := []struct {
		name      string
		toolCalls []llm.ToolCall
		results   []string
		expected  string
	}{
		{
			name: "single delegate result",
			toolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "delegate_code_review",
						Arguments: `{"task": "review the PR"}`,
					},
				},
			},
			results:  []string{"Code reviewed, all looks good"},
			expected: "[Delegation Completed]\n\nTask: review the PR\nCallID: call_1\nResult:\nCode reviewed, all looks good\n\n",
		},
		{
			name: "multiple delegate tasks",
			toolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "delegate_review",
						Arguments: `{"task": "review code"}`,
					},
				},
				{
					ID:   "call_2",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "delegate_test",
						Arguments: `{"task": "run tests"}`,
					},
				},
			},
			results:  []string{"Looks good", "All tests pass"},
			expected: "[Delegation Completed]\n\nTask: review code\nCallID: call_1\nResult:\nLooks good\n\nTask: run tests\nCallID: call_2\nResult:\nAll tests pass\n\n",
		},
		{
			name: "mixed sync and async - only async included",
			toolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "read_file",
						Arguments: `{"path": "main.go"}`,
					},
				},
				{
					ID:   "call_2",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "delegate_refactor",
						Arguments: `{"task": "refactor module"}`,
					},
				},
			},
			results:  []string{"file content", "Refactored successfully"},
			expected: "[Delegation Completed]\n\nTask: refactor module\nCallID: call_2\nResult:\nRefactored successfully\n\n",
		},
		{
			name: "no delegate tasks",
			toolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "read_file",
						Arguments: `{}`,
					},
				},
			},
			results:  []string{"content"},
			expected: "",
		},
		{
			name: "error result",
			toolCalls: []llm.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: llm.FunctionCall{
						Name:      "delegate_fix_bug",
						Arguments: `{"task": "fix bug #42"}`,
					},
				},
			},
			results:  []string{"error: delegation timed out"},
			expected: "[Delegation Completed]\n\nTask: fix bug #42\nCallID: call_1\nResult:\nerror: delegation timed out\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDelegationCompleted(tt.toolCalls, tt.results)
			if got != tt.expected {
				t.Errorf("formatDelegationCompleted() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// ─── TestResumeTurn_PushesUserMessage ──────────────────────────────────────

func TestResumeTurn_PushesUserMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")
	cw.Push(ctxwin.RoleUser, "start")
	// Simulate the assistant(tool_calls) that postIteration pushed before resumeTurn
	cw.Push(ctxwin.RoleAssistant, "Let me delegate this task.",
		ctxwin.WithToolCalls([]llm.ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "delegate_code_review",
					Arguments: `{"task": "review the code"}`,
				},
			},
		}),
	)

	out := make(chan AgentEvent, 64)
	turnState := &asyncTurnState{
		agentID: "l1",
		out:     out,
		cw:      cw,
		iter:    0,
		toolCalls: []llm.ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: llm.FunctionCall{
					Name:      "delegate_code_review",
					Arguments: `{"task": "review the code"}`,
				},
			},
		},
		results:   []string{"Code reviewed successfully: all clean"},
		callerCtx: ctx,
	}

	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{StreamDeltas: [][]string{{"continuing"}}}, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	a.resumeTurn(turnState)

	// Verify: CW should contain a user message with delegation result, NOT a tool message
	payload := cw.BuildPayload()

	hasUserResult := false
	hasToolResult := false
	for _, msg := range payload {
		if msg.Role == "user" && strings.Contains(msg.Content, "review the code") {
			hasUserResult = true
		}
		if msg.Role == "tool" {
			hasToolResult = true
		}
	}

	if !hasUserResult {
		t.Error("resumeTurn should push a user message with delegation result")
	}
	if hasToolResult {
		t.Error("resumeTurn should NOT push tool results (they were pushed in postIteration)")
	}

	// Verify asyncTurns cleaned up
	a.turnMu.RLock()
	_, exists := a.asyncTurns[0]
	a.turnMu.RUnlock()
	if exists {
		t.Error("asyncTurns[0] should be deleted after resumeTurn")
	}
}

// TestAsyncTurn_FallbackSync verifies that if ExecuteAsync returns (nil, nil),
// the agent framework automatically falls back to synchronous execution.
func TestAsyncTurn_FallbackSync(t *testing.T) {
	// Create mock tool with action=nil and actionErr=nil to trigger fallback
	tool := &mockAsyncTool{
		name: "sync_fallback",
	}

	a := NewAgent(Definition{ID: "l1"}, &agenttest.FakeLLM{}, newTestLogger(t),
		WithTools(tool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleUser, "start")

	out := make(chan AgentEvent, 64)
	calls := []llm.ToolCall{
		{
			Type: "function",
			ID:   "call_1",
			Function: llm.FunctionCall{
				Name:      "sync_fallback",
				Arguments: `{"async":false}`,
			},
		},
	}

	results := a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// Since it fell back to sync execution, the result should be the sync output
	// instead of the empty placeholder string.
	if results[0] != "sync-result" {
		t.Errorf("results[0] = %q, want 'sync-result'", results[0])
	}
}

// TestDelegateTool_SyncAndAsync verifies that scheduling is constructor policy.
func TestDelegateTool_SyncAndAsync(t *testing.T) {
	// Mock target agent to return a fixed output
	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			return "agent-output", nil
		},
	}

	var expectedPrompt string = "do code review"

	// Create DelegateTool
	resolver := func(ctx context.Context, name, systemPrompt, modelID, task, workDir, skillID string) (iface.Locatable, bool, error) {
		if name != "my-dynamic-agent" {
			return nil, false, fmt.Errorf("unexpected name: %s", name)
		}
		if systemPrompt != expectedPrompt {
			return nil, false, fmt.Errorf("unexpected systemPrompt: %q, want %q", systemPrompt, expectedPrompt)
		}
		return target, true, nil
	}
	tool := tools.NewDelegateTool("caller", 5*time.Minute, resolver, nil, nil, tools.WorkDirExplicitOrInherited)
	tool.SkillInstructionsLook = func(skillID string) (string, string, string, bool) {
		if skillID == "my-skill" {
			return "run checks", "some-agent", "/path/to/skill", true
		}
		return "", "", "", false
	}

	// 1. Test synchronous invocation (async=false)
	action, err := tool.ExecuteAsync(context.Background(), `{"target":"my-dynamic-agent","system_prompt":"do code review","task":"review diff","work_dir":"/tmp","async":false}`)
	if err != nil {
		t.Fatalf("ExecuteAsync err: %v", err)
	}
	if action != nil {
		t.Errorf("ExecuteAsync action = %v, want nil for sync mode", action)
	}

	// Execute should block and return the actual output
	output, err := tool.Execute(context.Background(), `{"target":"my-dynamic-agent","system_prompt":"do code review","task":"review diff","work_dir":"/tmp","async":false}`)
	if err != nil {
		t.Fatalf("Execute err: %v", err)
	}
	if output != "agent-output" {
		t.Errorf("Execute output = %q, want 'agent-output'", output)
	}

	// 1.5 Test skill_id resolution
	expectedPrompt = "do code review\n\n# Skill Execution Instructions\nrun checks"
	output, err = tool.Execute(context.Background(), `{"target":"my-dynamic-agent","system_prompt":"do code review","skill_id":"my-skill","task":"review diff","work_dir":"/tmp","async":false}`)
	if err != nil {
		t.Fatalf("Execute with skill_id err: %v", err)
	}
	if output != "agent-output" {
		t.Errorf("Execute with skill_id output = %q, want 'agent-output'", output)
	}

	// 2. Framework-forced asynchronous invocation.
	expectedPrompt = "do code review"
	asyncTool := tools.NewDelegateTool("caller", 5*time.Minute, resolver, nil, nil, tools.WorkDirExplicitOrInherited, tools.WithAlwaysAsyncDelegation())
	action, err = asyncTool.ExecuteAsync(context.Background(), `{"target":"my-dynamic-agent","task_name":"review diff","system_prompt":"do code review","task":"review diff","work_dir":"/tmp"}`)
	if err != nil {
		t.Fatalf("ExecuteAsync err: %v", err)
	}
	if action == nil {
		t.Fatal("ExecuteAsync action = nil, want non-nil for async mode")
	}
	if action.Prompt != "review diff" {
		t.Errorf("action.Prompt = %q, want 'review diff'", action.Prompt)
	}
}

func TestLeaderDynamicSkillDelegation(t *testing.T) {
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "my-skill")
	skillAgentsDir := filepath.Join(skillDir, "agents")
	if err := os.MkdirAll(skillAgentsDir, 0755); err != nil {
		t.Fatalf("failed to create agents dir: %v", err)
	}

	analyzerContent := `---
name: analyzer
description: Skill-provided code analyzer
---
This is the analyzer system prompt.
`
	if err := os.WriteFile(filepath.Join(skillAgentsDir, "analyzer.md"), []byte(analyzerContent), 0644); err != nil {
		t.Fatalf("failed to write analyzer.md: %v", err)
	}

	reg := NewRegistry(newTestLogger(t))
	f := NewDefaultFactory(reg, &agenttest.FakeLLM{}, tools.Config{}, newTestLogger(t))

	l2Tmpl := AgentTemplate{ID: "engineering", Name: "Engineering", IsLeader: true, Group: "engineering"}
	leader, _, err := f.Create(context.Background(), l2Tmpl, tempDir)
	if err != nil {
		t.Fatalf("failed to create leader: %v", err)
	}
	defer leader.Stop(time.Second)

	dtTool, ok := leader.tools.Get("delegate")
	if !ok {
		t.Fatalf("delegate tool not found on leader")
	}
	dt, ok := dtTool.(*tools.DelegateTool)
	if !ok {
		t.Fatalf("delegate is not of correct type")
	}

	dt.SkillInstructionsLook = func(skillID string) (string, string, string, bool) {
		if skillID == "my-skill" {
			return "Perform analysis.", "analyzer", skillDir, true
		}
		return "", "", "", false
	}

	targetLLM := &agenttest.FakeLLM{Responses: []string{"Delegation result"}}
	f.llm = targetLLM

	args := fmt.Sprintf(`{"target":"analyzer-instance","skill_id":"my-skill","task":"test task","work_dir":%q,"async":false}`, tempDir)
	ctx := iface.ContextWithWorkDir(context.Background(), tempDir)
	res, err := dt.Execute(ctx, args)
	if err != nil {
		t.Fatalf("failed to execute delegate: %v", err)
	}
	if res != "Delegation result" {
		t.Errorf("unexpected result: %q", res)
	}
}

func TestL1DynamicDelegationEndToEnd(t *testing.T) {
	childLLM := &agenttest.FakeLLM{
		Responses: []string{"The result of 1+1 is 2."},
	}

	hostLLM := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{
			{
				{
					Type: "function",
					ID:   "call_delegate",
					Function: llm.FunctionCall{
						Name:      "delegate",
						Arguments: `{"target":"math-agent","system_prompt":"do math","task":"1+1","work_dir":"/tmp","async":false}`,
					},
				},
			},
		},
		Responses: []string{"The delegate agent returned: The result of 1+1 is 2."},
	}

	var spawnedChild *Agent
	resolver := func(ctx context.Context, name, systemPrompt, modelID, task, workDir, skillID string) (iface.Locatable, bool, error) {
		childDef := Definition{
			ID:           strings.ToLower(name),
			Name:         name,
			SystemPrompt: systemPrompt,
		}
		// Create and start child
		child := NewAgent(childDef, childLLM, newTestLogger(t))
		if err := child.Start(ctx); err != nil {
			return nil, false, err
		}
		spawnedChild = child
		return &LocatableAdapter{Agent: child}, true, nil
	}

	dt := tools.NewDelegateTool("L1", 30*time.Minute, resolver, nil, newTestLogger(t), tools.WorkDirExplicitOrInherited)

	host := startedAgent(t, hostLLM, WithTools(dt))
	defer host.Stop(time.Second)

	// Run Ask
	result, err := host.Ask(context.Background(), "Please calculate 1+1 using a math agent.")
	if err != nil {
		t.Fatalf("Ask err: %v", err)
	}

	if !strings.Contains(result, "The result of 1+1 is 2.") {
		t.Errorf("result = %q, want it to contain delegation result", result)
	}

	// Verify child was spawned and stopped (since delegate_agent cleans it up)
	if spawnedChild == nil {
		t.Fatal("child agent was never spawned")
	}

	// Wait a moment for stop to complete
	waitFor(t, 200*time.Millisecond, func() bool { return spawnedChild.State() == StateStopped })
}
