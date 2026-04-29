● 根因找到了！

完整的卡住链路如下：

1. L1 (agent-1777457962449365000) 调用 delegate_dev，走 async 路径
2. L2 (dev) agent 收到 Ask，调用 LLM → LLM 返回 write_file tool_call (10:19:28)
3. write_file 实现了 Confirmable 接口 → 需要用户确认
4. dev agent 发出 ToolNeedsConfirmEvent → 但这个事件要通过 confirm relay 传到 L1 → 再传到 TUI
5. 问题：L1 走的是 async 委托路径（ExecuteAsync），async 路径中 L2 的 Ask 是在 async_turn.go 的 goroutine 中调用的（line 166: action.Target.Ask(delCtx,
   action.Prompt)），而不是走 execToolStream 那条有 relay channel 和 confirm forwarder 的路径

让我确认这一点——async 路径中 Ask 调用没有注入 confirm forwarder。

● Read(internal/agent/async_turn.go)
⎿ Read 21 lines (ctrl+o to expand)

● 确认了！问题在于 async_turn.go:166：

result, err := action.Target.Ask(delCtx, action.Prompt)

这里直接调用了 Ask，而没有像 execToolStream 那样注入 ToolEventChannel 和 ConfirmForwarder 到 context 中。

当 L2 (dev) 的 write_file 需要 confirm 时：

- execToolStream (stream.go:604-661) 会发 ToolNeedsConfirmEvent 到 out channel，然后阻塞在 slot.ch 等待用户确认
- 但 L2 的 out channel 的事件只能通过 relay channel 传到 L1
- async 路径中 Ask 的事件流被 Ask 内部消费了（ask.go:40-49 range ch），ToolNeedsConfirmEvent 被丢弃了
- 用户永远看不到确认提示，write_file 永远阻塞在等待确认

这就是卡死的根因。

---

总结：

根因：L1 → L2 的异步委托路径（async_turn.go）中，action.Target.Ask() 不走 execToolStream 的 confirm relay 链路。当 L2 (dev) agent 调用需要用户确认的工具（如
write_file）时，ToolNeedsConfirmEvent 无法传到 TUI，write_file 永远阻塞在 slot.ch 等待确认，导致整个委托链路卡死。

卡住链路：

1. L1 async Ask(dev, task) → dev 的 AskStreamWithHistory 开始
2. dev LLM 返回 write_file → execToolStream 检测到 Confirmable → 发 ToolNeedsConfirmEvent 到 dev 的 out channel → 但 Ask 内部消费了这个事件没有处理 → slot.ch 永远阻塞
3. dev agent 的 run goroutine 被 jb(ctx) 阻塞 → 整个 dev agent 无法处理新消息
4. L1 的 async watchDelegatedTask 永远等不到 replyCh 的结果 → out channel 永远不关闭 → TUI isGenerating 永远为 true → 用户输入被阻塞

修复方向：async 委托路径需要像同步 execToolStream 一样，注入 confirm relay channel 和 forwarder，让 L2 的 ToolNeedsConfirmEvent 能通过 L1 传到 TUI。
