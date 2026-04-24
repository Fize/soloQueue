package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── Helpers ────────────────────────────────────────────────────────────────

// drainEvents reads all events from ch until close; returns full slice.
//
// Times out if channel doesn't close within d (guards deadlock bugs in
// runOnceStream's defer close(out) path).
func drainEvents(t *testing.T, ch <-chan AgentEvent, d time.Duration) []AgentEvent {
	t.Helper()
	var evs []AgentEvent
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return evs
			}
			evs = append(evs, ev)
		case <-deadline.C:
			t.Fatalf("drainEvents: channel did not close within %v (collected %d events)", d, len(evs))
			return evs
		}
	}
}

// assembleContent joins all ContentDeltaEvent.Delta.
func assembleContent(evs []AgentEvent) string {
	var b strings.Builder
	for _, ev := range evs {
		if e, ok := ev.(ContentDeltaEvent); ok {
			b.WriteString(e.Delta)
		}
	}
	return b.String()
}

// countEvents returns N of each AgentEvent concrete type.
func countEvents(evs []AgentEvent) map[reflect.Type]int {
	m := map[reflect.Type]int{}
	for _, ev := range evs {
		m[reflect.TypeOf(ev)]++
	}
	return m
}

// lastEvent returns the last emitted event (or nil if none).
func lastEvent(evs []AgentEvent) AgentEvent {
	if len(evs) == 0 {
		return nil
	}
	return evs[len(evs)-1]
}

// findEvent returns the first event matching type T; ok=false if none.
func findEvent[T AgentEvent](evs []AgentEvent) (T, bool) {
	var zero T
	for _, ev := range evs {
		if t, ok := ev.(T); ok {
			return t, true
		}
	}
	return zero, false
}

// ─── Basic streaming (no tools) ──────────────────────────────────────────────

// TestAskStream_NoTools_SingleDelta: one ContentDelta emitted, then Done.
// Checks content_delta → iteration_done → done ordering.
func TestAskStream_NoTools_SingleDelta(t *testing.T) {
	fake := &FakeLLM{
		StreamDeltas: [][]string{{"hello"}},
	}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	if got := assembleContent(evs); got != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
	counts := countEvents(evs)
	if counts[reflect.TypeOf(ContentDeltaEvent{})] != 1 {
		t.Errorf("ContentDeltaEvent count = %d, want 1", counts[reflect.TypeOf(ContentDeltaEvent{})])
	}
	if counts[reflect.TypeOf(IterationDoneEvent{})] != 1 {
		t.Errorf("IterationDoneEvent count = %d, want 1", counts[reflect.TypeOf(IterationDoneEvent{})])
	}
	if counts[reflect.TypeOf(DoneEvent{})] != 1 {
		t.Errorf("DoneEvent count = %d, want 1", counts[reflect.TypeOf(DoneEvent{})])
	}
	// Done event must be last
	if _, ok := lastEvent(evs).(DoneEvent); !ok {
		t.Errorf("last event = %T, want DoneEvent", lastEvent(evs))
	}
}

// TestAskStream_NoTools_MultipleDeltas: multiple content deltas, assembled in order.
func TestAskStream_NoTools_MultipleDeltas(t *testing.T) {
	fake := &FakeLLM{
		StreamDeltas: [][]string{{"he", "ll", "o"}},
	}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	if got := assembleContent(evs); got != "hello" {
		t.Errorf("assembled = %q, want 'hello'", got)
	}
	counts := countEvents(evs)
	if counts[reflect.TypeOf(ContentDeltaEvent{})] != 3 {
		t.Errorf("ContentDeltaEvent count = %d, want 3", counts[reflect.TypeOf(ContentDeltaEvent{})])
	}
	// Verify deltas preserve order
	var order []string
	for _, ev := range evs {
		if e, ok := ev.(ContentDeltaEvent); ok {
			order = append(order, e.Delta)
		}
	}
	if !reflect.DeepEqual(order, []string{"he", "ll", "o"}) {
		t.Errorf("delta order = %v", order)
	}
}

// TestAskStream_EmitsDoneOnce: exactly one DoneEvent per stream.
func TestAskStream_EmitsDoneOnce(t *testing.T) {
	fake := &FakeLLM{StreamDeltas: [][]string{{"x"}}}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	n := 0
	for _, ev := range evs {
		if _, ok := ev.(DoneEvent); ok {
			n++
		}
	}
	if n != 1 {
		t.Errorf("DoneEvent count = %d, want 1", n)
	}
}

// TestAsk_WrapsStreamCorrectly: Ask built on AskStream returns final content.
func TestAsk_WrapsStreamCorrectly(t *testing.T) {
	fake := &FakeLLM{StreamDeltas: [][]string{{"foo", "bar"}}}
	a := startedAgent(t, fake)

	reply, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "foobar" {
		t.Errorf("reply = %q, want 'foobar'", reply)
	}
}

// ─── Reasoning (deepseek-reasoner) ──────────────────────────────────────────

// TestAskStream_EmitsReasoningDeltas: reasoning_content_delta → ReasoningDeltaEvent.
func TestAskStream_EmitsReasoningDeltas(t *testing.T) {
	fake := &FakeLLM{
		StreamDeltas:          [][]string{{"answer"}},
		ReasoningDeltasByTurn: [][]string{{"think1", "think2"}},
	}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	counts := countEvents(evs)
	if n := counts[reflect.TypeOf(ReasoningDeltaEvent{})]; n != 2 {
		t.Errorf("ReasoningDeltaEvent count = %d, want 2", n)
	}
	var parts []string
	for _, ev := range evs {
		if e, ok := ev.(ReasoningDeltaEvent); ok {
			parts = append(parts, e.Delta)
		}
	}
	if !reflect.DeepEqual(parts, []string{"think1", "think2"}) {
		t.Errorf("reasoning parts = %v", parts)
	}
}

// ─── Tool loop (serial) ──────────────────────────────────────────────────────

// TestAskStream_OneToolCall_EmitsStartDoneIterDone:
// Verify event ordering for a single tool_call: ToolCallDelta → IterationDone(iter=0) →
// ToolExecStart → ToolExecDone → ContentDelta/IterationDone(iter=1) → Done
func TestAskStream_OneToolCall_EmitsStartDoneIterDone(t *testing.T) {
	echo := newFakeTool("echo")
	echo.result = `{"ok":true}`

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "c1",
			Type:     "function",
			Function: llm.FunctionCall{Name: "echo", Arguments: `{"m":"hi"}`},
		}}},
		StreamDeltas: [][]string{nil, {"final"}}, // turn 0: tool; turn 1: content
	}
	a := startedAgentWithTools(t, fake, echo)

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	// Ordered event types expected:
	//   ToolCallDeltaEvent(iter=0) -> IterationDone(iter=0) ->
	//   ToolExecStart(iter=0) -> ToolExecDone(iter=0) ->
	//   ContentDelta(iter=1) -> IterationDone(iter=1) -> Done
	var (
		seenToolStart bool
		seenToolDone  bool
		seenIter0Done bool
		seenIter1Done bool
		seenDone      bool
	)
	for _, ev := range evs {
		switch e := ev.(type) {
		case ToolExecStartEvent:
			if e.Iter != 0 || e.CallID != "c1" || e.Name != "echo" {
				t.Errorf("ToolExecStart wrong: %+v", e)
			}
			if !seenIter0Done {
				t.Error("ToolExecStart before IterationDone(iter=0)")
			}
			seenToolStart = true
		case ToolExecDoneEvent:
			if !seenToolStart {
				t.Error("ToolExecDone before ToolExecStart")
			}
			if e.Result != `{"ok":true}` {
				t.Errorf("ToolExecDone.Result = %q", e.Result)
			}
			if e.Err != nil {
				t.Errorf("ToolExecDone.Err = %v", e.Err)
			}
			seenToolDone = true
		case IterationDoneEvent:
			switch e.Iter {
			case 0:
				if e.FinishReason != llm.FinishToolCalls {
					t.Errorf("iter 0 finish = %q, want tool_calls", e.FinishReason)
				}
				seenIter0Done = true
			case 1:
				if !seenToolDone {
					t.Error("IterationDone(iter=1) before tool completed")
				}
				seenIter1Done = true
			default:
				t.Errorf("unexpected iter = %d", e.Iter)
			}
		case DoneEvent:
			if !seenIter1Done {
				t.Error("Done before IterationDone(iter=1)")
			}
			if e.Content != "final" {
				t.Errorf("final content = %q", e.Content)
			}
			seenDone = true
		}
	}
	if !seenToolStart || !seenToolDone || !seenIter0Done || !seenIter1Done || !seenDone {
		t.Fatalf("missing expected events; evs=%+v", evs)
	}
}

// TestAskStream_ToolResultFedBackToLLM: the tool role message is appended to
// the second Chat request.
func TestAskStream_ToolResultFedBackToLLM(t *testing.T) {
	echo := newFakeTool("echo")
	echo.result = `{"v":42}`

	var capturedMsgs []LLMMessage
	var mu sync.Mutex
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "echo", Arguments: `{}`},
		}}},
		StreamDeltas: [][]string{nil, {"ok"}},
		Hook: func(req LLMRequest) {
			mu.Lock()
			defer mu.Unlock()
			// keep the last (longest) seen — second iteration has tool-role msg
			if len(req.Messages) > len(capturedMsgs) {
				capturedMsgs = append([]LLMMessage(nil), req.Messages...)
			}
		},
	}
	a := startedAgentWithTools(t, fake, echo)

	_, err := a.Ask(context.Background(), "go")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(capturedMsgs) < 3 {
		t.Fatalf("captured msgs = %d, want >= 3", len(capturedMsgs))
	}
	last := capturedMsgs[len(capturedMsgs)-1]
	if last.Role != "tool" || last.ToolCallID != "c1" || last.Content != `{"v":42}` {
		t.Errorf("tool msg wrong: %+v", last)
	}
}

// TestAskStream_MaxIterations: over limit → ErrorEvent with ErrMaxIterations.
func TestAskStream_MaxIterations(t *testing.T) {
	tool := newFakeTool("loop")
	turns := make([][]llm.ToolCall, 5)
	for i := range turns {
		turns[i] = []llm.ToolCall{{
			ID:       fmt.Sprintf("c%d", i),
			Function: llm.FunctionCall{Name: "loop", Arguments: `{}`},
		}}
	}
	fake := &FakeLLM{ToolCallsByTurn: turns}

	a := NewAgent(Definition{ID: "a1", MaxIterations: 3}, fake, nil, WithTools(tool))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	ch, err := a.AskStream(context.Background(), "")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	last, ok := lastEvent(evs).(ErrorEvent)
	if !ok {
		t.Fatalf("last event = %T, want ErrorEvent", lastEvent(evs))
	}
	if !errors.Is(last.Err, ErrMaxIterations) {
		t.Errorf("err = %v, want ErrMaxIterations", last.Err)
	}
	if tool.CallCount() != 3 {
		t.Errorf("tool called %d times, want 3", tool.CallCount())
	}
}

// ─── Parallel tool execution ─────────────────────────────────────────────────

// slowTool records the concurrency peak while its Execute is in-flight.
type concurrencyTool struct {
	name    string
	delay   time.Duration
	started chan struct{}

	// shared across tools to observe concurrent peak
	inFlight *int32
	peak     *int32
}

func (c *concurrencyTool) Name() string                { return c.name }
func (c *concurrencyTool) Description() string         { return "concurrency probe " + c.name }
func (c *concurrencyTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *concurrencyTool) Execute(ctx context.Context, _ string) (string, error) {
	cur := atomic.AddInt32(c.inFlight, 1)
	for {
		prev := atomic.LoadInt32(c.peak)
		if cur <= prev || atomic.CompareAndSwapInt32(c.peak, prev, cur) {
			break
		}
	}
	defer atomic.AddInt32(c.inFlight, -1)

	if c.started != nil {
		// broadcast without closing twice; use non-blocking send
		select {
		case c.started <- struct{}{}:
		default:
		}
	}

	select {
	case <-time.After(c.delay):
		return "ok-" + c.name, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestAskStream_ParallelTools_PreserveOrder: three tools run concurrently but
// the role=tool messages are fed back in original tool_calls order.
func TestAskStream_ParallelTools_PreserveOrder(t *testing.T) {
	var inFlight, peak int32
	mkTool := func(name string) *concurrencyTool {
		return &concurrencyTool{
			name:     name,
			delay:    80 * time.Millisecond,
			inFlight: &inFlight,
			peak:     &peak,
		}
	}
	t1 := mkTool("t1")
	t2 := mkTool("t2")
	t3 := mkTool("t3")

	var capturedMsgs []LLMMessage
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "c1", Function: llm.FunctionCall{Name: "t1", Arguments: `{}`}},
			{ID: "c2", Function: llm.FunctionCall{Name: "t2", Arguments: `{}`}},
			{ID: "c3", Function: llm.FunctionCall{Name: "t3", Arguments: `{}`}},
		}},
		StreamDeltas: [][]string{nil, {"ok"}},
		Hook: func(req LLMRequest) {
			if len(req.Messages) >= 5 {
				capturedMsgs = req.Messages
			}
		},
	}
	a := NewAgent(Definition{ID: "a1"}, fake, nil,
		WithTools(t1, t2, t3),
		WithParallelTools(true),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	start := time.Now()
	_, err := a.Ask(context.Background(), "go")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	// 3 tools * 80ms serial would be ~240ms; parallel should finish in ~80ms
	if elapsed > 200*time.Millisecond {
		t.Errorf("parallel Ask elapsed = %v (likely serial; want <200ms)", elapsed)
	}
	if p := atomic.LoadInt32(&peak); p < 2 {
		t.Errorf("observed peak concurrency = %d, want >= 2", p)
	}

	// feedback messages order must match calls order (c1, c2, c3)
	if len(capturedMsgs) < 5 {
		t.Fatalf("captured msgs = %d", len(capturedMsgs))
	}
	toolMsgs := capturedMsgs[len(capturedMsgs)-3:]
	wantIDs := []string{"c1", "c2", "c3"}
	wantResults := []string{"ok-t1", "ok-t2", "ok-t3"}
	for i, m := range toolMsgs {
		if m.Role != "tool" || m.ToolCallID != wantIDs[i] || m.Content != wantResults[i] {
			t.Errorf("tool msg[%d] wrong: %+v", i, m)
		}
	}
}

// TestAskStream_ParallelTools_OneFailsOthersSucceed: single tool failure doesn't
// short-circuit the group.
func TestAskStream_ParallelTools_OneFailsOthersSucceed(t *testing.T) {
	good1 := newFakeTool("good1")
	good1.result = "r1"
	bad := newFakeTool("bad")
	bad.err = errors.New("kaboom")
	good2 := newFakeTool("good2")
	good2.result = "r2"

	var capturedMsgs []LLMMessage
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "c1", Function: llm.FunctionCall{Name: "good1", Arguments: `{}`}},
			{ID: "c2", Function: llm.FunctionCall{Name: "bad", Arguments: `{}`}},
			{ID: "c3", Function: llm.FunctionCall{Name: "good2", Arguments: `{}`}},
		}},
		StreamDeltas: [][]string{nil, {"recovered"}},
		Hook: func(req LLMRequest) {
			if len(req.Messages) >= 5 {
				capturedMsgs = req.Messages
			}
		},
	}
	a := NewAgent(Definition{ID: "a1"}, fake, nil,
		WithTools(good1, bad, good2),
		WithParallelTools(true),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	reply, err := a.Ask(context.Background(), "go")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "recovered" {
		t.Errorf("reply = %q", reply)
	}
	if good1.CallCount() != 1 || good2.CallCount() != 1 || bad.CallCount() != 1 {
		t.Errorf("calls: good1=%d bad=%d good2=%d", good1.CallCount(), bad.CallCount(), good2.CallCount())
	}
	if len(capturedMsgs) < 5 {
		t.Fatalf("captured msgs = %d", len(capturedMsgs))
	}
	toolMsgs := capturedMsgs[len(capturedMsgs)-3:]
	if toolMsgs[0].Content != "r1" {
		t.Errorf("c1 content = %q", toolMsgs[0].Content)
	}
	if !strings.HasPrefix(toolMsgs[1].Content, "error: ") || !strings.Contains(toolMsgs[1].Content, "kaboom") {
		t.Errorf("c2 content = %q, want prefixed 'error: ... kaboom'", toolMsgs[1].Content)
	}
	if toolMsgs[2].Content != "r2" {
		t.Errorf("c3 content = %q", toolMsgs[2].Content)
	}
}

// TestAskStream_ParallelTools_CtxCancelAborts: cancelling caller ctx during
// parallel execution aborts the Ask with an ErrorEvent.
func TestAskStream_ParallelTools_CtxCancelAborts(t *testing.T) {
	var inFlight, peak int32
	slow := func(name string) *concurrencyTool {
		return &concurrencyTool{
			name: name, delay: 5 * time.Second,
			started:  make(chan struct{}, 1),
			inFlight: &inFlight, peak: &peak,
		}
	}
	t1 := slow("t1")
	t2 := slow("t2")

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "c1", Function: llm.FunctionCall{Name: "t1", Arguments: `{}`}},
			{ID: "c2", Function: llm.FunctionCall{Name: "t2", Arguments: `{}`}},
		}},
		StreamDeltas: [][]string{nil, {"unreached"}},
	}
	a := NewAgent(Definition{ID: "a1"}, fake, nil,
		WithTools(t1, t2),
		WithParallelTools(true),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// cancel once both tools have started
	go func() {
		<-t1.started
		<-t2.started
		cancel()
	}()

	start := time.Now()
	_, err := a.Ask(ctx, "")
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("cancel too slow: %v", elapsed)
	}
}

// TestAskStream_ParallelTools_NotEnabledFallsBackSerial: without WithParallelTools,
// tools run strictly serially (peak concurrency = 1).
func TestAskStream_ParallelTools_NotEnabledFallsBackSerial(t *testing.T) {
	var inFlight, peak int32
	mk := func(name string) *concurrencyTool {
		return &concurrencyTool{
			name: name, delay: 40 * time.Millisecond,
			inFlight: &inFlight, peak: &peak,
		}
	}
	t1 := mk("t1")
	t2 := mk("t2")
	t3 := mk("t3")

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "c1", Function: llm.FunctionCall{Name: "t1", Arguments: `{}`}},
			{ID: "c2", Function: llm.FunctionCall{Name: "t2", Arguments: `{}`}},
			{ID: "c3", Function: llm.FunctionCall{Name: "t3", Arguments: `{}`}},
		}},
		StreamDeltas: [][]string{nil, {"ok"}},
	}
	// default: parallelTools=false
	a := startedAgentWithTools(t, fake, t1, t2, t3)

	_, err := a.Ask(context.Background(), "")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if p := atomic.LoadInt32(&peak); p != 1 {
		t.Errorf("serial peak concurrency = %d, want 1", p)
	}
}

// ─── Timeout decorator ──────────────────────────────────────────────────────

// slowTool: blocks for delay or until ctx cancel
type slowTool struct {
	name  string
	delay time.Duration
	count atomic.Int32
}

func (s *slowTool) Name() string                { return s.name }
func (s *slowTool) Description() string         { return "slow tool " + s.name }
func (s *slowTool) Parameters() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *slowTool) Execute(ctx context.Context, _ string) (string, error) {
	s.count.Add(1)
	select {
	case <-time.After(s.delay):
		return "done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestAskStream_ToolTimeout_EmitsErrorFedBack: configured timeout fires, tool
// result is "error: tool timeout after Xs", loop continues.
func TestAskStream_ToolTimeout_EmitsErrorFedBack(t *testing.T) {
	slow := &slowTool{name: "slow", delay: 5 * time.Second}

	var capturedContent string
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "slow", Arguments: `{}`},
		}}},
		StreamDeltas: [][]string{nil, {"ok"}},
		Hook: func(req LLMRequest) {
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
				capturedContent = req.Messages[n-1].Content
			}
		},
	}
	a := NewAgent(Definition{ID: "a1"}, fake, nil,
		WithTools(slow),
		WithToolTimeout("slow", 60*time.Millisecond),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	start := time.Now()
	reply, err := a.Ask(context.Background(), "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q, want 'ok'", reply)
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout did not fire promptly; elapsed = %v", elapsed)
	}
	if !strings.HasPrefix(capturedContent, "error: tool timeout after") {
		t.Errorf("fed-back content = %q, want 'error: tool timeout after ...'", capturedContent)
	}
}

// TestAskStream_ToolTimeout_OtherToolsUnaffected: timeout on tool A does not
// kill tool B's execution when running in parallel.
func TestAskStream_ToolTimeout_OtherToolsUnaffected(t *testing.T) {
	slow := &slowTool{name: "slow", delay: 5 * time.Second}
	quick := newFakeTool("quick")
	quick.result = "done"

	var capturedMsgs []LLMMessage
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "c1", Function: llm.FunctionCall{Name: "slow", Arguments: `{}`}},
			{ID: "c2", Function: llm.FunctionCall{Name: "quick", Arguments: `{}`}},
		}},
		StreamDeltas: [][]string{nil, {"ok"}},
		Hook: func(req LLMRequest) {
			if len(req.Messages) >= 4 {
				capturedMsgs = req.Messages
			}
		},
	}
	a := NewAgent(Definition{ID: "a1"}, fake, nil,
		WithTools(slow, quick),
		WithParallelTools(true),
		WithToolTimeout("slow", 50*time.Millisecond),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	_, err := a.Ask(context.Background(), "")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if len(capturedMsgs) < 4 {
		t.Fatalf("captured msgs = %d", len(capturedMsgs))
	}
	toolMsgs := capturedMsgs[len(capturedMsgs)-2:]
	if !strings.HasPrefix(toolMsgs[0].Content, "error: tool timeout") {
		t.Errorf("slow tool content = %q, want 'error: tool timeout ...'", toolMsgs[0].Content)
	}
	if toolMsgs[1].Content != "done" {
		t.Errorf("quick tool content = %q, want 'done'", toolMsgs[1].Content)
	}
}

// TestWithToolTimeout_ZeroDeletes: WithToolTimeout(name, 0) removes the entry.
func TestWithToolTimeout_ZeroDeletes(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil,
		WithToolTimeout("foo", 5*time.Second),
		WithToolTimeout("foo", 0), // 删除
	)
	if _, ok := a.toolTimeouts["foo"]; ok {
		t.Error("WithToolTimeout(name, 0) should delete entry")
	}
	// negative also deletes
	a2 := NewAgent(Definition{ID: "a2"}, &FakeLLM{}, nil,
		WithToolTimeout("foo", 5*time.Second),
		WithToolTimeout("foo", -1),
	)
	if _, ok := a2.toolTimeouts["foo"]; ok {
		t.Error("WithToolTimeout(name, negative) should delete entry")
	}
}

// ─── Error paths ────────────────────────────────────────────────────────────

// TestAskStream_LLMReturnsErrorEvent_AbortsStream: llm.EventError → ErrorEvent + close.
func TestAskStream_LLMReturnsErrorEvent_AbortsStream(t *testing.T) {
	myErr := errors.New("llm stream died")
	fake := &FakeLLM{Err: myErr}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	last, ok := lastEvent(evs).(ErrorEvent)
	if !ok {
		t.Fatalf("last = %T, want ErrorEvent", lastEvent(evs))
	}
	if !errors.Is(last.Err, myErr) {
		t.Errorf("err = %v, want %v", last.Err, myErr)
	}
}

// TestAskStream_CtxCancelMidStream: caller cancels, ErrorEvent(ctx.Canceled) emitted.
func TestAskStream_CtxCancelMidStream(t *testing.T) {
	fake := &FakeLLM{
		StreamDeltas: [][]string{{"x"}},
		Delay:        500 * time.Millisecond,
	}
	a := startedAgent(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := a.AskStream(ctx, "")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	// give Ask time to enter Processing, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	evs := drainEvents(t, ch, 3*time.Second)
	last, ok := lastEvent(evs).(ErrorEvent)
	if !ok {
		t.Fatalf("last = %T, want ErrorEvent", lastEvent(evs))
	}
	if !errors.Is(last.Err, context.Canceled) {
		t.Errorf("err = %v, want Canceled", last.Err)
	}
}

// TestAskStream_NotStarted_ReturnsErr: AskStream before Start returns
// (nil, ErrNotStarted).
func TestAskStream_NotStarted_ReturnsErr(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	ch, err := a.AskStream(context.Background(), "hi")
	if !errors.Is(err, ErrNotStarted) {
		t.Errorf("err = %v, want ErrNotStarted", err)
	}
	if ch != nil {
		t.Errorf("ch = %v, want nil", ch)
	}
}

// TestAskStream_AfterStop_ReturnsErr: AskStream after Stop returns ErrStopped.
func TestAskStream_AfterStop_ReturnsErr(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = a.Stop(time.Second)

	ch, err := a.AskStream(context.Background(), "hi")
	if !errors.Is(err, ErrStopped) {
		t.Errorf("err = %v, want ErrStopped", err)
	}
	if ch != nil {
		t.Errorf("ch = %v, want nil", ch)
	}
}

// TestAskStream_NilCtxHandled: nil ctx defaults to Background.
func TestAskStream_NilCtxHandled(t *testing.T) {
	a := startedAgent(t, &FakeLLM{StreamDeltas: [][]string{{"ok"}}})
	//nolint:staticcheck // intentionally testing nil ctx
	ch, err := a.AskStream(nil, "hi")
	if err != nil {
		t.Fatalf("AskStream(nil): %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)
	if _, ok := lastEvent(evs).(DoneEvent); !ok {
		t.Errorf("last = %T, want DoneEvent", lastEvent(evs))
	}
}

// ─── Channel lifecycle ──────────────────────────────────────────────────────

// TestAskStream_ChannelClosedAfterDoneOrError: receiving from channel after
// Done/Error returns (zero, false).
func TestAskStream_ChannelClosedAfterDoneOrError(t *testing.T) {
	fake := &FakeLLM{StreamDeltas: [][]string{{"x"}}}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	_ = drainEvents(t, ch, 2*time.Second)

	// extra receive: must be closed (ok=false)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel not closed after Done")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("receive on closed channel hung")
	}
}

// TestAskStream_NoEventsAfterError: no events emitted after ErrorEvent.
func TestAskStream_NoEventsAfterError(t *testing.T) {
	fake := &FakeLLM{Err: errors.New("boom")}
	a := startedAgent(t, fake)

	ch, err := a.AskStream(context.Background(), "")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	// ErrorEvent must be the final event
	foundErr := false
	for _, ev := range evs {
		if foundErr {
			t.Errorf("event after ErrorEvent: %T", ev)
		}
		if _, ok := ev.(ErrorEvent); ok {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("no ErrorEvent emitted")
	}
}

// ─── Backpressure ────────────────────────────────────────────────────────────

// TestAskStream_SlowConsumerBlocksProducer_ButCancelWorks:
// If caller doesn't consume and channel fills up, producer blocks in emit.
// Cancelling caller ctx releases the producer.
func TestAskStream_SlowConsumerBlocksProducer_ButCancelWorks(t *testing.T) {
	// 200 deltas will definitely fill the 64-buffer
	deltas := make([]string, 200)
	for i := range deltas {
		deltas[i] = "x"
	}
	fake := &FakeLLM{StreamDeltas: [][]string{deltas}}

	a := startedAgent(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := a.AskStream(ctx, "")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	// don't consume; give producer time to fill buffer & block
	time.Sleep(100 * time.Millisecond)
	cancel()

	// now drain; channel must close eventually
	evs := drainEvents(t, ch, 3*time.Second)
	_ = evs // we don't care what we got; only that close happened without deadlock
}

// ─── Tool-call delta accumulation ───────────────────────────────────────────

// TestAskStream_ToolCallDelta_AccumulatedToFullArgs: multiple ToolCallDelta
// slices for the same Index accumulate into one ToolCall fed to the tool.
func TestAskStream_ToolCallDelta_AccumulatedToFullArgs(t *testing.T) {
	echo := newFakeTool("echo")
	echo.result = "r"

	var gotArgs string
	var mu sync.Mutex
	// wrap echo to record args
	echoWrap := &wrapTool{base: echo, onArgs: func(a string) { mu.Lock(); gotArgs = a; mu.Unlock() }}

	fake := &FakeLLM{
		// Turn 0: emit tool_call via streaming deltas (3 args fragments)
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{
			{Index: 0, ID: "c1", Name: "echo", Arguments: `{"m":`},
			{Index: 0, Arguments: `"hi"`},
			{Index: 0, Arguments: `}`},
		}, nil},
		// Turn 1: final content
		StreamDeltas: [][]string{nil, {"done"}},
	}
	a := startedAgentWithTools(t, fake, echoWrap)

	reply, err := a.Ask(context.Background(), "")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "done" {
		t.Errorf("reply = %q", reply)
	}
	mu.Lock()
	if gotArgs != `{"m":"hi"}` {
		t.Errorf("args = %q, want '{\"m\":\"hi\"}'", gotArgs)
	}
	mu.Unlock()
}

// wrapTool delegates to base but captures the args via onArgs.
type wrapTool struct {
	base   *fakeTool
	onArgs func(args string)
}

func (w *wrapTool) Name() string                { return w.base.Name() }
func (w *wrapTool) Description() string         { return w.base.Description() }
func (w *wrapTool) Parameters() json.RawMessage { return w.base.Parameters() }
func (w *wrapTool) Execute(ctx context.Context, args string) (string, error) {
	if w.onArgs != nil {
		w.onArgs(args)
	}
	return w.base.Execute(ctx, args)
}

// TestAskStream_MultipleToolCallSlotsStream: two concurrent tool_calls (different
// Index) streamed via ToolCallDeltasByTurn; both executed with correct args.
func TestAskStream_MultipleToolCallSlotsStream(t *testing.T) {
	t1 := newFakeTool("t1")
	t1.result = "r1"
	t2 := newFakeTool("t2")
	t2.result = "r2"

	var a1, a2 string
	var mu sync.Mutex
	w1 := &wrapTool{base: t1, onArgs: func(a string) { mu.Lock(); a1 = a; mu.Unlock() }}
	w2 := &wrapTool{base: t2, onArgs: func(a string) { mu.Lock(); a2 = a; mu.Unlock() }}

	fake := &FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{
			// interleaved slot 0 and slot 1 deltas
			{Index: 0, ID: "c1", Name: "t1", Arguments: `{"a":`},
			{Index: 1, ID: "c2", Name: "t2", Arguments: `{"b":`},
			{Index: 0, Arguments: `1}`},
			{Index: 1, Arguments: `2}`},
		}, nil},
		StreamDeltas: [][]string{nil, {"done"}},
	}
	a := startedAgentWithTools(t, fake, w1, w2)

	_, err := a.Ask(context.Background(), "")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if a1 != `{"a":1}` {
		t.Errorf("t1 args = %q, want '{\"a\":1}'", a1)
	}
	if a2 != `{"b":2}` {
		t.Errorf("t2 args = %q, want '{\"b\":2}'", a2)
	}
}

// ─── Panic recovery ─────────────────────────────────────────────────────────

// TestAskStream_LLMPanic_EmitsErrorEvent: LLM panic → ErrorEvent
// then run goroutine's recover kicks in (state=Stopped, Err non-nil).
func TestAskStream_LLMPanic_EmitsErrorEvent(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &panickyLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ch, err := a.AskStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	evs := drainEvents(t, ch, 2*time.Second)

	last, ok := lastEvent(evs).(ErrorEvent)
	if !ok {
		t.Fatalf("last = %T, want ErrorEvent", lastEvent(evs))
	}
	if last.Err == nil {
		t.Error("ErrorEvent.Err = nil")
	}
	if !strings.Contains(last.Err.Error(), "panic") {
		t.Errorf("err = %v, want to contain 'panic'", last.Err)
	}

	// agent should end up Stopped
	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("agent did not exit after panic")
	}
	if a.Err() == nil {
		t.Error("a.Err() = nil after panic")
	}
}

// ─── RunOnce streaming wrapper ──────────────────────────────────────────────

// TestRunOnce_UsesStreamPath: RunOnce consumes AskStream events and still
// returns final content for no-tools happy case.
func TestRunOnce_UsesStreamPath(t *testing.T) {
	fake := &FakeLLM{StreamDeltas: [][]string{{"hello"}}}
	reply, err := RunOnce(context.Background(), Definition{ID: "a1"}, fake, nil, "hi")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q", reply)
	}
}

// TestRunOnce_FallsBackToResponses: when only Responses is set (no
// per-turn streaming script), ChatStream falls back and emits single delta.
func TestRunOnce_FallsBackToResponses(t *testing.T) {
	fake := &FakeLLM{Responses: []string{"legacy"}}
	reply, err := RunOnce(context.Background(), Definition{ID: "a1"}, fake, nil, "hi")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if reply != "legacy" {
		t.Errorf("reply = %q", reply)
	}
}

// ─── FakeLLM call counters ──────────────────────────────────────────────────

// TestFakeLLM_StreamCallCount_Increments: each ChatStream increments counter.
func TestFakeLLM_StreamCallCount_Increments(t *testing.T) {
	fake := &FakeLLM{StreamDeltas: [][]string{{"a"}, {"b"}}}
	a := startedAgent(t, fake)

	_, _ = a.Ask(context.Background(), "1")
	_, _ = a.Ask(context.Background(), "2")

	if n := fake.StreamCallCount(); n != 2 {
		t.Errorf("StreamCallCount = %d, want 2", n)
	}
}
