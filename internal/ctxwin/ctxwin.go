package ctxwin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/xiaobaitu/soloqueue/internal/llm"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── PayloadMessage ─────────────────────────────────────────────────────────

// PayloadMessage 是 BuildPayload 的返回类型
//
// 独立于 agent.LLMMessage，避免 ctxwin → agent 的循环依赖。
// Agent 包负责将 PayloadMessage 转为 agent.LLMMessage。
type PayloadMessage struct {
	Role             string
	Content          string
	ReasoningContent string
	Name             string
	ToolCallID       string
	ToolCalls        []llm.ToolCall
}

// ─── MessageRole ────────────────────────────────────────────────────────────

type MessageRole string

const (
	RoleSystem    MessageRole = "system"
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleTool      MessageRole = "tool"
)

// ─── Message ────────────────────────────────────────────────────────────────

// Message 是上下文窗口中的一条消息
//
// Token 计数说明：
//   - Tokens 在 Push 时通过 tiktoken 估算并固化
//   - Calibrate 后，sum(messages.Tokens) 不一定等于 currentTokens（漂移是正常的）
//   - 淘汰策略使用 currentTokens 做决策，msg.Tokens 仅用于增量计算
type Message struct {
	Role             MessageRole
	Content          string
	Tokens           int            // 插入时估算；Calibrate 后不再保证 sum == currentTokens
	IsEphemeral      bool           // 标记冗长工具输出（大段报错日志、文件读取结果）
	ReasoningContent string         // DeepSeek reasoning；API roundtrip 需要
	Name             string         // 工具名（role=tool）
	ToolCallID       string         // 工具调用 ID（role=tool）
	ToolCalls        []llm.ToolCall // role=assistant 时的 tool_calls
}

// ─── PushOption ─────────────────────────────────────────────────────────────

// PushOption 配置 Message 的可选字段
type PushOption func(*Message)

// WithEphemeral 设置消息的 IsEphemeral 标记
func WithEphemeral(isEphemeral bool) PushOption {
	return func(m *Message) { m.IsEphemeral = isEphemeral }
}

// WithReasoningContent 设置 DeepSeek thinking 模式的推理内容
func WithReasoningContent(rc string) PushOption {
	return func(m *Message) { m.ReasoningContent = rc }
}

// WithToolName 设置工具名（role=tool 时使用）
func WithToolName(name string) PushOption {
	return func(m *Message) { m.Name = name }
}

// WithToolCallID 设置工具调用 ID（role=tool 时使用）
func WithToolCallID(id string) PushOption {
	return func(m *Message) { m.ToolCallID = id }
}

// WithToolCalls 设置工具调用列表（role=assistant 时使用）
func WithToolCalls(tcs []llm.ToolCall) PushOption {
	return func(m *Message) { m.ToolCalls = tcs }
}

// ─── PushHook ───────────────────────────────────────────────────────────────

// PushHook 在 Push 完成后被调用（用于持久化到 timeline）
//
// Hook 在 Session 的 mutex 保护内执行，无需额外同步。
// replayMode 期间 Hook 不会被调用，避免双重写入。
type PushHook func(msg Message)

// SummaryHook 在异步压缩完成后被调用（用于持久化 summary 到 timeline）
// messages is the snapshot of all messages before compaction (useful for memory systems).
type SummaryHook func(summary string, messages []Message)

// ─── Option ─────────────────────────────────────────────────────────────────

// Option 配置 ContextWindow 的可选行为
type Option func(*ContextWindow)

// WithPushHook 设置 Push 完成后的回调
func WithPushHook(hook PushHook) Option {
	return func(cw *ContextWindow) { cw.pushHook = hook }
}

// WithSummaryHook 设置异步压缩完成后的回调
func WithSummaryHook(hook SummaryHook) Option {
	return func(cw *ContextWindow) { cw.summaryHook = hook }
}

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	// summaryTokensThreshold is the maxTokens value above which the soft
	// waterline is calculated at 75% of maxTokens. Below this threshold,
	// 85% is used to give smaller models more context room.
	summaryTokensThreshold = 512 * 1024 // 512k
)

// ─── ContextWindow ──────────────────────────────────────────────────────────

// ContextWindow is an in-memory, rule-based linear context truncator with
// dual waterlines and async compression support.
//
// Core invariants:
//   - currentTokens is the best approximation of the total token count in the
//     context window
//   - sum(messages[i].Tokens) may not equal currentTokens (drift after Calibrate)
//   - currentTokens is used for eviction decisions and is trusted
//   - msg.Tokens is a per-message estimate, only used for incremental calculation
//
// Concurrency: protected by sync.RWMutex for async compression safety.
// Write operations use Lock()/Unlock(), read operations use RLock()/RUnlock().
type ContextWindow struct {
	sync.RWMutex
	messages      []Message
	maxTokens     int          // hard waterline: physical capacity limit
	bufferTokens  int          // reserved for model output (from config)
	summaryTokens int          // soft waterline: triggers async compression
	currentTokens int          // real-time token count; exact after Calibrate
	tokenizer     *Tokenizer   // shared, immutable after init
	compactor     Compactor    // context compressor (may be nil)
	pushHook      PushHook     // callback after Push (may be nil)
	summaryHook   SummaryHook  // callback after compaction (may be nil)
	replayMode    bool         // disable pushHook during replay
	log           *logger.Logger // optional logger for message tracking
	summarizing   atomic.Bool  // true while async compression is in progress
}

// NewContextWindow creates a context window
//
// maxTokens: from config.LLMModel.ContextWindow (hard waterline).
// bufferTokens: reserved for model output (from config, dynamic).
// summaryTokens: soft waterline for triggering async compression.
// Pass 0 to auto-calculate (75% for ≥512k, 85% for <512k).
func NewContextWindow(maxTokens, bufferTokens, summaryTokens int, tokenizer *Tokenizer, opts ...Option) *ContextWindow {
	if summaryTokens <= 0 {
		if maxTokens >= summaryTokensThreshold {
			summaryTokens = maxTokens * 75 / 100
		} else {
			summaryTokens = maxTokens * 85 / 100
		}
	}
	cw := &ContextWindow{
		maxTokens:     maxTokens,
		bufferTokens:  bufferTokens,
		summaryTokens: summaryTokens,
		tokenizer:     tokenizer,
	}
	for _, opt := range opts {
		opt(cw)
	}
	return cw
}

// SetLogger sets the logger for tracking context window message changes
// nil is valid; logging is optional and gracefully degraded
func (cw *ContextWindow) SetLogger(log *logger.Logger) {
	cw.Lock()
	defer cw.Unlock()
	cw.log = log
}

// ─── Core API ───────────────────────────────────────────────────────────────

// Push appends a new message, estimates tokens, and triggers eviction if overloaded.
//
// Token calculation includes Content + ReasoningContent + ToolCalls JSON.
// If currentTokens + new message tokens exceeds maxTokens - bufferTokens,
// the two-step eviction policy runs synchronously (middle-out truncation +
// Turn-granularity FIFO).
func (cw *ContextWindow) Push(role MessageRole, content string, opts ...PushOption) {
	cw.Lock()
	defer cw.Unlock()

	msg := Message{Role: role, Content: content}
	for _, opt := range opts {
		opt(&msg)
	}
	// Token count includes Content + ReasoningContent + ToolCalls
	msg.Tokens = cw.tokenizer.Count(content) + cw.tokenizer.Count(msg.ReasoningContent)
	if len(msg.ToolCalls) > 0 {
		msg.Tokens += cw.tokenizer.Count(toolCallsToJSON(msg.ToolCalls))
	}

	if cw.log != nil {
		cw.log.DebugContext(context.Background(), logger.CatMessages, "push: message entry",
			"role", string(msg.Role),
			"content_len", len(msg.Content),
			"tokens", msg.Tokens,
			"is_ephemeral", msg.IsEphemeral,
			"has_reasoning", len(msg.ReasoningContent) > 0,
			"tool_calls_count", len(msg.ToolCalls),
		)
	}

	// Capacity check & eviction
	capacity := cw.maxTokens - cw.bufferTokens
	if cw.currentTokens+msg.Tokens > capacity {
		if cw.log != nil {
			cw.log.DebugContext(context.Background(), logger.CatMessages, "push: capacity check triggered",
				"current_tokens", cw.currentTokens,
				"new_msg_tokens", msg.Tokens,
				"capacity", capacity,
				"messages_count_before", len(cw.messages),
			)
		}
		cw.evict(msg.Tokens)
		if cw.log != nil {
			cw.log.DebugContext(context.Background(), logger.CatMessages, "push: eviction completed",
				"current_tokens_after", cw.currentTokens,
				"messages_count_after", len(cw.messages),
			)
		}
	}
	cw.messages = append(cw.messages, msg)
	cw.currentTokens += msg.Tokens

	if cw.log != nil {
		cw.log.DebugContext(context.Background(), logger.CatMessages, "push: message appended",
			"total_messages", len(cw.messages),
			"total_tokens", cw.currentTokens,
			"tokens_used_pct", float64(cw.currentTokens)*100.0/float64(cw.maxTokens),
		)
	}

	// Soft waterline check: trigger async compression
	if cw.compactor != nil && cw.currentTokens > cw.summaryTokens && !cw.summarizing.Load() {
		if cw.log != nil {
			cw.log.DebugContext(context.Background(), logger.CatMessages, "push: soft waterline exceeded, triggering async compression",
				"current_tokens", cw.currentTokens,
				"summary_waterline", cw.summaryTokens,
				"waterline_exceeded_pct", float64(cw.currentTokens-cw.summaryTokens)*100.0/float64(cw.summaryTokens),
			)
		}
		go cw.asyncCompact()
	}

	// Push hook (not called during replay)
	if cw.pushHook != nil && !cw.replayMode {
		cw.pushHook(msg)
	}
}

// BuildPayload converts the current Message slice to a PayloadMessage slice.
//
// Called before each API request. Returns a new slice; caller can safely modify.
// Agent package is responsible for converting PayloadMessage to agent.LLMMessage.
//
// Safety: filters out orphaned tool messages (role=tool without a preceding
// assistant(tool_calls) with matching tool_call_id). This defends against
// CW corruption from async delegation timing or truncation bugs.
func (cw *ContextWindow) BuildPayload() []PayloadMessage {
	cw.RLock()
	defer cw.RUnlock()

	// First pass: collect valid tool_call_ids from assistant messages
	validIDs := make(map[string]bool, len(cw.messages))
	for _, m := range cw.messages {
		for _, tc := range m.ToolCalls {
			validIDs[tc.ID] = true
		}
	}

	out := make([]PayloadMessage, 0, len(cw.messages))
	for _, m := range cw.messages {
		// Skip orphaned tool messages: role=tool but no matching tool_call_id
		if m.Role == RoleTool && m.ToolCallID != "" && !validIDs[m.ToolCallID] {
			continue
		}
		out = append(out, PayloadMessage{
			Role:             string(m.Role),
			Content:          m.Content,
			ReasoningContent: m.ReasoningContent,
			Name:             m.Name,
			ToolCallID:       m.ToolCallID,
			ToolCalls:        m.ToolCalls,
		})
	}
	return out
}

// Calibrate updates currentTokens to the exact value from the API response.
//
// Timing requirement: MUST be called BEFORE Push-ing new messages (assistant/tool).
// The call order must be:
//  1. Receive API EventDone → Calibrate(usage.PromptTokens)
//  2. Then Push(assistant+tool_calls) / Push(tool result)
//
// If reversed, Calibrate will overwrite the incremental estimate from the new Push.
//
// Drift note: After Calibrate, currentTokens is exact, but individual msg.Tokens
// remain estimates. sum(messages.Tokens) != currentTokens is normal.
// FIFO eviction subtracts estimates, causing minor drift corrected by next Calibrate.
func (cw *ContextWindow) Calibrate(promptTokens int) {
	cw.Lock()
	defer cw.Unlock()

	if cw.log != nil {
		drift := cw.currentTokens - promptTokens
		cw.log.DebugContext(context.Background(), logger.CatMessages, "calibrate: token count updated",
			"estimated_tokens", cw.currentTokens,
			"actual_tokens", promptTokens,
			"drift", drift,
			"drift_pct", float64(drift)*100.0/float64(promptTokens),
			"messages_count", len(cw.messages),
		)
	}

	cw.currentTokens = promptTokens
}

// Overflow checks if the current payload exceeds the hard limit.
//
// Called before sending an API request. If true, abort and report error.
// hardLimit comes from config.LLMModel.ContextWindow (model's physical limit).
func (cw *ContextWindow) Overflow(hardLimit int) bool {
	cw.RLock()
	defer cw.RUnlock()

	return cw.currentTokens > hardLimit
}

// ─── Queries ────────────────────────────────────────────────────────────────

// TokenUsage returns (currentTokens, maxTokens, bufferTokens)
func (cw *ContextWindow) TokenUsage() (current, max, buffer int) {
	cw.RLock()
	defer cw.RUnlock()

	return cw.currentTokens, cw.maxTokens, cw.bufferTokens
}

// Len returns the number of messages.
func (cw *ContextWindow) Len() int {
	cw.RLock()
	defer cw.RUnlock()

	return len(cw.messages)
}

// MessageAt returns a copy of the message at index i.
func (cw *ContextWindow) MessageAt(i int) (Message, bool) {
	cw.RLock()
	defer cw.RUnlock()

	if i < 0 || i >= len(cw.messages) {
		return Message{}, false
	}
	return cw.messages[i], true
}

// PopLast removes and returns the last message.
//
// Used by Session to remove a failed user prompt push.
func (cw *ContextWindow) PopLast() (Message, bool) {
	cw.Lock()
	defer cw.Unlock()

	if len(cw.messages) == 0 {
		return Message{}, false
	}
	last := cw.messages[len(cw.messages)-1]
	cw.messages = cw.messages[:len(cw.messages)-1]
	cw.currentTokens -= last.Tokens
	if cw.currentTokens < 0 {
		cw.currentTokens = 0 // drift correction
	}
	return last, true
}

// Reset clears the context window, keeping only the system prompt (index 0).
//
// Used for /clear command.
func (cw *ContextWindow) Reset() {
	cw.Lock()
	defer cw.Unlock()

	if len(cw.messages) > 0 && cw.messages[0].Role == RoleSystem {
		sysMsg := cw.messages[0]
		cw.messages = cw.messages[:1]
		cw.messages[0] = sysMsg
		cw.currentTokens = sysMsg.Tokens
	} else {
		cw.messages = nil
		cw.currentTokens = 0
	}
}

// SetReplayMode enables or disables replay mode.
//
// During replay, Push hooks are not called to avoid double writes.
func (cw *ContextWindow) SetReplayMode(on bool) {
	cw.Lock()
	defer cw.Unlock()

	cw.replayMode = on
}

// Recalculate recomputes the sum of all message token estimates from scratch.
//
// For debugging/testing only. Not called in production code.
// Note: after Calibrate, this may not equal currentTokens (normal drift).
func (cw *ContextWindow) Recalculate() int {
	cw.RLock()
	defer cw.RUnlock()

	total := 0
	for _, m := range cw.messages {
		total += m.Tokens
	}
	return total
}

// ─── Async Compression ─────────────────────────────────────────────────────

// asyncCompact compresses the conversation history using the Compactor.
//
// Runs in a separate goroutine triggered by the soft waterline check in Push.
// Uses CAS on summarizing to ensure only one compression runs at a time.
//
// Flow:
//  1. CAS summarizing false→true
//  2. RLock: snapshot messages
//  3. Release lock, call Compactor.Compact (allows concurrent reads/writes)
//  4. Lock: replace messages[1:] with single summary message
//  5. Update currentTokens
//  6. Set summarizing false
func (cw *ContextWindow) asyncCompact() {
	if !cw.summarizing.CompareAndSwap(false, true) {
		return
	}
	defer cw.summarizing.Store(false)

	if cw.log != nil {
		cw.log.DebugContext(context.Background(), logger.CatMessages, "async_compact: compression initiated")
	}

	// Snapshot messages under read lock
	cw.RLock()
	msgs := make([]Message, len(cw.messages))
	copy(msgs, cw.messages)
	tokensBefore := cw.currentTokens
	cw.RUnlock()

	if cw.log != nil {
		cw.log.DebugContext(context.Background(), logger.CatMessages, "async_compact: snapshot taken",
			"messages_count", len(msgs),
			"tokens_before", tokensBefore,
		)
	}

	// Compress without holding any lock (allows concurrent operations)
	summary, err := cw.compactor.Compact(context.Background(), msgs)
	if err != nil {
		if cw.log != nil {
			cw.log.WarnContext(context.Background(), logger.CatMessages, "async_compact: compression failed",
				"err", err.Error(),
			)
		}
		return // compression failed, keep current state
	}

	// Replace history with summary under write lock
	cw.Lock()
	summaryTokens := cw.tokenizer.Count(summary)
	summaryMsg := Message{
		Role:    RoleSystem,
		Content: "[Conversation Summary]\n" + summary,
		Tokens:  summaryTokens,
	}
	if len(cw.messages) > 0 {
		tokensAfter := cw.messages[0].Tokens + summaryTokens
		removedTokens := tokensBefore - tokensAfter

		cw.messages = append(cw.messages[:1], summaryMsg)
		cw.currentTokens = tokensAfter

		if cw.log != nil {
			cw.log.InfoContext(context.Background(), logger.CatMessages, "async_compact: compression completed",
				"messages_count_before", len(msgs),
				"messages_count_after", len(cw.messages),
				"tokens_before", tokensBefore,
				"tokens_after", tokensAfter,
				"tokens_saved", removedTokens,
				"tokens_saved_pct", float64(removedTokens)*100.0/float64(tokensBefore),
				"summary_len", len(summary),
			)
		}
	}
	cw.Unlock()

	// Persist summary to timeline (outside lock)
	if cw.summaryHook != nil {
		cw.summaryHook(summary, msgs)
	}
}

// ─── Internal ───────────────────────────────────────────────────────────────

// evict runs the two-step eviction policy.
//
// Step 1: Middle-Out Truncation — targeting IsEphemeral Tool output
// Step 2: Turn-granularity FIFO sliding window
func (cw *ContextWindow) evict(newMsgTokens int) {
	cw.truncateMiddleOut()
	capacity := cw.maxTokens - cw.bufferTokens
	target := capacity - newMsgTokens
	if cw.currentTokens > target {
		cw.slideFIFO(target)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// toolCallsToJSON serializes ToolCalls to JSON string for token counting.
func toolCallsToJSON(tcs []llm.ToolCall) string {
	if len(tcs) == 0 {
		return ""
	}
	b, err := json.Marshal(tcs)
	if err != nil {
		return fmt.Sprintf("%v", tcs)
	}
	return string(b)
}
