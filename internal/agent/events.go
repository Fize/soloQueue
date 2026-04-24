package agent

import (
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── AgentEvent（sealed interface）─────────────────────────────────────────
//
// AskStream 向调用方产出的事件。采用 sealed interface 模式（未导出
// marker method → 包外无法实现）—— 这是 Go 社区"error / slog.Value"
// 同款写法，好处是：
//
//   - 调用方 type switch 时，IDE 补全只看见该事件类型的合法字段，
//     不会出现 tagged struct 中"Type=X 时 Y 字段才有效"的心智负担。
//   - 增加新事件类型时，已有 switch 语句的 default 分支会自然兜底；
//     配 exhaustive linter 可静态检查覆盖。
//   - 未来给 WebSocket 写 `MarshalJSON` 时，每个类型独立，天然对应
//     discriminated union 协议。
//
// 下层 llm.Event 继续用 tagged struct（wire 层字段少且固定，一次分配
// 最省），两者在 runOnceStream 中做唯一一次映射。
type AgentEvent interface {
	agentEvent() // unexported marker → 仅本包内类型可实现
}

// ContentDeltaEvent 本轮 LLM 响应的 content 增量
type ContentDeltaEvent struct {
	Iter  int
	Delta string
}

// ReasoningDeltaEvent deepseek-reasoner 专用的 reasoning_content 增量
type ReasoningDeltaEvent struct {
	Iter  int
	Delta string
}

// ToolCallDeltaEvent LLM 正在流式组装一次 tool_call 的 arguments 增量
//
// 首个 delta 通常携带 CallID / Name；后续同 CallID 的 delta 只带 ArgsDelta。
// UI 可以据此实时展示"模型正在调用哪个工具"（虚线占位符之类）。
type ToolCallDeltaEvent struct {
	Iter      int
	CallID    string
	Name      string
	ArgsDelta string
}

// ToolExecStartEvent agent 开始真正执行某个 tool（args 已完整）
type ToolExecStartEvent struct {
	Iter   int
	CallID string
	Name   string
	Args   string // 完整 JSON 字符串（已拼好）
}

// ToolExecDoneEvent 一个 tool 执行完成（成功或失败）
//
// 当 Err != nil，Result 通常为空；Err 的文本已被 runOnceStream
// 格式化为 "error: ..." 喂回 LLM，这里也一并暴露给 UI 展示。
type ToolExecDoneEvent struct {
	Iter     int
	CallID   string
	Name     string
	Result   string
	Err      error
	Duration time.Duration
}

// IterationDoneEvent 当前 LLM 迭代结束（一次 Chat 流式读完）
//
// 在真正开始 tool 执行之前产出：UI 可据此显示"LLM 说完了、正在执行工具..."
type IterationDoneEvent struct {
	Iter         int
	FinishReason llm.FinishReason
	Usage        llm.Usage
}

// DoneEvent 整个 AskStream 正常结束；Content 是最终 assistant content
type DoneEvent struct {
	Content string
}

// ToolNeedsConfirmEvent 某个 tool 需要用户确认才能继续执行
//
// UI/TUI/Web 收到后应向用户展示 Prompt 和 Options（若有）。
// 用户选择后调用 Agent.Confirm(callID, choice)。
// Options 为空表示二元确认，choice 用 "yes"（确认）或 ""（拒绝）。
// 此外，若 AllowInSession 为 true，客户端可提供 "allow-in-session" 选项，
// 选择后会将该 tool 加入当前 session 的放行白名单，本轮后续不再确认。
type ToolNeedsConfirmEvent struct {
	Iter           int
	CallID         string
	Name           string
	Args           string
	Prompt         string
	Options        []string
	AllowInSession bool
}

// ErrorEvent AskStream 因错误终止（ctx cancel / LLM 错误 / 超出 MaxIterations）
//
// ErrorEvent 总是最后一条事件；产出后 channel 立即关闭。
type ErrorEvent struct {
	Err error
}

// marker impls —— 每个类型实现 AgentEvent
func (ContentDeltaEvent) agentEvent()     {}
func (ReasoningDeltaEvent) agentEvent()   {}
func (ToolCallDeltaEvent) agentEvent()    {}
func (ToolExecStartEvent) agentEvent()    {}
func (ToolExecDoneEvent) agentEvent()     {}
func (ToolNeedsConfirmEvent) agentEvent() {}
func (IterationDoneEvent) agentEvent()    {}
func (DoneEvent) agentEvent()             {}
func (ErrorEvent) agentEvent()            {}
