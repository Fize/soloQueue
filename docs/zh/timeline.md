# Timeline 与回放

[English: Timeline and replay](../timeline.md)

Timeline 子系统位于 internal/memory/timeline。我用追加式 JSONL 记录会话、工具、
委派、路由和 Workflow 相关事件。

## 不变量

- System Prompt 不写入用户 Timeline。
- 事件只追加；状态变化通过新事件解释。
- Tool Call Payload 会在发送给 Provider 前修复，避免孤立调用产生无效请求。
- Timeline 文件可以根据 Session 配置轮换。

我让实时 WebSocket 流服务于当前 UI，同时把 Timeline 作为重启恢复、回放和诊断的持久证据。
Timeline 可能包含 Prompt、路径、工具参数和 Provider 输出，我会像保护源代码一样保护它。
