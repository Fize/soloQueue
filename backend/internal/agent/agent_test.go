package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// newTestLogger 返回一个写到临时目录的 Session 层 logger
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess",
		logger.WithConsole(false),
		logger.WithLevel(slog.LevelDebug),
	)
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

// ─── RunOnce (migrated from old Agent.Run) ───────────────────────────────────

func TestRunOnce_Happy(t *testing.T) {
	def := Definition{ID: "a1", Kind: KindChat, SystemPrompt: "you are helpful"}
	llm := &FakeLLM{Responses: []string{"hello"}}

	reply, err := RunOnce(context.Background(), def, llm, newTestLogger(t), "hi")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q, want hello", reply)
	}
}

func TestRunOnce_NilLLM(t *testing.T) {
	_, err := RunOnce(context.Background(), Definition{ID: "a1"}, nil, nil, "hi")
	if err == nil {
		t.Fatal("RunOnce with nil LLM should error")
	}
}

func TestRunOnce_LLMError_Propagated(t *testing.T) {
	myErr := errors.New("kaboom")
	_, err := RunOnce(context.Background(),
		Definition{ID: "a1"}, &FakeLLM{Err: myErr}, newTestLogger(t), "hi")
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
}

func TestRunOnce_CtxCancel_StopsLLMCall(t *testing.T) {
	llm := &FakeLLM{Responses: []string{"ignored"}, Delay: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := RunOnce(ctx, Definition{ID: "a1"}, llm, newTestLogger(t), "hi")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel did not propagate promptly: took %v", elapsed)
	}
}

func TestRunOnce_SystemPromptIncluded(t *testing.T) {
	var seenReq LLMRequest
	var captured bool
	fake := &FakeLLM{
		Responses: []string{"ok"},
		Hook: func(req LLMRequest) {
			if !captured {
				seenReq = req
				captured = true
			}
		},
	}

	def := Definition{
		ID:           "a1",
		Kind:         KindChat,
		ModelID:      "deepseek-chat",
		SystemPrompt: "you are a poetic assistant",
		Temperature:  0.4,
		MaxTokens:    512,
	}
	_, err := RunOnce(context.Background(), def, fake, newTestLogger(t), "hello")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if seenReq.Model != "deepseek-chat" {
		t.Errorf("Model = %q, want deepseek-chat", seenReq.Model)
	}
	if seenReq.Temperature != 0.4 {
		t.Errorf("Temperature = %v", seenReq.Temperature)
	}
	if seenReq.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d", seenReq.MaxTokens)
	}
	if len(seenReq.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(seenReq.Messages))
	}
	if seenReq.Messages[0].Role != "system" || seenReq.Messages[0].Content != "you are a poetic assistant" {
		t.Errorf("messages[0] = %+v", seenReq.Messages[0])
	}
	if seenReq.Messages[1].Role != "user" || seenReq.Messages[1].Content != "hello" {
		t.Errorf("messages[1] = %+v", seenReq.Messages[1])
	}
}

func TestRunOnce_NoSystemPrompt_OmittedFromMessages(t *testing.T) {
	var seenReq LLMRequest
	fake := &FakeLLM{
		Responses: []string{"ok"},
		Hook:      func(req LLMRequest) { seenReq = req },
	}

	_, _ = RunOnce(context.Background(), Definition{ID: "a1"}, fake, nil, "hi")

	if len(seenReq.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1 (system prompt should be omitted)", len(seenReq.Messages))
	}
	if seenReq.Messages[0].Role != "user" {
		t.Errorf("sole message should be user, got %q", seenReq.Messages[0].Role)
	}
}

func TestRunOnce_NilLogger_LLMError_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil logger + LLM error panicked: %v", r)
		}
	}()
	_, err := RunOnce(context.Background(),
		Definition{ID: "a1"}, &FakeLLM{Err: errors.New("boom")}, nil, "hi")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunOnce_NilLogger_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil logger caused panic: %v", r)
		}
	}()
	reply, err := RunOnce(context.Background(),
		Definition{ID: "a1"}, &FakeLLM{Responses: []string{"ok"}}, nil, "hi")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q", reply)
	}
}

func TestRunOnce_LogsLLMCategory(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "team", "sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	_, err = RunOnce(context.Background(),
		Definition{ID: "a1", Kind: KindChat},
		&FakeLLM{Responses: []string{"hi"}},
		log.Child(slog.String("actor_id", "a1")),
		"hello",
	)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	_ = log.Close() // flush

	path := filepath.Join(dir, "logs", "sessions", "team", "sess", "llm.jsonl")
	found, err := checkFileHasCategory(path, "llm")
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !found {
		t.Errorf("expected 'llm' category in log file %s", path)
	}
}

// ─── Lifecycle ───────────────────────────────────────────────────────────────

// startedAgent 是测试辅助：New + Start；t.Cleanup 负责 Stop
func startedAgent(t *testing.T, llm LLMClient, opts ...Option) *Agent {
	t.Helper()
	a := NewAgent(Definition{ID: "test"}, llm, newTestLogger(t), opts...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Stop(2 * time.Second)
	})
	return a
}

func TestAgent_StartStop(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{Responses: []string{"x"}}, nil)
	if got := a.State(); got != StateStopped {
		t.Errorf("before Start state = %s, want stopped", got)
	}

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 允许一个 scheduling tick 让 run goroutine 设置 state
	waitFor(t, 200*time.Millisecond, func() bool { return a.State() == StateIdle })

	if err := a.Stop(1 * time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := a.State(); got != StateStopped {
		t.Errorf("after Stop state = %s, want stopped", got)
	}
	select {
	case <-a.Done():
		// ok
	default:
		t.Error("Done should be closed after Stop")
	}
	if err := a.Err(); err != nil {
		t.Errorf("Err = %v, want nil (normal exit)", err)
	}
}

func TestAgent_StartTwice_ErrAlreadyStarted(t *testing.T) {
	a := startedAgent(t, &FakeLLM{Responses: []string{"x"}})
	err := a.Start(context.Background())
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second Start err = %v, want ErrAlreadyStarted", err)
	}
}

func TestAgent_StopWithoutStart(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	err := a.Stop(time.Second)
	if !errors.Is(err, ErrNotStarted) {
		t.Errorf("Stop without Start err = %v, want ErrNotStarted", err)
	}
}

func TestAgent_AskWithoutStart_ErrNotStarted(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{Responses: []string{"x"}}, nil)
	_, err := a.Ask(context.Background(), "hi")
	if !errors.Is(err, ErrNotStarted) {
		t.Errorf("Ask before Start err = %v, want ErrNotStarted", err)
	}
}

func TestAgent_SubmitWithoutStart_ErrNotStarted(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	err := a.Submit(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrNotStarted) {
		t.Errorf("Submit before Start err = %v, want ErrNotStarted", err)
	}
}

func TestAgent_SubmitNilFn(t *testing.T) {
	a := startedAgent(t, &FakeLLM{})
	err := a.Submit(context.Background(), nil)
	if err == nil {
		t.Error("Submit nil fn should error")
	}
}

func TestAgent_StopTimeout(t *testing.T) {
	// 一个不响应 ctx 的 job：故意用 time.Sleep 不 select ctx
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 投递一个不响应 ctx 的长任务
	started := make(chan struct{})
	block := make(chan struct{})
	_ = a.Submit(context.Background(), func(ctx context.Context) error {
		close(started)
		<-block // 不响应 ctx，等测试结束才释放
		return nil
	})

	<-started
	// Stop 50ms 超时
	err := a.Stop(50 * time.Millisecond)
	if !errors.Is(err, ErrStopTimeout) {
		t.Errorf("Stop err = %v, want ErrStopTimeout", err)
	}

	// 释放 job，让 goroutine 退出
	close(block)
	waitFor(t, 2*time.Second, func() bool { return a.State() == StateStopped })
}

func TestAgent_Restart(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{Responses: []string{"a", "b"}}, newTestLogger(t))

	// 第一次 Start
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start 1: %v", err)
	}
	r1, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask 1: %v", err)
	}
	if r1 != "a" {
		t.Errorf("r1 = %q", r1)
	}
	if err := a.Stop(time.Second); err != nil {
		t.Fatalf("Stop 1: %v", err)
	}

	// Restart
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start 2: %v", err)
	}
	r2, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask 2: %v", err)
	}
	if r2 != "b" {
		t.Errorf("r2 = %q", r2)
	}
	if err := a.Stop(time.Second); err != nil {
		t.Fatalf("Stop 2: %v", err)
	}
}

func TestAgent_ParentCtxCancel_StopsAgent(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{Responses: []string{"x"}}, nil)
	parent, cancel := context.WithCancel(context.Background())
	if err := a.Start(parent); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, 200*time.Millisecond, func() bool { return a.State() == StateIdle })

	cancel()

	select {
	case <-a.Done():
		// ok
	case <-time.After(time.Second):
		t.Fatal("agent did not exit after parent ctx cancel")
	}
}

// ─── Ask happy path ──────────────────────────────────────────────────────────

func TestAgent_Ask_Happy(t *testing.T) {
	a := startedAgent(t, &FakeLLM{Responses: []string{"hello"}})
	reply, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q", reply)
	}
}

func TestAgent_Ask_LLMError(t *testing.T) {
	myErr := errors.New("boom")
	a := startedAgent(t, &FakeLLM{Err: myErr})
	_, err := a.Ask(context.Background(), "hi")
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
}

// ─── Serialization ───────────────────────────────────────────────────────────

// TestAgent_SerializesAsks：并发 N 个 Ask，用 Hook 记录进出 LLM 的时间戳区间；
// 断言任意两次 LLM 调用不重叠。
func TestAgent_SerializesAsks(t *testing.T) {
	const N = 10

	var mu sync.Mutex
	type interval struct{ start, end time.Time }
	intervals := []interval{}

	fake := &FakeLLM{
		Responses: []string{"r"},
		Delay:     20 * time.Millisecond,
		Hook: func(_ LLMRequest) {
			mu.Lock()
			intervals = append(intervals, interval{start: time.Now()})
			mu.Unlock()
		},
	}
	a := startedAgent(t, fake)

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.Ask(context.Background(), "hi")
			mu.Lock()
			// 给最后一个 interval 盖上 end
			intervals[len(intervals)-1].end = time.Now()
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(intervals) != N {
		t.Fatalf("intervals len = %d, want %d", len(intervals), N)
	}

	// Hook 的调用是串行（因为 FakeLLM 内部持锁），所以 intervals[i].start 是严格递增
	// 关键断言：intervals[i].start >= intervals[i-1].start + Delay（前一次已经完成）
	// 注：Hook 调用其实在 sleep 之前，但我们的断言目标是"不重叠"，即 N 次总耗时 ≥ N*Delay
	total := intervals[N-1].end.Sub(intervals[0].start)
	if total < time.Duration(N-1)*20*time.Millisecond {
		t.Errorf("total %v too short: likely concurrent (want ≥ %v)", total, (N-1)*20)
	}
}

// TestAgent_Ask_OrderPreserved：FakeLLM 按 Responses 顺序返回；agent 串行；
// 因此不同 Ask 的 reply 必然在 Responses 中是连续不重复的段。但跨 goroutine
// reply 的接收顺序不保证 —— 我们用 Responses 长度 == 调用次数 + 内容唯一性来验证。
func TestAgent_Ask_CallsExactlyOncePerAsk(t *testing.T) {
	const N = 20
	fake := &FakeLLM{Responses: []string{"only"}}
	a := startedAgent(t, fake)

	var wg sync.WaitGroup
	replies := make([]string, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := a.Ask(context.Background(), "hi")
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
			replies[i] = r
		}(i)
	}
	wg.Wait()

	if got := fake.CallCount(); got != N {
		t.Errorf("CallCount = %d, want %d", got, N)
	}
}

// TestAgent_Ask_MailboxBackpressure：mailboxCap=1 + long-running job → 第二个 Ask 阻塞；
// 用 ctx.Deadline 让它返回 ctx.DeadlineExceeded。
func TestAgent_Ask_MailboxBackpressure(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"},
		&FakeLLM{Responses: []string{"r"}, Delay: 500 * time.Millisecond},
		nil, WithMailboxCap(1))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })

	// 第 1 个 Ask 开始占用 run goroutine
	go func() {
		_, _ = a.Ask(context.Background(), "one")
	}()
	// 稍等让第 1 个进入 Processing
	waitFor(t, 200*time.Millisecond, func() bool { return a.State() == StateProcessing })

	// 第 2 个 Ask 进 mailbox（cap=1）
	go func() {
		_, _ = a.Ask(context.Background(), "two")
	}()

	// 第 3 个 Ask 应阻塞在 mailbox send；用短 ctx 让它返回
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := a.Ask(ctx, "three")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("third Ask err = %v, want DeadlineExceeded", err)
	}
}

// ─── Interrupt ──────────────────────────────────────────────────────────────

func TestAgent_Ask_CancelledByCaller(t *testing.T) {
	a := startedAgent(t, &FakeLLM{Responses: []string{"r"}, Delay: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := a.Ask(ctx, "hi")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel too slow: %v", elapsed)
	}

	// agent 应仍健在
	if s := a.State(); s == StateStopped {
		t.Error("agent should still be running after Ask cancel")
	}
	// 能继续接受下个 Ask（LLM 换短 delay 不影响这个测试 —— 另起 fake？这里不再测）
}

func TestAgent_Ask_CancelledByStop(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"},
		&FakeLLM{Responses: []string{"r"}, Delay: 5 * time.Second},
		nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 起一个慢 Ask
	errCh := make(chan error, 1)
	go func() {
		_, err := a.Ask(context.Background(), "hi")
		errCh <- err
	}()
	// 等它进入 Processing
	waitFor(t, 500*time.Millisecond, func() bool { return a.State() == StateProcessing })

	// Stop
	_ = a.Stop(time.Second)

	select {
	case err := <-errCh:
		// LLM 看到 ctx.Canceled；或 Ask 看到 a.Done() 先到 → ErrStopped
		if err == nil {
			t.Error("Ask should return error after Stop")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Ask did not return after Stop")
	}
}

// TestAgent_PendingJobsDrained：mailbox 里 pending 的 Ask 在 Stop 时收到 cancel，
// 不会永远卡在 reply chan 上
func TestAgent_PendingJobsDrained(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"},
		&FakeLLM{Responses: []string{"r"}, Delay: 2 * time.Second},
		nil, WithMailboxCap(5))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 第 1 个 Ask 进入 Processing（长任务）
	go func() { _, _ = a.Ask(context.Background(), "processing") }()
	waitFor(t, 500*time.Millisecond, func() bool { return a.State() == StateProcessing })

	// 再 3 个 Ask 进 mailbox
	const Pending = 3
	errs := make(chan error, Pending)
	for i := 0; i < Pending; i++ {
		go func() {
			_, err := a.Ask(context.Background(), "pending")
			errs <- err
		}()
	}
	// 稍等让它们入队
	time.Sleep(100 * time.Millisecond)

	// Stop
	_ = a.Stop(2 * time.Second)

	// 3 个 pending 都应在 2s 内拿到 error，不会 hang
	deadline := time.After(3 * time.Second)
	received := 0
	for received < Pending {
		select {
		case err := <-errs:
			if err == nil {
				t.Error("pending Ask should return error after Stop")
			}
			received++
		case <-deadline:
			t.Fatalf("only %d/%d pending Asks returned; rest hang", received, Pending)
		}
	}
}

// ─── Observation ─────────────────────────────────────────────────────────────

func TestAgent_State_IdleProcessingTransition(t *testing.T) {
	a := startedAgent(t, &FakeLLM{Responses: []string{"r"}, Delay: 100 * time.Millisecond})

	// Idle
	if s := a.State(); s != StateIdle {
		t.Errorf("initial state = %s, want idle", s)
	}

	// Ask 中
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = a.Ask(context.Background(), "hi")
	}()

	waitFor(t, 200*time.Millisecond, func() bool { return a.State() == StateProcessing })

	<-done
	waitFor(t, 200*time.Millisecond, func() bool { return a.State() == StateIdle })
}

func TestAgent_Err_Panic(t *testing.T) {
	// 构造一个 LLM，让它在 Chat 里 panic
	a := NewAgent(Definition{ID: "a1"}, &panickyLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// 触发 panic（Ask 会 block，因为 run goroutine 已经死了）
	errCh := make(chan error, 1)
	go func() {
		_, err := a.Ask(context.Background(), "hi")
		errCh <- err
	}()

	// 等 agent 死
	select {
	case <-a.Done():
	case <-time.After(time.Second):
		t.Fatal("agent did not exit after panic")
	}
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Ask should error after agent panic")
		}
	case <-time.After(time.Second):
		// Ask 可能还在 replyCh 上，但 a.Done() close 后 Ask 的 select 会走 ErrStopped 分支
		t.Error("Ask did not return")
	}

	if err := a.Err(); err == nil {
		t.Error("Err should reflect panic, got nil")
	}
}

// panickyLLM 在 Chat 里 panic
type panickyLLM struct{}

func (p *panickyLLM) Chat(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	panic("kaboom from LLM")
}

func (p *panickyLLM) ChatStream(_ context.Context, _ LLMRequest) (<-chan llm.Event, error) {
	panic("kaboom from LLM stream")
}

// ─── Submit ──────────────────────────────────────────────────────────────────

func TestAgent_Submit_CustomJob(t *testing.T) {
	a := startedAgent(t, &FakeLLM{})

	done := make(chan struct{})
	var gotCtx context.Context
	err := a.Submit(context.Background(), func(ctx context.Context) error {
		gotCtx = ctx
		close(done)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("submit fn not executed")
	}
	if gotCtx == nil {
		t.Error("fn did not receive ctx")
	}
	if gotCtx.Err() != nil {
		t.Errorf("ctx should not be cancelled during normal run, got %v", gotCtx.Err())
	}
}

func TestAgent_Submit_ReturnsAfterEnqueue(t *testing.T) {
	// Submit 只等入队，不等 fn 执行完
	a := startedAgent(t, &FakeLLM{})

	block := make(chan struct{})
	defer close(block)

	start := time.Now()
	err := a.Submit(context.Background(), func(ctx context.Context) error {
		<-block
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("Submit waited for fn; elapsed = %v", elapsed)
	}
}

func TestAgent_Submit_FnErrorLogged(t *testing.T) {
	// fn 返回 error 会被日志记录，但不影响 agent 继续
	dir := t.TempDir()
	log, err := logger.Session(dir, "team", "sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{Responses: []string{"x"}}, log.Child())
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	done := make(chan struct{})
	_ = a.Submit(context.Background(), func(ctx context.Context) error {
		defer close(done)
		return errors.New("fn failed")
	})
	<-done

	// 下一个 Ask 应仍成功
	reply, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask after failing Submit: %v", err)
	}
	if reply != "x" {
		t.Errorf("reply = %q", reply)
	}

	_ = log.Close() // flush
	path := filepath.Join(dir, "logs", "sessions", "team", "sess", "actor.jsonl")
	found, err := checkFileHasCategory(path, "actor")
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !found {
		t.Errorf("expected 'actor' category log for submit error")
	}
}

// ─── Done on not-started ─────────────────────────────────────────────────────

func TestAgent_Done_BeforeStart_ClosedImmediately(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	select {
	case <-a.Done():
		// ok
	case <-time.After(10 * time.Millisecond):
		t.Error("Done() before Start should be closed immediately")
	}
}

// ─── Coverage fill-in ────────────────────────────────────────────────────────

func TestState_String(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateIdle, "idle"},
		{StateProcessing, "processing"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{State(99), "unknown"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}

// TestAgent_Submit_CallerCtxCancel 覆盖 Submit 入队等待时 caller ctx 取消
// （mailbox 满时被阻塞）
func TestAgent_Submit_CallerCtxCancel(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"},
		&FakeLLM{Responses: []string{"r"}, Delay: 2 * time.Second},
		nil, WithMailboxCap(1))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })

	// 占住 run goroutine
	_ = a.Submit(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})
	waitFor(t, 200*time.Millisecond, func() bool { return a.State() == StateProcessing })
	// 占住 mailbox slot（cap=1）
	_ = a.Submit(context.Background(), func(ctx context.Context) error { return nil })

	// 这次 Submit 应阻塞，caller ctx 50ms 取消
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := a.Submit(ctx, func(ctx context.Context) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Submit err = %v, want DeadlineExceeded", err)
	}
}

// TestAgent_Submit_FastPathErrStopped 覆盖 submit 的 fast-path
// （agentDone 已 close 时立刻返回 ErrStopped）
func TestAgent_Submit_FastPathErrStopped(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = a.Stop(time.Second)

	// 此时 a.done 仍引用已 close 的 chan；mailbox 仍非 nil
	err := a.Submit(context.Background(), func(ctx context.Context) error { return nil })
	if !errors.Is(err, ErrStopped) {
		t.Errorf("Submit after Stop err = %v, want ErrStopped", err)
	}
}

// TestMergeCtx_NilB 覆盖 mergeCtx 当 b 为 nil 或 b.Done() 为 nil 的分支
func TestMergeCtx_NilB(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	// b == nil
	merged, cancel := mergeCtx(parent, nil)
	if merged == nil {
		t.Fatal("merged ctx is nil")
	}
	cancel()

	// b.Done() == nil —— Background() 的 Done() 实际上返回 nil
	merged2, cancel2 := mergeCtx(parent, context.Background())
	if merged2 == nil {
		t.Fatal("merged2 is nil")
	}
	cancel2()
}

// TestMergeCtx_BCancelFirst 覆盖 mergeCtx goroutine 里 b.Done() 先触发的分支
func TestMergeCtx_BCancelFirst(t *testing.T) {
	a := context.Background()
	b, cancelB := context.WithCancel(context.Background())
	merged, cancelMerged := mergeCtx(a, b)
	defer cancelMerged()

	cancelB()

	select {
	case <-merged.Done():
		if !errors.Is(merged.Err(), context.Canceled) {
			t.Errorf("merged.Err = %v", merged.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("merged did not cancel after b cancelled")
	}
}

// TestAgent_NilCtx_DefaultsToBackground 覆盖 Ask / Submit 对 nil ctx 的处理
func TestAgent_Ask_NilCtxHandled(t *testing.T) {
	a := startedAgent(t, &FakeLLM{Responses: []string{"ok"}})
	//nolint:staticcheck // intentionally testing nil ctx
	reply, err := a.Ask(nil, "hi")
	if err != nil {
		t.Fatalf("Ask(nil): %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q", reply)
	}
}

func TestAgent_Submit_NilCtxHandled(t *testing.T) {
	a := startedAgent(t, &FakeLLM{})
	done := make(chan struct{})
	//nolint:staticcheck // intentionally testing nil ctx
	err := a.Submit(nil, func(ctx context.Context) error {
		close(done)
		return nil
	})
	if err != nil {
		t.Fatalf("Submit(nil): %v", err)
	}
	<-done
}

// TestAgent_Start_NilParent 覆盖 Start(nil) 默认到 Background
func TestAgent_Start_NilParent(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{Responses: []string{"x"}}, nil)
	//nolint:staticcheck // intentionally testing nil parent
	if err := a.Start(nil); err != nil {
		t.Fatalf("Start(nil): %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	if _, err := a.Ask(context.Background(), "hi"); err != nil {
		t.Errorf("Ask: %v", err)
	}
}

// TestWithMailboxCap_IgnoresNonPositive 覆盖 WithMailboxCap 对非正数的忽略
func TestWithMailboxCap_IgnoresNonPositive(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil,
		WithMailboxCap(0), WithMailboxCap(-5))
	if a.mailboxCap != DefaultMailboxCap {
		t.Errorf("mailboxCap = %d, want default %d", a.mailboxCap, DefaultMailboxCap)
	}

	a2 := NewAgent(Definition{ID: "a2"}, &FakeLLM{}, nil, WithMailboxCap(42))
	if a2.mailboxCap != 42 {
		t.Errorf("mailboxCap = %d, want 42", a2.mailboxCap)
	}
}

// TestAgent_Stop_ZeroTimeout 覆盖 Stop 的 timeout==0 分支（无限等待）
func TestAgent_Stop_ZeroTimeout(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, &FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := a.Stop(0); err != nil {
		t.Errorf("Stop(0): %v", err)
	}
}

// waitFor 轮询 condFn 直到返回 true 或超时
func waitFor(t *testing.T, timeout time.Duration, condFn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condFn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %v", timeout)
}

// checkFileHasCategory 轻量检查日志文件中出现指定 category
func checkFileHasCategory(path, cat string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	needle := []byte(`"category":"` + cat + `"`)
	return bytes.Contains(data, needle), nil
}
