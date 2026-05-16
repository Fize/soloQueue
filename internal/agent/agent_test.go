package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	log, err := logger.System(dir,
		logger.WithConsole(false),
		logger.WithLevel(logger.ParseLogLevel("debug")),
	)
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

func today() string { return time.Now().Format("2006-01-02") }

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

// ─── Model override ──────────────────────────────────────────────────────────

func TestEffectiveContextWindow(t *testing.T) {
	// Does not require a started agent — SetModelOverride is atomic-pointer only
	a := NewAgent(Definition{ID: "test", ContextWindow: 1048576}, &FakeLLM{}, nil)

	// Default: falls back to Definition.ContextWindow
	if got := a.EffectiveContextWindow(); got != 1048576 {
		t.Errorf("EffectiveContextWindow = %d, want %d", got, 1048576)
	}

	// Override takes precedence
	a.SetModelOverride(&ModelParams{ContextWindow: 131072})
	if got := a.EffectiveContextWindow(); got != 131072 {
		t.Errorf("after override = %d, want %d", got, 131072)
	}

	// Override with ContextWindow=0 does NOT override (uses Definition fallback)
	a.SetModelOverride(&ModelParams{ContextWindow: 0, ModelID: "test-v2"})
	if got := a.EffectiveContextWindow(); got != 1048576 {
		t.Errorf("override with 0 should fall back to default, got %d", got)
	}

	// Clear reverts to Definition
	a.SetModelOverride(&ModelParams{ContextWindow: 131072})
	a.ClearModelOverride()
	if got := a.EffectiveContextWindow(); got != 1048576 {
		t.Errorf("after clear = %d, want %d", got, 1048576)
	}
}

func TestEffectiveContextWindow_ExplicitModel(t *testing.T) {
	// ExplicitModel makes SetModelOverride a no-op
	a := NewAgent(Definition{ID: "test", ContextWindow: 1048576, ExplicitModel: true}, &FakeLLM{}, nil)

	a.SetModelOverride(&ModelParams{ContextWindow: 131072})
	if got := a.EffectiveContextWindow(); got != 1048576 {
		t.Errorf("ExplicitModel should prevent override, got %d", got)
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
	log, err := logger.System(dir, logger.WithConsole(false))
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
	path := filepath.Join(dir, "logs", "system", "actor-"+today()+".jsonl")
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

// checkFileHasCategory checks if a log file contains the specified category.
func checkFileHasCategory(path, cat string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	needle := []byte(`"category":"` + cat + `"`)
	return bytes.Contains(data, needle), nil
}

// --- Agent Options ---

func TestWithPriorityMailbox(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil, WithPriorityMailbox())
	if a.priorityMailbox == nil {
		t.Fatal("priorityMailbox is nil")
	}
}

// --- Agent.PendingDelegations ---

func TestAgent_PendingDelegations_Initial(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	if got := a.PendingDelegations(); got != 0 {
		t.Errorf("PendingDelegations() = %d, want 0", got)
	}
}

func TestAgent_PendingDelegations_WithAsyncTurns(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)

	a.turnMu.Lock()
	a.asyncTurns[1] = &asyncTurnState{iter: 1}
	a.asyncTurns[3] = &asyncTurnState{iter: 3}
	a.turnMu.Unlock()

	if got := a.PendingDelegations(); got != 2 {
		t.Errorf("PendingDelegations() = %d, want 2", got)
	}

	a.turnMu.Lock()
	delete(a.asyncTurns, 1)
	a.turnMu.Unlock()

	if got := a.PendingDelegations(); got != 1 {
		t.Errorf("PendingDelegations() after delete = %d, want 1", got)
	}
}

// --- Agent.MailboxDepth ---

func TestAgent_MailboxDepth_NotStarted(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	high, normal := a.MailboxDepth()
	if high != 0 || normal != 0 {
		t.Errorf("MailboxDepth() = (%d, %d), want (0, 0)", high, normal)
	}
}

func TestAgent_MailboxDepth_WithPriorityMailbox(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil, WithPriorityMailbox())
	high, normal := a.MailboxDepth()
	if high != 0 || normal != 0 {
		t.Errorf("MailboxDepth() = (%d, %d), want (0, 0)", high, normal)
	}

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

	// Occupy run goroutine
	go func() { _, _ = a.Ask(context.Background(), "blocking") }()
	waitFor(t, 500*time.Millisecond, func() bool { return a.State() == StateProcessing })

	// Queue a few to mailbox
	for i := 0; i < 2; i++ {
		go func() { _, _ = a.Ask(context.Background(), "queued") }()
	}
	time.Sleep(50 * time.Millisecond)

	high, normal := a.MailboxDepth()
	if high != 0 {
		t.Errorf("high = %d, want 0 (regular mailbox)", high)
	}
	if normal < 1 {
		t.Errorf("normal = %d, want >= 1", normal)
	}
}

// ─── Work tracking (CurrentWork) ───────────────────────────────────────

func TestAgent_CurrentWork_NewAgent_IsStopped(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	w := a.CurrentWork()
	if w.State != StateStopped {
		t.Errorf("State = %s, want stopped", w.State)
	}
	if w.Iteration != 0 {
		t.Errorf("Iteration = %d, want 0", w.Iteration)
	}
}

func TestAgent_CurrentWork_IdleAfterStart(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{Responses: []string{"r"}}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	waitForState(t, a, time.Second, StateIdle)
	w := a.CurrentWork()
	if w.State != StateIdle {
		t.Errorf("State = %s, want idle", w.State)
	}
}

func TestAgent_CurrentWork_TracksPromptAndIteration(t *testing.T) {
	blockedTool := newBlockingTool()
	defer close(blockedTool.ch)

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "block", Arguments: `{}`},
		}}},
		Responses: []string{"done"},
	}
	a := NewAgent(Definition{ID: "test", ModelID: "m"}, fake, nil, WithTools(blockedTool))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	go a.Ask(context.Background(), "hello world")
	waitForState(t, a, time.Second, StateProcessing)

	w := a.CurrentWork()
	if w.Prompt != "hello world" {
		t.Errorf("Prompt = %q, want %q", w.Prompt, "hello world")
	}
	if w.Iteration < 0 {
		t.Errorf("Iteration = %d, want >= 0", w.Iteration)
	}
	if w.Elapsed == "" {
		t.Errorf("Elapsed should not be empty")
	}
}

func TestAgent_CurrentWork_TracksToolExecution(t *testing.T) {
	blockedTool := newBlockingTool()
	defer close(blockedTool.ch)

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{{
			ID:       "block1",
			Function: llm.FunctionCall{Name: "block", Arguments: `{"arg":"val"}`},
		}}},
		Responses: []string{"unreached"},
	}
	a := NewAgent(Definition{ID: "test", ModelID: "m"}, fake, nil, WithTools(blockedTool))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	// Start a task that blocks on tool execution.
	go a.Ask(context.Background(), "do work")
	waitForState(t, a, time.Second, StateProcessing)

	// During tool execution, CurrentWork should show the tool name.
	w := a.CurrentWork()
	if w.CurrentTool != "block" {
		t.Errorf("CurrentTool = %q, want %q", w.CurrentTool, "block")
	}
	if w.CurrentToolArgs != `{"arg":"val"}` {
		t.Errorf("CurrentToolArgs = %q, want %q", w.CurrentToolArgs, `{"arg":"val"}`)
	}
}

func TestAgent_CurrentWork_ErrorTracking(t *testing.T) {
	fake := &FakeLLM{
		Err: errors.New("llm service unavailable"),
	}
	a := NewAgent(Definition{ID: "test", ModelID: "m"}, fake, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	_, _ = a.Ask(context.Background(), "make error")
	waitForState(t, a, 2*time.Second, StateIdle)

	w := a.CurrentWork()
	if w.ErrorCount < 1 {
		t.Errorf("ErrorCount = %d, want >= 1", w.ErrorCount)
	}
	if !strings.Contains(w.LastError, "llm service unavailable") {
		t.Errorf("LastError = %q, want to contain 'llm service unavailable'", w.LastError)
	}
}

func TestAgent_CurrentWork_ErrorAndCountConsistency(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	a.RecordError(errors.New("err1"))
	a.RecordError(errors.New("err2"))

	w := a.CurrentWork()
	if w.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2", w.ErrorCount)
	}
	if w.LastError != "err2" {
		t.Errorf("LastError = %q, want %q", w.LastError, "err2")
	}

	// ResetErrors should clear both.
	a.ResetErrors()
	w = a.CurrentWork()
	if w.ErrorCount != 0 {
		t.Errorf("ErrorCount after reset = %d, want 0", w.ErrorCount)
	}
	if w.LastError != "" {
		t.Errorf("LastError after reset = %q, want empty", w.LastError)
	}
}

func TestAgent_ConsecutiveFailures_CircuitBreaker(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)

	for i := int32(1); i <= 5; i++ {
		if got := a.IncrementConsecutiveFailures(); got != i {
			t.Errorf("IncrementConsecutiveFailures = %d, want %d", got, i)
		}
	}
	if cf := a.ConsecutiveFailures(); cf != 5 {
		t.Errorf("ConsecutiveFailures = %d, want 5", cf)
	}

	a.ResetConsecutiveFailures()
	if cf := a.ConsecutiveFailures(); cf != 0 {
		t.Errorf("ConsecutiveFailures after reset = %d, want 0", cf)
	}
}

func TestAgent_State_LifecycleTransitions(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{Responses: []string{"r"}}, nil)
	if s := a.State(); s != StateStopped {
		t.Errorf("State before Start = %s, want stopped", s)
	}

	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	waitForState(t, a, time.Second, StateIdle)

	_, err := a.Ask(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	waitForState(t, a, time.Second, StateIdle)

	if err := a.Stop(time.Second); err != nil {
		t.Errorf("Stop: %v", err)
	}
	waitForState(t, a, time.Second, StateStopped)
}

func TestAgent_Err_ReturnsPanicError(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	if err := a.Err(); err != nil {
		t.Errorf("Err on new agent should be nil, got %v", err)
	}

	a.setRuntimeExitErr(errors.New("test error"))
	if err := a.Err(); err == nil || err.Error() != "test error" {
		t.Errorf("Err = %v, want 'test error'", err)
	}
}

// ─── Watch() ────────────────────────────────────────────────────────────

func TestAgent_Watch_ReceivesEvents(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{Responses: []string{"hello"}}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	waitForState(t, a, time.Second, StateIdle)

	ch, cancel := a.Watch()
	defer cancel()

	go a.Ask(context.Background(), "hi")

	// Collect events until DoneEvent or ErrorEvent
	var gotContent, gotDone bool
	for ev := range ch {
		switch ev.(type) {
		case ContentDeltaEvent:
			gotContent = true
		case DoneEvent:
			gotDone = true
			return
		case ErrorEvent:
			return
		}
	}
	if !gotContent {
		t.Error("expected ContentDeltaEvent via Watch channel")
	}
	if !gotDone {
		t.Error("expected DoneEvent via Watch channel")
	}
}

func TestAgent_Watch_CancelRemovesWatcher(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{Responses: []string{"r"}}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	waitForState(t, a, time.Second, StateIdle)

	ch, cancel := a.Watch()
	cancel() // immediately cancel

	// Channel should be closed.
	_, ok := <-ch
	if ok {
		t.Error("Watch channel should be closed after cancel")
	}
}

func TestAgent_Watch_MultipleWatchers(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{Responses: []string{"hello"}}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	waitForState(t, a, time.Second, StateIdle)

	ch1, cancel1 := a.Watch()
	defer cancel1()
	ch2, cancel2 := a.Watch()
	defer cancel2()

	go a.Ask(context.Background(), "hi")

	// Both watchers should receive events.
	done1, done2 := false, false
	for !done1 || !done2 {
		select {
		case ev, ok := <-ch1:
			if !ok {
				done1 = true
			} else if _, isDone := ev.(DoneEvent); isDone {
				done1 = true
			}
		case ev, ok := <-ch2:
			if !ok {
				done2 = true
			} else if _, isDone := ev.(DoneEvent); isDone {
				done2 = true
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for watch events")
		}
	}
}

func TestAgent_Watch_IdleAgentProducesNoEvents(t *testing.T) {
	a := NewAgent(Definition{ID: "test"}, &FakeLLM{}, nil)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(time.Second) })
	waitForState(t, a, time.Second, StateIdle)

	ch, cancel := a.Watch()
	defer cancel()

	// No Ask sent — no events should arrive.
	select {
	case ev := <-ch:
		t.Errorf("unexpected event from idle agent: %T", ev)
	case <-time.After(100 * time.Millisecond):
		// expected: no events
	}
}
