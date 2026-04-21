// Package agent 提供 agent 的最小骨架：
//
//   - Agent：绑定 LLM + 配置 + 日志的同步调用单元。每次 Run 调一次 LLM
//     返回结果；不维护历史；不启动后台 goroutine。
//   - Registry：ID → Agent 的并发安全映射。
//   - LLMClient：LLM 调用的最小接口（下 phase 接入真实 HTTP 客户端）。
//   - FakeLLM：供测试 / demo 使用的 LLMClient 实现。
//
// 本 phase 不含：后台 goroutine / channel / 状态机 / supervisor / tool 系统。
// 交互语义（delegate / reply_to_user / ask_user 等）属于下 phase 的 tool 系统。
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
)
