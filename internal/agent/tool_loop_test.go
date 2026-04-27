package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Test helpers ────────────────────────────────────────────────────────────

// startedAgentWithTools 启动一个带 tools 的 agent，自动 Stop
func startedAgentWithTools(t *testing.T, fake *FakeLLM, ts ...tools.Tool) *Agent {
	t.Helper()
	a := NewAgent(Definition{ID: "a1"}, fake, nil, WithTools(ts...))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	return a
}

// ─── 无 tools：循环第一轮就退出（向后兼容）──────────────────────────────

func TestAgent_Ask_NoTools_SingleChat(t *testing.T) {
	// 未 WithTools；FakeLLM 返回单条 Responses；Ask 行为完全等价旧版
	fake := &FakeLLM{Responses: []string{"final"}}
	a := startedAgent(t, fake)

	reply, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "final" {
		t.Errorf("reply = %q, want final", reply)
	}
	if fake.CallCount() != 1 {
		t.Errorf("LLM called %d times, want 1", fake.CallCount())
	}
}

func TestAgent_ToolSpecs_NilSafe(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	if specs := a.ToolSpecs(); specs != nil {
		t.Errorf("ToolSpecs without WithTools should be nil, got %v", specs)
	}
}

// ─── 单次 tool_call → 最终答复 ───────────────────────────────────────────

func TestAgent_Ask_SingleToolCall_ThenFinal(t *testing.T) {
	echo := newFakeTool("echo")
	echo.result = `{"ok":true}`

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID: "call_1", Type: "function",
			Function: llm.FunctionCall{Name: "echo", Arguments: `{"msg":"hello"}`},
		}}},
		Responses: []string{"final answer"},
	}
	a := startedAgentWithTools(t, fake, echo)

	reply, err := a.Ask(context.Background(), "please echo")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "final answer" {
		t.Errorf("reply = %q, want 'final answer'", reply)
	}
	if echo.CallCount() != 1 {
		t.Errorf("tool called %d times, want 1", echo.CallCount())
	}
	// LLM 被调 2 次（一次要工具、一次最终答复）
	if total := fake.CallCount() + fake.ToolCallCount(); total != 2 {
		t.Errorf("LLM total calls = %d, want 2", total)
	}
}

// ─── 一轮多个 tool_call：顺序执行 ────────────────────────────────────────

func TestAgent_Ask_MultipleToolCalls_PerTurn(t *testing.T) {
	a1 := newFakeTool("t1")
	a1.result = "r1"
	a2 := newFakeTool("t2")
	a2.result = "r2"

	var capturedMsgs []LLMMessage
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "c1", Function: llm.FunctionCall{Name: "t1", Arguments: `{}`}},
			{ID: "c2", Function: llm.FunctionCall{Name: "t2", Arguments: `{}`}},
		}},
		Responses: []string{"done"},
		Hook: func(req LLMRequest) {
			// 最后一次 Chat 的 msgs 应含 2 条 role=tool
			capturedMsgs = req.Messages
		},
	}
	a := startedAgentWithTools(t, fake, a1, a2)

	reply, _ := a.Ask(context.Background(), "go")
	if reply != "done" {
		t.Errorf("reply = %q", reply)
	}
	if a1.CallCount() != 1 || a2.CallCount() != 1 {
		t.Errorf("tool calls: t1=%d t2=%d, want 1,1", a1.CallCount(), a2.CallCount())
	}

	// 验证第二次 Chat 的 msgs：倒数 3 条应是 assistant(tool_calls) + tool(c1) + tool(c2)
	if len(capturedMsgs) < 4 {
		t.Fatalf("captured msgs = %d, want >= 4", len(capturedMsgs))
	}
	asst := capturedMsgs[len(capturedMsgs)-3]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 2 {
		t.Errorf("assistant msg wrong: %+v", asst)
	}
	tool1 := capturedMsgs[len(capturedMsgs)-2]
	tool2 := capturedMsgs[len(capturedMsgs)-1]
	if tool1.Role != "tool" || tool1.ToolCallID != "c1" || tool1.Content != "r1" {
		t.Errorf("tool msg 1 wrong: %+v", tool1)
	}
	if tool2.Role != "tool" || tool2.ToolCallID != "c2" || tool2.Content != "r2" {
		t.Errorf("tool msg 2 wrong: %+v", tool2)
	}
}

// ─── Tool 未找到：反馈 "error: ..." 给 LLM，不中断循环 ──────────────────

func TestAgent_Ask_ToolNotFound_FedBackAsError(t *testing.T) {
	var capturedContent string
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID: "call_1",
			Function: llm.FunctionCall{Name: "ghost", Arguments: `{}`},
		}}},
		Responses: []string{"ok"},
		Hook: func(req LLMRequest) {
			// 第 2 次 Chat 的最后一条消息应是 tool role 带 "error: ..."
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
				capturedContent = req.Messages[n-1].Content
			}
		},
	}
	a := startedAgentWithTools(t, fake) // 无工具注册

	reply, err := a.Ask(context.Background(), "call ghost")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q", reply)
	}
	if !strings.HasPrefix(capturedContent, "error: ") {
		t.Errorf("tool result not prefixed with 'error: ': %q", capturedContent)
	}
	if !strings.Contains(capturedContent, "tool not found") {
		t.Errorf("tool result missing 'tool not found': %q", capturedContent)
	}
}

// ─── Tool Execute 错误：反馈给 LLM，不中断循环 ─────────────────────────

func TestAgent_Ask_ToolExecError_FedBack(t *testing.T) {
	boom := newFakeTool("boom")
	boom.err = errors.New("kaboom")

	var capturedContent string
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID: "c1", Function: llm.FunctionCall{Name: "boom", Arguments: `{}`},
		}}},
		Responses: []string{"recovered"},
		Hook: func(req LLMRequest) {
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" {
				capturedContent = req.Messages[n-1].Content
			}
		},
	}
	a := startedAgentWithTools(t, fake, boom)

	reply, err := a.Ask(context.Background(), "")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "recovered" {
		t.Errorf("reply = %q", reply)
	}
	if capturedContent != "error: kaboom" {
		t.Errorf("tool result = %q, want 'error: kaboom'", capturedContent)
	}
	if boom.CallCount() != 1 {
		t.Errorf("boom called %d times, want 1", boom.CallCount())
	}
}

// ─── MaxIterations 兜底 ────────────────────────────────────────────────

func TestAgent_Ask_MaxIterations(t *testing.T) {
	// LLM 无限要 tool_calls；MaxIterations=3 应在 3 轮后抛 ErrMaxIterations
	tool := newFakeTool("loop")
	turns := make([][]llm.ToolCall, 10)
	for i := range turns {
		turns[i] = []llm.ToolCall{{
			ID: fmt.Sprintf("c%d", i),
			Function: llm.FunctionCall{Name: "loop", Arguments: `{}`},
		}}
	}
	fake := &FakeLLM{ToolCallsByTurn: turns}

	a := NewAgent(Definition{ID: "a1", MaxIterations: 3}, fake, nil, WithTools(tool))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	reply, err := a.Ask(context.Background(), "")
	if !errors.Is(err, ErrMaxIterations) {
		t.Errorf("err = %v, want ErrMaxIterations", err)
	}
	if reply != "" {
		t.Errorf("reply should be empty on max iter, got %q", reply)
	}
	if fake.ToolCallCount() != 3 {
		t.Errorf("LLM called %d times, want 3", fake.ToolCallCount())
	}
	if tool.CallCount() != 3 {
		t.Errorf("tool called %d times, want 3", tool.CallCount())
	}
}

func TestAgent_Ask_DefaultMaxIterations(t *testing.T) {
	// 不设 MaxIterations，默认 DefaultMaxIterations (10)
	tool := newFakeTool("loop")
	turns := make([][]llm.ToolCall, 20)
	for i := range turns {
		turns[i] = []llm.ToolCall{{
			ID: fmt.Sprintf("c%d", i),
			Function: llm.FunctionCall{Name: "loop", Arguments: `{}`},
		}}
	}
	fake := &FakeLLM{ToolCallsByTurn: turns}

	a := startedAgentWithTools(t, fake, tool)
	_, err := a.Ask(context.Background(), "")
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("err = %v, want ErrMaxIterations", err)
	}
	if fake.ToolCallCount() != DefaultMaxIterations {
		t.Errorf("LLM called %d times, want %d", fake.ToolCallCount(), DefaultMaxIterations)
	}
}

// ─── ctx cancel 中断循环 ──────────────────────────────────────────────

func TestAgent_Ask_CtxCancel_BeforeTool(t *testing.T) {
	// 用一个会 block 的 tool，从中间取消 ctx
	blockedTool := newBlockingTool()
	defer close(blockedTool.ch)

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "block", Arguments: `{}`},
		}}},
		Responses: []string{"unreached"},
	}
	a := startedAgentWithTools(t, fake, blockedTool)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Go routine：等 tool 开始执行后 cancel
	go func() {
		// 等 tool 进入 Execute
		<-blockedTool.started
		cancel()
	}()

	_, err := a.Ask(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// ─── LLM error 透传 ───────────────────────────────────────────────────

func TestAgent_Ask_LLMError_Propagates(t *testing.T) {
	want := errors.New("llm down")
	fake := &FakeLLM{Err: want}
	a := startedAgent(t, fake)

	_, err := a.Ask(context.Background(), "")
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

// ─── WithTools panic 场景 ─────────────────────────────────────────────

func TestAgent_WithTools_DuplicatePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("WithTools with duplicate names should panic")
		}
	}()
	t1 := newFakeTool("same")
	t2 := newFakeTool("same")
	_ = NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil, WithTools(t1, t2))
}

func TestAgent_WithTools_EmptyIsNoop(t *testing.T) {
	// WithTools() 无参不应分配 registry
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil, WithTools())
	if a.caps != nil {
		t.Error("empty WithTools should not allocate registry")
	}
}

// ─── End-to-end：日志中 trace_id / actor_id / tool_name 完整串联 ────────

func TestAgent_Ask_ToolLog_HasTraceAndActorID(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "team", "sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	echo := newFakeTool("echo")
	echo.result = "ok"
	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID: "call_1",
			Function: llm.FunctionCall{Name: "echo", Arguments: `{"m":"hi"}`},
		}}},
		Responses: []string{"final"},
	}

	a := NewAgent(
		Definition{ID: "demo-agent", ModelID: "fake-model"},
		fake,
		log,
		WithTools(echo),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	// 注入已知 trace_id 验证贯穿
	ctx := logger.WithTraceID(context.Background(), "trace-xyz")
	reply, err := a.Ask(ctx, "echo hi")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "final" {
		t.Errorf("reply = %q", reply)
	}

	_ = log.Close() // flush

	base := filepath.Join(dir, "logs", "sessions", "team", "sess")

	// tool.jsonl：应含 tool_name / tool_call_id / trace_id / actor_id
	toolData, err := os.ReadFile(filepath.Join(base, "tool.jsonl"))
	if err != nil {
		t.Fatalf("read tool.jsonl: %v", err)
	}
	for _, need := range [][]byte{
		[]byte(`"tool_name":"echo"`),
		[]byte(`"tool_call_id":"call_1"`),
		[]byte(`"trace_id":"trace-xyz"`),
		[]byte(`"actor_id":"demo-agent"`),
	} {
		if !bytes.Contains(toolData, need) {
			t.Errorf("tool.jsonl missing %s\ncontent:\n%s", need, toolData)
		}
	}

	// llm.jsonl：应有 2 条 "llm chat done"（iter=0 含 tool_calls=1，iter=1 tool_calls=0）
	llmData, err := os.ReadFile(filepath.Join(base, "llm.jsonl"))
	if err != nil {
		t.Fatalf("read llm.jsonl: %v", err)
	}
	if n := bytes.Count(llmData, []byte(`"msg":"llm chat done"`)); n != 2 {
		t.Errorf("llm.jsonl has %d 'chat done' lines, want 2\ncontent:\n%s", n, llmData)
	}
}

// ─── blockingTool ────────────────────────────────────────────────────────────

// blockingTool 在 Execute 里 block 直到 ctx 取消或 ch close
type blockingTool struct {
	started chan struct{}
	once    sync.Once
	ch      chan struct{}
}

func (b *blockingTool) Name() string                { return "block" }
func (b *blockingTool) Description() string         { return "blocks forever" }
func (b *blockingTool) Parameters() json.RawMessage { return nil }
func (b *blockingTool) Execute(ctx context.Context, _ string) (string, error) {
	b.once.Do(func() {
		if b.started != nil {
			close(b.started)
		}
	})
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-b.ch:
		return "unblocked", nil
	}
}

func newBlockingTool() *blockingTool {
	return &blockingTool{
		started: make(chan struct{}),
		ch:      make(chan struct{}),
	}
}
