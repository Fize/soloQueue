// Package session 提供"对话会话"抽象，封装 agent + 上下文窗口
//
// 设计原则：
//
//   - 一个 Session 对应"一次独立的对话"：绑定一个 *agent.Agent，持有
//     *ctxwin.ContextWindow 管理完整对话历史（含工具调用中间消息）。
//   - 同一 Session 内 Ask/AskStream **串行**：上一轮未结束时新 Ask 直接返回
//     ErrSessionBusy（避免上下文窗口错序）。agent 本身也串行，双重保护。
//   - SessionManager 管理唯一的活跃 session；全局只有一个会话。
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// ─── Errors ────────────────────────────────────────────────────────────────

var (
	// ErrSessionBusy Session 内已有 Ask 在跑，新 Ask 被拒绝
	ErrSessionBusy = errors.New("session: busy (another Ask in flight)")

	// ErrQueued 消息已进入 pending 队列，将在下一次 LLM API 调用前合并发送
	ErrQueued = errors.New("session: message queued")

	// ErrSessionClosed Session 已被 Close / Shutdown
	ErrSessionClosed = errors.New("session: closed")

	// ErrNoActiveTask CancelCurrent 时无活跃任务
	ErrNoActiveTask = errors.New("session: no active task")
)

// CurrentLevel returns the classification level of the last routed task.
// Returns "" if no task has been routed yet or routing is disabled.
func (s *Session) CurrentLevel() string {
	s.lastLevelMu.RLock()
	defer s.lastLevelMu.RUnlock()
	return s.lastLevel
}

// ─── TaskRouter Interface ─────────────────────────────────────────────────────

// RouteResult is a minimal routing decision passed to the session layer.
type RouteResult struct {
	ProviderID      string // LLM provider to use (e.g., "deepseek"); empty = default
	ModelID         string // API model to use (e.g., "deepseek-v4-pro")
	ThinkingEnabled bool   // whether to enable thinking mode
	ReasoningEffort string // "high" | "max" | ""
	Level           string // classification level label (e.g., "L1-SimpleSingleFile")
}

// TaskRouterFunc classifies a user prompt and returns model routing parameters.
// priorLevel is the session's current task level string ("" if none).
// Used to inject the router without creating import cycles.
// Returns error if classification fails; caller proceeds with defaults.
type TaskRouterFunc func(ctx context.Context, prompt string, priorLevel string) (RouteResult, error)

// MemoryHook is called when conversation context is being discarded (compaction or /clear).
// conversationText is a plain-text representation of the messages being forgotten.
// recordedAt indicates the date of the conversation segment for correct file routing.
type MemoryHook func(ctx context.Context, conversationText string, recordedAt time.Time)

// ─── Session ──────────────────────────────────────────────────────────────

// Session 是一个对话会话
type Session struct {
	ID      string
	TeamID  string
	Agent   *agent.Agent
	Router  TaskRouterFunc // 可选：任务路由分类器（nil = 不做路由，使用默认模型）
	Created time.Time

	mu     sync.Mutex
	cw     *ctxwin.ContextWindow // 替代原 history，管理完整对话上下文
	tl     *timeline.Writer      // 时间线持久化（可为 nil，表示不持久化）
	logger *logger.Logger        // 会话级日志

	// pending 排队消息：当 session busy 时新消息入队，在 agent 的 tool loop 下一次
	// LLM API 调用前一次性取出并注入 ContextWindow，实现连续消息合并
	pending *PendingQueue

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
	turnMu            sync.Mutex    // 保护 turnDone 的创建和关闭
	turnDone          chan struct{} // 当异步委派所在轮次完成时关闭
	turnDoneClosed    bool          // 防止重复关闭 turnDone

	// cancel 支持：CancelCurrent 取消正在执行的 AskStream
	cancelMu     sync.Mutex
	activeCancel context.CancelFunc // 当前 AskStream 的取消函数（forwarder 管理生命周期）
	cancelled    atomic.Bool       // forwarder 在检测到取消时设置，由适配器消耗并重置

	lastLevel       string       // last classified task level (L0-L3)
	lastLevelMu     sync.RWMutex // protects lastLevel and levelLocked
	levelLocked     bool         // true when user explicitly locked level via /l0-/l3
	lastRouteResult RouteResult  // cached route result for locked mode (model params preserved)

	memoryHook    MemoryHook        // optional callback for short-term memory (nil = disabled)
	memoryManager *memory.Manager   // for dedup cursor; set alongside memoryHook
}

// NewSession 构造并启动一个 session（agent 已应 Start）
//
// cw 应已包含 system prompt（在 factory 中 push）。
// tl 可为 nil（不持久化）。
// logger 为会话级日志记录器（nil 时会创建默认记录器）。
func NewSession(id, teamID string, a *agent.Agent, cw *ctxwin.ContextWindow, tl *timeline.Writer, l *logger.Logger) *Session {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}

	s := &Session{
		ID:      id,
		TeamID:  teamID,
		Agent:   a,
		Created: time.Now(),
		cw:      cw,
		tl:      tl,
		logger:  l,
		pending: &PendingQueue{},
	}
	s.lastActive.Store(time.Now().UnixNano())

	// Wire pending message drainer so the agent injects queued messages
	// before each LLM API call.
	if cw != nil {
		cw.SetPendingDrainer(func() string {
			if pending := s.pending.Drain(); pending != "" {
				s.logger.InfoContext(context.Background(), logger.CatApp, "pending messages injected into context window",
					"session_id", s.ID,
					"prompt_len", len(pending),
				)
				return pending
			}
			return ""
		})
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session created",
		"session_id", id,
		"team_id", teamID,
	)

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

// CW returns the underlying ContextWindow pointer without locking.
// Safe for read-only access: the cw pointer is set at construction time
// and never changes. Use this in hot paths (e.g., UI tick) to avoid
// contending with Session.mu.
func (s *Session) CW() *ctxwin.ContextWindow {
	return s.cw
}

// QueueMessage enqueues a user message into the pending queue without blocking.
// The message will be injected into the agent's context window before the next
// LLM API call, merged with any other pending messages into a single user turn.
func (s *Session) QueueMessage(prompt string) {
	s.pending.Enqueue(prompt)
	s.logger.InfoContext(context.Background(), logger.CatApp, "message queued via QueueMessage",
		"session_id", s.ID,
		"prompt_len", len(prompt),
	)
}

// SetMemoryHook sets the optional callback for short-term memory recording.
// The hook is called when conversation context is discarded via compaction or /clear.
func (s *Session) SetMemoryHook(hook MemoryHook) {
	s.memoryHook = hook
}

// SetMemoryManager sets the memory manager for dedup cursor tracking.
// Must be set alongside SetMemoryHook for dedup to work.
func (s *Session) SetMemoryManager(mm *memory.Manager) {
	s.memoryManager = mm
}

// Clear 执行软清除：追加 /clear 控制事件到 timeline，重置 ContextWindow
//
// 不删除任何持久化数据。ContextWindow 仅保留 system prompt。
func (s *Session) Clear() error {
	s.mu.Lock()

	// Snapshot messages for memory recording before clearing.
	// Filter by dedup cursor and group by date.
	var dateGroups []payloadDateGroup
	if s.memoryHook != nil {
		payload := s.cw.BuildPayload()
		cursor := time.Time{}
		if s.memoryManager != nil {
			cursor = s.memoryManager.LastRecordedAt()
		}
		filtered := filterPayloadSince(payload, cursor)
		if len(filtered) > 0 {
			dateGroups = groupPayloadByDate(filtered)
		}
	}

	// 追加 /clear 控制事件到 timeline
	if s.tl != nil {
		if err := s.tl.AppendControl(&timeline.ControlPayload{
			Action: "clear",
			Reason: "user_command",
		}); err != nil {
			s.mu.Unlock()
			s.logger.LogError(context.Background(), logger.CatApp, "session clear failed", err)
			return fmt.Errorf("session: clear: %w", err)
		}
	}

	// 重置 ContextWindow（保留 system prompt）
	s.cw.Reset()
	s.mu.Unlock()

	// Call memory hook for each date group (outside lock)
	if s.memoryHook != nil && len(dateGroups) > 0 {
		var latest time.Time
		for _, g := range dateGroups {
			text := formatPayloadForMemory(g.msgs)
			s.memoryHook(context.Background(), text, g.date)
			for _, m := range g.msgs {
				if m.Timestamp.After(latest) {
					latest = m.Timestamp
				}
			}
		}
		if s.memoryManager != nil {
			s.memoryManager.AdvanceLastRecordedAt(latest)
		}
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session cleared",
		"session_id", s.ID,
	)

	return nil
}

// LastMessageTime returns the timestamp of the last non-system message.
// Returns zero time if no messages exist or only system prompt is present.
func (s *Session) LastMessageTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := s.cw.BuildPayload()
	for i := len(payload) - 1; i >= 0; i-- {
		if payload[i].Role != "system" {
			return payload[i].Timestamp
		}
	}
	return time.Time{}
}

// ShouldClearContext returns true if (a) the last message is older than idleTimeout,
// AND (b) currentTokens >= minTokens. This prevents wasting tokens on short sessions.
func (s *Session) ShouldClearContext(idleTimeout time.Duration, minTokens int) bool {
	last := s.LastMessageTime()
	if last.IsZero() {
		return false
	}
	if time.Since(last) < idleTimeout {
		return false
	}
	// Time condition met; now check token threshold
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cw.CurrentTokens() >= minTokens
}

// ClearSilent performs a "silent" clear of the context window.
// Unlike Clear(), it does NOT write a control event to the timeline.
// It triggers the memory hook (if set) for short-term memory storage.
func (s *Session) ClearSilent() error {
	s.mu.Lock()

	// Snapshot messages for memory recording.
	// Filter by dedup cursor and group by date.
	var dateGroups []payloadDateGroup
	if s.memoryHook != nil {
		payload := s.cw.BuildPayload()
		cursor := time.Time{}
		if s.memoryManager != nil {
			cursor = s.memoryManager.LastRecordedAt()
		}
		filtered := filterPayloadSince(payload, cursor)
		if len(filtered) > 0 {
			dateGroups = groupPayloadByDate(filtered)
		}
	}

	// Reset context window (preserves system prompt)
	s.cw.Reset()
	s.mu.Unlock()

	// Call memory hook for each date group (outside lock)
	if s.memoryHook != nil && len(dateGroups) > 0 {
		var latest time.Time
		for _, g := range dateGroups {
			text := formatPayloadForMemory(g.msgs)
			s.memoryHook(context.Background(), text, g.date)
			for _, m := range g.msgs {
				if m.Timestamp.After(latest) {
					latest = m.Timestamp
				}
			}
		}
		if s.memoryManager != nil {
			s.memoryManager.AdvanceLastRecordedAt(latest)
		}
	}

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
		s.logger.DebugContext(ctx, logger.CatApp, "ask rejected: session closed")
		return "", ErrSessionClosed
	}
	if !s.inFlight.CompareAndSwap(0, 1) {
		s.logger.InfoContext(ctx, logger.CatApp, "ask rejected: session busy, message queued",
			"session_id", s.ID,
			"prompt_len", len(prompt),
		)
		s.pending.Enqueue(prompt)
		return "", ErrQueued
	}
	defer s.inFlight.Store(0)
	defer s.touch()

	start := time.Now()

	// 先 push user prompt（让 Agent 在 BuildPayload 时能看到）
	s.mu.Lock()
	s.cw.Push(ctxwin.RoleUser, prompt)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "ask: prompt pushed to context window",
		"session_id", s.ID,
		"prompt_len", len(prompt),
	)

	reply, reasoningContent, err := s.Agent.AskWithHistory(ctx, s.cw, prompt)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		s.mu.Lock()
		s.cw.PopLast()
		s.mu.Unlock()

		s.logger.WarnContext(ctx, logger.CatApp, "ask failed, user prompt removed",
			"session_id", s.ID,
			"duration_ms", duration,
			"err", err.Error(),
		)
		return "", err
	}

	// Empty assistant reply with no tool calls is invalid for LLM API.
	// Skip the push but keep the user prompt so LLM retains context.
	if reply == "" {
		s.logger.WarnContext(ctx, logger.CatApp, "ask: empty assistant reply skipped",
			"session_id", s.ID,
			"duration_ms", duration,
			"reasoning_len", len(reasoningContent),
		)
		return "", fmt.Errorf("session: assistant returned empty reply")
	}

	s.mu.Lock()
	opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(reasoningContent)}
	s.cw.Push(ctxwin.RoleAssistant, reply, opts...)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "ask complete",
		"session_id", s.ID,
		"reply_len", len(reply),
		"reasoning_len", len(reasoningContent),
		"duration_ms", duration,
	)

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
		s.logger.DebugContext(ctx, logger.CatApp, "askstream rejected: session closed")
		return nil, ErrSessionClosed
	}

	// L1 异步委派期间不阻塞新消息。Agent mailbox 保证 job 串行执行：
	// resumeTurn（高优先级）会先于新消息 job 执行，CW 排序自然正确。

	if !s.inFlight.CompareAndSwap(0, 1) {
		s.logger.InfoContext(ctx, logger.CatApp, "askstream rejected: session busy, message queued",
			"session_id", s.ID,
			"prompt_len", len(prompt),
		)
		s.pending.Enqueue(prompt)
		return nil, ErrQueued
	}
	// 注意：inFlight 的释放由下面的 forwarder goroutine 负责
	s.touch()

	start := time.Now()

	// ── Task routing: classify prompt and set model override ──
	if s.Router != nil {
		// Check for explicit level lock/unlock commands (/l0, /l1, /l2, /l3)
		if newLevel, isLock := parseLevelLockCommand(prompt); isLock {
			s.lastLevelMu.Lock()
			s.levelLocked = true
			s.lastLevel = newLevel
			s.lastLevelMu.Unlock()
			s.logger.DebugContext(ctx, logger.CatApp, "task level locked by user",
				"session_id", s.ID,
				"level", newLevel,
			)
		}

		s.lastLevelMu.RLock()
		locked := s.levelLocked
		priorLevel := s.lastLevel
		cachedResult := s.lastRouteResult
		s.lastLevelMu.RUnlock()

		var result RouteResult
		var err error

		if locked && !isLevelLockCommand(prompt) {
			// Locked: skip routing, reuse cached model params
			result = cachedResult
			s.logger.DebugContext(ctx, logger.CatApp, "task routing skipped (level locked)",
				"session_id", s.ID,
				"level", result.Level,
			)
		} else {
			result, err = s.Router(ctx, prompt, priorLevel)
			if err != nil {
				s.logger.DebugContext(ctx, logger.CatApp, "task router failed, using default model",
					"session_id", s.ID,
					"err", err.Error(),
				)
				// Don't return — proceed with defaults (no model override)
				result = RouteResult{}
			}
		}

		if result.Level != "" {
			s.logger.DebugContext(ctx, logger.CatApp, "task router applied model override",
				"session_id", s.ID,
				"provider_id", result.ProviderID,
				"model_id", result.ModelID,
				"thinking_enabled", result.ThinkingEnabled,
				"reasoning_effort", result.ReasoningEffort,
				"level", result.Level,
			)
			s.Agent.SetModelOverride(&agent.ModelParams{
				ProviderID:      result.ProviderID,
				ModelID:         result.ModelID,
				ThinkingEnabled: result.ThinkingEnabled,
				ReasoningEffort: result.ReasoningEffort,
				Level:           result.Level,
			})
			s.lastLevelMu.Lock()
			s.lastLevel = result.Level
			s.lastRouteResult = result
			s.lastLevelMu.Unlock()
		}
	}

	// ── 创建可取消的 askCtx ──
	askCtx, askCancel := context.WithCancel(ctx)

	// 先 push user prompt
	s.mu.Lock()
	s.cw.Push(ctxwin.RoleUser, prompt)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "askstream: prompt pushed to context window",
		"session_id", s.ID,
		"prompt_len", len(prompt),
	)

	// 存储取消函数（必须在启动 goroutine 之前，确保 CancelCurrent 可以立即工作）
	s.cancelMu.Lock()
	s.activeCancel = askCancel
	s.cancelMu.Unlock()

	srcCh, err := s.Agent.AskStreamWithHistory(askCtx, s.cw, prompt)
	if err != nil {
		// 入队失败：清理
		s.cancelMu.Lock()
		s.activeCancel = nil
		s.cancelMu.Unlock()
		askCancel()

		s.mu.Lock()
		s.cw.PopLast()
		s.mu.Unlock()
		s.inFlight.Store(0)

		s.logger.WarnContext(ctx, logger.CatApp, "askstream: agent stream setup failed",
			"session_id", s.ID,
			"err", err.Error(),
		)
		return nil, err
	}

	out := make(chan agent.AgentEvent, 64)
	go func() {
		// 清理：goroutine 结束时清除 activeCancel 并释放 askCtx
		defer func() {
			s.cancelMu.Lock()
			s.activeCancel = nil
			s.cancelMu.Unlock()
			askCancel()
		}()
		defer close(out)
		defer s.inFlight.Store(0)
		defer s.touch()
		defer s.closeTurnDone()
		defer func() {
			if r := recover(); r != nil {
				s.logger.ErrorContext(ctx, logger.CatApp, "session event processor panic recovered",
					"session_id", s.ID,
					"panic", fmt.Sprintf("%v", r),
				)
			}
		}()

		var finalContent string
		var finalReasoning string
		var gotDone bool
		var eventCount int

		for {
			var ev agent.AgentEvent
			select {
			case e, ok := <-srcCh:
				if !ok {
					goto done
				}
				ev = e
			case <-askCtx.Done():
				// askCtx 取消：移除 user prompt，标记 cancelled
				s.cancelled.Store(true)
				s.mu.Lock()
				s.cw.PopLast()
				s.mu.Unlock()

				s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (read)",
					"session_id", s.ID,
					"events_processed", eventCount,
					"duration_ms", time.Since(start).Milliseconds(),
				)
				return
			}
			select {
			case out <- ev:
				eventCount++
			case <-askCtx.Done():
				// askCtx 取消：移除 user prompt，标记 cancelled
				s.cancelled.Store(true)
				s.mu.Lock()
				s.cw.PopLast()
				s.mu.Unlock()

				s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (write)",
					"session_id", s.ID,
					"events_processed", eventCount,
					"duration_ms", time.Since(start).Milliseconds(),
				)
				return
			}

			switch e := ev.(type) {
			case agent.ToolNeedsConfirmEvent:
				s.logger.InfoContext(ctx, logger.CatApp, "session-forwarder: confirm event received and forwarded",
					"session_id", s.ID,
					"call_id", e.CallID,
					"tool_name", e.Name,
				)
			case agent.DelegationStartedEvent:
				// 异步委派开始：释放 inFlight，允许用户发送新消息
				s.logger.DebugContext(ctx, logger.CatApp, "delegation started",
					"session_id", s.ID,
				)
				s.newTurnDone()
				s.inFlight.Store(0)
			case agent.DoneEvent:
				finalContent = e.Content
				finalReasoning = e.ReasoningContent
				gotDone = true
				s.logger.DebugContext(ctx, logger.CatApp, "askstream done event received",
					"session_id", s.ID,
					"content_len", len(e.Content),
					"reasoning_len", len(e.ReasoningContent),
				)
			case agent.ErrorEvent:
				// 错误：移除 user prompt
				s.mu.Lock()
				s.cw.PopLast()
				s.mu.Unlock()

				s.logger.WarnContext(ctx, logger.CatApp, "askstream error event, user prompt removed",
					"session_id", s.ID,
					"err", e.Err.Error(),
				)
			}
		}
	done:
		// 检查是否在 goto done 和此标签之间发生了取消（极窄竞态窗口）
		if askCtx.Err() != nil {
			s.cancelled.Store(true)
			s.mu.Lock()
			s.cw.PopLast()
			s.mu.Unlock()
		} else if gotDone {
			if finalContent != "" {
				s.mu.Lock()
				opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
				s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)
				s.mu.Unlock()

				s.logger.DebugContext(ctx, logger.CatApp, "askstream: assistant reply pushed to context window",
					"session_id", s.ID,
				)
			} else {
				// Empty assistant reply — invalid for LLM API.
				// Skip the push but keep the user prompt for context.
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: empty assistant reply skipped",
					"session_id", s.ID,
					"reasoning_len", len(finalReasoning),
				)
			}
		}
		// 委派轮次完成：关闭 turnDone 通道，通知等待的新消息
		s.closeTurnDone()

		s.logger.DebugContext(ctx, logger.CatApp, "askstream complete",
			"session_id", s.ID,
			"events_processed", eventCount,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()
	return out, nil
}

// Close 标记 session 为 closed，阻止新 Ask；不停 agent
func (s *Session) Close() {
	s.closed.Store(true)

	s.logger.InfoContext(context.Background(), logger.CatApp, "session closed",
		"session_id", s.ID,
		"lifetime_sec", time.Since(s.Created).Seconds(),
	)

	// 关闭 timeline Writer，刷盘并释放文件句柄
	if s.tl != nil {
		s.tl.Close()
	}

	// 关闭 session 日志
	if err := s.logger.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "session close: logger close error: %v\n", err)
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
		s.logger.DebugContext(context.Background(), logger.CatApp, "delegation turn completed",
			"session_id", s.ID,
		)
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

// CancelCurrent 取消当前正在执行的 AskStream（如果有）。
// 取消通过 askCtx 传播到 agent 的 streamLoop，进而中断 LLM 调用和工具执行。
// 幂等安全：无活跃任务时返回 ErrNoActiveTask。
func (s *Session) CancelCurrent(reason string) error {
	s.cancelMu.Lock()
	cancel := s.activeCancel
	s.cancelMu.Unlock()

	if cancel == nil {
		return ErrNoActiveTask
	}

	// 仅调用 cancel() — 不主动设 cancelled 标志。
	// cancelled 由 forwarder goroutine 在检测到 <-askCtx.Done() 时设置，
	// 确保只有在 forwarder 真正被取消时适配器才会收到 ErrCancelled，
	// 避免任务正常完成后误报取消。
	cancel()

	s.logger.InfoContext(context.Background(), logger.CatApp, "session task cancelled",
		"session_id", s.ID,
		"reason", reason,
	)
	return nil
}

// isCancelledAndReset 检查 forwarder 是否因取消而退出。
// 消耗一次性的 cancelled 标志并重置为 false，确保 ErrCancelled 只返回一次。
// 由 SessionAskAdapter 在 AskStream 事件循环后调用。
func (s *Session) isCancelledAndReset() bool {
	return s.cancelled.CompareAndSwap(true, false)
}

// ─── SessionManager ──────────────────────────────────────────────────────

// AgentFactory 给定 teamID 构造并 Start 一个新 agent，同时返回 ContextWindow 和可选的 TimelineWriter
//
// **重要**：传入的 ctx **不应**被直接传给 agent.Start —— agent 生命周期独立于
// 单次 Init 调用。factory 应使用 context.Background() 作为 agent 的 parent ctx。
// 这里 ctx 仅供 factory 内部短暂使用（比如网络配置加载、超时检查）。
//
// 返回的 *timeline.Writer 在 Session.Close 时自动关闭；不需要时可返回 nil。
type AgentFactory func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error)

// SessionManager 管理唯一的活跃 session
type SessionManager struct {
	factory       AgentFactory
	routerFunc    TaskRouterFunc
	memoryHook    MemoryHook
	memoryManager *memory.Manager
	logger        *logger.Logger

	mu      sync.Mutex
	session *Session
	closed  atomic.Bool
}

// NewSessionManager 构造 manager
func NewSessionManager(factory AgentFactory, l *logger.Logger) *SessionManager {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}
	return &SessionManager{
		factory: factory,
		logger:  l,
	}
}

// SetRouter sets the task router function for the session.
// Must be called before Init(). Not thread-safe for setup.
func (m *SessionManager) SetRouter(fn TaskRouterFunc) {
	m.routerFunc = fn
}

// SetMemoryHook sets the callback for short-term memory recording.
// Must be called before Init(). Not thread-safe for setup.
func (m *SessionManager) SetMemoryHook(hook MemoryHook) {
	m.memoryHook = hook
}

// SetMemoryManager sets the memory manager for dedup cursor tracking.
// Must be set alongside SetMemoryHook. Not thread-safe for setup.
func (m *SessionManager) SetMemoryManager(mm *memory.Manager) {
	m.memoryManager = mm
}

// Init 创建唯一 session；重复调用返回已存在的 session
func (m *SessionManager) Init(ctx context.Context, teamID string) (*Session, error) {
	initStart := time.Now()
	m.mu.Lock()
	if m.session != nil {
		s := m.session
		m.mu.Unlock()
		m.logger.DebugContext(ctx, logger.CatApp, "session init: reusing existing session",
			"duration", time.Since(initStart).String())
		return s, nil
	}
	m.mu.Unlock()

	if m.closed.Load() {
		m.logger.DebugContext(ctx, logger.CatApp, "session init rejected: manager closed")
		return nil, ErrSessionClosed
	}

	m.logger.InfoContext(ctx, logger.CatApp, "session init: calling factory")
	factoryStart := time.Now()
	a, cw, tl, err := m.factory(ctx, teamID)
	m.logger.InfoContext(ctx, logger.CatApp, "session init: factory returned",
		"duration", time.Since(factoryStart).String(), "err", fmt.Sprintf("%v", err))
	if err != nil {
		m.logger.WarnContext(ctx, logger.CatApp, "session factory failed",
			"team_id", teamID,
			"err", err.Error(),
		)
		return nil, fmt.Errorf("agent factory: %w", err)
	}
	id := newSessionID()

	sessionLogger := m.logger.Child()
	s := NewSession(id, teamID, a, cw, tl, sessionLogger)

	if m.routerFunc != nil {
		s.Router = m.routerFunc
	}
	if m.memoryHook != nil {
		s.memoryHook = m.memoryHook
	}
	if m.memoryManager != nil {
		s.memoryManager = m.memoryManager
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		_ = a.Stop(time.Second)
		return nil, ErrSessionClosed
	}
	m.session = s

	m.logger.InfoContext(ctx, logger.CatApp, "session initialized",
		"session_id", id,
		"team_id", teamID,
		"total_duration", time.Since(initStart).String(),
	)

	return s, nil
}

// Session 返回当前 session；未初始化时返回 nil
func (m *SessionManager) Session() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.session
}

// Shutdown 关闭 session 并阻止新 Init
func (m *SessionManager) Shutdown(stopTimeout time.Duration) {
	m.closed.Store(true)
	m.mu.Lock()
	s := m.session
	m.session = nil
	m.mu.Unlock()

	if s != nil {
		_ = s.Agent.Stop(stopTimeout)
		s.Close()
	}

	m.logger.InfoContext(context.Background(), logger.CatApp, "session manager shutdown completed")
}

// newSessionID returns a 32-char hex id (16 random bytes).
func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return strings.ToLower(hex.EncodeToString(b[:]))
}

// ─── Level lock helpers ─────────────────────────────────────────────────────

// levelLockCommands maps slash commands to level labels.
var levelLockCommands = map[string]string{
	"l0":     "L0-Conversation",
	"chat":   "L0-Conversation",
	"l1":     "L1-SimpleSingleFile",
	"l2":     "L2-MediumMultiFile",
	"l3":     "L3-ComplexRefactoring",
	"max":    "L3-ComplexRefactoring",
	"expert": "L3-ComplexRefactoring",
}

// parseLevelLockCommand checks if prompt starts with a level-lock command.
// Returns (levelLabel, true) if found, ("", false) otherwise.
func parseLevelLockCommand(prompt string) (string, bool) {
	trimmed := strings.TrimSpace(prompt)
	for cmd, label := range levelLockCommands {
		prefix := "/" + cmd
		if strings.HasPrefix(trimmed, prefix+" ") || trimmed == prefix {
			return label, true
		}
	}
	return "", false
}

// isLevelLockCommand returns true if prompt is a level-lock command
// (including when followed by content, e.g., "/l2 analyze this").
func isLevelLockCommand(prompt string) bool {
	_, ok := parseLevelLockCommand(prompt)
	return ok
}

// formatPayloadForMemory converts ctxwin payload messages to a plain-text string
// suitable for short-term memory summarization. Skips system messages.
// Emits [timestamp] headers at each role transition so the LLM can group events
// chronologically by when they actually occurred.
func formatPayloadForMemory(payload []ctxwin.PayloadMessage) string {
	var b strings.Builder
	var lastTS time.Time
	for _, m := range payload {
		switch m.Role {
		case "system":
			continue
		case "user":
		case "assistant":
		case "tool":
		default:
		}
		// Emit timestamp when it changes (new turn boundary), regardless of role.
		if !m.Timestamp.IsZero() && !m.Timestamp.Equal(lastTS) {
			b.WriteString("[" + m.Timestamp.Format("2006-01-02 15:04") + "]\n")
			lastTS = m.Timestamp
		}
		b.WriteString(roleLabel(m.Role))
		if m.Role == "tool" {
			b.WriteString("(" + m.Name + ")")
		}
		b.WriteString(": ")
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "...(truncated)"
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// roleLabel returns a capitalized display label for a message role.
func roleLabel(role string) string {
	switch role {
	case "user":
		return "User"
	case "assistant":
		return "Assistant"
	case "tool":
		return "Tool"
	default:
		return role
	}
}

// payloadDateGroup is a group of payload messages sharing the same date.
type payloadDateGroup struct {
	date time.Time
	msgs []ctxwin.PayloadMessage
}

// filterPayloadSince returns messages whose Timestamp is strictly after cursor.
// Returns the full slice when cursor is zero (never recorded).
func filterPayloadSince(payload []ctxwin.PayloadMessage, cursor time.Time) []ctxwin.PayloadMessage {
	if cursor.IsZero() {
		return payload
	}
	var out []ctxwin.PayloadMessage
	for _, m := range payload {
		if m.Timestamp.After(cursor) {
			out = append(out, m)
		}
	}
	return out
}

// groupPayloadByDate groups payload messages by the calendar date of their Timestamp.
func groupPayloadByDate(payload []ctxwin.PayloadMessage) []payloadDateGroup {
	byDate := make(map[string][]ctxwin.PayloadMessage)
	for _, m := range payload {
		date := m.Timestamp.Format("2006-01-02")
		byDate[date] = append(byDate[date], m)
	}
	result := make([]payloadDateGroup, 0, len(byDate))
	for dateStr, msgs := range byDate {
		t, _ := time.Parse("2006-01-02", dateStr)
		result = append(result, payloadDateGroup{date: t, msgs: msgs})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].date.Before(result[j].date) })
	return result
}

// messageDateGroup is a group of ctxwin.Message sharing the same date.
type messageDateGroup struct {
	date time.Time
	msgs []ctxwin.Message
}

// filterMessagesSince returns messages whose Timestamp is strictly after cursor.
func filterMessagesSince(msgs []ctxwin.Message, cursor time.Time) []ctxwin.Message {
	if cursor.IsZero() {
		return msgs
	}
	var out []ctxwin.Message
	for _, m := range msgs {
		if m.Timestamp.After(cursor) {
			out = append(out, m)
		}
	}
	return out
}

// groupMessagesByDate groups ctxwin.Message by the calendar date of their Timestamp.
func groupMessagesByDate(msgs []ctxwin.Message) []messageDateGroup {
	byDate := make(map[string][]ctxwin.Message)
	for _, m := range msgs {
		date := m.Timestamp.Format("2006-01-02")
		byDate[date] = append(byDate[date], m)
	}
	result := make([]messageDateGroup, 0, len(byDate))
	for dateStr, msgs := range byDate {
		t, _ := time.Parse("2006-01-02", dateStr)
		result = append(result, messageDateGroup{date: t, msgs: msgs})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].date.Before(result[j].date) })
	return result
}
