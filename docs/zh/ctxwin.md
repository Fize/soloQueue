# 上下文窗口

[English: Context windows](../ctxwin.md)

上下文窗口子系统位于 internal/memory/ctxwin。我用它保护 Model 的上下文预算，
同时保留下一轮最有价值的信息。

我在上下文构建时组合 System Prompt、对话消息、工具调用和结果、Skill 指令、Memory
引用以及当前环境。我会在新请求中回放 System Prompt，但不会把它写入用户可见的
Timeline 事件。

达到双水位线时，我让 Compactor 摘要或移除较旧内容，并保留最近的 Turn 边界。
我过滤 Tool Call Pair，避免把孤立的工具调用发送给 Provider。我也按工具限制截断
过长的工具输出和 JSON 值；这保护请求大小，但不保证结果语义完整。

我会为每个启用 Model 配置合理的 context_window，并在排查实际发送内容时同时
查看 Timeline 和日志。
