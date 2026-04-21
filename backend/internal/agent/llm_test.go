package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
