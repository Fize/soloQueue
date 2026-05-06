package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// mockAsyncTool implements AsyncTool for testing.
type mockAsyncTool struct {
	name      string
	action    *tools.AsyncAction
	actionErr error
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
	// 创建目标 Agent（模拟 L2）
	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			time.Sleep(50 * time.Millisecond)
			return "async-result", nil
		},
	}

	// 创建 AsyncTool
	asyncTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  target,
			Prompt:  "test task",
			Timeout: 5 * time.Second,
		},
	}

	// 创建 Agent with PriorityMailbox
	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
		WithTools(asyncTool),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	// 创建 ContextWindow
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleUser, "start")

	// 创建 out channel
	out := make(chan AgentEvent, 64)

	// 构造 tool calls
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

	// 调用 execToolsWithAsync
	results := a.execToolsWithAsync(context.Background(), 0, calls, out, cw)

	// 验证结果占位符（异步工具返回空字符串作为占位）
	if results[0] != "" {
		t.Errorf("results[0] = %q, want empty (placeholder)", results[0])
	}

	// 验证 asyncTurns 已注册
	a.turnMu.RLock()
	_, hasAsync := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if !hasAsync {
		t.Error("asyncTurns[0] not registered")
	}

	// 等待异步任务完成（watchDelegatedTask 会填充结果并触发 resumeTurn）
	time.Sleep(200 * time.Millisecond)

	// 验证 asyncTurns 已清理
	a.turnMu.RLock()
	_, stillHas := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if stillHas {
		t.Error("asyncTurns[0] should be cleaned up after completion")
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

	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
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

	// 等待所有异步任务完成
	time.Sleep(300 * time.Millisecond)

	// 验证两个任务都被调用
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

	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
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

	// 验证同步工具立即有结果
	if results[0] != "sync-result" {
		t.Errorf("results[0] (sync) = %q, want 'sync-result'", results[0])
	}

	// 验证异步工具是占位符
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

	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
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

	// 验证错误结果
	if results[0] == "" {
		t.Error("expected error result, got empty")
	}
	if !strings.Contains(results[0], "error:") {
		t.Errorf("results[0] = %q, want contains 'error:'", results[0])
	}
}

// ─── TestExecToolsWithAsync_PendingCount ─────────────────────────────────

func TestExecToolsWithAsync_PendingCount(t *testing.T) {
	// 验证 pending 计数正确
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

	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
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

	// 等待异步任务完成
	time.Sleep(200 * time.Millisecond)

	// 验证 asyncTurns 已清理
	a.turnMu.RLock()
	_, exists := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if exists {
		t.Error("asyncTurns[0] should be cleaned up after task completes")
	}
}

// ─── TestWatchDelegatedTask_ContextCancel ────────────────────────────────

func TestWatchDelegatedTask_ContextCancel(t *testing.T) {
	// 验证 caller context 取消时，watchDelegatedTask 填入错误结果并触发 resumeTurn
	// （新行为：不再简单删除 asyncTurns，而是通过 submitHighPriority 让 resumeTurn 处理清理和 close(out)）
	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
		WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(time.Second)

	// 创建未完成的 replyCh
	replyCh := make(chan delegateResult)

	// 创建已取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 创建 asyncTurnState
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

	// 注册到 agent
	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	// 创建 task
	task := &delegatedTask{
		correlationID: "test-1",
		targetAgentID: "l2",
		replyCh:       replyCh,
		callID:        "call_1",
		callIndex:     0,
		turn:          turnState,
	}

	// 启动 watchDelegatedTask
	done := make(chan struct{})
	go func() {
		a.watchDelegatedTask(task)
		close(done)
	}()

	// 等待 watchDelegatedTask 完成（包括 100ms grace period）
	select {
	case <-done:
		// 验证：结果已填入错误信息
		if turnState.results[0] != "error: delegation cancelled" {
			t.Errorf("results[0] = %q, want %q", turnState.results[0], "error: delegation cancelled")
		}
		// 验证：pending 已归零（触发了 resumeTurn 投递）
		if turnState.pending.Load() != 0 {
			t.Errorf("pending = %d, want 0", turnState.pending.Load())
		}
	case <-time.After(2 * time.Second):
		t.Error("watchDelegatedTask did not handle context cancel within timeout")
	}
}

// ─── TestResumeTurn_CleansUpAndContinues ────────────────────────────────

func TestResumeTurn_CleansUpAndContinues(t *testing.T) {
	// 创建 FakeLLM that returns a final answer after resume
	fakeLLM := &FakeLLM{
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

	// 创建 out channel
	out := make(chan AgentEvent, 64)

	// 创建 asyncTurnState（pending=0 表示所有异步任务已完成）
	var pending atomic.Int32
	pending.Store(0)

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
		pending:   pending,
		callerCtx: context.Background(),
	}

	// 注册到 agent
	a.turnMu.Lock()
	a.asyncTurns[0] = turnState
	a.turnMu.Unlock()

	// 调用 resumeTurn（它会清理 asyncTurns 并继续工具循环）
	a.resumeTurn(turnState)

	// 验证 asyncTurns 已清理
	a.turnMu.RLock()
	_, exists := a.asyncTurns[0]
	a.turnMu.RUnlock()

	if exists {
		t.Error("asyncTurns[0] should be deleted after resumeTurn")
	}

	// 验证 out channel 收到事件
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
	fakeLLM := &FakeLLM{
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

	// 从 iter=1 开始（模拟恢复）
	a.continueToolLoop(context.Background(), out, cw, 1)

	// 验证 LLM 被调用
	if callCount == 0 {
		t.Error("LLM should be called by continueToolLoop")
	}

	// 验证 out channel 收到 DoneEvent 或 ContentDeltaEvent
	select {
	case ev := <-out:
		t.Logf("received event: %T", ev)
	case <-time.After(time.Second):
		t.Error("no event received from continueToolLoop")
	}
}

// ─── TestEndToEnd_AsyncDelegation ────────────────────────────────────────

func TestEndToEnd_AsyncDelegation(t *testing.T) {
	// 端到端测试：L1 异步委托 -> L2 执行 -> 结果返回
	// 使用 FakeLLM with ToolCallDeltasByTurn 模拟 L1 返回 delegate tool call
	// 然后模拟 L2 返回最终结果

	target := &mockLocatable{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			return "delegation-result", nil
		},
	}

	delegateTool := &mockAsyncTool{
		name: "delegate",
		action: &tools.AsyncAction{
			Target:  target,
			Prompt:  "test task",
			Timeout: 5 * time.Second,
		},
	}

	// L1 的 FakeLLM：第一轮返回 tool call (via ToolCallDeltasByTurn)，第二轮返回最终答案
	fakeLLM := &FakeLLM{
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

	// 使用 AskStreamWithHistory 触发完整流程
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := a.AskStreamWithHistory(ctx, cw, "start delegation")
	if err != nil {
		t.Fatalf("AskStreamWithHistory: %v", err)
	}

	// 收集事件
	var events []AgentEvent
	for ev := range ch {
		events = append(events, ev)
		t.Logf("event: %T", ev)
	}

	// 验证收到 DelegationStartedEvent
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

	// 验证最终有回答
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
	// With SpawnFn → async
	dtAsync := &tools.DelegateTool{
		LeaderID: "dev",
		SpawnFn:  func(ctx context.Context, task string) (iface.Locatable, error) { return nil, nil },
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
	target := &mockLocatable{}
	dt := &tools.DelegateTool{
		LeaderID: "dev",
		SpawnFn: func(ctx context.Context, task string) (iface.Locatable, error) {
			return target, nil
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

func TestAsyncAction_TargetID(t *testing.T) {
	action := &tools.AsyncAction{Target: nil}
	if action.TargetID() != "" {
		t.Errorf("TargetID() = %q, want empty", action.TargetID())
	}

	action2 := &tools.AsyncAction{Target: &mockLocatable{}}
	if action2.TargetID() != "" {
		t.Logf("TargetID() = %q (expected empty until Locatable has ID)", action2.TargetID())
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
	fakeLLM := &FakeLLM{
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

func TestWatchDelegatedTask_GracePeriodCatchesInFlightResult(t *testing.T) {
	// Verify that when callerCtx is cancelled but the result arrives within
	// the 100ms grace period, the result is NOT lost.
	a := NewAgent(Definition{ID: "l1"}, &FakeLLM{}, newTestLogger(t),
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
		correlationID: "test-grace",
		targetAgentID: "l2",
		replyCh:       replyCh,
		callID:        "call_1",
		callIndex:     0,
		turn:          turnState,
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
