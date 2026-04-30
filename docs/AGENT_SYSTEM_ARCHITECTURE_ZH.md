# Agent System Architecture

## 概览

`internal/agent` 是 SoloQueue 的执行核心。它把 `LLMClient`、工具系统、技能系统和异步委托能力组合成一个长期运行的 actor 风格运行时，对外提供 `Ask`、`AskStream`、`Submit` 等接口。

这一层的主要职责是：

- 管理 agent 生命周期
- 用 mailbox 保证单 agent 串行执行
- 驱动 LLM 的流式 tool-use 循环
- 协调工具执行、确认、超时和委托
- 基于模板构建 L2/L3 agent

## 代码设计

这个包采用 actor 模型。一个 `Agent` 持有一个 mailbox，同一时刻只执行一个 `job`，因此并发边界比较清晰。阻塞式 API 只是流式 API 的包装，真正的主路径始终是事件流。

主循环通过 `streamLoop(...)` 统一实现，再由 `simpleStrategy` 和 `historyStrategy` 注入普通模式与 `ContextWindow` 模式的差异，避免维护两套几乎重复的 LLM 循环。

异步委托不是简单地阻塞等待子任务，而是把状态保存到 `asyncTurnState`，先通过 `DelegationStartedEvent` 让当前轮次 yield，之后再由 `resumeTurn(...)` 借助高优先级 mailbox 恢复执行。

## 核心类型

### `Definition`

`Definition` 定义在 [internal/agent/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/types.go#L23)，描述一个 agent 的静态配置：

- `ID`、`Name`
- `Role`、`Kind`
- `ModelID`、`Temperature`、`MaxTokens`
- `ThinkingEnabled`、`ReasoningEffort`
- `MaxIterations`、`ContextWindow`
- `ExplicitModel`

### `Agent`

`Agent` 定义在 [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go#L41)，同时包含：

- 不变依赖：`LLM`、logger、tool registry、skill registry
- 执行选项：`parallelTools`、`toolTimeouts`
- 生命周期状态：`ctx`、`cancel`、`mailbox`、`done`
- 确认状态：`pendingConfirm`
- 异步委托状态：`asyncTurns`
- 高优先级执行通道：`priorityMailbox`

## 生命周期与执行模型

生命周期入口在 [internal/agent/lifecycle.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/lifecycle.go)。

- `Start()`：创建 runtime context，初始化 mailbox，启动 run goroutine
- `Stop()`：取消 context，drain mailbox，并等待 goroutine 退出

真正的执行循环有两种：

- 普通 mailbox：`run(...)`，见 [internal/agent/run.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/run.go#L17)
- 优先级 mailbox：`runWithPriorityMailbox(...)`，见 [internal/agent/run.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/run.go#L50)

高优先级 mailbox 主要用于 L1 场景，保证委托恢复不会被普通用户消息长期阻塞。

## 流式事件模型

Agent 的对外输出由 [internal/agent/events.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/events.go#L12) 中的 `AgentEvent` 定义。

关键事件包括：

- LLM 输出：`ContentDeltaEvent`、`ReasoningDeltaEvent`、`DoneEvent`、`ErrorEvent`
- 工具相关：`ToolCallDeltaEvent`、`ToolExecStartEvent`、`ToolExecDoneEvent`
- 循环统计：`IterationDoneEvent`
- 人工确认：`ToolNeedsConfirmEvent`
- 委托控制：`DelegationStartedEvent`、`DelegationCompletedEvent`

这些事件既服务于 TUI 和 session，也服务于父子 agent 之间的 relay。

## LLM 主循环

核心循环是 [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L158) 中的 `streamLoop(...)`。

执行步骤如下：

1. 构建本轮消息
2. 调用 `LLM.ChatStream`
3. 累积 content、reasoning、tool_call 增量
4. 发出 `AgentEvent`
5. 若无 tool call，则发出 `DoneEvent`
6. 若有 tool call，则执行工具并进入下一轮

`historyStrategy` 额外负责：

- ContextWindow overflow 检查
- prompt token 校准
- assistant/tool 消息写回 `ContextWindow`
- 在异步委托场景中 yield 当前轮次

## 工具执行与确认

同步工具执行路径主要在：

- [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L541) 的 `execTools(...)`
- [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L578) 的 `execToolStream(...)`

单个工具调用的编排包括：

- tool 查找
- `ToolExecStartEvent` / `ToolExecDoneEvent`
- `Confirmable` 的确认流程
- `WithToolTimeout(...)` 注入的超时包装
- 委托场景中的 confirm relay 和 event relay

工具错误默认不会中断整个 ask，而是被格式化成 `error: ...` 再喂回 LLM。

## 异步委托

异步委托逻辑集中在 [internal/agent/async_turn.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/async_turn.go)。

核心结构：

- `delegatedTask`：追踪单个异步 tool call
- `asyncTurnState`：聚合一整轮异步工具结果

执行流程：

1. `execToolsWithAsync(...)` 扫描本轮工具，识别 `AsyncTool`
2. 预先构建好 `asyncTurnState`
3. 注册到 `a.asyncTurns`
4. 启动异步 action 和 watcher
5. `historyStrategy.postIteration(...)` 检测到异步轮次后发出 `DelegationStartedEvent`
6. 全部结果到齐后，`watchDelegatedTask(...)` 触发 `resumeTurn(...)`
7. `resumeTurn(...)` 把 tool result 写回 `ContextWindow`，发出 `DelegationCompletedEvent`，继续后续 LLM 轮次

## Factory、Registry 与 Supervisor

### `DefaultFactory`

[internal/agent/factory.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/factory.go) 中的 `DefaultFactory` 负责把模板变成运行中的 agent。它会：

- 组装最终 system prompt
- 解析模型配置
- 构建内置工具
- 为 leader 注入 `delegate_*` 工具
- 加载并注册 skill
- 创建 `ContextWindow`
- 注册并启动 agent

### `Registry`

[internal/agent/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/registry.go#L18) 中的 `Registry` 是进程内 `id -> *Agent` 的权威映射，同时也实现了 `iface.AgentLocator`。

### `Supervisor`

[internal/agent/supervisor.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/supervisor.go#L15) 中的 `Supervisor` 负责 L2 对 L3 子 agent 的生命周期管理：spawn、track、reap。它不直接负责调度，真正的并发调度仍由 tool loop 完成。

## 与其他系统的关系

- `session`：调用 `AskStreamWithHistory(...)`，并根据委托事件控制会话顺序
- `tools`：由 `Agent` 执行工具，但调度权仍在 agent
- `skill`：由 factory 或 session builder 注入 skill registry 与 `SkillTool`
- `llm`：通过 `LLMClient` 接口解耦底层 provider

## 关键文件

- [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go)
- [internal/agent/ask.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/ask.go)
- [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go)
- [internal/agent/async_turn.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/async_turn.go)
- [internal/agent/factory.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/factory.go)
- [internal/agent/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/registry.go)
- [internal/agent/supervisor.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/supervisor.go)
