package tools

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// Tool 相关错误
var (
	// ErrToolNameEmpty Register 时 Tool.Name() 为空
	ErrToolNameEmpty = errors.New("tools: tool name is empty")
	// ErrToolAlreadyRegistered 同名工具重复注册
	ErrToolAlreadyRegistered = errors.New("tools: tool already registered")
	// ErrToolNil Register(nil)
	ErrToolNil = errors.New("tools: tool is nil")
	// ErrToolNotFound execTool 时 LLM 请求的工具名不存在
	ErrToolNotFound = errors.New("tools: tool not found")
)

// ToolRegistry 是 name → Tool 的并发安全映射
//
// 设计原则：
//   - Register 写锁；Get / Specs / Len / Names 读锁
//   - Specs() 返回新切片（非共享），工具数量通常 <100，复制代价可忽略
//   - nil receiver 安全（SafeGet 返回 (nil,false) 不 panic）
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry 构造空 registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register 注册工具
//
// 错误：
//   - t == nil         → ErrToolNil
//   - t.Name() == ""   → ErrToolNameEmpty
//   - 同名已注册       → ErrToolAlreadyRegistered
func (r *ToolRegistry) Register(t Tool) error {
	if t == nil {
		return ErrToolNil
	}
	name := t.Name()
	if name == "" {
		return ErrToolNameEmpty
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[name]; ok {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, name)
	}
	r.tools[name] = t
	return nil
}

// Get 按 name 查工具；不存在返回 (nil, false)
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// SafeGet 是 Get 的 nil-receiver 友好版本
//
// 供 Agent.execTool 调用 —— Agent 可能未注册任何工具（a.caps == nil）。
func (r *ToolRegistry) SafeGet(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	return r.Get(name)
}

// Specs 返回所有工具的 llm.ToolDef 快照，供 LLMRequest.Tools 用
//
// 顺序按 name 字典序（稳定输出，利于日志 diff / 测试断言）。
// 空 registry 返回 nil（非空切片）。
func (r *ToolRegistry) Specs() []llm.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]llm.ToolDef, 0, len(names))
	for _, n := range names {
		t := r.tools[n]
		out = append(out, llm.ToolDef{
			Type: "function",
			Function: llm.FunctionDecl{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  t.Parameters(),
			},
		})
	}
	return out
}

// Len 当前工具数量
func (r *ToolRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// Names 返回所有工具名（字典序），供日志 / debug 用
func (r *ToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Unregister 内部删除方法，仅供 SkillRegistry 回滚使用
func (r *ToolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}
