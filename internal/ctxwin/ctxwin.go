package ctxwin

import (
	"encoding/json"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/llm"
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

// ─── Option ─────────────────────────────────────────────────────────────────

// Option 配置 ContextWindow 的可选行为
type Option func(*ContextWindow)

// WithPushHook 设置 Push 完成后的回调
func WithPushHook(hook PushHook) Option {
	return func(cw *ContextWindow) { cw.pushHook = hook }
}

// ─── ContextWindow ──────────────────────────────────────────────────────────

// ContextWindow 是纯内存态、基于规则的线性上下文截断器
//
// 核心不变式：
//   - currentTokens 是"当前上下文窗口中消息的 token 总量"的最佳近似
//   - sum(messages[i].Tokens) 不一定等于 currentTokens（Calibrate 后产生漂移）
//   - currentTokens 用于淘汰决策，是可信的
//   - msg.Tokens 是单条消息的估算值，仅用于增量计算
//
// 非并发安全：由 Session 的 mutex 保护，Session 保证 Ask 串行。
type ContextWindow struct {
	messages      []Message
	maxTokens     int        // 来自 config.LLMModel.ContextWindow
	bufferTokens  int        // 预留给模型回答的空间（默认 2000）
	currentTokens int        // 实时值；Calibrate 后为精确值，Push 后含估算增量
	tokenizer     *Tokenizer // 共享，初始化后不可变
	pushHook      PushHook   // Push 完成后的回调（可为 nil）
	replayMode    bool       // replay 期间禁用 pushHook
}

// NewContextWindow 创建上下文窗口
//
// maxTokens 来自 config.LLMModel.ContextWindow。
// bufferTokens 预留给模型回答的空间（默认 2000）。
func NewContextWindow(maxTokens, bufferTokens int, tokenizer *Tokenizer, opts ...Option) *ContextWindow {
	cw := &ContextWindow{
		maxTokens:    maxTokens,
		bufferTokens: bufferTokens,
		tokenizer:    tokenizer,
	}
	for _, opt := range opts {
		opt(cw)
	}
	return cw
}

// ─── 核心外部 API ───────────────────────────────────────────────────────────

// Push 追加新消息，计算 token，超载时触发淘汰
//
// Token 计算包含 Content + ReasoningContent + ToolCalls 的 JSON 表示。
// 如果 currentTokens + 新消息的 token 数超过 maxTokens - bufferTokens，
// 会同步执行两步淘汰策略（中间截断 + Turn 粒度 FIFO）。
func (cw *ContextWindow) Push(role MessageRole, content string, opts ...PushOption) {
	msg := Message{Role: role, Content: content}
	for _, opt := range opts {
		opt(&msg)
	}
	// Token 计数包含 Content + ReasoningContent + ToolCalls
	msg.Tokens = cw.tokenizer.Count(content) + cw.tokenizer.Count(msg.ReasoningContent)
	if len(msg.ToolCalls) > 0 {
		msg.Tokens += cw.tokenizer.Count(toolCallsToJSON(msg.ToolCalls))
	}
	// 容量检查 & 淘汰
	capacity := cw.maxTokens - cw.bufferTokens
	if cw.currentTokens+msg.Tokens > capacity {
		cw.evict(msg.Tokens)
	}
	cw.messages = append(cw.messages, msg)
	cw.currentTokens += msg.Tokens

	// Push 完成后调用 Hook（replay 期间不调用）
	if cw.pushHook != nil && !cw.replayMode {
		cw.pushHook(msg)
	}
}

// BuildPayload 将当前内存中的 Message 切片转为 PayloadMessage 切片
//
// 每次 DeepSeek API 请求前调用。返回新切片，调用方可安全修改。
// Agent 包负责将 PayloadMessage 转为 agent.LLMMessage。
func (cw *ContextWindow) BuildPayload() []PayloadMessage {
	out := make([]PayloadMessage, 0, len(cw.messages))
	for _, m := range cw.messages {
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

// Calibrate 用 API 返回的 PromptTokens 精确校准 currentTokens
//
// ⚠️ 时序要求：必须在 Push 新消息（assistant/tool）之前调用。
// 调用顺序必须是：
//   1. 收到 API EventDone → Calibrate(usage.PromptTokens)
//   2. 然后 Push(assistant+tool_calls) / Push(tool result)
//
// 如果顺序反了，Calibrate 会把新 Push 的估算增量抹掉。
//
// ⚠️ 漂移说明：Calibrate 后 currentTokens 是精确值，但各 msg.Tokens
// 仍是原始估算值。因此 sum(messages.Tokens) != currentTokens 是正常的。
// FIFO 淘汰减去的是估算值，会产生轻微漂移，但下次 Calibrate 会纠正。
func (cw *ContextWindow) Calibrate(promptTokens int) {
	cw.currentTokens = promptTokens
}

// Overflow 检查当前 payload 是否超过硬上限
//
// 在发送 API 请求前调用。如果返回 true，应中止请求并报错。
// hardLimit 取 config.LLMModel.ContextWindow（模型物理上限）。
func (cw *ContextWindow) Overflow(hardLimit int) bool {
	return cw.currentTokens > hardLimit
}

// ─── 查询 & 变更 ───────────────────────────────────────────────────────────

// TokenUsage 返回 (currentTokens, maxTokens, bufferTokens)
func (cw *ContextWindow) TokenUsage() (current, max, buffer int) {
	return cw.currentTokens, cw.maxTokens, cw.bufferTokens
}

// Len 返回消息数量
func (cw *ContextWindow) Len() int {
	return len(cw.messages)
}

// MessageAt 返回索引 i 处的消息拷贝
func (cw *ContextWindow) MessageAt(i int) (Message, bool) {
	if i < 0 || i >= len(cw.messages) {
		return Message{}, false
	}
	return cw.messages[i], true
}

// PopLast 移除并返回最后一条消息
//
// Session 用于移除失败 push 的 user prompt。
// 返回被移除的消息和 true，或零值 Message 和 false（如果为空）。
func (cw *ContextWindow) PopLast() (Message, bool) {
	if len(cw.messages) == 0 {
		return Message{}, false
	}
	last := cw.messages[len(cw.messages)-1]
	cw.messages = cw.messages[:len(cw.messages)-1]
	cw.currentTokens -= last.Tokens
	if cw.currentTokens < 0 {
		cw.currentTokens = 0 // 漂移修正
	}
	return last, true
}

// Reset 重置上下文窗口，仅保留 system prompt（索引 0）
//
// 用于 /clear 命令：清空对话历史，保留系统提示词。
func (cw *ContextWindow) Reset() {
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

// SetReplayMode 设置 replay 模式
//
// replay 期间 Push Hook 不会被调用，避免双重写入。
func (cw *ContextWindow) SetReplayMode(on bool) {
	cw.replayMode = on
}

// Recalculate 从头重算所有消息的估算 token 总和
//
// 仅用于调试/测试。生产代码不调用。
// 注意：Calibrate 后此值可能不等于 currentTokens（这是正常的漂移）。
func (cw *ContextWindow) Recalculate() int {
	total := 0
	for _, m := range cw.messages {
		total += m.Tokens
	}
	return total
}

// ─── 内部方法 ───────────────────────────────────────────────────────────────

// evict 执行两步淘汰策略
//
// Step 1: 中间截断法（Middle-Out Truncation）—— 针对 IsEphemeral 的 Tool 输出
// Step 2: Turn 粒度 FIFO 滑动窗口
func (cw *ContextWindow) evict(newMsgTokens int) {
	cw.truncateMiddleOut()
	capacity := cw.maxTokens - cw.bufferTokens
	target := capacity - newMsgTokens
	if cw.currentTokens > target {
		cw.slideFIFO(target)
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// toolCallsToJSON 将 ToolCalls 序列化为 JSON 字符串（用于 token 计数）
func toolCallsToJSON(tcs []llm.ToolCall) string {
	if len(tcs) == 0 {
		return ""
	}
	b, err := json.Marshal(tcs)
	if err != nil {
		// 序列化失败时返回空字符串，token 计数会略低
		return fmt.Sprintf("%v", tcs)
	}
	return string(b)
}
