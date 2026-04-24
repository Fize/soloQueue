package agent

import "sync"

// ─── ConfirmChoice ──────────────────────────────────────────────────────────

// ConfirmChoice 是用户在确认对话框中做出的选择。
type ConfirmChoice string

const (
	// ChoiceDeny 表示拒绝/取消执行。
	ChoiceDeny ConfirmChoice = ""

	// ChoiceApprove 表示仅确认本次执行（不加白名单）。
	ChoiceApprove ConfirmChoice = "yes"

	// ChoiceAllowInSession 表示确认本次执行，并将该工具加入当前会话白名单，
	// 后续同会话内调用不再触发确认。
	ChoiceAllowInSession ConfirmChoice = "allow-in-session"
)

// ─── SessionConfirmStore ────────────────────────────────────────────────────

// SessionConfirmStore 是会话级工具放行存储的抽象。
//
// 设计原则：
//   - 当前只有内存实现；未来如需 Redis/DB 持久化，只需通过 WithConfirmStore
//     注入新实现即可，Agent 内部无感知。
//   - 接口保持极简：只按 toolName 维度判断，未来如需扩展为按参数特征判断，
//     可在不破坏现有实现的前提下新增方法（如 IsConfirmedWithArgs）。
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
