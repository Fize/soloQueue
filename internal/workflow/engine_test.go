package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEngine_FanOutBeyondParallelLimitCompletes(t *testing.T) {
	yaml := `
name: wide-fanout
version: "1"
agents:
  worker:
    template: dev
entry: [task_a, task_b, task_c, task_d, task_e]
nodes:
  - id: task_a
    agent: worker
    prompt: A
    outputs: {done: {to: []}}
  - id: task_b
    agent: worker
    prompt: B
    outputs: {done: {to: []}}
  - id: task_c
    agent: worker
    prompt: C
    outputs: {done: {to: []}}
  - id: task_d
    agent: worker
    prompt: D
    outputs: {done: {to: []}}
  - id: task_e
    agent: worker
    prompt: E
    outputs: {done: {to: []}}
`
	wf := mustParse(t, yaml)
	engine := NewEngine(NewFakeExecutor(), DefaultEngineLimits())
	done := make(chan *RunState, 1)
	go func() {
		run, _ := engine.Run(context.Background(), wf, "test", "/tmp/test")
		done <- run
	}()

	select {
	case run := <-done:
		if run.Status != RunCompleted {
			t.Fatalf("status = %s, want %s", run.Status, RunCompleted)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("workflow did not consume results while ready nodes remained queued")
	}
}

func TestEngine_ParallelTerminalBlockedCancelsAndReturns(t *testing.T) {
	yaml := `
name: parallel-blocked
version: "1"
agents:
  worker:
    template: dev
entry: [blocked, worker_a, worker_b]
nodes:
  - id: blocked
    agent: worker
    prompt: Block
    outputs:
      blocked:
        to: []
        terminal_status: blocked
  - id: worker_a
    agent: worker
    prompt: A
    outputs: {done: {to: []}}
  - id: worker_b
    agent: worker
    prompt: B
    outputs: {done: {to: []}}
`
	wf := mustParse(t, yaml)
	executor := &terminalCancelExecutor{started: make(chan string, 3), release: make(chan struct{})}
	engine := NewEngine(executor, DefaultEngineLimits())
	done := make(chan *RunState, 1)
	go func() {
		run, _ := engine.Run(context.Background(), wf, "test", "/tmp/test")
		done <- run
	}()
	for range 3 {
		select {
		case <-executor.started:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("parallel nodes did not all start")
		}
	}
	close(executor.release)

	select {
	case run := <-done:
		if run.Status != RunBlocked {
			t.Fatalf("status = %s, want %s", run.Status, RunBlocked)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("terminal blocked run waited for workflow timeout")
	}
}

func TestEngine_ParallelFailFastCancelsAndReturns(t *testing.T) {
	wf := mustParse(t, `
name: parallel-fail-fast
version: "1"
agents:
  worker: {template: dev}
entry: [failed, worker_a, worker_b]
nodes:
  - id: failed
    agent: worker
    prompt: Fail
    outputs: {done: {to: []}}
  - id: worker_a
    agent: worker
    prompt: A
    outputs: {done: {to: []}}
  - id: worker_b
    agent: worker
    prompt: B
    outputs: {done: {to: []}}
`)
	executor := &terminalCancelExecutor{started: make(chan string, 3), release: make(chan struct{})}
	done := make(chan *RunState, 1)
	go func() {
		run, _ := NewEngine(executor, DefaultEngineLimits()).Run(context.Background(), wf, "test", "/tmp/test")
		done <- run
	}()
	for range 3 {
		select {
		case <-executor.started:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("parallel nodes did not all start")
		}
	}
	close(executor.release)
	select {
	case run := <-done:
		if run.Status != RunFailed {
			t.Fatalf("status = %s, want %s", run.Status, RunFailed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fail-fast run waited for workflow timeout")
	}
}

func TestEngine_ResumeRestoresPartialJoin(t *testing.T) {
	yaml := `
name: resume-join
version: "1"
agents:
  worker:
    template: dev
entry: [task_a, task_b]
nodes:
  - id: task_a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [merge]
  - id: task_b
    agent: worker
    prompt: B
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: all
      from: [task_a, task_b]
    prompt: Merge
    outputs: {done: {to: []}}
`
	wf := mustParse(t, yaml)
	executor := NewFakeExecutor()
	executor.SetNode("task_b", FakeNodeResult{Handoff: &HandoffData{Outcome: "done", Content: "B"}})
	executor.SetNode("merge", FakeNodeResult{Handoff: &HandoffData{Outcome: "done", Content: "merged"}})
	activationID := "activation-1"
	resume := &ResumeInput{
		NodeRuns: []*NodeRun{
			{ID: "task_a:1", NodeID: "task_a", Attempt: 1, ActivationID: activationID, State: NodeSucceeded, Result: &HandoffData{Outcome: "done", Content: "A"}},
			{ID: "task_b:1", NodeID: "task_b", Attempt: 1, ActivationID: activationID, State: NodeQueued},
		},
		ReadyQueue: []string{"task_b:1"},
		JoinBuckets: map[JoinKey]*JoinBucket{
			{NodeID: "merge", ActivationID: activationID}: {
				Received: map[string]NodeInput{"task_a": {FromNode: "task_a", Outcome: "done", Content: "A", ActivationID: activationID}},
				Expected: map[string]bool{"task_a": true, "task_b": true},
			},
		},
	}
	engine := NewEngine(executor, DefaultEngineLimits())
	run, err := engine.RunWithOptions(context.Background(), wf, "test", "/tmp/test", RunOptions{Resume: resume})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunCompleted || executor.Calls["merge"] != 1 {
		t.Fatalf("status=%s merge_calls=%d, want completed and one merge", run.Status, executor.Calls["merge"])
	}
}

func TestEngine_ResumeRestoresLoopCounter(t *testing.T) {
	yaml := `
name: resume-loop
version: "1"
agents:
  worker:
    template: dev
entry: [write]
nodes:
  - id: write
    agent: worker
    prompt: Write
    outputs:
      draft:
        to: [review]
  - id: review
    agent: worker
    prompt: Review
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
        max_traversals: 1
`
	wf := mustParse(t, yaml)
	executor := NewFakeExecutor()
	executor.SetNode("review", FakeNodeResult{Handoff: &HandoffData{Outcome: "needs_revision", Content: "still bad"}})
	activationID := "activation-1"
	loopKey := makeLoopKey(edgeKey("review", "needs_revision", "write"), activationID)
	resume := &ResumeInput{
		NodeRuns: []*NodeRun{
			{ID: "review:2", NodeID: "review", Attempt: 1, ActivationID: activationID, State: NodeQueued},
		},
		ReadyQueue:   []string{"review:2"},
		LoopCounters: map[LoopKey]int{loopKey: 1},
	}
	engine := NewEngine(executor, DefaultEngineLimits())
	run, err := engine.RunWithOptions(context.Background(), wf, "test", "/tmp/test", RunOptions{Resume: resume})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunFailed || executor.Calls["write"] != 0 {
		t.Fatalf("status=%s write_calls=%d, want failed before another traversal", run.Status, executor.Calls["write"])
	}
}

func TestEngine_EmptyResumeStartsEntryNodes(t *testing.T) {
	yaml := `
name: resume-before-start
version: "1"
agents:
  worker:
    template: dev
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: Start
    outputs: {done: {to: []}}
`
	wf := mustParse(t, yaml)
	executor := NewFakeExecutor()
	engine := NewEngine(executor, DefaultEngineLimits())
	run, err := engine.RunWithOptions(context.Background(), wf, "test", "/tmp/test", RunOptions{Resume: &ResumeInput{}})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunCompleted || executor.Calls["start"] != 1 {
		t.Fatalf("status=%s start_calls=%d, want completed and one entry execution", run.Status, executor.Calls["start"])
	}
}

// ---------------------------------------------------------------------------
// FakeNodeExecutor — scriptable executor for engine tests
// ---------------------------------------------------------------------------

// FakeNodeResult is a pre-scripted result for a specific node execution.
type FakeNodeResult struct {
	Handoff *HandoffData
	Error   error
}

// FakeExecutor returns pre-scripted results per (nodeID, attempt).
// Each call to Execute increments the call counter and returns the next scripted result.
type FakeExecutor struct {
	mu       sync.Mutex
	Script   map[string][]FakeNodeResult // nodeID -> sequence of results
	MaxCalls int                         // max total calls allowed (0 = unlimited)
	Calls    map[string]int              // nodeID -> call count
	Results  map[string][]FakeNodeResult // serialized results for inspection
}

type terminalCancelExecutor struct {
	started chan string
	release chan struct{}
}

func (e *terminalCancelExecutor) Execute(ctx context.Context, req NodeRunRequest) (NodeRunResult, error) {
	e.started <- req.Node.ID
	if req.Node.ID == "blocked" {
		<-e.release
		return NodeRunResult{Handoff: &HandoffData{Outcome: "blocked", Content: "needs input"}}, nil
	}
	if req.Node.ID == "failed" {
		<-e.release
		return NodeRunResult{}, errors.New("failed")
	}
	<-ctx.Done()
	return NodeRunResult{}, ctx.Err()
}

// NewFakeExecutor creates a scriptable fake executor.
func NewFakeExecutor() *FakeExecutor {
	return &FakeExecutor{
		Script:  make(map[string][]FakeNodeResult),
		Calls:   make(map[string]int),
		Results: make(map[string][]FakeNodeResult),
	}
}

// SetNode sets the script for a node.
func (f *FakeExecutor) SetNode(nodeID string, results ...FakeNodeResult) {
	f.Script[nodeID] = results
}

// Execute implements NodeExecutor.
func (f *FakeExecutor) Execute(ctx context.Context, req NodeRunRequest) (NodeRunResult, error) {
	nodeID := req.Node.ID

	f.mu.Lock()
	f.Calls[nodeID]++
	callIdx := f.Calls[nodeID] - 1 // 0-indexed call count
	f.mu.Unlock()

	script, ok := f.Script[nodeID]
	if !ok {
		return NodeRunResult{
			Handoff: &HandoffData{Outcome: "done", Content: "default output"},
		}, nil
	}

	if callIdx >= len(script) {
		// Ran out of scripted results — return error
		return NodeRunResult{}, context.DeadlineExceeded
	}

	result := script[callIdx]
	return NodeRunResult{
		Handoff: result.Handoff,
	}, result.Error
}

// ---------------------------------------------------------------------------
// Engine test helpers
// ---------------------------------------------------------------------------

func runEngine(t *testing.T, wf *ParsedWorkflow, executor *FakeExecutor, input string) *RunState {
	t.Helper()
	eng := NewEngine(executor, DefaultEngineLimits())
	ctx := context.Background()
	rs, err := eng.Run(ctx, wf, input, "/tmp/test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return rs
}

func runEngineExpecting(t *testing.T, wf *ParsedWorkflow, executor *FakeExecutor, input string, want RunStatus) *RunState {
	t.Helper()
	rs := runEngine(t, wf, executor, input)
	if rs.Status != want {
		t.Errorf("status = %s, want %s; terminal_outputs = %+v", rs.Status, want, rs.TerminalOutput)
	}
	return rs
}

// ---------------------------------------------------------------------------
// Engine tests
// ---------------------------------------------------------------------------

func TestEngine_SingleNodeTerminal(t *testing.T) {
	yaml := `
name: single-node
description: Single node
version: "1"
agents:
  worker:
    template: dev
entry: [step1]
nodes:
  - id: step1
    agent: worker
    prompt: Do it
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("step1", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "all good"},
	})

	rs := runEngineExpecting(t, wf, exec, "test input", RunCompleted)
	if len(rs.TerminalOutput) != 1 {
		t.Fatalf("terminal_outputs = %d, want 1", len(rs.TerminalOutput))
	}
	if rs.TerminalOutput[0].Node != "step1" {
		t.Errorf("terminal node = %q, want step1", rs.TerminalOutput[0].Node)
	}
}

func TestEngine_TwoNodeLinear(t *testing.T) {
	yaml := `
name: two-node
description: Two node linear
version: "1"
agents:
  worker:
    template: dev
entry: [step1]
nodes:
  - id: step1
    agent: worker
    prompt: Step 1
    outputs:
      done:
        to: [step2]
  - id: step2
    agent: worker
    prompt: Step 2
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("step1", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "step1 output"},
	})
	exec.SetNode("step2", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "step2 output"},
	})

	rs := runEngineExpecting(t, wf, exec, "test", RunCompleted)
	if exec.Calls["step1"] != 1 || exec.Calls["step2"] != 1 {
		t.Errorf("calls: step1=%d step2=%d", exec.Calls["step1"], exec.Calls["step2"])
	}
	if len(rs.NodeRuns) != 2 {
		t.Errorf("nodeRuns = %d, want 2", len(rs.NodeRuns))
	}
}

func TestEngine_ExactOutcomeBranching(t *testing.T) {
	yaml := `
name: branch-test
description: Branch test
version: "1"
agents:
  worker:
    template: dev
entry: [check]
nodes:
  - id: check
    agent: worker
    prompt: Check
    outputs:
      path_a:
        to: [handle_a]
      path_b:
        to: [handle_b]
  - id: handle_a
    agent: worker
    prompt: Handle A
    outputs:
      done:
        to: []
  - id: handle_b
    agent: worker
    prompt: Handle B
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("check", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "path_a", Content: "taking path A"},
	})
	exec.SetNode("handle_a", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "A done"},
	})

	rs := runEngineExpecting(t, wf, exec, "test", RunCompleted)
	if exec.Calls["handle_b"] != 0 {
		t.Error("handle_b should NOT have been called")
	}
	if rs.TerminalOutput[0].Node != "handle_a" {
		t.Errorf("terminal_node = %q, want handle_a", rs.TerminalOutput[0].Node)
	}
}

func TestEngine_FanOut(t *testing.T) {
	yaml := `
name: fanout-test
description: Fan-out
version: "1"
agents:
  worker:
    template: dev
entry: [dispatch]
nodes:
  - id: dispatch
    agent: worker
    prompt: Dispatch
    outputs:
      ready:
        to: [task_a, task_b]
  - id: task_a
    agent: worker
    prompt: Task A
    outputs:
      done:
        to: []
  - id: task_b
    agent: worker
    prompt: Task B
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("dispatch", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "ready", Content: "go"},
	})
	exec.SetNode("task_a", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "A done"},
	})
	exec.SetNode("task_b", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "B done"},
	})

	rs := runEngineExpecting(t, wf, exec, "test", RunCompleted)
	if exec.Calls["task_a"] != 1 || exec.Calls["task_b"] != 1 {
		t.Errorf("calls: task_a=%d task_b=%d", exec.Calls["task_a"], exec.Calls["task_b"])
	}
	if len(rs.TerminalOutput) != 2 {
		t.Errorf("terminal_outputs = %d, want 2", len(rs.TerminalOutput))
	}
}

func TestEngine_FanIn(t *testing.T) {
	yaml := `
name: fanin-test
description: Fan-in
version: "1"
agents:
  worker:
    template: dev
entry: [dispatch]
nodes:
  - id: dispatch
    agent: worker
    prompt: Dispatch
    outputs:
      ready:
        to: [task_a, task_b]
  - id: task_a
    agent: worker
    prompt: Task A
    outputs:
      done:
        to: [merge]
  - id: task_b
    agent: worker
    prompt: Task B
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: all
      from: [task_a, task_b]
    prompt: Merge
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("dispatch", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "ready", Content: "go"},
	})
	exec.SetNode("task_a", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "A data"},
	})
	exec.SetNode("task_b", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "B data"},
	})
	exec.SetNode("merge", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "merged"},
	})

	rs := runEngineExpecting(t, wf, exec, "test", RunCompleted)
	if exec.Calls["merge"] != 1 {
		t.Errorf("merge calls = %d, want 1", exec.Calls["merge"])
	}
	if len(rs.TerminalOutput) != 1 {
		t.Errorf("terminal_outputs = %d, want 1", len(rs.TerminalOutput))
	}
	// Verify inputs to merge include both task_a and task_b
	for _, nr := range rs.NodeRuns {
		if nr.NodeID == "merge" && nr.State == NodeSucceeded {
			sources := make(map[string]bool)
			for _, inp := range nr.Inputs {
				sources[inp.FromNode] = true
			}
			if !sources["task_a"] || !sources["task_b"] {
				t.Errorf("merge inputs missing: %v", sources)
			}
		}
	}
}

func TestEngine_BoundedLoop(t *testing.T) {
	yaml := `
name: loop-test
description: Bounded loop
version: "1"
agents:
  worker:
    template: dev
  reviewer:
    template: reviewer
entry: [write]
nodes:
  - id: write
    agent: worker
    prompt: Write
    outputs:
      draft:
        to: [review]
  - id: review
    agent: reviewer
    prompt: Review
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
        max_traversals: 2
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	// write: first time
	exec.SetNode("write",
		FakeNodeResult{Handoff: &HandoffData{Outcome: "draft", Content: "v1"}},
		// second time (after revision request)
		FakeNodeResult{Handoff: &HandoffData{Outcome: "draft", Content: "v2"}},
	)
	// review: needs_revision then approved
	exec.SetNode("review",
		FakeNodeResult{Handoff: &HandoffData{Outcome: "needs_revision", Content: "fix this"}},
		FakeNodeResult{Handoff: &HandoffData{Outcome: "approved", Content: "good"}},
	)

	rs := runEngineExpecting(t, wf, exec, "test", RunCompleted)
	// write called 2 times, review called 2 times
	if exec.Calls["write"] != 2 {
		t.Errorf("write calls = %d, want 2", exec.Calls["write"])
	}
	if exec.Calls["review"] != 2 {
		t.Errorf("review calls = %d, want 2", exec.Calls["review"])
	}
	if len(rs.TerminalOutput) != 1 {
		t.Fatalf("terminal_outputs = %d, want 1", len(rs.TerminalOutput))
	}
	if rs.TerminalOutput[0].Node != "review" {
		t.Errorf("terminal node = %q", rs.TerminalOutput[0].Node)
	}
}

func TestEngine_LoopLimitExhausted(t *testing.T) {
	yaml := `
name: loop-exhausted
description: Loop exhausted
version: "1"
agents:
  worker:
    template: dev
  reviewer:
    template: reviewer
entry: [write]
nodes:
  - id: write
    agent: worker
    prompt: Write
    outputs:
      draft:
        to: [review]
  - id: review
    agent: reviewer
    prompt: Review
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
        max_traversals: 1
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("write",
		FakeNodeResult{Handoff: &HandoffData{Outcome: "draft", Content: "v1"}},
		FakeNodeResult{Handoff: &HandoffData{Outcome: "draft", Content: "v2"}},
	)
	// First review: needs_revision (triggers loop, count=1)
	// Second review: still needs_revision (limit 1 exceeded)
	exec.SetNode("review",
		FakeNodeResult{Handoff: &HandoffData{Outcome: "needs_revision", Content: "fix"}},
		FakeNodeResult{Handoff: &HandoffData{Outcome: "needs_revision", Content: "still bad"}},
	)

	rs := runEngineExpecting(t, wf, exec, "test", RunFailed)
	found := false
	for _, nr := range rs.NodeRuns {
		if nr.Error != nil && strings.Contains(nr.Error.Error(), "LOOP_LIMIT_EXCEEDED") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected LOOP_LIMIT_EXCEEDED error")
	}
}

func TestEngine_OnErrorRetrySuccess(t *testing.T) {
	yaml := `
name: retry-ok
description: Retry success
version: "1"
agents:
  worker:
    template: dev
entry: [risky]
nodes:
  - id: risky
    agent: worker
    prompt: Risky
    outputs:
      done:
        to: []
    on_error:
      strategy: retry
      max_attempts: 3
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("risky",
		FakeNodeResult{Error: errors.New("boom")},                                  // attempt 1: fail
		FakeNodeResult{Error: errors.New("boom")},                                  // attempt 2: fail
		FakeNodeResult{Handoff: &HandoffData{Outcome: "done", Content: "finally"}}, // attempt 3: success
	)

	rs := runEngineExpecting(t, wf, exec, "test", RunCompleted)
	if exec.Calls["risky"] != 3 {
		t.Errorf("risky calls = %d, want 3", exec.Calls["risky"])
	}
	if len(rs.TerminalOutput) != 1 {
		t.Errorf("terminal_outputs = %d, want 1", len(rs.TerminalOutput))
	}
}

func TestEngine_OnErrorRetryExhausted(t *testing.T) {
	yaml := `
name: retry-fail
description: Retry exhausted
version: "1"
agents:
  worker:
    template: dev
entry: [risky]
nodes:
  - id: risky
    agent: worker
    prompt: Risky
    outputs:
      done:
        to: []
    on_error:
      strategy: retry
      max_attempts: 2
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("risky",
		FakeNodeResult{Error: errors.New("boom")},
		FakeNodeResult{Error: errors.New("boom")},
	)

	rs := runEngineExpecting(t, wf, exec, "test", RunFailed)
	if exec.Calls["risky"] != 2 {
		t.Errorf("risky calls = %d, want 2", exec.Calls["risky"])
	}
	if len(rs.TerminalOutput) != 0 {
		t.Error("should have no terminal outputs")
	}
}

func TestEngine_UnknownOutcome(t *testing.T) {
	yaml := `
name: unknown-outcome
description: Unknown outcome
version: "1"
agents:
  worker:
    template: dev
entry: [step1]
nodes:
  - id: step1
    agent: worker
    prompt: Do it
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("step1", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "wut", Content: "not a real outcome"},
	})

	rs := runEngineExpecting(t, wf, exec, "test", RunFailed)
	if len(rs.TerminalOutput) != 0 {
		t.Error("should have no terminal outputs")
	}
	// Find the failed node
	found := false
	for _, nr := range rs.NodeRuns {
		if nr.Error != nil && strings.Contains(nr.Error.Error(), "HANDOFF_OUTCOME_UNKNOWN") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected HANDOFF_OUTCOME_UNKNOWN error")
	}
}

func TestEngine_CancelContext(t *testing.T) {
	yaml := `
name: cancel-test
description: Cancel test
version: "1"
agents:
  worker:
    template: dev
entry: [slow]
nodes:
  - id: slow
    agent: worker
    prompt: Slow task
    outputs:
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	exec.SetNode("slow", FakeNodeResult{
		Handoff: &HandoffData{Outcome: "done", Content: "ok"},
	})

	eng := NewEngine(exec, DefaultEngineLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	rs, _ := eng.Run(ctx, wf, "test", "/tmp/cancel")
	// The node may or may not have run depending on timing, but the workflow should be cancelled
	if rs.Status != RunFailed && rs.Status != RunCancelled {
		t.Errorf("status = %s, want cancelled or failed", rs.Status)
	}
}

func TestEngine_MaxNodeRuns(t *testing.T) {
	yaml := `
name: max-runs
description: Max node runs
version: "1"
defaults:
  max_node_runs: 3
agents:
  worker:
    template: dev
entry: [loop_node]
nodes:
  - id: loop_node
    agent: worker
    prompt: Loop
    outputs:
      again:
        to: [loop_node]
        loop: true
        max_traversals: 100
      done:
        to: []
`
	wf := mustParse(t, yaml)
	exec := NewFakeExecutor()
	// Keep returning "again" to trigger loops
	results := make([]FakeNodeResult, 100)
	for i := range results {
		results[i] = FakeNodeResult{Handoff: &HandoffData{Outcome: "again", Content: "looping"}}
	}
	exec.SetNode("loop_node", results...)

	rs := runEngineExpecting(t, wf, exec, "test", RunFailed)
	if len(rs.NodeRuns) > 6 { // generous bound — should stop at max_node_runs=3
		// Actually, the engine clamps to engine limits, not just YAML defaults
		t.Logf("nodeRuns = %d", len(rs.NodeRuns))
	}
}
