package ctxwin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	Timestamp        time.Time // 消息原始时间戳（用于 memory 等需要时间上下文的场景）
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
//   - Calibrate 会按比例修正每个 msg.Tokens，使 sum ≈ currentTokens
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
	Timestamp        time.Time      // 消息 push 时的时间戳；replay 时从 timeline event 恢复
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

// WithTimestamp 设置消息的时间戳（用于 timeline replay 恢复原始时间）
func WithTimestamp(ts time.Time) PushOption {
	return func(m *Message) { m.Timestamp = ts }
}

// ─── PushHook ───────────────────────────────────────────────────────────────

// PushHook 在 Push 完成后被调用（用于持久化到 timeline）
//
// Hook 在 Session 的 mutex 保护内执行，无需额外同步。
// replayMode 期间 Hook 不会被调用，避免双重写入。
type PushHook func(msg Message)

// SummarySegment is one compressed chunk of conversation history,
// produced by the new segmented compaction logic.
type SummarySegment struct {
	Summary string    // the LLM-generated summary for this chunk
	Msgs    []Message // the original messages in this chunk (for cursor filtering)
	Date    time.Time // the calendar date of the messages (for routing to short vs permanent memory)
}

// SummaryHook 在压缩完成后被调用，传入所有分段（含已过期和近期）。
// 调用方负责按 Date 决定存入短期 memory、长期 memory 还是 timeline。
type SummaryHook func(segments []SummarySegment)

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
	maxTokens     int            // hard waterline: physical capacity limit
	bufferTokens  int            // reserved for model output (from config)
	summaryTokens int            // soft waterline: triggers async compression
	currentTokens int            // real-time token count; exact after Calibrate
	tokenizer     *Tokenizer     // shared, immutable after init
	compactor     Compactor      // context compressor (may be nil)
	pushHook      PushHook       // callback after Push (may be nil)
	summaryHook   SummaryHook    // callback after compaction (may be nil)
	replayMode    bool           // disable pushHook during replay
	log           *logger.Logger // optional logger for message tracking
	summarizing   atomic.Bool    // true while async compression is in progress
	pendingDrain  func() string  // callback to drain session pending queue (set once at construction)
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

	msg := Message{Role: role, Content: content, Timestamp: time.Now()}
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
	if msg.Tokens > capacity || len(msg.Content) > cw.maxTokens {
		msg.Content = charLevelTruncate(msg.Content, 0.02, 0.02)
		msg.Tokens = cw.tokenizer.Count(msg.Content) + cw.tokenizer.Count(msg.ReasoningContent)
		if len(msg.ToolCalls) > 0 {
			msg.Tokens += cw.tokenizer.Count(toolCallsToJSON(msg.ToolCalls))
		}
	}
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
		go func() {
			defer func() {
				if r := recover(); r != nil {
					if cw.log != nil {
						cw.log.ErrorContext(context.Background(), logger.CatMessages, "asyncCompact panic recovered", fmt.Errorf("panic: %v", r))
					}
					cw.summarizing.Store(false)
				}
			}()
			cw.asyncCompact()
		}()
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
// Safety: filters out incomplete tool_call/tool_result pairs. This defends
// against CW corruption from async delegation timing, truncation bugs, or
// user cancellation during tool execution. Both directions are handled:
//   - tool messages without a preceding assistant(tool_calls)
//   - assistant(tool_calls) without complete tool result messages
func (cw *ContextWindow) BuildPayload() []PayloadMessage {
	cw.RLock()
	defer cw.RUnlock()

	return filterCompletePairs(cw.messages)
}

// filterCompletePairs filters out incomplete tool_call/tool_result pairs from
// a message list. It ensures that every tool message has a matching
// assistant(tool_calls), and every assistant(tool_calls) has all its tool
// results present. Messages not involved in tool interactions pass through
// unchanged.
//
// The same filtering logic is applied in timeline.replaySegment for replay
// paths, but the replay path uses a streaming algorithm (buffering pending
// groups) while this function uses a three-pass scan suitable for snapshot reads.
func filterCompletePairs(msgs []Message) []PayloadMessage {
	// Pass 1: record which tool_call_ids have tool result messages
	hasResult := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		if m.Role == RoleTool && m.ToolCallID != "" {
			hasResult[m.ToolCallID] = true
		}
	}

	// Pass 2: determine which tool_call_ids belong to complete assistant(tool_calls).
	// An assistant is complete only when ALL its tool_call_ids have results.
	// tool_call_ids from complete groups are stored in valid.
	valid := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		if len(m.ToolCalls) == 0 {
			continue
		}
		allComplete := true
		for _, tc := range m.ToolCalls {
			if !hasResult[tc.ID] {
				allComplete = false
				break
			}
		}
		if allComplete {
			for _, tc := range m.ToolCalls {
				valid[tc.ID] = true
			}
		}
	}

	// Pass 3: emit only messages that form valid conversations.
	// Enforces structural ordering: tool results must immediately follow
	// their assistant(tool_calls) without interleaved user/assistant messages.
	out := make([]PayloadMessage, 0, len(msgs))
	pendingCalls := 0
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {
			// Skip assistant(tool_calls) whose results are not all present
			allValid := true
			for _, tc := range m.ToolCalls {
				if !valid[tc.ID] {
					allValid = false
					break
				}
			}
			if !allValid {
				continue
			}
			pendingCalls = len(m.ToolCalls)
			out = append(out, PayloadMessage{
				Role:             string(m.Role),
				Content:          m.Content,
				ReasoningContent: m.ReasoningContent,
				Name:             m.Name,
				ToolCallID:       m.ToolCallID,
				ToolCalls:        m.ToolCalls,
				Timestamp:        m.Timestamp,
			})
		} else if m.Role == RoleTool && m.ToolCallID != "" {
			if !valid[m.ToolCallID] {
				continue
			}
			if pendingCalls > 0 {
				pendingCalls--
			}
			out = append(out, PayloadMessage{
				Role:             string(m.Role),
				Content:          m.Content,
				ReasoningContent: m.ReasoningContent,
				Name:             m.Name,
				ToolCallID:       m.ToolCallID,
				ToolCalls:        m.ToolCalls,
				Timestamp:        m.Timestamp,
			})
		} else {
			// user or assistant(content) message
			if pendingCalls > 0 {
				// Order violation: non-tool message before all tool results.
				// Remove the entire pending group (assistant + tool results).
				for len(out) > 0 {
					last := out[len(out)-1]
					out = out[:len(out)-1]
					if len(last.ToolCalls) > 0 {
						break
					}
				}
				pendingCalls = 0
			}
			out = append(out, PayloadMessage{
				Role:             string(m.Role),
				Content:          m.Content,
				ReasoningContent: m.ReasoningContent,
				Name:             m.Name,
				ToolCallID:       m.ToolCallID,
				ToolCalls:        m.ToolCalls,
				Timestamp:        m.Timestamp,
			})
		}
	}
	// If the final group is incomplete (pendingCalls > 0), truncate it
	if pendingCalls > 0 {
		for len(out) > 0 {
			last := out[len(out)-1]
			out = out[:len(out)-1]
			if len(last.ToolCalls) > 0 {
				break
			}
		}
	}
	return out
}

// Calibrate updates currentTokens to the exact value from the API response
// and redistributes the exact count proportionally across all msg.Tokens.
//
// Timing requirement: MUST be called BEFORE Push-ing new messages (assistant/tool).
// The call order must be:
//  1. Receive API EventDone → Calibrate(usage.PromptTokens)
//  2. Then Push(assistant+tool_calls) / Push(tool result)
//
// After Calibrate, both currentTokens and sum(msg.Tokens) equal promptTokens
// (within rounding). This prevents the drift cascade where FIFO eviction
// subtracts stale estimates while currentTokens was set to the exact value,
// causing a growing gap between currentTokens and the real payload size.
func (cw *ContextWindow) Calibrate(promptTokens int) {
	cw.Lock()
	defer cw.Unlock()

	drift := cw.currentTokens - promptTokens

	if cw.log != nil {
		cw.log.DebugContext(context.Background(), logger.CatMessages, "calibrate: token count updated",
			"estimated_tokens", cw.currentTokens,
			"actual_tokens", promptTokens,
			"drift", drift,
			"drift_pct", float64(drift)*100.0/float64(promptTokens),
			"messages_count", len(cw.messages),
		)
	}

	// Redistribute exact token count across messages proportionally.
	// This ensures FIFO eviction subtracts accurate amounts.
	if len(cw.messages) > 0 && promptTokens > 0 {
		sumEstimates := 0
		for _, m := range cw.messages {
			sumEstimates += m.Tokens
		}
		if sumEstimates > 0 && sumEstimates != promptTokens {
			ratio := float64(promptTokens) / float64(sumEstimates)
			runningSum := 0
			for i := range cw.messages {
				newTokens := int(float64(cw.messages[i].Tokens) * ratio)
				if newTokens < 1 {
					newTokens = 1
				}
				cw.messages[i].Tokens = newTokens
				runningSum += newTokens
			}
			if diff := promptTokens - runningSum; diff != 0 && len(cw.messages) > 0 {
				cw.messages[len(cw.messages)-1].Tokens += diff
				if cw.messages[len(cw.messages)-1].Tokens < 1 {
					cw.messages[len(cw.messages)-1].Tokens = 1
				}
			}
		}
	}

	cw.currentTokens = promptTokens
}

// Overflow checks if the current payload exceeds the effective capacity.
//
// Uses cw.maxTokens as the hard limit, making the CW the single source of
// truth for capacity. The effective capacity is maxTokens minus bufferTokens
// (reserved for model output). This matches the eviction capacity in Push,
// ensuring the overflow check is always consistent with capacity management.
func (cw *ContextWindow) Overflow() bool {
	cw.RLock()
	defer cw.RUnlock()

	capacity := cw.maxTokens - cw.bufferTokens
	if capacity < 0 {
		capacity = 0
	}
	return cw.currentTokens > capacity
}

// ─── Resize ──────────────────────────────────────────────────────────────────

// Resize updates the context window parameters and triggers eviction when
// the new capacity is smaller than current usage. Called when the model is
// switched to one with a different context window.
//
// maxTokens: new hard waterline (from config.LLMModel.ContextWindow).
// bufferTokens: reserved for model output. 0 auto-calculates as maxTokens/10.
// summaryTokens: soft waterline. 0 auto-calculates (75% for >=512k, 85% for <512k).
//
// Idempotent: if maxTokens and bufferTokens match the current state, this
// is a fast no-op (skips the entire eviction path).
func (cw *ContextWindow) Resize(maxTokens, bufferTokens, summaryTokens int) {
	cw.Lock()
	defer cw.Unlock()

	newBuffer := bufferTokens
	if newBuffer <= 0 {
		newBuffer = maxTokens / 10
	}
	if maxTokens == cw.maxTokens && newBuffer == cw.bufferTokens {
		return
	}

	cw.maxTokens = maxTokens
	cw.bufferTokens = newBuffer
	if summaryTokens <= 0 {
		if maxTokens >= summaryTokensThreshold {
			cw.summaryTokens = maxTokens * 75 / 100
		} else {
			cw.summaryTokens = maxTokens * 85 / 100
		}
	} else {
		cw.summaryTokens = summaryTokens
	}

	capacity := cw.maxTokens - cw.bufferTokens
	if cw.currentTokens > capacity {
		cw.evictTo(capacity)
	}
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

// CurrentTokens returns the current token count of the context window.
// Thread-safe (read-locked).
func (cw *ContextWindow) CurrentTokens() int {
	cw.RLock()
	defer cw.RUnlock()
	return cw.currentTokens
}

// SummaryTokens returns the soft waterline threshold for triggering async compression.
// Thread-safe (read-locked).
func (cw *ContextWindow) SummaryTokens() int {
	cw.RLock()
	defer cw.RUnlock()
	return cw.summaryTokens
}

// SetReplayMode enables or disables replay mode.
//
// During replay, Push hooks are not called to avoid double writes.
func (cw *ContextWindow) SetReplayMode(on bool) {
	cw.Lock()
	defer cw.Unlock()

	cw.replayMode = on
}

// SetPendingDrainer sets the function used to drain pending user messages
// from the session queue. Called once during construction before the CW is
// shared. The function is not guarded by CW's mutex — it is read without
// a lock in DrainPending, which is safe because it is set once at startup.
func (cw *ContextWindow) SetPendingDrainer(fn func() string) {
	cw.pendingDrain = fn
}

// DrainPending checks the session's pending message queue and, if non-empty,
// pushes all pending messages as a single user turn into the context window.
//
// Called by the agent's tool loop at the top of each iteration, before
// building messages for the next LLM API call. This ensures queued user
// messages are injected at the next natural break point.
func (cw *ContextWindow) DrainPending() {
	if cw.pendingDrain == nil {
		return
	}
	if pending := cw.pendingDrain(); pending != "" {
		cw.Push(RoleUser, pending)
	}
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

// ─── Compression ──────────────────────────────────────────────────────────

// constMaxToolContentLen is the max content length (runes) for a tool
// message to be kept for compaction. Tool outputs longer than this are
// dropped entirely — memory only needs to know "what tool was called",
// not the full output.
const maxToolContentLen = 2000

// CompactAndReplace compresses all messages synchronously and replaces
// the context window content with system_prompt + summary.
//
// Unlike asyncCompact:
//   - runs synchronously (caller blocks until compression completes)
//   - respects the provided context for cancellation/timeout
//   - no CAS (caller must ensure no concurrent compact)
//
// Flow:
//  1. RLock: snapshot messages
//  2. Release lock, call segmented compaction engine
//  3. Lock: replace messages[1:] with merged summary
//  4. Unlock: call summaryHook with all segments
//
// Returns the summary on success, error on failure. On error, CW is NOT modified.
// Returns ("", nil) if no compactor is set.
func (cw *ContextWindow) CompactAndReplace(ctx context.Context) (string, error) {
	if cw.compactor == nil {
		return "", nil
	}

	cw.RLock()
	msgs := make([]Message, len(cw.messages))
	copy(msgs, cw.messages)
	tokensBefore := cw.currentTokens
	cw.RUnlock()

	if len(msgs) <= 1 {
		return "", nil // nothing to compact (only system prompt)
	}

	if cw.log != nil {
		cw.log.DebugContext(ctx, logger.CatMessages, "compact_and_replace: starting",
			"messages_count", len(msgs), "tokens_before", tokensBefore)
	}

	segments, finalSummary, err := cw.compactSegments(ctx, msgs)
	if err != nil {
		if cw.log != nil {
			cw.log.WarnContext(ctx, logger.CatMessages, "compact_and_replace: compression failed",
				"err", err.Error())
		}
		return "", err
	}

	cw.Lock()
	if len(cw.messages) > 0 && finalSummary != "" {
		summaryTokens := cw.tokenizer.Count(finalSummary)
		summaryMsg := Message{
			Role:    RoleSystem,
			Content: "[Previous Conversation Summary]\n" + finalSummary,
			Tokens:  summaryTokens,
		}
		tokensAfter := cw.messages[0].Tokens + summaryTokens
		removedTokens := tokensBefore - tokensAfter

		cw.messages = append(cw.messages[:1], summaryMsg)
		cw.currentTokens = tokensAfter

		if cw.log != nil {
			cw.log.InfoContext(ctx, logger.CatMessages, "compact_and_replace: completed",
				"messages_count_before", len(msgs),
				"messages_count_after", len(cw.messages),
				"tokens_before", tokensBefore,
				"tokens_after", tokensAfter,
				"tokens_saved", removedTokens,
				"summary_len", len(finalSummary),
				"segments", len(segments),
			)
		}
	}
	cw.Unlock()

	if cw.summaryHook != nil && len(segments) > 0 {
		cw.summaryHook(segments)
	}

	return finalSummary, nil
}

// asyncCompact compresses the conversation history using the Compactor.
//
// Runs in a separate goroutine triggered by the soft waterline check in Push.
// Uses CAS on summarizing to ensure only one compression runs at a time.
//
// Flow:
//  1. CAS summarizing false→true
//  2. RLock: snapshot messages
//  3. Release lock, call segmented compaction engine
//  4. Lock: replace messages[1:] with merged summary message
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

	if len(msgs) <= 1 {
		return
	}

	if cw.log != nil {
		cw.log.DebugContext(context.Background(), logger.CatMessages, "async_compact: snapshot taken",
			"messages_count", len(msgs),
			"tokens_before", tokensBefore,
		)
	}

	segments, finalSummary, err := cw.compactSegments(context.Background(), msgs)
	if err != nil {
		if cw.log != nil {
			cw.log.WarnContext(context.Background(), logger.CatMessages, "async_compact: compression failed",
				"err", err.Error())
		}
		return // compression failed, keep current state
	}

	// Replace history with summary under write lock
	cw.Lock()
	if len(cw.messages) > 0 && finalSummary != "" {
		summaryTokens := cw.tokenizer.Count(finalSummary)
		summaryMsg := Message{
			Role:    RoleSystem,
			Content: "[Conversation Summary]\n" + finalSummary,
			Tokens:  summaryTokens,
		}
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
				"summary_len", len(finalSummary),
				"segments", len(segments),
				"compact_duration", "", // not tracked in this path; add if needed
			)
		}
	}
	cw.Unlock()

	// Persist all segments (outside lock)
	if cw.summaryHook != nil && len(segments) > 0 {
		cw.summaryHook(segments)
	}
}

// compactSegments is the shared compaction engine.
//
// It filters oversized tool messages, groups by date, splits by tokens,
// compacts each batch, and merges recent (≤3 days) segments into a single
// final summary. Segments older than 3 days are kept in the returned slice
// but do NOT participate in the final CW summary.
//
// Returns:
//   - segments: all successfully compressed segments (including expired)
//   - finalSummary: the merged summary for recent segments (empty if none)
//   - err: only if every single batch failed
func (cw *ContextWindow) compactSegments(ctx context.Context, msgs []Message) ([]SummarySegment, string, error) {
	// 1. Drop oversized tool messages (memory only needs "what tool was called")
	filtered := filterOversizedToolMessages(msgs[1:]) // skip system prompt

	// 2. Group by calendar date
	byDate := groupMessagesByDate(filtered)
	if len(byDate) == 0 {
		return nil, "", nil
	}

	// 3. Compact each date group (split if > summaryTokens)
	var segments []SummarySegment
	for _, g := range byDate {
		tokens := cw.estimateBatchTokens(g.msgs)
		if tokens > cw.summaryTokens {
			subBatches := splitBatchByTokens(g.msgs, cw.summaryTokens)
			for _, sub := range subBatches {
				summary, err := cw.compactor.Compact(ctx, sub)
				if err != nil {
					if cw.log != nil {
						cw.log.WarnContext(ctx, logger.CatMessages, "compact_segments: sub-batch failed",
							"err", err.Error(), "date", g.date.Format("2006-01-02"))
					}
					continue // skip this sub-batch, keep going
				}
				segments = append(segments, SummarySegment{
					Summary: summary,
					Msgs:    sub,
					Date:    g.date,
				})
			}
		} else {
			summary, err := cw.compactor.Compact(ctx, g.msgs)
			if err != nil {
				if cw.log != nil {
					cw.log.WarnContext(ctx, logger.CatMessages, "compact_segments: batch failed",
						"err", err.Error(), "date", g.date.Format("2006-01-02"))
				}
				continue // skip this date group, keep going
			}
			segments = append(segments, SummarySegment{
				Summary: summary,
				Msgs:    g.msgs,
				Date:    g.date,
			})
		}
	}

	// If every batch failed, return empty segments but no error.
	// The caller (CompactAndReplace / asyncCompact) will keep CW unchanged.
	if len(segments) == 0 {
		return nil, "", nil
	}

	// 4. Extract recent segments (≤3 days)
	cutoff := time.Now().AddDate(0, 0, -3)
	var recent []SummarySegment
	for _, seg := range segments {
		if !seg.Date.Before(cutoff) { // seg.Date >= cutoff
			recent = append(recent, seg)
		}
	}

	// 5. Merge recent summaries into a single final summary
	var finalSummary string
	switch len(recent) {
	case 0:
		finalSummary = ""
	case 1:
		finalSummary = recent[0].Summary
	default:
		finalSummary = cw.mergeSummaries(ctx, recent)
	}

	return segments, finalSummary, nil
}

// mergeSummaries merges multiple daily summaries into one concise summary.
// If the second compactor call fails, it falls back to simple concatenation.
func (cw *ContextWindow) mergeSummaries(ctx context.Context, segments []SummarySegment) string {
	// Build a synthetic conversation for the compactor
	mergeMsgs := []Message{
		{
			Role:    RoleSystem,
			Content: "You are a context compression assistant. Merge the following conversation summaries into a single concise summary. Preserve all key decisions, file paths, code changes, and outcomes. Omit redundant details.",
		},
	}
	for _, seg := range segments {
		mergeMsgs = append(mergeMsgs, Message{
			Role:    RoleUser,
			Content: seg.Summary,
		})
	}

	merged, err := cw.compactor.Compact(ctx, mergeMsgs)
	if err != nil {
		if cw.log != nil {
			cw.log.WarnContext(ctx, logger.CatMessages, "merge_summaries: second-pass compaction failed, falling back to concat",
				"err", err.Error())
		}
		// Fallback: concatenate with separators
		var parts []string
		for _, seg := range segments {
			parts = append(parts, seg.Summary)
		}
		merged = strings.Join(parts, "\n\n---\n\n")
	}
	return merged
}

// ─── Compaction Helpers ────────────────────────────────────────────────────

// filterOversizedToolMessages drops tool messages whose content exceeds
// maxToolContentLen runes.  Memory only needs to know "what tool was
// called", not the full multi-megabyte output.
func filterOversizedToolMessages(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == RoleTool && len([]rune(m.Content)) > maxToolContentLen {
			continue
		}
		out = append(out, m)
	}
	return out
}

// dateGroup holds messages sharing the same calendar date.
type dateGroup struct {
	date time.Time
	msgs []Message
}

// groupMessagesByDate groups messages by the calendar date of their Timestamp.
// Messages with zero timestamp are skipped.  System prompt (index 0) must be
// excluded before calling.  Results are sorted oldest → newest.
func groupMessagesByDate(msgs []Message) []dateGroup {
	byDate := make(map[string][]Message)
	for _, m := range msgs {
		if m.Timestamp.IsZero() {
			continue
		}
		key := m.Timestamp.Format("2006-01-02")
		byDate[key] = append(byDate[key], m)
	}

	var groups []dateGroup
	for key, gmsgs := range byDate {
		t, _ := time.Parse("2006-01-02", key)
		groups = append(groups, dateGroup{date: t, msgs: gmsgs})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].date.Before(groups[j].date)
	})
	return groups
}

// estimateBatchTokens returns the total estimated tokens for a batch of
// messages, using the same logic as Push.
func (cw *ContextWindow) estimateBatchTokens(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		t := cw.tokenizer.Count(m.Content) + cw.tokenizer.Count(m.ReasoningContent)
		if len(m.ToolCalls) > 0 {
			t += cw.tokenizer.Count(toolCallsToJSON(m.ToolCalls))
		}
		total += t
	}
	return total
}

// minMsgsPerBatch is the minimum number of messages in a compaction sub-batch.
// It prevents excessive compactor calls when the message count is small.
const minMsgsPerBatch = 3

// splitBatchByTokens splits msgs into sub-batches so that each sub-batch
// has roughly equal message counts.  The caller already verified that the
// total exceeds maxTokens.
func splitBatchByTokens(msgs []Message, maxTokens int) [][]Message {
	// Fast path: if a single message already exceeds maxTokens,
	// we can't split it further; return as-is and let the compactor truncate.
	if len(msgs) == 1 {
		return [][]Message{msgs}
	}

	// Compute how many sub-batches we need.
	// Use the average tokens per message as a heuristic.
	totalTokens := 0
	for _, m := range msgs {
		t := len(m.Content) + len(m.ReasoningContent)
		if len(m.ToolCalls) > 0 {
			t += len(toolCallsToJSON(m.ToolCalls))
		}
		totalTokens += t
	}
	// If total fits within maxTokens, no split needed.
	if totalTokens <= maxTokens {
		return [][]Message{msgs}
	}

	avg := totalTokens / len(msgs)
	if avg == 0 {
		avg = 1
	}
	n := totalTokens / maxTokens
	if totalTokens%maxTokens > 0 {
		n++
	}
	if n < 2 {
		n = 2
	}

	// Cap by minimum messages per batch so we don't create tiny batches.
	maxBatches := len(msgs) / minMsgsPerBatch
	if len(msgs)%minMsgsPerBatch > 0 {
		maxBatches++
	}
	if n > maxBatches {
		n = maxBatches
	}
	if n < 2 {
		n = 2
	}
	if n > len(msgs) {
		n = len(msgs)
	}

	batchSize := len(msgs) / n
	remainder := len(msgs) % n

	var batches [][]Message
	idx := 0
	for i := 0; i < n; i++ {
		sz := batchSize
		if i < remainder {
			sz++
		}
		batches = append(batches, msgs[idx:idx+sz])
		idx += sz
	}
	return batches
}

// ─── Internal ───────────────────────────────────────────────────────────────

// evictTo runs the context eviction/truncation sequence.
func (cw *ContextWindow) evictTo(target int) {
	cw.pruneOlderTurnsEphemeralContent(2)
	cw.truncateMiddleOut()
	if cw.currentTokens > target {
		cw.slideFIFO(target)
	}
}

// evict runs the two-step eviction policy.
//
// Step 1: Middle-Out Truncation — targeting IsEphemeral Tool output
// Step 2: Turn-granularity FIFO sliding window
func (cw *ContextWindow) evict(newMsgTokens int) {
	capacity := cw.maxTokens - cw.bufferTokens
	target := capacity - newMsgTokens
	if target < 0 {
		target = 0
	}
	cw.evictTo(target)
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
