package agent

import (
	"fmt"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── SessionConfirmStore ────────────────────────────────────────────────────

// SessionConfirmStore 是会话级工具放行存储的抽象。
type SessionConfirmStore interface {
	// IsConfirmed 检查 toolName 是否已被当前会话放行。
	IsConfirmed(toolName string) bool

	// Confirm 将 toolName 标记为已放行。
	Confirm(toolName string)

	// Clear 清空所有放行标记；Agent.Start 时调用，保证每新 session 从零开始。
	Clear()
}

// ─── memoryConfirmStore ─────────────────────────────────────────────────────

// memoryConfirmStore 是 SessionConfirmStore 的内存实现。
type memoryConfirmStore struct {
	mu    sync.RWMutex
	tools map[string]struct{}
}

// NewMemoryConfirmStore 返回基于内存的 SessionConfirmStore 实现。
func NewMemoryConfirmStore() SessionConfirmStore {
	return &memoryConfirmStore{
		tools: make(map[string]struct{}),
	}
}

func (s *memoryConfirmStore) IsConfirmed(toolName string) bool {
	s.mu.RLock()
	_, ok := s.tools[toolName]
	s.mu.RUnlock()
	return ok
}

func (s *memoryConfirmStore) Confirm(toolName string) {
	s.mu.Lock()
	if s.tools == nil {
		s.tools = make(map[string]struct{})
	}
	s.tools[toolName] = struct{}{}
	s.mu.Unlock()
}

func (s *memoryConfirmStore) Clear() {
	s.mu.Lock()
	s.tools = make(map[string]struct{})
	s.mu.Unlock()
}

// Confirm 向 agent 注入用户对某个待确认 tool_call 的响应。
func (a *Agent) Confirm(callID string, choice string) error {
	a.confirmMu.RLock()
	slot, ok := a.pendingConfirm[callID]
	a.confirmMu.RUnlock()
	if !ok {
		return fmt.Errorf("agent: no pending confirmation for %s", callID)
	}
	if !slot.done.CompareAndSwap(false, true) {
		return fmt.Errorf("agent: confirmation %s already resolved", callID)
	}
	select {
	case slot.ch <- choice:
		return nil
	default:
		return fmt.Errorf("agent: confirmation %s channel blocked", callID)
	}
}

// ToolSpecs 返回当前 agent 注册的所有 tool 的 llm.ToolDef 快照
func (a *Agent) ToolSpecs() []llm.ToolDef {
	if a.caps == nil {
		return nil
	}
	return a.caps.ToolSpecs()
}

// confirmChoice 方便内部代码引用 tools.ConfirmChoice
type confirmChoice = tools.ConfirmChoice

const (
	choiceDeny           = tools.ChoiceDeny
	choiceApprove        = tools.ChoiceApprove
	choiceAllowInSession = tools.ChoiceAllowInSession
)

// 编译时断言：Agent 实现 skill.Locatable
var _ skill.Locatable = (*Agent)(nil)
