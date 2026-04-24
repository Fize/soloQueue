// Package llm 定义跨 provider 共享的类型和工具：
//
//   - 消息 / 请求 / 响应里用到的 ToolCall / ToolDef / FunctionCall / Usage 等共享结构
//   - Streaming 用的 Event（tagged struct）
//   - Provider-无关的 APIError（实现 error + IsRetryable）
//   - RunWithRetry：指数退避的通用 retry helper
//
// 具体 provider 实现（如 DeepSeek）放在子包（如 llm/deepseek/）。
//
// 本包本身不引入 net/http —— HTTP 客户端由子包自己实现。
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ─── Tool-calling shared types ───────────────────────────────────────────────

// ToolCall 是 assistant 消息里的一次工具调用请求。
//
// 请求和响应两个方向通用：
//   - 响应：LLM 告诉我们它想调哪个 tool
//   - 请求：assistant 历史消息里回放 LLM 之前的 tool_calls
//
// Function.Arguments 是 JSON 编码后的字符串（**不是 JSON object**），
// 按 OpenAI-compat 规范原样透传。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // 固定 "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 是一次具体的 function 调用
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串；由 caller 解析为具体对象
}

// ToolDef 是请求里告诉 LLM 有哪些 tool 可用
type ToolDef struct {
	Type     string       `json:"type"` // 固定 "function"
	Function FunctionDecl `json:"function"`
}

// FunctionDecl 是 function 的声明（不含运行时数据）
type FunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema，client 不做验证
}

// ─── Usage ───────────────────────────────────────────────────────────────────

// Usage 是 token 计数
//
// 标准字段（PromptTokens/CompletionTokens/TotalTokens）所有 provider 都有；
// 后面几个是 DeepSeek 特有，其他 provider 留零即可。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// DeepSeek 特有
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
	ReasoningTokens       int `json:"reasoning_tokens,omitempty"`
}

// ─── FinishReason ────────────────────────────────────────────────────────────

// FinishReason 是响应的终止原因
//
// 标准值：
//
//	"stop"                           正常结束
//	"length"                         达到 max_tokens
//	"tool_calls"                     LLM 产生了 tool_call，等待 caller 执行
//	"content_filter"                 被内容过滤拦截
//	"insufficient_system_resource"   DeepSeek 特有，服务资源不足
type FinishReason string

const (
	FinishStop          FinishReason = "stop"
	FinishLength        FinishReason = "length"
	FinishToolCalls     FinishReason = "tool_calls"
	FinishContentFilter FinishReason = "content_filter"
)

// ─── Streaming Event ─────────────────────────────────────────────────────────

// EventType 是 Event 的判别字段
type EventType int

const (
	// EventDelta 增量内容（content / reasoning_content / tool_call 增量）
	EventDelta EventType = iota
	// EventDone 流正常结束；带 FinishReason，可能带 Usage
	EventDone
	// EventError 流中途出错；带 Err
	EventError
)

// String 便于调试
func (t EventType) String() string {
	switch t {
	case EventDelta:
		return "delta"
	case EventDone:
		return "done"
	case EventError:
		return "error"
	default:
		return "unknown"
	}
}

// Event 是 streaming channel 上流动的事件
//
// 用 tagged struct（Type 字段 + 按类型填字段）而非 interface —— 零分配、
// 调用方 switch 就行，且零值安全。
//
// 字段规则：
//   - Delta  事件：只读 ContentDelta / ReasoningContentDelta / ToolCallDelta
//   - Done   事件：只读 FinishReason / Usage
//   - Error  事件：只读 Err
type Event struct {
	Type EventType

	// Delta
	ContentDelta          string
	ReasoningContentDelta string
	ToolCallDelta         *ToolCallDelta

	// Done
	FinishReason FinishReason
	Usage        *Usage

	// Error
	Err error
}

// ToolCallDelta 是 streaming 中 tool_call 的一个增量
//
// 累积规则（caller 实现）：
//   - 按 Index 作为 slot 归位
//   - 第一次出现带 ID + Name；后续仅带 Arguments 片段
//   - Arguments 字符串连接成完整 JSON
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// ─── APIError ────────────────────────────────────────────────────────────────

// APIError 是 provider 返回的结构化错误
//
// 对应 OpenAI / DeepSeek 共同的错误体：
//
//	{"error": {"message": ..., "type": ..., "code": ..., "param": ...}}
type APIError struct {
	StatusCode int
	Type       string // "authentication_error" / "rate_limit_reached" / "insufficient_balance" / ...
	Code       string // "invalid_api_key" / ...
	Message    string
	Param      string
}

// Error 实现 error
func (e *APIError) Error() string {
	if e == nil {
		return "<nil>"
	}
	parts := fmt.Sprintf("llm: http %d", e.StatusCode)
	if e.Type != "" {
		parts += ": " + e.Type
	}
	if e.Code != "" {
		parts += " (" + e.Code + ")"
	}
	if e.Message != "" {
		parts += ": " + e.Message
	}
	return parts
}

// IsRetryable 判断该错误是否值得 retry
//
// 策略：
//   - 5xx：服务端问题，应 retry
//   - 429：限流，应 retry（指数退避）
//   - 4xx（非 429）：客户端错误，不 retry
//
// 未知 status（0，网络错误）默认不当作 APIError —— 调用方若用 errors.As 拿不出
// APIError，走另一条路径（通常网络错误也该 retry）。
func (e *APIError) IsRetryable() bool {
	if e == nil {
		return false
	}
	if e.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return e.StatusCode >= 500 && e.StatusCode < 600
}

// IsRetryableErr 是通用判断：网络错误 retryable；APIError 看 status；其他不 retry
//
// 这是 provider client 给 RunWithRetry 的标配 shouldRetry 实现。
func IsRetryableErr(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsRetryable()
	}
	// 其他（网络 / EOF / timeout）默认 retry
	return true
}
