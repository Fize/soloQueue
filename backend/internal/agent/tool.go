package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── Tool interface ──────────────────────────────────────────────────────────

// Tool 是可被 agent 调用的工具
//
// Name / Description / Parameters 三个方法在 agent 构造 LLMRequest 时读取，
// 应视为只读常量（每次 agent 调用 LLM 前都会读一次，但返回值不应变化）。
// Execute 在 agent run goroutine 中串行调用（同一 agent 同一时刻只有一个
// 工具在跑），但不同 agent 可能并发调用同一个 Tool 实例 —— 因此 Execute
// 实现必须是并发安全的。
type Tool interface {
	// Name 返回工具名；必须非空、在同一 agent 内唯一
	Name() string

	// Description 给 LLM 看的自然语言描述；空串被允许但不推荐
	Description() string

	// Parameters 返回 JSON Schema（object 类型）描述参数；允许返回 nil
	// 表示"无参数"（对应 OpenAI function 声明 parameters 省略）
	Parameters() json.RawMessage

	// Execute 执行工具
	//
	// args 是 LLM 发来的原始 JSON 字符串（如 `""`、`"{}"`、`{"path":"foo"}`）。
	// 工具自己负责 Unmarshal 到具体结构体。
	//
	// 返回值：
	//   - result：给 LLM 的 tool-role 消息内容（建议短 + 结构化，文本/JSON 均可）
	//   - err   ：执行错误；agent 会把 "error: "+err.Error() 喂回 LLM，不中断循环
	//
	// ctx 的取消应尽快响应；若 Execute 实现不响应 ctx，agent 只能靠外层超时。
	Execute(ctx context.Context, args string) (result string, err error)
}

// ─── ToolRegistry ────────────────────────────────────────────────────────────

// tool 相关错误
var (
	// ErrToolNameEmpty Register 时 Tool.Name() 为空
	ErrToolNameEmpty = errors.New("agent: tool name is empty")
	// ErrToolAlreadyRegistered 同名工具重复注册
	ErrToolAlreadyRegistered = errors.New("agent: tool already registered")
	// ErrToolNil Register(nil)
	ErrToolNil = errors.New("agent: tool is nil")
	// ErrToolNotFound execTool 时 LLM 请求的工具名不存在
	//
	// 不从 Get 直接返回（Get 用 bool）；在 execTool 里被包装成
	// fmt.Errorf("%w: <name>", ErrToolNotFound)，测试可 errors.Is 匹配。
	ErrToolNotFound = errors.New("agent: tool not found")
)

// ToolRegistry 是 name → Tool 的并发安全映射
//
// 设计原则：
//   - Register 写锁；Get / Specs / Len / Names 读锁
//   - Specs() 返回新切片（非共享），工具数量通常 <100，复制代价可忽略
//   - nil receiver 安全（safeGet 返回 (nil,false) 不 panic）
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

// safeGet 是 Get 的 nil-receiver 友好版本
//
// 供 Agent.execTool 调用 —— Agent 可能未注册任何工具（a.tools == nil）。
func (r *ToolRegistry) safeGet(name string) (Tool, bool) {
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
