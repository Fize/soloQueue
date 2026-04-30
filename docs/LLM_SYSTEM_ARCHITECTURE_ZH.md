# LLM System Architecture

## 概览

LLM 系统分成两层：

- `internal/llm`：provider 无关的共享协议、事件模型和重试工具
- `internal/llm/deepseek`：DeepSeek 的 HTTP/SSE 具体实现

`agent` 层依赖的是 `LLMClient` 接口，而不是具体 HTTP 客户端，因此 provider 适配器可以替换，agent 的执行循环不需要跟着变化。

## 代码设计

这一层的核心设计是“协议”和“传输”分离。`internal/llm/types.go` 定义跨 provider 共享的数据结构；`internal/llm/deepseek` 负责把这些结构映射成 provider 的 wire JSON 和 SSE chunk。

另一个关键设计是 streaming-first：

- `ChatStream(...)` 是唯一主链路
- `Chat(...)` 只是消费 `ChatStream(...)` 的包装

这样 HTTP、解析、错误处理和重试只维护一套实现。

## 共享协议层

[internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go) 定义了跨 provider 共享的结构：

- 工具调用：`ToolCall`、`ToolDef`、`FunctionCall`、`FunctionDecl`
- 用量统计：`Usage`
- 结束原因：`FinishReason`
- 流式事件：`Event`、`EventType`、`ToolCallDelta`
- 结构化错误：`APIError`

其中最关键的是 `llm.Event`。这是 provider 输出给 agent 的原始流式协议，后续 agent 再把它翻译成更高层的 `AgentEvent`。

## Agent 侧请求/响应模型

虽然共享协议在 `internal/llm`，但 agent 需要更丰富的请求结构，所以在 [internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go) 里定义了：

- `LLMMessage`
- `LLMRequest`
- `LLMResponse`
- `LLMClient`

`LLMRequest` 包含模型名、消息列表、tool 定义、reasoning 参数和输出格式等，是 agent 与 provider client 的内部契约。

## DeepSeek Client

具体 provider 实现在 [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go)。

`NewClient(...)` 接收：

- `BaseURL`
- `APIKey`
- headers
- timeout
- retry 策略
- logger
- 可注入的 `http.Client`

运行时在 [cmd/soloqueue/main.go](/Users/xiaobaitu/github.com/soloQueue/cmd/soloqueue/main.go#L177) 中根据配置构造这个 client。

## 传输主流程

DeepSeek 的主链路是：

1. 把 `agent.LLMRequest` 转成 wire request
2. 通过 HTTP POST 发起请求，并带 retry
3. 用 SSE reader 逐条读取 `data:` 行
4. 把 provider chunk 转成 `llm.Event`
5. 通过 channel 输出给 agent

## Wire 转换层

[internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go) 定义了 DeepSeek 的 wire-level 请求与响应结构。

`buildWireRequest(...)` 负责把 `LLMRequest` 转成 provider 请求，主要处理：

- messages
- tools 与 `tool_choice`
- `response_format`
- `reasoning_effort`
- DeepSeek V4 的 `thinking` 参数

`buildWireMessages(...)` 还处理了一个 provider 特殊约束：当 thinking mode 开启时，assistant 历史消息必须带 `reasoning_content`。如果历史里没有，会插入占位值。

## SSE 解析

[internal/llm/deepseek/sse.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/sse.go) 提供极简的 `sseReader`：

- 跳过空行和注释行
- 只读取 `data:` 字段
- 识别 `[DONE]`
- 忽略当前不需要的 `event:`、`id:` 等字段

这个设计说明该层并不是做通用 SSE 框架，而是只实现当前 provider 需要的最小解析集。

## Chunk 到 Event 的转换

`chunkToEvents(...)` 在 [internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go#L253) 中实现。

一个 provider chunk 可能生成多条 `llm.Event`：

- 每个 tool_call delta 一条 event
- content / reasoning delta 合并成一条 event
- finish_reason 对应的 done event
- usage 尽量合并进最后一个 done event

这个转换层很关键，因为它把 provider 的 wire 细节标准化成 agent 可以直接消费的共享事件协议。

## 错误与重试模型

### 请求前错误

在流建立之前的错误会直接从 `ChatStream(...)` 返回，例如：

- 请求序列化失败
- HTTP 错误
- 网络错误
- API 4xx/5xx 且重试失败

### 流内错误

流已经建立后发生的错误不会直接返回，而是发出 `llm.EventError`，然后关闭 channel。例如：

- SSE 解析失败
- chunk JSON 非法
- 流过程中 context 被取消

### 重试

通用重试逻辑在 [internal/llm/retry.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/retry.go)。

它负责：

- 指数退避
- 最大重试次数
- context cancel
- retry hook

而“什么错误值得重试”由 `llm.IsRetryableErr(...)` 和 `APIError.IsRetryable()` 决定。

## 与配置系统的关系

配置系统通过 `GlobalService` 给 LLM 层提供两部分输入：

- provider 配置：API key、base URL、timeout、retry
- model 配置：模型名、thinking 开关、reasoning effort

当前 runtime 在 [cmd/soloqueue/main.go](/Users/xiaobaitu/github.com/soloQueue/cmd/soloqueue/main.go#L177) 中：

- 用 `DefaultProvider()` 构建 DeepSeek client
- 用 `DefaultModelByRole(...)` 和 model resolver 解析具体模型

Provider 和 model 是分开建模的，这也是配置系统和 LLM 系统之间的重要边界。

## 测试替身

[internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go#L76) 中的 `FakeLLM` 是面向 agent 测试的 `LLMClient` 实现，支持：

- 预设普通响应
- 预设 tool call
- 预设流式 delta
- 注入错误和 finish reason

这使得大部分 agent 测试可以围绕 `LLMClient` 契约编写，而不需要真的 mock HTTP。

## 关键文件

- [internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go)
- [internal/llm/retry.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/retry.go)
- [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go)
- [internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go)
- [internal/llm/deepseek/sse.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/sse.go)
- [internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go)
