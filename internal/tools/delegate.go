package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ─── AgentLocator / Locatable ─────────────────────────────────────────────

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

// ─── Delegate 常量 ─────────────────────────────────────────────────────────

const (
	// DelegateDefaultTimeout 委托任务默认超时
	DelegateDefaultTimeout = 5 * time.Minute

	// DelegateMaxTimeout 委托任务最大超时
	DelegateMaxTimeout = 15 * time.Minute
)

// ─── DelegateTool ──────────────────────────────────────────────────────────

// delegateArgs 是 DelegateTool 的参数结构
type delegateArgs struct {
	Task string `json:"task"`
}

// 预计算参数 schema
var delegateParamsSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "task": {
      "type": "string",
      "description": "Task description to delegate"
    }
  },
  "required": ["task"]
}`)

// DelegateTool 将任务委托给指定 Team Leader
//
// 实现 Tool 接口 → 可被 ToolRegistry 注册 → LLM 通过 function calling 调用。
// 对 LLM 而言，delegate_dev(task="...") 与 file_read(path="...") 无区别。
type DelegateTool struct {
	LeaderID string       // 目标 Agent 的标识（如 "dev"）
	Desc     string       // Leader 描述（用于 Tool.Description）
	Locator  AgentLocator // 查找 Agent 实例
	Timeout  time.Duration
}

// compile-time check
var _ Tool = (*DelegateTool)(nil)

func (dt *DelegateTool) Name() string {
	return "delegate_" + dt.LeaderID
}

func (dt *DelegateTool) Description() string {
	return fmt.Sprintf("Delegate a task to team leader '%s': %s", dt.LeaderID, dt.Desc)
}

func (dt *DelegateTool) Parameters() json.RawMessage {
	return delegateParamsSchema
}

func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
	// 1. 解析参数
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		return "error: task is empty", nil
	}

	// 2. 从 locator 找到目标 Agent
	targetAgent, ok := dt.Locator.Locate(dt.LeaderID)
	if !ok {
		return fmt.Sprintf("error: team leader '%s' not found", dt.LeaderID), nil
	}

	// 3. 委托超时
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	// 在 caller ctx 基础上叠加超时
	delCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 4. 调用目标 Agent
	result, err := targetAgent.Ask(delCtx, dArgs.Task)
	if err != nil {
		// 区分委托超时 vs 父 ctx 取消
		if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return fmt.Sprintf("error: delegation to %s timed out after %s, task has been cancelled", dt.LeaderID, timeout), nil
		}
		return "error: " + err.Error(), nil
	}

	return result, nil
}
