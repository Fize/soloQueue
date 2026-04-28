package agent

import (
	"context"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Fixtures ──────────────────────────────────────────────────────────────

// mockLocatable 是一个可模拟的 Locatable，用于测试
type mockLocatable struct {
	askFunc      func(ctx context.Context, prompt string) (string, error)
	askStreamFunc func(ctx context.Context, prompt string) (<-chan interface{}, error)
}

func (m *mockLocatable) Ask(ctx context.Context, prompt string) (string, error) {
	if m.askFunc != nil {
		return m.askFunc(ctx, prompt)
	}
	return "mock-result", nil
}

func (m *mockLocatable) AskStream(ctx context.Context, prompt string) (<-chan interface{}, error) {
	if m.askStreamFunc != nil {
		return m.askStreamFunc(ctx, prompt)
	}
	ch := make(chan interface{}, 1)
	ch <- &DoneEvent{Content: "mock-result"}
	close(ch)
	return ch, nil
}

func (m *mockLocatable) Confirm(callID string, choice string) error {
	return nil
}

// asyncFakeTool 实现 AsyncTool 接口，用于测试
type asyncFakeTool struct {
	name       string
	action     *tools.AsyncAction
	actionErr  error
	execResult string
	execErr    error
}

func (a *asyncFakeTool) Name() string                { return a.name }
func (a *asyncFakeTool) Description() string         { return "async fake " + a.name }
func (a *asyncFakeTool) Parameters() []byte          { return []byte(`{"type":"object"}`) }
func (a *asyncFakeTool) Execute(ctx context.Context, args string) (string, error) {
	return a.execResult, a.execErr
}
func (a *asyncFakeTool) ExecuteAsync(ctx context.Context, args string) (*tools.AsyncAction, error) {
	return a.action, a.actionErr
}

// ─── PriorityMailbox ───────────────────────────────────────────────────────

func TestPriorityMailbox_BasicOperations(t *testing.T) {
	pm := NewPriorityMailbox()

	var normalCalled, highCalled bool
	normalJob := func(ctx context.Context) { normalCalled = true }
	highJob := func(ctx context.Context) { highCalled = true }

	pm.SubmitNormal(normalJob)
	pm.SubmitHigh(highJob)

	// High priority should be available first
	select {
	case pj := <-pm.HighCh():
		if pj.priority != PriorityHigh {
			t.Errorf("priority = %d, want High", pj.priority)
		}
		pj.job(context.Background())
		if !highCalled {
			t.Error("high job was not called")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for high priority job")
	}

	// Normal priority should be available next
	select {
	case pj := <-pm.NormalCh():
		if pj.priority != PriorityNormal {
			t.Errorf("priority = %d, want Normal", pj.priority)
		}
		pj.job(context.Background())
		if !normalCalled {
			t.Error("normal job was not called")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for normal priority job")
	}
}

func TestPriorityMailbox_Channels(t *testing.T) {
	pm := NewPriorityMailbox()

	// Verify channels are non-nil
	if pm.HighCh() == nil {
		t.Error("HighCh() is nil")
	}
	if pm.NormalCh() == nil {
		t.Error("NormalCh() is nil")
	}

	// Verify capacity (buffered)
	pm.SubmitHigh(func(ctx context.Context) {})
	// should not block for first few items (buffer cap=4)
}

// ─── Supervisor ────────────────────────────────────────────────────────────

func TestSupervisor_New(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	if sv == nil {
		t.Fatal("NewSupervisor returned nil")
	}
	if sv.ChildCount() != 0 {
		t.Errorf("initial child count = %d, want 0", sv.ChildCount())
	}
	if len(sv.Children()) != 0 {
		t.Error("initial children should be empty")
	}
}

func TestSupervisor_SpawnChild_NilFactory(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	_, err := sv.SpawnChild(context.Background(), AgentTemplate{ID: "child1"})
	if err == nil {
		t.Error("expected error when factory is nil")
	}
}

func TestSupervisor_ReapChild_NotFound(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	err := sv.ReapChild("nonexistent", time.Second)
	if err == nil {
		t.Error("expected error when child not found")
	}
}

func TestSupervisor_ReapAll_Empty(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	errs := sv.ReapAll(time.Second)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %d", len(errs))
	}
}

func TestSupervisor_SpawnFnFor(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	tmpl := AgentTemplate{ID: "test-child", Name: "Test Child"}
	spawnFn := sv.SpawnFnFor(tmpl)

	if spawnFn == nil {
		t.Fatal("SpawnFnFor returned nil")
	}

	// SpawnFn with nil factory should return error
	_, err := spawnFn(context.Background(), "test task")
	if err == nil {
		t.Error("expected error when factory is nil")
	}
}

func TestSupervisor_SpawnFnForID_NotFound(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	templates := []AgentTemplate{
		{ID: "existing", Name: "Existing"},
	}

	// Existing template
	spawnFn := sv.SpawnFnForID("existing", templates)
	if spawnFn == nil {
		t.Fatal("SpawnFnForID returned nil for existing template")
	}

	// Non-existing template
	spawnFnNotFound := sv.SpawnFnForID("missing", templates)
	if spawnFnNotFound == nil {
		t.Fatal("SpawnFnForID returned nil for missing template")
	}

	_, err := spawnFnNotFound(context.Background(), "task")
	if err == nil {
		t.Error("expected error for missing template")
	}
}

// ─── PriorityMailbox.Len ──────────────────────────────────────────────────

func TestPriorityMailbox_Len_Empty(t *testing.T) {
	pm := NewPriorityMailbox()
	high, normal := pm.Len()
	if high != 0 || normal != 0 {
		t.Errorf("Len() = (%d, %d), want (0, 0)", high, normal)
	}
}

func TestPriorityMailbox_Len_WithItems(t *testing.T) {
	pm := NewPriorityMailbox()
	pm.SubmitHigh(func(ctx context.Context) {})
	pm.SubmitHigh(func(ctx context.Context) {})
	pm.SubmitNormal(func(ctx context.Context) {})

	high, normal := pm.Len()
	if high != 2 {
		t.Errorf("high = %d, want 2", high)
	}
	if normal != 1 {
		t.Errorf("normal = %d, want 1", normal)
	}

	// Drain one high, check again
	<-pm.HighCh()
	high2, _ := pm.Len()
	if high2 != 1 {
		t.Errorf("after drain high = %d, want 1", high2)
	}
}

// ─── Supervisor.Agent() ──────────────────────────────────────────────────

func TestSupervisor_Agent(t *testing.T) {
	fakeLLM := &FakeLLM{Responses: []string{"ok"}}
	a := NewAgent(Definition{ID: "l2-agent", Name: "DevLead"}, fakeLLM, nil)
	sv := NewSupervisor(a, nil, nil)

	got := sv.Agent()
	if got != a {
		t.Errorf("Agent() returned different pointer: got %p, want %p", got, a)
	}
	if got.Def.ID != "l2-agent" {
		t.Errorf("Agent().Def.ID = %q, want %q", got.Def.ID, "l2-agent")
	}
}

// ─── Agent.PendingDelegations ────────────────────────────────────────────

func TestAgent_PendingDelegations_Initial(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	if got := a.PendingDelegations(); got != 0 {
		t.Errorf("PendingDelegations() = %d, want 0", got)
	}
}

func TestAgent_PendingDelegations_WithAsyncTurns(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)

	// 直接注入 asyncTurns 模拟异步委托
	a.turnMu.Lock()
	a.asyncTurns[1] = &asyncTurnState{iter: 1}
	a.asyncTurns[3] = &asyncTurnState{iter: 3}
	a.turnMu.Unlock()

	if got := a.PendingDelegations(); got != 2 {
		t.Errorf("PendingDelegations() = %d, want 2", got)
	}

	// 移除一个
	a.turnMu.Lock()
	delete(a.asyncTurns, 1)
	a.turnMu.Unlock()

	if got := a.PendingDelegations(); got != 1 {
		t.Errorf("PendingDelegations() after delete = %d, want 1", got)
	}
}

// ─── Agent.MailboxDepth ──────────────────────────────────────────────────

func TestAgent_MailboxDepth_NotStarted(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	high, normal := a.MailboxDepth()
	if high != 0 || normal != 0 {
		t.Errorf("MailboxDepth() = (%d, %d), want (0, 0)", high, normal)
	}
}

func TestAgent_MailboxDepth_WithPriorityMailbox(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil, WithPriorityMailbox())
	// PriorityMailbox 创建后未投递，应为 (0, 0)
	high, normal := a.MailboxDepth()
	if high != 0 || normal != 0 {
		t.Errorf("MailboxDepth() = (%d, %d), want (0, 0)", high, normal)
	}

	// 通过 PriorityMailbox 直接投递（不通过 submit，因为 agent 未 Start）
	a.priorityMailbox.SubmitHigh(func(ctx context.Context) {})
	a.priorityMailbox.SubmitNormal(func(ctx context.Context) {})
	a.priorityMailbox.SubmitNormal(func(ctx context.Context) {})

	high, normal = a.MailboxDepth()
	if high != 1 {
		t.Errorf("high = %d, want 1", high)
	}
	if normal != 2 {
		t.Errorf("normal = %d, want 2", normal)
	}
}

func TestAgent_MailboxDepth_WithRegularMailbox(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{Responses: []string{"r"}, Delay: time.Second}, nil,
		WithMailboxCap(4))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })

	// 占住 run goroutine
	go func() { _, _ = a.Ask(context.Background(), "blocking") }()
	waitFor(t, 500*time.Millisecond, func() bool { return a.State() == StateProcessing })

	// 投递几个到 mailbox（会排队）
	for i := 0; i < 2; i++ {
		go func() { _, _ = a.Ask(context.Background(), "queued") }()
	}
	time.Sleep(50 * time.Millisecond) // 等它们入队

	high, normal := a.MailboxDepth()
	if high != 0 {
		t.Errorf("high = %d, want 0 (regular mailbox)", high)
	}
	if normal < 1 {
		t.Errorf("normal = %d, want ≥ 1", normal)
	}
}

// ─── Agent Options ─────────────────────────────────────────────────────────

func TestWithEphemeral(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil, WithEphemeral())
	if !a.IsEphemeral() {
		t.Error("IsEphemeral() = false, want true")
	}
	if a.mailboxCap != 1 {
		t.Errorf("mailboxCap = %d, want 1", a.mailboxCap)
	}
}

func TestWithEphemeral_PreservesCustomCap(t *testing.T) {
	// If mailboxCap is already set explicitly, WithEphemeral should not override
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil,
		WithMailboxCap(10),
		WithEphemeral(),
	)
	if a.mailboxCap != 10 {
		t.Errorf("mailboxCap = %d, want 10", a.mailboxCap)
	}
}

func TestWithPriorityMailbox(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil, WithPriorityMailbox())
	if a.priorityMailbox == nil {
		t.Fatal("priorityMailbox is nil")
	}
}

func TestAgent_DefaultNotEphemeral(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	if a.IsEphemeral() {
		t.Error("default IsEphemeral() = true, want false")
	}
}

// ─── AsyncTool type assertion ──────────────────────────────────────────────

func TestDelegateTool_IsAsync(t *testing.T) {
	// With SpawnFn → async
	dtAsync := &tools.DelegateTool{
		LeaderID: "dev",
		SpawnFn:  func(ctx context.Context, task string) (tools.Locatable, error) { return nil, nil },
	}
	if !dtAsync.IsAsync() {
		t.Error("IsAsync() = false, want true when SpawnFn is set")
	}

	// Without SpawnFn → sync
	dtSync := &tools.DelegateTool{
		LeaderID: "dev",
		Locator:  nil,
	}
	if dtSync.IsAsync() {
		t.Error("IsAsync() = true, want false when SpawnFn is nil")
	}
}

func TestDelegateTool_ExecuteAsync(t *testing.T) {
	mockTarget := &mockLocatable{}
	dt := &tools.DelegateTool{
		LeaderID: "dev",
		SpawnFn: func(ctx context.Context, task string) (tools.Locatable, error) {
			return mockTarget, nil
		},
		Timeout: 5 * time.Minute,
	}

	action, err := dt.ExecuteAsync(context.Background(), `{"task":"test"}`)
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
	if action.Timeout != 5*time.Minute {
		t.Errorf("action.Timeout = %v, want 5m", action.Timeout)
	}
}

func TestDelegateTool_ExecuteAsync_InvalidArgs(t *testing.T) {
	dt := &tools.DelegateTool{LeaderID: "dev"}

	// Empty task
	_, err := dt.ExecuteAsync(context.Background(), `{"task":""}`)
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
	dt := &tools.DelegateTool{LeaderID: "dev"}

	_, err := dt.ExecuteAsync(context.Background(), `{"task":"test"}`)
	if err == nil {
		t.Error("expected error when no Locator or SpawnFn configured")
	}
}

// ─── AsyncAction ───────────────────────────────────────────────────────────

func TestAsyncAction_TargetID(t *testing.T) {
	// With nil Target
	action := &tools.AsyncAction{Target: nil}
	if action.TargetID() != "" {
		t.Errorf("TargetID() = %q, want empty", action.TargetID())
	}

	// With mock Target
	action2 := &tools.AsyncAction{Target: &mockLocatable{}}
	if action2.TargetID() != "" {
		// Currently returns empty since Locatable has no ID method
		t.Logf("TargetID() = %q (expected empty until Locatable has ID)", action2.TargetID())
	}
}

// ─── Events ────────────────────────────────────────────────────────────────

func TestDelegationEvents_AreAgentEvents(t *testing.T) {
	// Verify new event types implement AgentEvent
	var _ AgentEvent = DelegationStartedEvent{}
	var _ AgentEvent = DelegationCompletedEvent{}

	// Verify they have the marker method (compile-time check)
	ev1 := DelegationStartedEvent{Iter: 1, NumTasks: 2}
	ev2 := DelegationCompletedEvent{Iter: 1, TargetAgentID: "dev"}

	// Type switch should work (via AgentEvent interface)
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
