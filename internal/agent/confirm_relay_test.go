package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Test Fixtures ───────────────────────────────────────────────────────────

// confirmBubbleFixture 模拟一个 3-层代理体系来测试 confirm event 冒泡
type confirmBubbleFixture struct {
	L1Agent  *Agent
	L2Agent  *Agent
	L3Agent  *Agent
	registry *Registry
	l1LLM    *FakeLLM
	l2LLM    *FakeLLM
	l3LLM    *FakeLLM
}

func setupConfirmBubbleFixture(t *testing.T) *confirmBubbleFixture {
	reg := NewRegistry(nil)

	// L3 agent: 有一个 confirmable tool
	l3LLM := &FakeLLM{
		Responses: []string{"task completed"},
		// L3 需要先调用 dangerous_op（confirmable），后面再返回结果
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				llm.ToolCallDelta{
					Index:     0,
					ID:        "call_dangerous",
					Name:      "dangerous_op",
					Arguments: `{}`,
				},
			},
		},
	}
	confirmTool := newFakeConfirmableTool("dangerous_op", true, "Execute dangerous operation?")
	l3Agent := NewAgent(
		Definition{ID: "l3"},
		l3LLM,
		nil,
		WithTools(confirmTool),
	)
	if err := l3Agent.Start(context.Background()); err != nil {
		t.Fatalf("failed to start L3: %v", err)
	}
	if err := reg.Register(l3Agent); err != nil {
		t.Fatalf("failed to register L3: %v", err)
	}

	// L2 agent: 有 delegate tool，会调用 L3
	l2LLM := &FakeLLM{
		Responses: []string{"delegated result"},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				llm.ToolCallDelta{
					Index:     0,
					ID:        "call_1",
					Name:      "delegate_l3",
					Arguments: `{"task":"invoke dangerous op"}`,
				},
			},
		},
	}
	delegateTool := &tools.DelegateTool{
		LeaderID: "l3",
		Desc:     "L3 team leader",
		Locator:  reg,
	}
	l2Agent := NewAgent(
		Definition{ID: "l2"},
		l2LLM,
		nil,
		WithTools(delegateTool),
	)
	if err := l2Agent.Start(context.Background()); err != nil {
		t.Fatalf("failed to start L2: %v", err)
	}
	if err := reg.Register(l2Agent); err != nil {
		t.Fatalf("failed to register L2: %v", err)
	}

	// L1 agent: 顶层 caller，会委托给 L2
	l1LLM := &FakeLLM{
		Responses: []string{"final result"},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				llm.ToolCallDelta{
					Index:     0,
					ID:        "call_1",
					Name:      "delegate_l2",
					Arguments: `{"task":"delegate to L2"}`,
				},
			},
		},
	}
	delegateTool2 := &tools.DelegateTool{
		LeaderID: "l2",
		Desc:     "L2 team leader",
		Locator:  reg,
	}
	l1Agent := NewAgent(
		Definition{ID: "l1"},
		l1LLM,
		nil,
		WithTools(delegateTool2),
	)
	if err := l1Agent.Start(context.Background()); err != nil {
		t.Fatalf("failed to start L1: %v", err)
	}

	return &confirmBubbleFixture{
		L1Agent:  l1Agent,
		L2Agent:  l2Agent,
		L3Agent:  l3Agent,
		registry: reg,
		l1LLM:    l1LLM,
		l2LLM:    l2LLM,
		l3LLM:    l3LLM,
	}
}

func (f *confirmBubbleFixture) Cleanup() {
	f.L1Agent.Stop(5 * time.Second)
	f.L2Agent.Stop(5 * time.Second)
	f.L3Agent.Stop(5 * time.Second)
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestConfirmEventBubble_L3DirectConfirm 测试 L3 单层的 confirm 机制
func TestConfirmEventBubble_L3DirectConfirm(t *testing.T) {
	reg := NewRegistry(nil)

	// 创建 L3 agent 直接使用 confirmable tool
	l3LLM := &FakeLLM{
		Responses: []string{"completed"},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{
			{
				llm.ToolCallDelta{
					Index:     0,
					ID:        "call_dangerous",
					Name:      "dangerous_op",
					Arguments: `{}`,
				},
			},
		},
	}
	confirmTool := newFakeConfirmableTool("dangerous_op", true, "Execute dangerous operation?")
	l3Agent := NewAgent(
		Definition{ID: "l3"},
		l3LLM,
		nil,
		WithTools(confirmTool),
	)
	if err := l3Agent.Start(context.Background()); err != nil {
		t.Fatalf("failed to start L3: %v", err)
	}
	if err := reg.Register(l3Agent); err != nil {
		t.Fatalf("failed to register L3: %v", err)
	}
	defer l3Agent.Stop(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 从 L3 直接投递
	eventCh, err := l3Agent.AskStream(ctx, "invoke dangerous op")
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	var (
		foundConfirm bool
		gotDone      bool
		confirmID    string
	)

	for ev := range eventCh {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			t.Logf("✓ Got ToolNeedsConfirmEvent: CallID=%s, Prompt=%s", e.CallID, e.Prompt)
			confirmID = e.CallID
			foundConfirm = true

			// 确认执行
			if err := l3Agent.Confirm(e.CallID, "yes"); err != nil {
				t.Fatalf("L3.Confirm failed: %v", err)
			}

		case ToolExecDoneEvent:
			if e.Name == "dangerous_op" {
				t.Logf("✓ Dangerous op executed: %s", e.Result)
			}

		case DoneEvent:
			t.Logf("Got DoneEvent")
			gotDone = true

		case ErrorEvent:
			t.Fatalf("Got ErrorEvent: %v", e.Err)
		}
	}

	if !foundConfirm {
		t.Error("Expected ToolNeedsConfirmEvent but never received it")
	}

	if confirmID == "" {
		t.Error("Did not capture confirm CallID")
	}

	if !gotDone {
		t.Error("Expected DoneEvent from L3 event stream")
	}
}

// TestConfirmEventBubble_L2Respond_L3Executes 测试 L2→L3 的 confirm 路由
func TestConfirmEventBubble_L2Respond_L3Executes(t *testing.T) {
	fix := setupConfirmBubbleFixture(t)
	defer fix.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 从 L2 直接投递（跳过 L1），测试 L2→L3 的 confirm 路由
	eventCh, err := fix.L2Agent.AskStream(ctx, "invoke L3 task")
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	var (
		foundConfirm bool
		gotDone      bool
		confirmID    string
	)

	for ev := range eventCh {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			t.Logf("✓ Got ToolNeedsConfirmEvent: CallID=%s, Prompt=%s", e.CallID, e.Prompt)
			confirmID = e.CallID
			foundConfirm = true

			// The confirm forwarder goroutine needs a moment to register
			// the proxy slot on L2 before we can call Confirm.
			// Retry a few times to handle the inherent race.
			go func() {
				for i := 0; i < 50; i++ {
					if err := fix.L2Agent.Confirm(e.CallID, "yes"); err == nil {
						return
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Logf("Warning: L2.Confirm failed after retries for %s", e.CallID)
			}()

		case ToolExecDoneEvent:
			if e.Name == "dangerous_op" {
				t.Logf("✓ Dangerous op executed via delegation: %s", e.Result)
			}

		case DoneEvent:
			t.Logf("Got DoneEvent")
			gotDone = true

		case ErrorEvent:
			t.Logf("Got ErrorEvent: %v", e.Err)
		}
	}

	if !foundConfirm {
		t.Error("Expected ToolNeedsConfirmEvent to bubble from L3 through L2 but never received it")
	}

	if confirmID == "" {
		t.Error("Did not capture confirm CallID")
	}

	if !gotDone {
		t.Error("Expected DoneEvent from L2 event stream")
	}
}

// TestConfirmEventBubble_Denied 测试用户拒绝确认时的行为
func TestConfirmEventBubble_Denied(t *testing.T) {
	fix := setupConfirmBubbleFixture(t)
	defer fix.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventCh, err := fix.L2Agent.AskStream(ctx, "invoke L3 task")
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	var (
		foundConfirm bool
		gotDoneOrErr bool
	)

	for ev := range eventCh {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			t.Logf("✓ Got ToolNeedsConfirmEvent: CallID=%s", e.CallID)
			foundConfirm = true

			// 拒绝（传空字符串）
			if err := fix.L2Agent.Confirm(e.CallID, ""); err != nil {
				t.Logf("L2.Confirm denied: %v", err)
			}

		case ToolExecDoneEvent:
			if e.Err != nil && e.Name == "dangerous_op" {
				t.Logf("✓ Tool execution denied as expected: %v", e.Err)
				gotDoneOrErr = true
			}

		case DoneEvent:
			t.Logf("Got DoneEvent")
			gotDoneOrErr = true

		case ErrorEvent:
			t.Logf("Got ErrorEvent (expected for denied operation): %v", e.Err)
			gotDoneOrErr = true
		}
	}

	if !foundConfirm {
		t.Error("Expected ToolNeedsConfirmEvent but never received it")
	}

	if !gotDoneOrErr {
		t.Error("Expected either DoneEvent or ErrorEvent after denial")
	}
}

// TestConfirmEventBubble_ContextPropagation 验证 context key 能正确跨包传递
func TestConfirmEventBubble_ContextPropagation(t *testing.T) {
	// 测试 tools 包中的 context helper 函数
	ctx := context.Background()

	// Create a typed channel
	ch := make(chan iface.AgentEvent, 1)
	defer close(ch)

	// Inject via tools package helper
	ctxWithCh := tools.WithToolEventChannel(ctx, ch)

	// 验证可以提取出来
	extracted, ok := tools.ToolEventChannelFromCtx(ctxWithCh)
	if !ok {
		t.Fatal("Failed to extract ToolEventChannel from context")
	}

	if extracted != ch {
		t.Error("Extracted channel is not the same as injected channel")
	}

	t.Log("✓ Context key propagation works correctly")

	// 测试 ConfirmForwarder
	testForwarder := iface.ConfirmForwarder(func(ctx context.Context, callID string, child iface.Locatable) (string, error) {
		return "yes", nil
	})

	ctxWithFwd := tools.WithConfirmForwarder(ctx, testForwarder)
	extracted2, ok := tools.ConfirmForwarderFromCtx(ctxWithFwd)
	if !ok {
		t.Fatal("Failed to extract ConfirmForwarder from context")
	}

	if extracted2 == nil {
		t.Error("Extracted forwarder is nil")
	}

	t.Log("✓ ConfirmForwarder context propagation works correctly")
}

// TestConfirmEventBubble_EventRelay 验证中继通道能将 AgentEvent 正确转换为 interface{}
func TestConfirmEventBubble_EventRelay(t *testing.T) {
	fix := setupConfirmBubbleFixture(t)
	defer fix.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 从 L2 投递一个简单的请求
	eventCh, err := fix.L2Agent.AskStream(ctx, "simple task")
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	var eventTypes []string
	for ev := range eventCh {
		eventTypes = append(eventTypes, fmt.Sprintf("%T", ev))
	}

	// 应该收到各种事件类型
	if len(eventTypes) == 0 {
		t.Error("No events received")
	} else {
		t.Logf("✓ Received %d events: %v", len(eventTypes), eventTypes)
	}
}

// TestConfirmEventBubble_LocatableAdapter 验证 LocatableAdapter 能正确适配接口
func TestConfirmEventBubble_LocatableAdapter(t *testing.T) {
	fix := setupConfirmBubbleFixture(t)
	defer fix.Cleanup()

	// 通过 Registry.Locate 获取 LocatableAdapter
	locatable, ok := fix.registry.Locate("l3")
	if !ok {
		t.Fatal("Failed to locate L3")
	}

	// Verify it implements iface.Locatable
	var _ iface.Locatable = locatable

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 调用 AskStream 应该返回 interface{} 通道
	eventCh, err := locatable.AskStream(ctx, "test prompt")
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	if eventCh == nil {
		t.Error("AskStream returned nil channel")
	}

	// 收集一些事件以验证转换成功
	count := 0
	for ev := range eventCh {
		_ = ev // interface{} 类型事件
		count++
		if count > 10 {
			break
		}
	}

	if count == 0 {
		t.Error("No events received through LocatableAdapter")
	} else {
		t.Logf("✓ LocatableAdapter successfully converted %d AgentEvent to interface{}", count)
	}
}
