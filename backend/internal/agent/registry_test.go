package agent

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func newBareAgent(id string) *Agent {
	return NewAgent(Definition{ID: id}, &FakeLLM{}, nil)
}

func TestRegistry_RegisterGet(t *testing.T) {
	r := NewRegistry()
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
	r := NewRegistry()
	err := r.Register(nil)
	if !errors.Is(err, ErrAgentNil) {
		t.Errorf("err = %v, want ErrAgentNil", err)
	}
}

func TestRegistry_Register_EmptyID(t *testing.T) {
	r := NewRegistry()
	err := r.Register(newBareAgent(""))
	if !errors.Is(err, ErrEmptyID) {
		t.Errorf("err = %v, want ErrEmptyID", err)
	}
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(newBareAgent("a1"))

	err := r.Register(newBareAgent("a1"))
	if !errors.Is(err, ErrAgentAlreadyExists) {
		t.Errorf("err = %v, want ErrAgentAlreadyExists", err)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nope")
	if ok {
		t.Error("Get should return ok=false for missing id")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
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
	r := NewRegistry()
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
	r := NewRegistry()
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
	r := NewRegistry()
	list := r.List()
	if len(list) != 0 {
		t.Errorf("empty registry List len = %d", len(list))
	}
}

func TestRegistry_ConcurrentRegisterGet(t *testing.T) {
	r := NewRegistry()
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
	r := NewRegistry()
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
	r := NewRegistry()
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
