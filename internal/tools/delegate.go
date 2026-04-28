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
//
// 两种模式：
//   - 同步模式（L2 → L3）：通过 Locator 查找已注册 Agent，Execute 内阻塞等待
//   - 异步模式（L1 → L2）：通过 SpawnFn 闭包获取目标 Agent，框架层统一调度
//
// 异步模式下实现 AsyncTool 接口，Tool 只声明意图不启动 goroutine。
type DelegateTool struct {
	LeaderID string       // 目标 Agent 的标识（如 "dev"）
	Desc     string       // Leader 描述（用于 Tool.Description）
	Timeout  time.Duration

	// 同步模式（L2 → L3）：查找已注册 Agent
	Locator AgentLocator

	// 异步模式（L1 → L2）：闭包注入，动态孵化或查找 Agent
	// nil = 同步模式（用 Locator）
	// non-nil = 异步模式（走 AsyncTool.ExecuteAsync 路径）
	SpawnFn func(ctx context.Context, task string) (Locatable, error)
}

// compile-time checks
var (
	_ Tool     = (*DelegateTool)(nil)
	_ AsyncTool = (*DelegateTool)(nil)
)

func (dt *DelegateTool) Name() string {
	return "delegate_" + dt.LeaderID
}

func (dt *DelegateTool) Description() string {
	return fmt.Sprintf("Delegate a task to team leader '%s': %s", dt.LeaderID, dt.Desc)
}

func (dt *DelegateTool) Parameters() json.RawMessage {
	return delegateParamsSchema
}

// Execute 同步执行委托（L2 → L3 路径）
//
// 在 Execute 内阻塞调用 target.Ask()，等待结果返回。
// 当 DelegateTool 有 SpawnFn 时，也会走同步路径（由框架决定何时走异步）。
func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
	// 1. 解析参数
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		return "error: task is empty", nil
	}

	// 2. 获取目标 Agent
	var targetAgent Locatable
	if dt.SpawnFn != nil {
		var err error
		targetAgent, err = dt.SpawnFn(ctx, dArgs.Task)
		if err != nil {
			return fmt.Sprintf("error: failed to spawn agent '%s': %s", dt.LeaderID, err), nil
		}
	} else {
		var ok bool
		targetAgent, ok = dt.Locator.Locate(dt.LeaderID)
		if !ok {
			return fmt.Sprintf("error: team leader '%s' not found", dt.LeaderID), nil
		}
	}

	// 3. 委托超时
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	// 在 caller ctx 基础上叠加超时（级联取消：callerCtx 取消 → delCtx 取消 → L2/L3 收到）
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

// ExecuteAsync 实现 AsyncTool 接口（L1 → L2 异步路径）
//
// 只声明异步执行意图，不启动 goroutine。框架层负责：
//  1. 组装 asyncTurnState（cw, out, results 等全部就位）
//  2. 注册到 Agent.asyncTurns
//  3. 启动 goroutine 执行 Ask
//  4. 监听结果，聚合后恢复 tool loop
//
// 返回的 AsyncAction 包含目标 Agent 的引用和任务描述。
func (dt *DelegateTool) ExecuteAsync(ctx context.Context, args string) (*AsyncAction, error) {
	// 1. 解析参数
	var dArgs delegateArgs
	if err := json.Unmarshal([]byte(args), &dArgs); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	if dArgs.Task == "" {
		return nil, fmt.Errorf("task is empty")
	}

	// 2. 获取目标 Agent（只声明意图，不执行）
	var target Locatable
	var err error

	if dt.SpawnFn != nil {
		target, err = dt.SpawnFn(ctx, dArgs.Task)
		if err != nil {
			return nil, fmt.Errorf("failed to spawn agent '%s': %w", dt.LeaderID, err)
		}
	} else if dt.Locator != nil {
		var ok bool
		target, ok = dt.Locator.Locate(dt.LeaderID)
		if !ok {
			return nil, fmt.Errorf("team leader '%s' not found", dt.LeaderID)
		}
	} else {
		return nil, fmt.Errorf("delegate tool '%s': no Locator or SpawnFn configured", dt.LeaderID)
	}

	// 3. 返回异步意图
	timeout := dt.Timeout
	if timeout <= 0 {
		timeout = DelegateDefaultTimeout
	}
	if timeout > DelegateMaxTimeout {
		timeout = DelegateMaxTimeout
	}

	return &AsyncAction{
		Target:  target,
		Prompt:  dArgs.Task,
		Timeout: timeout,
	}, nil
}

// IsAsync 返回此 DelegateTool 是否配置为异步模式
func (dt *DelegateTool) IsAsync() bool {
	return dt.SpawnFn != nil
}
