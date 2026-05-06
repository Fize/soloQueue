package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

func TestFakeLLM_RoundRobin(t *testing.T) {
	f := &FakeLLM{Responses: []string{"a", "b", "c"}}
	ctx := context.Background()

	got := []string{}
	for i := 0; i < 6; i++ {
		resp, err := f.Chat(ctx, LLMRequest{})
		if err != nil {
			t.Fatalf("Chat %d: %v", i, err)
		}
		got = append(got, resp.Content)
	}
	want := []string{"a", "b", "c", "a", "b", "c"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("iter %d: got %q, want %q", i, got[i], want[i])
		}
	}
	if f.CallCount() != 6 {
		t.Errorf("CallCount = %d, want 6", f.CallCount())
	}
}

func TestFakeLLM_EmptyResponses(t *testing.T) {
	f := &FakeLLM{}
	resp, err := f.Chat(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("content = %q, want empty", resp.Content)
	}
}

func TestFakeLLM_Delay(t *testing.T) {
	f := &FakeLLM{Responses: []string{"x"}, Delay: 80 * time.Millisecond}
	start := time.Now()
	_, err := f.Chat(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 70*time.Millisecond {
		t.Errorf("Chat returned in %v, want ≥ 70ms", elapsed)
	}
}

func TestFakeLLM_Delay_CancelledCtx(t *testing.T) {
	f := &FakeLLM{Responses: []string{"x"}, Delay: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := f.Chat(ctx, LLMRequest{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on cancelled ctx")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel didn't abort promptly: took %v", elapsed)
	}
}

func TestFakeLLM_AlreadyCancelledCtx(t *testing.T) {
	f := &FakeLLM{Responses: []string{"x"}} // 无 Delay
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.Chat(ctx, LLMRequest{})
	if err == nil {
		t.Fatal("expected error on pre-cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestFakeLLM_Err(t *testing.T) {
	myErr := errors.New("simulated llm failure")
	f := &FakeLLM{Err: myErr}

	_, err := f.Chat(context.Background(), LLMRequest{})
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
}

func TestFakeLLM_Err_OverridesCtx(t *testing.T) {
	// Err 情况仍尊重 ctx 取消
	myErr := errors.New("boom")
	f := &FakeLLM{Err: myErr}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.Chat(ctx, LLMRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	// 优先返回 ctx.Err（Err 还没到）
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want Canceled (ctx takes priority)", err)
	}
}

func TestFakeLLM_Hook(t *testing.T) {
	var seen LLMRequest
	var once sync.Once
	f := &FakeLLM{
		Responses: []string{"hi"},
		Hook: func(req LLMRequest) {
			once.Do(func() { seen = req })
		},
	}

	req := LLMRequest{
		Model: "deepseek-chat",
		Messages: []LLMMessage{
			{Role: "system", Content: "you are helpful"},
			{Role: "user", Content: "hello"},
		},
		Temperature: 0.7,
		MaxTokens:   1024,
	}
	_, err := f.Chat(context.Background(), req)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if seen.Model != "deepseek-chat" {
		t.Errorf("hook.Model = %q", seen.Model)
	}
	if len(seen.Messages) != 2 {
		t.Fatalf("hook.Messages len = %d, want 2", len(seen.Messages))
	}
	if seen.Messages[0].Role != "system" || seen.Messages[1].Role != "user" {
		t.Errorf("hook messages order wrong: %+v", seen.Messages)
	}
}

func TestFakeLLM_Concurrent(t *testing.T) {
	f := &FakeLLM{Responses: []string{"a", "b", "c"}}
	const N = 200

	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.Chat(context.Background(), LLMRequest{})
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if got := int32(f.CallCount()); got != int32(N) {
		// 用 atomic 只是为了 race detector；实际用 CallCount
		t.Errorf("CallCount = %d, want %d", got, N)
	}
	_ = atomic.Int32{} // silence import if other uses removed
}

// ─── FakeLLM.ChatStream ──────────────────────────────────────────────────────

func TestFakeLLM_ChatStream_DeltaThenDone(t *testing.T) {
	f := &FakeLLM{Responses: []string{"hello"}}
	ch, err := f.ChatStream(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}

	var got []llm.Event
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 (delta + done)", len(got))
	}
	if got[0].Type != llm.EventDelta || got[0].ContentDelta != "hello" {
		t.Errorf("event[0] = %+v", got[0])
	}
	if got[1].Type != llm.EventDone || got[1].FinishReason != llm.FinishStop {
		t.Errorf("event[1] = %+v", got[1])
	}
}

func TestFakeLLM_ChatStream_EmptyResponses_OnlyDone(t *testing.T) {
	// 空 Responses 不应发 Delta，只发 Done
	f := &FakeLLM{}
	ch, _ := f.ChatStream(context.Background(), LLMRequest{})
	var got []llm.Event
	for ev := range ch {
		got = append(got, ev)
	}
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1 (only done)", len(got))
	}
	if got[0].Type != llm.EventDone {
		t.Errorf("event[0].Type = %v, want Done", got[0].Type)
	}
}

func TestFakeLLM_ChatStream_Err_BecomesEventError(t *testing.T) {
	myErr := errors.New("boom")
	f := &FakeLLM{Err: myErr}
	ch, err := f.ChatStream(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var events []llm.Event
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Type != llm.EventError {
		t.Errorf("type = %v, want Error", events[0].Type)
	}
	if !errors.Is(events[0].Err, myErr) {
		t.Errorf("err = %v, want %v", events[0].Err, myErr)
	}
}

func TestFakeLLM_ChatStream_CtxCancel_DuringDelay(t *testing.T) {
	f := &FakeLLM{Responses: []string{"x"}, Delay: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	ch, _ := f.ChatStream(ctx, LLMRequest{})
	var got llm.Event
	for ev := range ch {
		got = ev
	}
	elapsed := time.Since(start)

	if got.Type != llm.EventError {
		t.Errorf("got %+v, want EventError", got)
	}
	if !errors.Is(got.Err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", got.Err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel too slow: %v", elapsed)
	}
}

func TestFakeLLM_ChatStream_CtxAlreadyCancelled(t *testing.T) {
	// ctx 已取消：Delay=0 时应走 "else if err := ctx.Err()" 分支
	f := &FakeLLM{Responses: []string{"x"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, _ := f.ChatStream(ctx, LLMRequest{})
	var got llm.Event
	for ev := range ch {
		got = ev
	}
	if got.Type != llm.EventError || !errors.Is(got.Err, context.Canceled) {
		t.Errorf("got %+v, want EventError(Canceled)", got)
	}
}

func TestFakeLLM_ChatStream_Hook(t *testing.T) {
	var seen LLMRequest
	f := &FakeLLM{
		Responses: []string{"ok"},
		Hook:      func(req LLMRequest) { seen = req },
	}
	req := LLMRequest{Model: "m1"}
	ch, _ := f.ChatStream(context.Background(), req)
	for range ch {
	}
	if seen.Model != "m1" {
		t.Errorf("hook did not capture req: %+v", seen)
	}
}

// ─── ToolCallsByTurn ─────────────────────────────────────────────────────────

func TestFakeLLM_ToolCallsByTurn_Happy(t *testing.T) {
	// 第 1 次 Chat 返回 tool_calls；第 2 次走 Responses
	tcs := []llm.ToolCall{{
		ID: "call_1", Type: "function",
		Function: llm.FunctionCall{Name: "echo", Arguments: `{"x":1}`},
	}}
	f := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{tcs},
		Responses:       []string{"final"},
	}
	ctx := context.Background()

	// 第 1 次：tool_calls
	resp1, err := f.Chat(ctx, LLMRequest{})
	if err != nil {
		t.Fatalf("Chat 1: %v", err)
	}
	if len(resp1.ToolCalls) != 1 || resp1.ToolCalls[0].ID != "call_1" {
		t.Errorf("resp1.ToolCalls = %+v", resp1.ToolCalls)
	}
	if resp1.FinishReason != llm.FinishToolCalls {
		t.Errorf("resp1.FinishReason = %q, want tool_calls", resp1.FinishReason)
	}
	if resp1.Content != "" {
		t.Errorf("resp1.Content should be empty, got %q", resp1.Content)
	}

	// 第 2 次：Responses 路径
	resp2, _ := f.Chat(ctx, LLMRequest{})
	if resp2.Content != "final" {
		t.Errorf("resp2.Content = %q, want final", resp2.Content)
	}
	if resp2.FinishReason != llm.FinishStop {
		t.Errorf("resp2.FinishReason = %q, want stop", resp2.FinishReason)
	}

	if f.ToolCallCount() != 1 {
		t.Errorf("ToolCallCount = %d, want 1", f.ToolCallCount())
	}
	if f.CallCount() != 1 {
		t.Errorf("CallCount = %d, want 1", f.CallCount())
	}
}

func TestFakeLLM_ToolCallsByTurn_EmptyTurn_FallsThrough(t *testing.T) {
	// 第 1 轮的 tool_calls 为 nil → fall-through 到 Responses
	f := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{nil},
		Responses:       []string{"hello"},
	}
	resp, err := f.Chat(context.Background(), LLMRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q, want hello", resp.Content)
	}
	if resp.FinishReason != llm.FinishStop {
		t.Errorf("FinishReason = %q", resp.FinishReason)
	}
	if f.ToolCallCount() != 1 {
		t.Errorf("ToolCallCount = %d, want 1 (empty turn still advances)", f.ToolCallCount())
	}
}

func TestFakeLLM_ToolCallsByTurn_MultiTurn(t *testing.T) {
	// 连续两轮 tool_calls + 最终答复
	f := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{
			{{ID: "c1", Function: llm.FunctionCall{Name: "t1"}}},
			{{ID: "c2", Function: llm.FunctionCall{Name: "t2"}}},
		},
		Responses: []string{"done"},
	}
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		resp, _ := f.Chat(ctx, LLMRequest{})
		if resp.FinishReason != llm.FinishToolCalls {
			t.Fatalf("turn %d FinishReason = %q", i, resp.FinishReason)
		}
	}
	resp3, _ := f.Chat(ctx, LLMRequest{})
	if resp3.Content != "done" {
		t.Errorf("final Content = %q", resp3.Content)
	}
	if f.ToolCallCount() != 2 {
		t.Errorf("ToolCallCount = %d, want 2", f.ToolCallCount())
	}
}

func TestFakeLLM_ToolCallsByTurn_Concurrent(t *testing.T) {
	// 并发 Chat（-race）无数据竞争
	f := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{
			{{ID: "c1"}},
			{{ID: "c2"}},
			{{ID: "c3"}},
		},
		Responses: []string{"ok"},
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.Chat(context.Background(), LLMRequest{})
		}()
	}
	wg.Wait()
}
