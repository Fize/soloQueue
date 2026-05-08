# 修复 resumeTurn() 工具调用验证

## 问题描述

当子代理（L3）的 LLM 响应因 `finish_reason="length"` 被截断时，`resumeTurn()` 会将不完整的工具结果推送到父代理（L2）的 ContextWindow，导致下次 API 请求返回 HTTP 400 错误：
"An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'"

## 根因分析

### 当前流程
1. L2 委托给 L3（异步）
2. L3 的 LLM 响应被截断（`finish_reason="length"`）
3. L3 的 `streamLoop()` 处理截断（stream.go:304-318）：
   - 设置 `toolCalls = nil`
   - 添加错误消息到 `acc.content`
4. L3 返回结果给 L2
5. `resumeTurn()` 被调用（async_turn.go:430-472）
6. **问题**：`resumeTurn()` 无条件推送 `turn.toolCalls` 和 `turn.results` 到 ContextWindow
7. 如果 `turn.toolCalls` 与 `turn.results` 不匹配（例如，部分 tool_calls 被丢弃），ContextWindow 会损坏

### 为什么 `filterCompletePairs()` 没发挥作用
- `filterCompletePairs()` 在 `BuildPayload()` 时调用（发送 API 请求前）
- 但它只能**过滤**不完整对，不能**修复**它们
- 如果 assistant 消息有 3 个 tool_calls，但只有 2 个 tool results，filterCompletePairs 会**丢弃整个 assistant 消息和所有 tool results**
- 这可能导致上下文丢失

## 解决方案

### 方案 A：在 `resumeTurn()` 中验证工具结果完整性（推荐）
在 `resumeTurn()` 推送工具结果前，验证：
1. `len(turn.toolCalls) == len(turn.results)`
2. 每个 tool result 都是有效的（非空的、格式正确的）

如果不完整：
- 记录错误日志
- 推送错误工具结果（包含错误信息）
- 或者丢弃不完整的 tool_calls/results

### 方案 B：在 `Push()` 时验证
在 `ContextWindow.Push()` 中添加验证，如果检测到不完整的 tool_calls/tool_results，自动修复或报错。

### 方案 C：在 `BuildPayload()` 中增强 `filterCompletePairs()`
改进 `filterCompletePairs()` 以更好地处理边缘情况。

## 推荐实现（方案 A）

### 修改文件
- `internal/agent/async_turn.go`：修改 `resumeTurn()` 函数

### 伪代码
```go
func (a *Agent) resumeTurn(turn *asyncTurnState) {
    // ... 清理 asyncTurns 注册 ...

    // 验证工具结果完整性
    if len(turn.toolCalls) != len(turn.results) {
        a.logError(ctx, "tool calls/results mismatch",
            slog.Int("tool_calls", len(turn.toolCalls)),
            slog.Int("results", len(turn.results)),
        )
        // 处理不匹配的情况
        // 选项1：填充错误结果
        // 选项2：丢弃整个 turn
        // 选项3：只推送有效的结果
    }

    // push 所有 tool result 到 cw
    for i, tc := range turn.toolCalls {
        // 验证 tool result 是否有效
        if i < len(turn.results) && turn.results[i] != "" {
            turn.cw.Push(ctxwin.RoleTool, turn.results[i],
                ctxwin.WithToolCallID(tc.ID),
                ctxwin.WithToolName(tc.Function.Name),
                ctxwin.WithEphemeral(true),
            )
        } else {
            // 推送错误结果
            turn.cw.Push(ctxwin.RoleTool, "error: tool result missing",
                ctxwin.WithToolCallID(tc.ID),
                ctxwin.WithToolName(tc.Function.Name),
                ctxwin.WithEphemeral(true),
            )
        }
    }

    // ... 继续 ...
}
```

## 测试计划

### 测试用例
1. **正常情况**：`len(toolCalls) == len(results)`，所有结果都有效
2. **截断情况**：`finish_reason="length"`，`toolCalls` 和 `results` 不匹配
3. **部分结果**：某些 `results[i]` 为空或包含错误
4. **空结果**：`len(results) == 0`
5. **多余结果**：`len(results) > len(toolCalls)`

### 测试文件
- `internal/agent/async_turn_test.go`：单元测试 `resumeTurn()` 的逻辑
- `internal/ctxwin/ctxwin_test.go`：测试 `filterCompletePairs()` 的边缘情况

## 影响分析

### 兼容性
- 修改只影响异步委托的恢复流程
- 不影响同步工具调用
- 不影响非委托场景

### 性能
- 添加验证步骤，但开销很小（只是长度检查和日志）
- 不影响正常路径的性能

## 待解决问题

1. **如何处理不匹配的 tool_calls/results**？
   - 选项1：填充错误结果（推荐，保持消息序列完整）
   - 选项2：丢弃整个 turn（可能导致上下文丢失）
   - 选项3：只推送有效的结果（可能导致 tool_call 没有对应的 tool result）

2. **是否需要通知 LLM**？
   - 如果 tool_calls 被截断，是否需要让 L2 的 LLM 知道？
   - 可以通过在 tool result 中包含错误信息来实现

## 决策

根据用户要求："本次暂不修复 maxtokens 的限制"，我们只修复 `resumeTurn()` 的验证逻辑的完整性检查，不修改 MaxTokens 设置。
