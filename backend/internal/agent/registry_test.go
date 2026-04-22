package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

func newBareAgent(id string) *Agent {
	return NewAgent(Definition{ID: id}, &FakeLLM{}, nil)
}

func TestRegistry_RegisterGet(t *testing.T) {
	r := NewRegistry(nil)
	a := newBareAgent("a1")

	if err := r.Register(a); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("a1")
	if !ok {
		t.Fatal("Get returned not-found")
	}
	if got != a {
		t.Errorf("Get returned different pointer")
	}
}

func TestRegistry_Register_Nil(t *testing.T) {
	r := NewRegistry(nil)
	err := r.Register(nil)
	if !errors.Is(err, ErrAgentNil) {
		t.Errorf("err = %v, want ErrAgentNil", err)
	}
}

func TestRegistry_Register_EmptyID(t *testing.T) {
	r := NewRegistry(nil)
	err := r.Register(newBareAgent(""))
	if !errors.Is(err, ErrEmptyID) {
		t.Errorf("err = %v, want ErrEmptyID", err)
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry(nil)
	_ = r.Register(newBareAgent("a1"))

	err := r.Register(newBareAgent("a1"))
	if !errors.Is(err, ErrAgentAlreadyExists) {
		t.Errorf("err = %v, want ErrAgentAlreadyExists", err)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry(nil)
	_, ok := r.Get("nope")
	if ok {
		t.Error("Get should return ok=false for missing id")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry(nil)
	_ = r.Register(newBareAgent("a1"))

	if !r.Unregister("a1") {
		t.Error("Unregister should return true for existing id")
	}
	if _, ok := r.Get("a1"); ok {
		t.Error("agent should be removed")
	}
	// 再次 Unregister 返回 false
	if r.Unregister("a1") {
		t.Error("Unregister non-existing should return false")
	}
}

func TestRegistry_Len(t *testing.T) {
	r := NewRegistry(nil)
	if r.Len() != 0 {
		t.Errorf("empty Len = %d", r.Len())
	}
	_ = r.Register(newBareAgent("a1"))
	_ = r.Register(newBareAgent("a2"))
	if r.Len() != 2 {
		t.Errorf("Len = %d, want 2", r.Len())
	}
	_ = r.Unregister("a1")
	if r.Len() != 1 {
		t.Errorf("after Unregister, Len = %d, want 1", r.Len())
	}
}

func TestRegistry_List_IndependentSlice(t *testing.T) {
	r := NewRegistry(nil)
	_ = r.Register(newBareAgent("a1"))
	_ = r.Register(newBareAgent("a2"))

	list := r.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}

	// 修改返回的 slice 不应影响 registry
	list[0] = nil
	if r.Len() != 2 {
		t.Errorf("registry affected by slice mutation")
	}
	list2 := r.List()
	if len(list2) != 2 || list2[0] == nil {
		t.Errorf("next List call returned mutated data: %v", list2)
	}
}

func TestRegistry_List_Empty(t *testing.T) {
	r := NewRegistry(nil)
	list := r.List()
	if len(list) != 0 {
		t.Errorf("empty registry List len = %d", len(list))
	}
}

func TestRegistry_ConcurrentRegisterGet(t *testing.T) {
	r := NewRegistry(nil)
	const N = 100
	var wg sync.WaitGroup

	// N 并发 Register 不同 ID
	var registered atomic.Int32
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := r.Register(newBareAgent(fmt.Sprintf("agent-%d", i))); err == nil {
				registered.Add(1)
			}
		}(i)
	}

	// 并发 Get（可能命中/未命中）
	for i := 0; i < N*2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = r.Get(fmt.Sprintf("agent-%d", i%N))
		}(i)
	}

	// 并发 List
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List()
		}()
	}

	wg.Wait()

	if registered.Load() != N {
		t.Errorf("registered = %d, want %d", registered.Load(), N)
	}
	if r.Len() != N {
		t.Errorf("Len = %d, want %d", r.Len(), N)
	}
}

func TestRegistry_ConcurrentRegisterDuplicate(t *testing.T) {
	// 多 goroutine 同时 Register 同一个 ID：只有 1 个成功
	r := NewRegistry(nil)
	const N = 50

	var successCount atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Register(newBareAgent("same-id")); err == nil {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successCount.Load(); got != 1 {
		t.Errorf("only 1 Register should succeed, got %d", got)
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}
}

func TestRegistry_ConcurrentRegisterUnregister(t *testing.T) {
	// 反复 Register / Unregister 不崩、race 干净
	r := NewRegistry(nil)
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("churn-%d", i)
			for {
				select {
				case <-stop:
					return
				default:
					_ = r.Register(newBareAgent(id))
					r.Unregister(id)
				}
			}
		}(i)
	}

	// 稍跑一会儿
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = r.List()
		}
		close(stop)
	}()

	wg.Wait()
}

// ─── Batch lifecycle ────────────────────────────────────────────────────────

// newAgentForReg 构造一个带 FakeLLM、未启动的 Agent
func newAgentForReg(id string) *Agent {
	return NewAgent(Definition{ID: id}, &FakeLLM{Responses: []string{"r"}}, nil)
}

func TestRegistry_StartAll_StopAll(t *testing.T) {
	r := NewRegistry(nil)
	for i := 0; i < 5; i++ {
		_ = r.Register(newAgentForReg(fmt.Sprintf("a-%d", i)))
	}

	errs := r.StartAll(context.Background())
	if len(errs) != 0 {
		t.Errorf("StartAll errors: %v", errs)
	}
	// 每个 agent 应都运行中（状态观察）
	for _, a := range r.List() {
		waitForState(t, a, 200*time.Millisecond, StateIdle)
	}

	stopErrs := r.StopAll(time.Second)
	if len(stopErrs) != 0 {
		t.Errorf("StopAll errors: %v", stopErrs)
	}
	for _, a := range r.List() {
		if got := a.State(); got != StateStopped {
			t.Errorf("after StopAll, agent %q state = %s, want stopped", a.Def.ID, got)
		}
	}
}

func TestRegistry_StartAll_EmptyRegistry(t *testing.T) {
	r := NewRegistry(nil)
	errs := r.StartAll(context.Background())
	if len(errs) != 0 {
		t.Errorf("empty StartAll errors: %v", errs)
	}
}

func TestRegistry_StartAll_AlreadyStarted_Errors(t *testing.T) {
	r := NewRegistry(nil)
	a := newAgentForReg("a1")
	_ = r.Register(a)
	_ = a.Start(context.Background())
	t.Cleanup(func() { _ = a.Stop(time.Second) })

	errs := r.StartAll(context.Background())
	if len(errs) != 1 {
		t.Fatalf("expected 1 error for already-started agent, got %d: %v", len(errs), errs)
	}
	if !errors.Is(errs[0], ErrAlreadyStarted) {
		t.Errorf("err = %v, want ErrAlreadyStarted", errs[0])
	}
}

func TestRegistry_StopAll_SkipNotStarted(t *testing.T) {
	// 未 Start 的 agent 被 StopAll 时 ErrNotStarted 应被静默跳过
	r := NewRegistry(nil)
	_ = r.Register(newAgentForReg("never-started"))

	errs := r.StopAll(time.Second)
	if len(errs) != 0 {
		t.Errorf("StopAll for not-started should be empty, got: %v", errs)
	}
}

func TestRegistry_Shutdown_UnregistersAndStops(t *testing.T) {
	r := NewRegistry(nil)
	for i := 0; i < 3; i++ {
		_ = r.Register(newAgentForReg(fmt.Sprintf("a-%d", i)))
	}
	_ = r.StartAll(context.Background())

	if err := r.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("after Shutdown, Len = %d, want 0", r.Len())
	}
}

func TestRegistry_Shutdown_ReturnsJoinedErrors(t *testing.T) {
	// 一个不响应 ctx 的 agent：Stop 会超时
	r := NewRegistry(nil)
	a := NewAgent(Definition{ID: "blocked"}, &FakeLLM{}, nil)
	_ = r.Register(a)
	_ = a.Start(context.Background())

	// 投递一个卡死 job
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	_ = a.Submit(context.Background(), func(ctx context.Context) error {
		<-block
		return nil
	})
	// 等 job 开始
	waitForState(t, a, 500*time.Millisecond, StateProcessing)

	err := r.Shutdown(50 * time.Millisecond)
	if err == nil {
		t.Fatal("Shutdown should error with timeout")
	}
	if !errors.Is(err, ErrStopTimeout) {
		t.Errorf("err = %v, should wrap ErrStopTimeout", err)
	}
	// registry 仍应被清空
	if r.Len() != 0 {
		t.Errorf("after Shutdown, Len = %d, want 0 (even if Stop errored)", r.Len())
	}
}

// waitForState 轮询 agent 状态直到等于 want 或超时
func waitForState(t *testing.T, a *Agent, timeout time.Duration, want State) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if a.State() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("agent %q: state = %s, want %s (timeout %v)", a.Def.ID, a.State(), want, timeout)
}

// ─── Logging ────────────────────────────────────────────────────────────────

// TestRegistry_LogsRegisterUnregister 验证 Registry 带 logger 时记录
// Register / Unregister / Shutdown 的结构化事件
func TestRegistry_LogsRegisterUnregister(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "team", "sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	r := NewRegistry(log)
	a := newBareAgent("a-log-1")
	if err := r.Register(a); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if ok := r.Unregister("a-log-1"); !ok {
		t.Fatal("Unregister should succeed")
	}

	// Shutdown 空 registry，验证它也产生日志
	if err := r.Shutdown(time.Second); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	_ = log.Close() // flush
	path := filepath.Join(dir, "logs", "sessions", "team", "sess", "actor.jsonl")
	found, err := checkFileHasCategory(path, "actor")
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !found {
		t.Errorf("expected 'actor' category log from registry operations")
	}
}

