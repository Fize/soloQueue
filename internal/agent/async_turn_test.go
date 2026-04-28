package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── mock types for testing ─────────────────────────────────────────────────

// mockAsyncTool 实现 AsyncTool 接口
type mockAsyncTool struct {
	name      string
	action    *tools.AsyncAction
	actionErr error
}

func (m *mockAsyncTool) Name() string                        { return m.name }
func (m *mockAsyncTool) Description() string                 { return "mock async tool" }
func (m *mockAsyncTool) Parameters() json.RawMessage          { return json.RawMessage(`{}`) }
func (m *mockAsyncTool) Execute(ctx context.Context, args string) (string, error) {
	return "sync-result", nil
}
func (m *mockAsyncTool) ExecuteAsync(ctx context.Context, args string) (*tools.AsyncAction, error) {
	return m.action, m.actionErr
}

// mockAskTarget 实现 Locatable 接口，用于测试
type mockAskTarget struct {
	askFunc func(ctx context.Context, prompt string) (string, error)
}

func (m *mockAskTarget) Ask(ctx context.Context, prompt string) (string, error) {
	if m.askFunc != nil {
		return m.askFunc(ctx, prompt)
	}
	return "mock-response", nil
}

func (m *mockAskTarget) AskStream(ctx context.Context, prompt string) (<-chan interface{}, error) {
	// Create a channel and send a DoneEvent with the mocked result
	ch := make(chan interface{}, 1)
	go func() {
		defer close(ch)
		result, err := m.Ask(ctx, prompt)
		if err != nil {
			ch <- ErrorEvent{Err: err}
		} else {
			ch <- DoneEvent{Content: result}
		}
	}()
	return ch, nil
}

func (m *mockAskTarget) Confirm(callID string, choice string) error {
	return nil // Mock implementation - no-op
}

// ─── TestExecToolsWithAsync_SingleAsyncTool ────────────────────────────────

func TestExecToolsWithAsync_SingleAsyncTool(t *testing.T) {
	// 创建目标 Agent（模拟 L2）
	target := &mockAskTarget{
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
	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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

	target1 := &mockAskTarget{
		askFunc: func(ctx context.Context, prompt string) (string, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return "result1", nil
		},
	}
	target2 := &mockAskTarget{
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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
			Target:  &mockAskTarget{askFunc: func(ctx context.Context, prompt string) (string, error) { return "async", nil }},
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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
	target := &mockAskTarget{
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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
	// 验证 caller context 取消时，watchDelegatedTask 会清理 asyncTurns
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
		cw:        ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer()),
		iter:      0,
		toolCalls: []llm.ToolCall{},
		results:   make([]string, 1),
		pending:   pending,
		callerCtx: ctx,
	}

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

	// 等待 watchDelegatedTask 处理 context 取消
	select {
	case <-done:
		// 验证 asyncTurns 已清理
		a.turnMu.RLock()
		_, exists := a.asyncTurns[0]
		a.turnMu.RUnlock()
		if exists {
			t.Error("asyncTurns[0] should be deleted after context cancel")
		}
	case <-time.After(time.Second):
		t.Error("watchDelegatedTask did not handle context cancel")
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "you are helpful")
	cw.Push(ctxwin.RoleUser, "start")

	// 创建 out channel
	out := make(chan AgentEvent, 64)

	// 创建 asyncTurnState（pending=0 表示所有异步任务已完成）
	var pending atomic.Int32
	pending.Store(0)

	turnState := &asyncTurnState{
		agentID:   "l1",
		out:       out,
		cw:        cw,
		iter:      0,
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
		results: []string{"async-result"},
		pending: pending,
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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

	target := &mockAskTarget{
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

	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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

func (m *mockSyncTool) Name() string                        { return m.name }
func (m *mockSyncTool) Description() string                 { return "mock sync tool" }
func (m *mockSyncTool) Parameters() json.RawMessage          { return json.RawMessage(`{}`) }
func (m *mockSyncTool) Execute(ctx context.Context, args string) (string, error) {
	return m.result, nil
}
