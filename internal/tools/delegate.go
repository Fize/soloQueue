package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"reflect"
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
// DelegateTool 支持两种调用模式：
//   - Ask: 简单阻塞式调用，返回最终结果字符串
//   - AskStream: 流式调用，返回所有事件（包括 ToolNeedsConfirmEvent）
//
// 在 tool execution context 中，parent event channel 可能通过 context.Value 注入
// （由 agent.execToolStream 负责）。DelegateTool.Execute 在流式调用时会检测并
// 使用它来中继子 Agent 的事件（特别是 ToolNeedsConfirmEvent）到 parent。
//
// 由 Agent 包的 Agent 类型实现。
type Locatable interface {
	// Ask 向目标 Agent 投递一个阻塞式请求，返回最终结果。
	// 此方法不返回中间事件（ContentDeltaEvent 等被内部消费），仅返回最终 content + error。
	Ask(ctx context.Context, prompt string) (string, error)

	// AskStream 向目标 Agent 投递一个流式请求，返回事件通道。
	// 返回值为泛型 interface{} 通道，实际接收到的是 agent.AgentEvent 类型
	// （通过 type assertion 转换）。
	//
	// 返回的通道可能包含以下事件类型：
	//   - ContentDeltaEvent: LLM 回复内容增量
	//   - ToolNeedsConfirmEvent: 工具需要用户确认（可通过 Confirm 方法响应）
	//   - DoneEvent: 成功完成
	//   - ErrorEvent: 失败或被取消
	//   - 其他事件类型: ToolCallDeltaEvent, ToolExecStartEvent, ToolExecDoneEvent 等
	//
	// 调用方应持续 range 直到通道关闭；中途放弃 range 会导致背压，
	// 因此放弃前必须 cancel ctx。
	AskStream(ctx context.Context, prompt string) (<-chan interface{}, error)

	// Confirm 响应一个未完成的工具确认请求。
	// 由 ToolNeedsConfirmEvent 触发，待决确认在工具层阻塞等待此方法的调用。
	//
	// 参数：
	//   - callID: 来自 ToolNeedsConfirmEvent.CallID，唯一标识该确认请求
	//   - choice: 用户的选择。合法值包括：
	//       * "yes": 确认执行（ChoiceApprove）
	//       * "": 拒绝执行（ChoiceDeny）
	//       * "allow-in-session": 确认执行并加入会话白名单（ChoiceAllowInSession）
	//       * 其他：ToolNeedsConfirmEvent.Options 中定义的选项值
	//
	// 返回错误条件：
	//   - 若不存在该 callID 的待决确认
	//   - 若该确认已被其他方式响应
	//   - 若响应超时
	Confirm(callID string, choice string) error
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
// 调用 AskStream 获取流式事件通道，中继所有事件（包括 ToolNeedsConfirmEvent）到
// parent event channel（如果通过 context 注入）。仅累积 ContentDeltaEvent 的内容
// 和最终的 DoneEvent/ErrorEvent 信息作为返回值。
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

	// 在 caller ctx 基础上叠加超时
	delCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 4. 从 ctx 提取 parent event channel（由 agent.execToolStream 注入）
	parentEventCh, _ := ToolEventChannelFromCtx(ctx)

	// 4b. 从 ctx 提取 confirm forwarder（由 agent.execToolStream 注入）
	confirmFwd, hasConfirmFwd := ConfirmForwarderFromCtx(ctx)

	// 5. 调用目标 Agent 的流式接口
	evCh, err := targetAgent.AskStream(delCtx, dArgs.Task)
	if err != nil {
		if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			return fmt.Sprintf("error: delegation to %s timed out after %s, task has been cancelled", dt.LeaderID, timeout), nil
		}
		return "error: " + err.Error(), nil
	}

	// 6. 消费事件：中继到 parent，并追踪内容/错误
	var content string
	var finalErr error

	for ev := range evCh {
		if ev == nil {
			continue
		}

		// 中继事件到 parent event channel（用于 ToolNeedsConfirmEvent 冒泡）
		if parentEventCh != nil {
			select {
			case parentEventCh <- ev:
			case <-delCtx.Done():
				// Parent 取消或超时，停止中继
			}
		}

		// 检测 ToolNeedsConfirmEvent 并启动 confirm 路由
		// 通过反射检查 Prompt + CallID 字段来识别（避免 tools→agent 循环导入）
		if callID, hasCallID := getStringField(ev, "CallID"); hasCallID {
			if _, hasPrompt := getStringField(ev, "Prompt"); hasPrompt {
				// 这是一个 ToolNeedsConfirmEvent
				if hasConfirmFwd {
					// 启动 goroutine：阻塞等待 parent 的 confirm → 转发给 child
					// goroutine 不阻塞事件循环；child 收到 confirm 后自行继续
					go confirmFwd(delCtx, callID, targetAgent)
				}
			}
		}

		// 使用反射提取事件字段
		// ContentDeltaEvent: Delta 字段
		if delta, ok := getStringField(ev, "Delta"); ok {
			content += delta
		}

		// DoneEvent: Content 字段（覆盖累积内容）
		if doneContent, ok := getStringField(ev, "Content"); ok && doneContent != "" {
			content = doneContent
		}

		// ErrorEvent: Err 字段
		if errValue, ok := getErrorField(ev, "Err"); ok && errValue != nil {
			finalErr = errValue
		}
	}

	if finalErr != nil {
		return "", finalErr
	}

	return content, nil
}

// getStringField 通过反射从 interface{} 中提取 string 类型的字段
// 用于在不导入 agent 包的情况下获取事件字段值
func getStringField(v interface{}, fieldName string) (string, bool) {
	r := reflect.ValueOf(v)
	if r.Kind() != reflect.Struct {
		return "", false
	}
	f := r.FieldByName(fieldName)
	if !f.IsValid() {
		return "", false
	}
	if f.Kind() == reflect.String {
		return f.String(), true
	}
	return "", false
}

// getErrorField 通过反射从 interface{} 中提取 error 类型的字段
func getErrorField(v interface{}, fieldName string) (error, bool) {
	r := reflect.ValueOf(v)
	if r.Kind() != reflect.Struct {
		return nil, false
	}
	f := r.FieldByName(fieldName)
	if !f.IsValid() {
		return nil, false
	}
	if f.Kind() == reflect.Interface {
		if errVal, ok := f.Interface().(error); ok {
			return errVal, true
		}
	}
	return nil, false
}

// ─── Context helpers for event relay & confirm routing ──────────────────────

// toolEventChannelCtxKey 是 context.Value 的键，用于在工具执行时传递 parent event channel。
// 这是唯一的定义点；agent 包通过导出的 helper 函数使用它（避免跨包类型不匹配）。
type toolEventChannelCtxKey struct{}

// WithToolEventChannel 将 parent event relay channel 注入到 context。
// 由 agent.execToolStream 在调用 tool.Execute 前设置。
func WithToolEventChannel(ctx context.Context, ch chan<- interface{}) context.Context {
	return context.WithValue(ctx, toolEventChannelCtxKey{}, ch)
}

// ToolEventChannelFromCtx 从 context 提取 parent event relay channel。
// 由 DelegateTool.Execute 读取，用于向 parent 中继子 Agent 的事件。
func ToolEventChannelFromCtx(ctx context.Context) (chan<- interface{}, bool) {
	ch, ok := ctx.Value(toolEventChannelCtxKey{}).(chan<- interface{})
	return ch, ok
}

// confirmForwarderCtxKey 是 context.Value 的键，用于注入 confirm 路由闭包。
type confirmForwarderCtxKey struct{}

// ConfirmForwarder 是一个函数类型，负责将子 Agent 的确认请求路由到 parent Agent。
//
// 调用语义：
//   - 在 parent agent 上注册代理 confirmSlot（与 callID 关联）
//   - 阻塞等待用户（通过 parent.Confirm）注入确认结果
//   - 将确认结果转发给 child agent（通过 child.Confirm）
//   - 返回用户的 choice 或 ctx 取消错误
//
// 由 agent.execToolStream 创建闭包并注入 context，DelegateTool.Execute 消费。
type ConfirmForwarder func(ctx context.Context, callID string, child Locatable) (string, error)

// WithConfirmForwarder 将 ConfirmForwarder 注入到 context。
func WithConfirmForwarder(ctx context.Context, f ConfirmForwarder) context.Context {
	return context.WithValue(ctx, confirmForwarderCtxKey{}, f)
}

// ConfirmForwarderFromCtx 从 context 提取 ConfirmForwarder。
func ConfirmForwarderFromCtx(ctx context.Context) (ConfirmForwarder, bool) {
	f, ok := ctx.Value(confirmForwarderCtxKey{}).(ConfirmForwarder)
	return f, ok
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
