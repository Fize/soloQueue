package skill

import "context"

// AgentLocator 按 ID 查找运行中的 Agent 实例
//
// DelegateTool 使用此接口查找目标 Agent，解耦对具体 Registry 的直接依赖。
// 由 Agent 包的 Registry 实现。
type AgentLocator interface {
	// Locate 按 ID 查找 Agent；不存在返回 (nil, false)
	Locate(id string) (Locatable, bool)
}

// Locatable 是 Agent 的最小抽象，供 DelegateTool 调用
//
// DelegateTool 只需要 Ask 能力，不需要知道 Agent 的完整接口。
// 由 Agent 包的 Agent 类型实现。
type Locatable interface {
	Ask(ctx context.Context, prompt string) (string, error)
}
