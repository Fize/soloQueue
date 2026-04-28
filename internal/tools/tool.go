// Package tools 定义 Tool 接口和内置工具实现
//
// 核心概念：
//   - Tool：最小粒度的可调用单元，1:1 映射 LLM function calling
//   - Confirmable：可选接口，支持"执行前需用户确认"流程
//   - ToolRegistry：name → Tool 的并发安全映射（定义在 registry.go）
//
// 依赖方向：
//
//	tools 不依赖任何人（定义 Tool 接口）
//	skill → tools（SkillRegistry 组合 ToolRegistry）
//	agent → skill + tools
package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

// ─── Tool interface ──────────────────────────────────────────────────────────

// Tool 是可被 Agent 调用的工具
//
// Name / Description / Parameters 三个方法在 Agent 构造 LLMRequest 时读取，
// 应视为只读常量（每次 Agent 调用 LLM 前都会读一次，但返回值不应变化）。
// Execute 在 Agent run goroutine 中串行调用（同一 Agent 同一时刻只有一个
// 工具在跑），但不同 Agent 可能并发调用同一个 Tool 实例 —— 因此 Execute
// 实现必须是并发安全的。
type Tool interface {
	// Name 返回工具名；必须非空、在同一 Agent 内唯一
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
	//   - err   ：执行错误；Agent 会把 "error: "+err.Error() 喂回 LLM，不中断循环
	//
	// ctx 的取消应尽快响应；若 Execute 实现不响应 ctx，Agent 只能靠外层超时。
	Execute(ctx context.Context, args string) (result string, err error)
}

// Confirmable 是可选接口；工具可实现它以支持"执行前需用户确认"流程。
//
// 不实现该接口的工具保持原行为（直接 Execute）。
// 实现该接口的工具在 CheckConfirmation 返回 true 时，Agent 会：
//  1. 发送 ToolNeedsConfirmEvent（含 Options）
//  2. 阻塞等待外部调用 Agent.Confirm(callID, choice)
//  3. choice != "" 时：调用 ConfirmArgs 修改 args，然后 Execute
//  4. choice == ""（取消/拒绝）时：返回 "error: user denied execution"
type Confirmable interface {
	Tool
	// CheckConfirmation 检查给定 args 是否需要用户确认。
	// 返回 (needsConfirm bool, prompt string)：prompt 用于展示给用户。
	CheckConfirmation(args string) (needsConfirm bool, prompt string)
	// ConfirmationOptions 返回可选操作列表。
	// 返回 nil 或空切片表示二元确认（确认/拒绝），UI 可用 "yes"/"" 表示。
	// 非空时，UI 应展示选项供用户选择，choice 为用户选中的选项值。
	ConfirmationOptions(args string) []string
	// ConfirmArgs 在用户做出选择后修改原始 args。
	// choice 为用户选择的选项值；二元确认时 ChoiceApprove 表示确认，ChoiceDeny 表示拒绝。
	// 修改后的 args 将传入 Execute。
	ConfirmArgs(originalArgs string, choice ConfirmChoice) string
	// SupportsSessionWhitelist 返回该工具是否支持 "allow-in-session" 选项。
	// 返回 false 时，UI 不应展示"本次全部允许"按钮。
	SupportsSessionWhitelist() bool
}

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

// ─── AsyncTool interface ────────────────────────────────────────────────────

// AsyncAction describes the intent for an asynchronous tool execution.
//
// Returned by AsyncTool.ExecuteAsync. The tool only declares "what I want
// to do asynchronously" — it does not start a goroutine. The framework
// is fully responsible for scheduling.
type AsyncAction struct {
	Target  iface.Locatable // target agent (already located)
	Prompt  string          // task description to send
	Timeout time.Duration   // delegation timeout
}

// TargetID returns the target agent's identifier for logging and tracing.
// The current Locatable interface does not expose an ID method, so this
// returns an empty string. Record targetID externally if needed.
func (a *AsyncAction) TargetID() string {
	return ""
}

// AsyncTool 可选接口；工具可实现它以声明异步执行意图。
//
// 不实现该接口的工具走普通 Execute 路径。
// 实现该接口的工具由 Agent 的 execTools 统一调度：
//
//   - execTools 在启动 goroutine 前完成所有上下文拼装（asyncTurnState）
//   - 彻底消灭两阶段注册的竞态风险
//   - Tool 不启动 goroutine，只返回意图；框架负责 go + 注册 + 回收
//
// 模式与 Confirmable 一致：在 execTools 中通过 type assertion 检测。
type AsyncTool interface {
	Tool
	// ExecuteAsync 返回异步执行意图，不启动 goroutine。
	// 框架负责：组装 asyncTurnState → 注册到 Agent → 启动 goroutine → 监听结果。
	ExecuteAsync(ctx context.Context, args string) (*AsyncAction, error)
}
