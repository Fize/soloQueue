package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// ─── Test fixtures ───────────────────────────────────────────────────────────

// fakeTool 是仅用于测试的极简 Tool 实现
type fakeTool struct {
	name        string
	description string
	parameters  json.RawMessage

	mu        sync.Mutex
	callCount int
	lastArgs  string
	result    string
	err       error
}

func (f *fakeTool) Name() string                { return f.name }
func (f *fakeTool) Description() string         { return f.description }
func (f *fakeTool) Parameters() json.RawMessage { return f.parameters }
func (f *fakeTool) Execute(_ context.Context, args string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	f.lastArgs = args
	if f.err != nil {
		return "", f.err
	}
	return f.result, nil
}

func (f *fakeTool) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

func newFakeTool(name string) *fakeTool {
	return &fakeTool{
		name:        name,
		description: "fake tool " + name,
		parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

// emptyNameTool 一个 Name() 返回空的 Tool，用于测试 ErrToolNameEmpty
type emptyNameTool struct{}

func (emptyNameTool) Name() string                                    { return "" }
func (emptyNameTool) Description() string                             { return "" }
func (emptyNameTool) Parameters() json.RawMessage                     { return nil }
func (emptyNameTool) Execute(context.Context, string) (string, error) { return "", nil }

// ─── Register / Get ──────────────────────────────────────────────────────────

func TestToolRegistry_Register_Get(t *testing.T) {
	r := NewToolRegistry()
	tool := newFakeTool("echo")
	if err := r.Register(tool); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got, ok := r.Get("echo")
	if !ok {
		t.Fatal("Get returned not-found")
	}
	if got != tool {
		t.Errorf("Get returned different pointer")
	}
}

func TestToolRegistry_Register_Duplicate(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(newFakeTool("echo"))
	err := r.Register(newFakeTool("echo"))
	if !errors.Is(err, ErrToolAlreadyRegistered) {
		t.Errorf("err = %v, want ErrToolAlreadyRegistered", err)
	}
}

func TestToolRegistry_Register_EmptyName(t *testing.T) {
	r := NewToolRegistry()
	err := r.Register(emptyNameTool{})
	if !errors.Is(err, ErrToolNameEmpty) {
		t.Errorf("err = %v, want ErrToolNameEmpty", err)
	}
}

func TestToolRegistry_Register_Nil(t *testing.T) {
	r := NewToolRegistry()
	err := r.Register(nil)
	if !errors.Is(err, ErrToolNil) {
		t.Errorf("err = %v, want ErrToolNil", err)
	}
}

func TestToolRegistry_Get_NotFound(t *testing.T) {
	r := NewToolRegistry()
	tool, ok := r.Get("ghost")
	if tool != nil || ok {
		t.Errorf("Get(\"ghost\") = (%v, %v), want (nil, false)", tool, ok)
	}
}

// ─── Specs ───────────────────────────────────────────────────────────────────

func TestToolRegistry_Specs_Format(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(newFakeTool("bravo"))
	_ = r.Register(newFakeTool("alpha"))
	specs := r.Specs()
	if len(specs) != 2 {
		t.Fatalf("len = %d, want 2", len(specs))
	}
	// 字典序：alpha, bravo
	if specs[0].Function.Name != "alpha" || specs[1].Function.Name != "bravo" {
		t.Errorf("order wrong: %q %q", specs[0].Function.Name, specs[1].Function.Name)
	}
	for _, s := range specs {
		if s.Type != "function" {
			t.Errorf("Type = %q, want function", s.Type)
		}
		if s.Function.Description == "" {
			t.Errorf("Description empty for %q", s.Function.Name)
		}
		if len(s.Function.Parameters) == 0 {
			t.Errorf("Parameters empty for %q", s.Function.Name)
		}
	}
}

func TestToolRegistry_Specs_Empty(t *testing.T) {
	r := NewToolRegistry()
	specs := r.Specs()
	if len(specs) != 0 {
		t.Errorf("empty registry should return empty specs, got %d", len(specs))
	}
}

// ─── Len / Names ─────────────────────────────────────────────────────────────

func TestToolRegistry_Len(t *testing.T) {
	r := NewToolRegistry()
	if r.Len() != 0 {
		t.Errorf("empty Len = %d", r.Len())
	}
	_ = r.Register(newFakeTool("a"))
	_ = r.Register(newFakeTool("b"))
	if r.Len() != 2 {
		t.Errorf("Len = %d, want 2", r.Len())
	}
}

func TestToolRegistry_Names_Sorted(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(newFakeTool("charlie"))
	_ = r.Register(newFakeTool("alpha"))
	_ = r.Register(newFakeTool("bravo"))
	names := r.Names()
	want := []string{"alpha", "bravo", "charlie"}
	if len(names) != len(want) {
		t.Fatalf("len = %d", len(names))
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("Names[%d] = %q, want %q", i, names[i], n)
		}
	}
}

func TestToolRegistry_Names_Empty(t *testing.T) {
	r := NewToolRegistry()
	if names := r.Names(); names != nil {
		t.Errorf("empty Names should be nil, got %v", names)
	}
}

// ─── safeGet（nil receiver）─────────────────────────────────────────────────

func TestToolRegistry_SafeGet_NilReceiver(t *testing.T) {
	var r *ToolRegistry // nil
	tool, ok := r.safeGet("x")
	if tool != nil || ok {
		t.Errorf("nil.safeGet = (%v, %v), want (nil, false)", tool, ok)
	}
}

func TestToolRegistry_SafeGet_Existing(t *testing.T) {
	r := NewToolRegistry()
	_ = r.Register(newFakeTool("x"))
	tool, ok := r.safeGet("x")
	if !ok || tool == nil {
		t.Errorf("safeGet failed for existing tool")
	}
}

// ─── Concurrency ─────────────────────────────────────────────────────────────

// TestToolRegistry_Concurrent_ReadWrite 并发 Register + Get + Specs，验证 -race 无数据竞争
func TestToolRegistry_Concurrent_ReadWrite(t *testing.T) {
	r := NewToolRegistry()
	var wg sync.WaitGroup
	const N = 20

	// writers
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = r.Register(newFakeTool(tname(i)))
		}(i)
	}
	// readers
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.Specs()
			_ = r.Len()
			_ = r.Names()
		}()
	}
	wg.Wait()

	if r.Len() != N {
		t.Errorf("after concurrent register: Len = %d, want %d", r.Len(), N)
	}
}

func tname(i int) string {
	// 简单唯一名：t-00, t-01, ...
	return "t-" + itoa2(i)
}

func itoa2(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	// 只需要支持 N=20
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
