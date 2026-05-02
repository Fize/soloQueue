// Package agent 提供 agent 的骨架：
//
//   - Agent：持有 LLM + 配置 + 日志的长期运行单元。Start 启动一个内部
//     goroutine 串行消费 mailbox 里的 job；Ask 是"投递 prompt → 等 reply"
//     的同步糖衣；Submit 是投递任意自定义 job 的低层逃生门。
//   - Registry：ID → Agent 的并发安全映射，支持批量 Start/Stop/Shutdown。
//   - LLMClient：LLM 调用的最小接口（对接 DeepSeek / FakeLLM）。
//   - Tool / ToolRegistry：供 LLM 调用的工具抽象；agent 的 runOnce 会在
//     收到 tool_calls 时自动循环调度 —— 执行 → 塞回 → 再 Chat，直到
//     LLM 不再要工具为止（封顶 MaxIterations）。
//   - FakeLLM：供测试 / demo 使用的 LLMClient 实现（支持 ToolCallsByTurn
//     以便脚本化多轮 tool-use 场景）。
//   - RunOnce：不启动 goroutine 的一次性调用（脚本 / 简单 CLI 场景）。
//
// 本 phase 不含：并行 tool 执行、per-tool 超时、ChatStream 的 tool 循环、
// supervisor / restart。这些留给后续 phase。
package agent

import "errors"

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	// ErrAgentNotFound Registry 中找不到目标 agent
	ErrAgentNotFound = errors.New("agent: not found")
	// ErrAgentAlreadyExists Register 时 ID 已存在
	ErrAgentAlreadyExists = errors.New("agent: already exists")
	// ErrAgentNil Register 传入 nil agent
	ErrAgentNil = errors.New("agent: nil")
	// ErrEmptyID agent 的 Definition.ID 为空
	ErrEmptyID = errors.New("agent: empty id")

	// ErrAlreadyStarted Start 被调用但 agent 已在运行
	ErrAlreadyStarted = errors.New("agent: already started")
	// ErrNotStarted Ask / Submit / Stop 被调用但 agent 从未 Start，或 Start 后已 Stop
	ErrNotStarted = errors.New("agent: not started")
	// ErrStopped agent 已进入 Stopped 态；等待中的 Ask / Submit 通过此 error 返回
	ErrStopped = errors.New("agent: stopped")
	// ErrStopTimeout Stop 带 timeout 时超时；goroutine 最终仍会退出
	ErrStopTimeout = errors.New("agent: stop timeout")

	// ErrMaxIterations runOnce 的 tool-use 循环超过 Definition.MaxIterations
	ErrMaxIterations = errors.New("agent: too many tool calls without finishing — rephrase your request or split it into smaller steps")
)


