# Tool System Architecture

## 概览

Tool 系统是 SoloQueue 的底层可执行能力层。每个 tool 都直接映射到一次 LLM function calling 调用，是 skill 和 agent 所依赖的执行原语。

这一层负责：

- 提供统一的 `Tool` 接口
- 通过 `Confirmable` 支持危险操作确认
- 通过 `AsyncTool` 声明异步委托意图
- 通过 `ToolRegistry` 统一注册、查找与 `ToolDef` 生成

## 代码设计

这个包的设计强调“能力”和“编排”分离。tool 实现只描述自己的能力和执行逻辑，不负责 mailbox、生命周期或流式调度。

横切策略被拆分在三个地方：

- `tools.Config`：安全限制、超时、大小上限
- `ToolRegistry`：元数据注册和 provider 暴露
- `agent` 层：真正的执行编排，例如确认流程、tool timeout 包装、异步恢复

`DelegateTool` 是一个例外。它形式上仍然是 tool，但实际上承担了从 tool 调用桥接到 agent-to-agent 委托的职责。

## 核心接口

### `Tool`

定义在 [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L25)。每个 tool 都必须实现：

- `Name()`
- `Description()`
- `Parameters()`
- `Execute(ctx, args)`

设计约束包括：

- metadata 应视为不可变
- `Execute` 必须可并发安全调用
- 应尽量响应 `ctx.Done()`

### `Confirmable`

[internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L56) 中的 `Confirmable` 允许 tool 声明：

- 是否需要确认
- 展示给用户的 prompt / options
- 是否支持 session 白名单
- 用户确认后如何改写参数

### `AsyncTool`

[internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L117) 中的 `AsyncTool` 不直接启动异步执行，而是返回 `AsyncAction`：

- `Target`
- `Prompt`
- `Timeout`

后续调度由 agent 框架统一处理。

## 共享配置

[internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go#L28) 中的 `tools.Config` 统一描述所有内置工具的运行时限制，包括：

- 文件沙箱目录
- 读写大小限制
- HTTP fetch 限制
- shell regex、timeout、output 限制
- web search timeout

`Build(cfg)` 会基于这个配置构造当前可用的内置工具集合。

## Registry 层

[internal/tools/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/registry.go#L24) 中的 `ToolRegistry` 是并发安全的 `name -> Tool` 映射，负责：

- 注册与去重
- 查找 tool
- 通过 `Specs()` 生成稳定顺序的 `[]llm.ToolDef`

这层的意义在于：provider 看到的工具声明和 runtime 实际执行查找共用同一个真源。

## 内置工具族

`Build(cfg)` 当前返回的内置工具包括：

- 文件类：read、grep、glob、write、replace、multi-replace、multi-write
- 网络类：`WebFetch`、`WebSearch`
- 命令类：shell execution

整个包采用扁平布局，一个工具一个 `.go` 文件，这一点在 [internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go#L3) 中有明确说明。

## 典型示例：WebFetch

[internal/tools/http_fetch.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/http_fetch.go) 是一个典型的内置工具实现，展示了这个包的实现风格：

- 参数 JSON 解析
- scheme / host 校验
- 私网 SSRF 防护
- body 大小截断
- `http.Client` timeout
- 通过 `Confirmable` 支持人工确认

也就是说，单个 tool 自己负责能力语义与局部安全控制，而更上层的确认编排和 timeout 归因则交给 agent。

## DelegateTool

最特殊的 tool 是 [internal/tools/delegate.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/delegate.go#L42) 中的 `DelegateTool`。

### 两种模式

同一个结构支持两种模式：

- 同步 L2 -> L3 委托：`Execute(...)`
- 异步 L1 -> L2 委托：`ExecuteAsync(...)`

模式由 wiring 决定：

- `Locator`：查找已存在 agent
- `SpawnFn`：动态获取 target，并启用 async path

### 同步委托流程

`Execute(...)` 主要做：

1. 解析 `task`
2. 定位或创建目标 agent
3. 设置 delegation timeout
4. 透传 model override
5. 调用 child `AskStream`
6. relay confirm request 和 event
7. 累积最终结果返回给父 agent

### 异步委托流程

`ExecuteAsync(...)` 不负责真的执行委托，只负责返回 `AsyncAction`，后续由 agent 的异步 tool framework 接管。

## Context Relay Hook

`DelegateTool` 还定义了一组 context helper：

- `WithToolEventChannel`
- `ToolEventChannelFromCtx`
- `WithConfirmForwarder`
- `ConfirmForwarderFromCtx`

这些 helper 让 agent 可以把父级 event relay channel 和 confirm forwarder 注入 tool 的执行 context，从而让子 agent 的确认请求继续向上冒泡。

## FallbackTool

[internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L134) 中的 `FallbackTool` 和 `WithFallbackPrefix(...)` 用于给 L1 agent 的普通工具描述加上 fallback-only 前缀。

它不是强制约束，而是通过 tool description 影响 LLM 的调用倾向：优先委托，不行再直接执行底层工具。

## 与 Agent 的分工

Tool 包不负责调度，它依赖 agent 层来处理：

- tool 的 provider 暴露
- 执行顺序
- per-tool timeout 包装
- 确认 gating
- async tool 的调度、等待和恢复

因此可以概括为：

- tool 拥有能力语义
- agent 拥有编排语义

## 关键文件

- [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go)
- [internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go)
- [internal/tools/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/registry.go)
- [internal/tools/delegate.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/delegate.go)
- [internal/tools/http_fetch.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/http_fetch.go)
