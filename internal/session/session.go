// Package session 提供"对话会话"抽象，封装 agent + 上下文窗口
//
// 设计原则：
//
//   - 一个 Session 对应"一次独立的对话"：绑定一个 *agent.Agent，持有
//     *ctxwin.ContextWindow 管理完整对话历史（含工具调用中间消息）。
//   - Session 的生命周期独立于网络连接：REST 显式 Create/Delete，WebSocket
//     断开后 Session 仍活着（REST 才是 owner）。
//   - 同一 Session 内 Ask/AskStream **串行**：上一轮未结束时新 Ask 直接返回
//     ErrSessionBusy（避免上下文窗口错序）。agent 本身也串行，双重保护。
//   - SessionManager 维护 id→Session 映射；提供 idle 清理。
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// ─── Errors ────────────────────────────────────────────────────────────────

var (
	// ErrSessionNotFound SessionManager.Get 找不到 id
	ErrSessionNotFound = errors.New("session: not found")

	// ErrSessionBusy Session 内已有 Ask 在跑，新 Ask 被拒绝
	ErrSessionBusy = errors.New("session: busy (another Ask in flight)")

	// ErrSessionClosed Session 已被 Delete / Shutdown
	ErrSessionClosed = errors.New("session: closed")
)

// ─── Session ──────────────────────────────────────────────────────────────

// Session 是一个对话会话
type Session struct {
	ID      string
	TeamID  string
	Agent   *agent.Agent
	Created time.Time

	mu sync.Mutex
	cw *ctxwin.ContextWindow // 替代原 history，管理完整对话上下文
	tl *timeline.Writer      // 时间线持久化（可为 nil，表示不持久化）

	// inFlight 并发 Ask 的 CAS 锁：0 → 1 入场；失败返回 ErrSessionBusy
	inFlight atomic.Int32

	// closed 标志 Session 是否已 Delete
	closed atomic.Bool

	// lastActive 供 reaper 清理；每次 Ask 更新
	lastActive atomic.Int64 // unix nanos

	// delegationPending 标志是否有异步委派正在进行
	// 当 DelegationStartedEvent 到达时设为 true，表示 L1 已委派任务给 L2
	// 此时 inFlight 会被释放，允许用户发送新消息
	// 新消息的 CW push 会被延迟到 turnDone 信号后，保证 CW 顺序正确
	delegationPending atomic.Bool
	turnMu            sync.Mutex   // 保护 turnDone 的创建和关闭
	turnDone          chan struct{} // 当异步委派所在轮次完成时关闭
	turnDoneClosed    bool         // 防止重复关闭 turnDone
}

// NewSession 构造并启动一个 session（agent 已应 Start）
//
// cw 应已包含 system prompt（在 factory 中 push）。
// tl 可为 nil（不持久化）。
func NewSession(id, teamID string, a *agent.Agent, cw *ctxwin.ContextWindow, tl *timeline.Writer) *Session {
	s := &Session{
		ID:      id,
		TeamID:  teamID,
		Agent:   a,
		Created: time.Now(),
		cw:      cw,
		tl:      tl,
	}
	s.lastActive.Store(time.Now().UnixNano())
	return s
}

// History 返回当前上下文的快照（兼容旧 API）
//
// 返回 []agent.LLMMessage 格式，供 REST /v1/sessions/{id}/history 使用。
func (s *Session) History() []agent.LLMMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := s.cw.BuildPayload()
	out := make([]agent.LLMMessage, 0, len(payload))
	for _, p := range payload {
		out = append(out, agent.LLMMessage{
			Role:             p.Role,
			Content:          p.Content,
			ReasoningContent: p.ReasoningContent,
			Name:             p.Name,
			ToolCallID:       p.ToolCallID,
			ToolCalls:        p.ToolCalls,
		})
	}
	return out
}

// ContextWindow 返回底层 ContextWindow（供需要直接访问的场景）
func (s *Session) ContextWindow() *ctxwin.ContextWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cw
}

// Clear 执行软清除：追加 /clear 控制事件到 timeline，重置 ContextWindow
//
// 不删除任何持久化数据。ContextWindow 仅保留 system prompt。
func (s *Session) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 追加 /clear 控制事件到 timeline
	if s.tl != nil {
		if err := s.tl.AppendControl(&timeline.ControlPayload{
			Action: "clear",
			Reason: "user_command",
		}); err != nil {
			return fmt.Errorf("session: clear: %w", err)
		}
	}

	// 重置 ContextWindow（保留 system prompt）
	s.cw.Reset()

	return nil
}

// Ask 发送一轮 prompt，返回最终 content
//
// 语义：
//   - 同一 session 内 Ask 串行（inFlight CAS 0→1，否则 ErrSessionBusy）
//   - 先 push user prompt 到 ContextWindow
//   - 调用 Agent.AskWithHistory（携带完整历史）
//   - 成功：push assistant reply 到 ContextWindow
//   - 失败：PopLast 移除刚 push 的 user prompt
//   - ctx 取消透传到 agent；Session 不代管超时。
func (s *Session) Ask(ctx context.Context, prompt string) (string, error) {
	if s.closed.Load() {
		return "", ErrSessionClosed
	}
	if !s.inFlight.CompareAndSwap(0, 1) {
		return "", ErrSessionBusy
	}
	defer s.inFlight.Store(0)
	defer s.touch()

	// 先 push user prompt（让 Agent 在 BuildPayload 时能看到）
	s.mu.Lock()
	s.cw.Push(ctxwin.RoleUser, prompt)
	s.mu.Unlock()

	reply, reasoningContent, err := s.Agent.AskWithHistory(ctx, s.cw, prompt)
	if err != nil {
		// 失败：移除刚 push 的 user prompt
		s.mu.Lock()
		s.cw.PopLast()
		s.mu.Unlock()
		return "", err
	}
	// 成功：push assistant reply（含 reasoning_content，DeepSeek thinking mode 跨轮必须回传）
	s.mu.Lock()
	opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(reasoningContent)}
	s.cw.Push(ctxwin.RoleAssistant, reply, opts...)
	s.mu.Unlock()
	return reply, nil
}

// AskStream 流式版本；caller 必须 range 返回的通道直到关闭
//
// 上下文窗口在收到 DoneEvent 时 push user + assistant；
// 收到 ErrorEvent 时 PopLast 移除 user prompt。
// caller 放弃 range 必须 cancel ctx。
//
// 异步委派支持：当 L1 委派任务给 L2 时，DelegationStartedEvent 会释放 inFlight，
// 允许用户在此期间发送新消息。新消息的 CW push 会等待委派轮次完成后执行，
// 以保证 ContextWindow 中的消息顺序正确（先完成委派回复，再出现新用户消息）。
func (s *Session) AskStream(ctx context.Context, prompt string) (<-chan agent.AgentEvent, error) {
	if s.closed.Load() {
		return nil, ErrSessionClosed
	}

	// 如果有异步委派正在进行，等待其完成后再操作 CW
	// 这保证了 CW 中的消息顺序：委派结果在前，新用户消息在后
	if s.delegationPending.Load() {
		s.turnMu.Lock()
		td := s.turnDone
		closed := s.turnDoneClosed
		s.turnMu.Unlock()
		if td != nil && !closed {
			select {
			case <-td:
				// 委派轮次已完成，可以继续
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}

	if !s.inFlight.CompareAndSwap(0, 1) {
		return nil, ErrSessionBusy
	}
	// 注意：inFlight 的释放由下面的 forwarder goroutine 负责
	s.touch()

	// 先 push user prompt
	s.mu.Lock()
	s.cw.Push(ctxwin.RoleUser, prompt)
	s.mu.Unlock()

	srcCh, err := s.Agent.AskStreamWithHistory(ctx, s.cw, prompt)
	if err != nil {
		// 入队失败：移除 user prompt
		s.mu.Lock()
		s.cw.PopLast()
		s.mu.Unlock()
		s.inFlight.Store(0)
		return nil, err
	}

	out := make(chan agent.AgentEvent, 64)
	go func() {
		defer close(out)
		defer s.inFlight.Store(0)
		defer s.touch()

		var finalContent string
		var finalReasoning string
		var gotDone bool
		for {
			var ev agent.AgentEvent
			select {
			case e, ok := <-srcCh:
				if !ok {
					goto done
				}
				ev = e
			case <-ctx.Done():
				// ctx 取消：移除 user prompt
				s.mu.Lock()
				s.cw.PopLast()
				s.mu.Unlock()
				return
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				s.mu.Lock()
				s.cw.PopLast()
				s.mu.Unlock()
				return
			}
		switch e := ev.(type) {
		case agent.DelegationStartedEvent:
			// 异步委派开始：释放 inFlight，允许用户发送新消息
			s.newTurnDone()
			s.inFlight.Store(0)
		case agent.DoneEvent:
			finalContent = e.Content
			finalReasoning = e.ReasoningContent
			gotDone = true
		case agent.ErrorEvent:
			// 错误：移除 user prompt
			s.mu.Lock()
			s.cw.PopLast()
			s.mu.Unlock()
		}
		}
	done:
		if gotDone {
			s.mu.Lock()
			opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
			s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)
			s.mu.Unlock()
		}
		// 委派轮次完成：关闭 turnDone 通道，通知等待的新消息
		s.closeTurnDone()
	}()
	return out, nil
}

// Close 标记 session 为 closed，阻止新 Ask；不停 agent
//
// SessionManager.Delete 会在调用 Close 之后主动 Stop agent。
func (s *Session) Close() {
	s.closed.Store(true)
	// 关闭 timeline Writer，刷盘并释放文件句柄
	if s.tl != nil {
		s.tl.Close()
	}
}

// closeTurnDone 安全关闭 turnDone 通道并清理状态。
// 可安全多次调用（幂等）。
func (s *Session) closeTurnDone() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.turnDone != nil && !s.turnDoneClosed {
		close(s.turnDone)
		s.turnDoneClosed = true
	}
	s.delegationPending.Store(false)
}

// newTurnDone 创建一个新的 turnDone 通道。
func (s *Session) newTurnDone() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.turnDone = make(chan struct{})
	s.turnDoneClosed = false
	s.delegationPending.Store(true)
}

func (s *Session) touch() {
	s.lastActive.Store(time.Now().UnixNano())
}

// ─── SessionManager ──────────────────────────────────────────────────────

// AgentFactory 给定 teamID 构造并 Start 一个新 agent，同时返回 ContextWindow 和可选的 TimelineWriter
//
// **重要**：传入的 ctx **不应**被直接传给 agent.Start —— agent 生命周期独立于
// 单次 Create 调用。factory 应使用 context.Background() 或 SessionManager
// 持有的"根 ctx"（未来若有）作为 agent 的 parent ctx。
// 这里 ctx 仅供 factory 内部短暂使用（比如网络配置加载、超时检查）。
//
// 返回的 *timeline.Writer 在 Session.Close 时自动关闭；不需要时可返回 nil。
type AgentFactory func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error)

// SessionManager 管理所有活跃 session
type SessionManager struct {
	factory  AgentFactory
	idleTTL  time.Duration

	mu       sync.RWMutex
	sessions map[string]*Session
	closed   atomic.Bool
}

// NewSessionManager 构造 manager；idleTTL<=0 禁用自动 reap
func NewSessionManager(factory AgentFactory, idleTTL time.Duration) *SessionManager {
	return &SessionManager{
		factory:  factory,
		idleTTL:  idleTTL,
		sessions: make(map[string]*Session),
	}
}

// Create 创建新 session；factory 启动 agent 并返回 ContextWindow
func (m *SessionManager) Create(ctx context.Context, teamID string) (*Session, error) {
	if m.closed.Load() {
		return nil, ErrSessionClosed
	}
	a, cw, tl, err := m.factory(ctx, teamID)
	if err != nil {
		return nil, fmt.Errorf("agent factory: %w", err)
	}
	id := newSessionID()
	s := NewSession(id, teamID, a, cw, tl)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		// race: shutdown between check and lock
		_ = a.Stop(time.Second)
		return nil, ErrSessionClosed
	}
	m.sessions[id] = s
	return s, nil
}

// Get 按 id 查找
func (m *SessionManager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// Delete 删除 session：Close → agent.Stop → 从 map 摘除
func (m *SessionManager) Delete(id string, stopTimeout time.Duration) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !ok {
		return ErrSessionNotFound
	}
	s.Close()
	return s.Agent.Stop(stopTimeout)
}

// Count 返回活跃 session 数量
func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ReapIdle 清理 idle 超过 idleTTL 的 session；返回清理数
func (m *SessionManager) ReapIdle(stopTimeout time.Duration) int {
	if m.idleTTL <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-m.idleTTL).UnixNano()
	m.mu.Lock()
	var victims []*Session
	for id, s := range m.sessions {
		if s.lastActive.Load() < cutoff && s.inFlight.Load() == 0 {
			victims = append(victims, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range victims {
		s.Close()
		_ = s.Agent.Stop(stopTimeout)
	}
	return len(victims)
}

// ReapLoop 定期调 ReapIdle，直到 ctx 取消
func (m *SessionManager) ReapLoop(ctx context.Context, interval, stopTimeout time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.ReapIdle(stopTimeout)
		}
	}
}

// Shutdown 停所有 session；阻止新 Create
func (m *SessionManager) Shutdown(stopTimeout time.Duration) {
	m.closed.Store(true)
	m.mu.Lock()
	all := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.sessions = nil
	m.mu.Unlock()

	for _, s := range all {
		s.Close()
		_ = s.Agent.Stop(stopTimeout)
	}
}

// newSessionID returns a 32-char hex id (16 random bytes).
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return strings.ToLower(hex.EncodeToString(b[:]))
}
