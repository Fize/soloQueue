# 实时对话流 v2：前后端协议改造方案

> 状态：已实施（2026-11-25，阶段 1–4 完成，v1 路径已删除）。与原方案的差异见 §10。
> 范围：Web Console 聊天页（`ChatPage` / `AssistantPage`），以及后端的 WS、会话和持久化层

## 0. 目标

| 编号 | 目标 | 验收标准 |
|---|---|---|
| G1 | 渲染顺序等于服务端产生顺序 | 任意时刻刷新，顺序与实时看到的一致；稳态下不重排 |
| G2 | 不静默丢帧 | 断线重连、多标签页、channel 来源的会话都能看到完整内容；补不齐时显式重同步 |
| G3 | 一套身份 | 实时帧、快照 API、timeline 落盘行使用同一个 `item_id` |
| G4 | 去掉整表替换 | 每轮结束不再 `loadHistory`；只在首次进入、翻页、重同步时加载快照 |

本期不做：

- 子 agent（被委派方）的实时活动视图，即委派卡片、Agent 详情页和 status-ui 使用的 `agent_streams`。保留现状，只降低广播频率。
- Cron `AskIsolated` 执行过程的展示。
- 鉴权和多用户。

## 1. 现状问题（摘要）

1. 帧没有 seq，也没有产生时间。前端按插入顺序渲染，助手气泡在首帧到达时追加到末尾（`web/src/hooks/useChatStream.ts:181`）。
2. 实时 ID（`msg-<requestId>`）与历史 ID（位置序号 `hist-N`，`internal/server/session_handlers.go:948`）不同。每轮结束 `loadHistory` 整表替换（`web/src/stores/chatStore.ts:458`），多次调用之间没有序号，旧快照可能覆盖新快照。
3. `chat_done` 先发出，最终回复后落盘（`internal/session/session.go:3016`），立即重新加载可能拿到没有答案的快照。
4. 流绑定在发起请求的那条 socket 上（`internal/server/chat_ws.go:376`）。`deliveryAttached` 失败一次就永久关闭（`chat_ws.go:623`），重连后这个请求不会再有实时帧。
5. Hub 广播通道 64 槽，满了直接丢（`internal/server/hub.go:517`）；agent watcher 同样 64 槽满即丢（`internal/agent/agent.go:672`）。
6. 流式输出期间，每个 token 都触发一次全量 `state` 广播，其中包含 `agent_streams` 的累计全文。
7. 排队聚合后的用户行只记录最后一个 request_id（`internal/memory/ctxwin/ctxwin.go:1057`），历史无法拆回多条消息。

## 2. 总体思路

```
                 ┌──────────── 在产生时刻分配 seq ────────────┐
 Agent.emit ─────┤ agent 串行 goroutine，唯一事件出口          │
 CW pushHook ────┤ 输入提交、迭代落盘                          ├──► SessionJournal（每个会话一个）
 Session / Hub ──┘ 路由、排队、终态、压缩、dispatch、合成输出   │      epoch + seq + ring + items
                                                              │
                 timeline 行写入 item_id / seq / iter ◄────────┘
                                                              │
  WS 客户端 A ◄── cursor 拉取 ──┐                              │
  WS 客户端 B ◄── cursor 拉取 ──┼── subscribe(session, after_seq)
  快照 API   ◄── head_seq 水位 + 已落盘条目 + 进行中条目 ───────┘
```

三个核心点：

1. 每个会话一个只追加的 **Journal**。所有会进入对话记录的事件，都在**产生时刻**分配会话内单调递增的 `seq`。Journal 是唯一的实时数据源。
2. 每个对话条目有**稳定的 `item_id`**，同时写进实时帧和 timeline 行。
3. 客户端按会话**订阅** Journal，并能从 `after_seq` 续传。快照 API 返回带 `seq` 水位的条目集合，保证 `快照(k)` 加上 `(k, n]` 的帧等于 `快照(n)`。

## 3. 后端设计

### 3.1 SessionJournal

新包 `internal/transcript`：

```go
type Journal struct {
    mu     sync.Mutex
    epoch  string                // 每次会话激活随机生成
    next   uint64                // 下一个 seq；激活时初始化为 max(已落盘 seq)+1
    ring   *frameRing            // 最近 N 帧，帧数和字节数双上限
    items  map[string]*itemState // 进行中/近期条目：open_seq、last_seq、segments、done
    calls  map[callKey]string    // (request_id, call_id) → item_id
    wake   chan struct{}         // close-and-replace 广播唤醒
}

// Publish：加锁分配 seq/ts → 更新 items → 写入 ring → 唤醒订阅者。O(1)，从不阻塞调用方。
func (j *Journal) Publish(f Frame) Frame
// ReadAfter：返回 seq > after 的帧；after 早于 ring 最旧帧返回 ErrLagged，epoch 不一致返回 ErrEpoch。
func (j *Journal) ReadAfter(epoch string, after uint64, max int) ([]Frame, error)
// Snapshot：在锁内取 (epoch, head_seq, 进行中条目副本)。
func (j *Journal) Snapshot() (epoch string, head uint64, inflight []ItemState)
```

`Registry` 以逻辑会话 ID 为键（`l1` / `l2:<uuid>`，复用 `logicalRuntimeSessionID`）。空闲会话的 Journal 可以回收，epoch 和 seq 保留在 Session 上。

**发布点**（都在事件产生的 goroutine 里同步调用）：

| 帧 | 发布位置 | 说明 |
|---|---|---|
| `item_delta`、`tool_started`、`tool_finished`、`item_done`、`delegation_*` | `Agent.emit`（`internal/agent/stream.go:857`） | agent 事件的唯一出口，同一 agent 串行执行，所以 seq 顺序就是产生顺序 |
| `input_committed` | CW pushHook（`internal/session/builder.go:645` L1、BuildL2 中的同类 hook），RoleUser 且带 request_id | 直接输入（`session.go:2551`）和排队输入（`DrainPending`）走同一个入口；replay 模式下 hook 不触发（`ctxwin.go:537`） |
| `input_queued`、`input_dropped` | `enqueuePending`；pending drainer 丢弃陈旧命令处（`session.go:519`） | 现在只有 channel 来源会广播排队消息 |
| `request_started`、`request_route` | `OnStart`、`OnRoute` hook | |
| `request_end` | `OnFinish`（`session.go:2656` 的 defer） | 此时最终回复已落盘、continuePending 已结束，取代 `chat_done` 作为终态 |
| `dispatch_terminal`、`compaction_status`、`session_meta` | 现有 hook | 改为写 Journal，不再走 Hub 广播 |
| 合成输出 | Session 直接调用 sink | `/compact` 摘要（`session.go:2247`）、`/help` 等命令、终态错误不经过 `Agent.emit` |
| `transcript_reset` | clear、rewind、delete 之后 | 客户端收到后重同步 |

**归属保护**：Session 在 `AskStream`（以及 `/compact` 分支）把 sink 放进 ctx；`emit` 只在 `sink.Owner() == a.InstanceID` 时发布。这样被委派的子 agent 和 Cron 的 `AskIsolated` 都不会写进父会话的对话记录。Cron 会话（`BuildL2ForCron`）有自己的 timeline，不接入 sink。

### 3.2 稳定身份

| 条目 | `item_id` | open_seq 来源 | 落盘 |
|---|---|---|---|
| 用户输入 | `in:<request_id>` | pushHook 发布 `input_committed` 时 | 用户行写入 `item_id`、`seq` |
| 排队聚合输入 | 每段一个 `in:<request_id>` | 同上，每段一帧 | `TemporalPart` 新增 `RequestID`，由 pending drainer（`session.go:528`）填入 |
| 助手迭代 | `out:<request_id>:<iter>` | 该迭代第一个输出事件（delta 或 tool_started） | 迭代行（`stream.go:692`）、最终回复（`session.go:3016`）、部分落盘（`session.go:2757`）写入 `item_id`、`seq`、`iter` |
| 压缩摘要 | `sys:<compaction_id>` | summary 发布时 | summary control 行写入 `seq` |
| 请求错误 | `err:<request_id>` | 带 error 的 `request_end` | `request_failed` control 行写入 `seq` |
| 旧数据 | `legacy:<role>:<ts>` | 无 | 排在所有带 seq 的条目之前，保持原 timeline 顺序 |

`(request_id, iter)` 唯一的依据：

- 每个请求的迭代从 0 开始（`stream.go:694` 的注释）；
- 委派恢复从 `turn.iter+1` 继续（`internal/agent/async_turn.go:799`）；
- 排队输入在同一个 job 内被注入时，迭代号继续递增，request_id 切换为新请求（`stream.go:258-281`）。

**为什么不会出现"行已落盘、Journal 还不认识这个条目"：**

- 助手迭代的第一个事件在 agent goroutine 的 `emit` 中发布，早于同一 goroutine 里 postIteration 的落盘；
- 最终回复的 delta 在 agent goroutine 发布，`DoneEvent` 经 channel 送达 session（happens-before），之后 session 才落盘；
- 用户输入的发布和落盘在同一次 hook 调用里完成；
- 兜底：pushHook 发现条目尚未打开（例如某次迭代只有被取消的工具调用），就当场打开它。

**渲染分组**（纯展示）：同一 request 的连续助手条目合成一个气泡。如果中间插入了其他条目（例如委派期间提交的新消息），恢复后的输出会成为一个新气泡，标注"继续回复：<原问题摘要>"。

### 3.3 帧格式

Journal 帧发给该会话的所有订阅者，seq 连续：

```jsonc
{
  "type": "frames", "session_id": "l2:…", "epoch": "e7f…",
  "frames": [
    { "seq": 1041, "ts": "2026-11-24T12:00:01.123456Z", "kind": "input_committed",
      "request_id": "req-1", "item_id": "in:req-1", "prompt": "…", "files": [] },
    { "seq": 1042, "ts": "…", "kind": "item_delta", "request_id": "req-1",
      "item_id": "out:req-1:0", "iter": 0, "channel": "reasoning", "text": "…" },
    { "seq": 1043, "ts": "…", "kind": "tool_started", "request_id": "req-1",
      "item_id": "out:req-1:0", "call_id": "call_1", "name": "Edit", "args": "{…}" },
    { "seq": 1050, "ts": "…", "kind": "request_end", "request_id": "req-1",
      "request_ids": ["req-1"], "terminal_code": "completed" }
  ]
}
```

`kind` 全集：`input_queued`、`input_committed`、`input_dropped`、`request_started`、`request_route`、`item_delta`、`tool_started`、`tool_finished`、`dispatch_terminal`、`delegation_started`、`delegation_finished`、`item_done`、`request_end`、`compaction_status`、`session_meta`、`transcript_reset`。

点对点消息（不占 seq）：

- `connected {protocol: 2}`
- `chat_ack {request_id, status: accepted | queued | rejected, error}`
- `resync_required {session_id, epoch, head_seq}`
- `command_result`：`/help`、`/version` 这类不落盘的命令输出
- `pong`

全局消息：`state`（只保留最新值，最高 4 Hz）、`notification`。

`ts` 只用于展示，排序只看 `seq`。

### 3.4 订阅、续传与投递

客户端发给服务端：

- `subscribe {session_id, epoch?, after_seq?}`：不带 `after_seq` 表示只要新帧；
- `unsubscribe {session_id}`；
- `chat_send`、`chat_cancel` 不变，仍由客户端生成 `request_id`。

服务端：

- 每个客户端一个写协程，每个订阅维护一个 **cursor**，从 Journal 的 ring 里**拉取**，不再给每个客户端推送队列。每次写出当前所有可用帧，负载高时自然攒批，空闲时没有额外延迟。
- `after_seq` 早于 ring 里最旧的帧、`epoch` 不一致、或客户端太慢导致 cursor 落后于 ring 时，一律回复 `resync_required`。**不再静默丢帧。**
- 投递与发起请求的 socket 解耦。重连、多标签页、QQ/微信来源走同一条路径。删除 `forwardAgentEvents` 的 WS 转发和 `prepareChannelMessage` 的镜像广播；`forwardAgentEvents` 只负责排空 `out` 通道。
- `state` 改为"最新值覆盖"，不再因 `state` 积压而断开客户端。

### 3.5 快照 API

`GET /api/session/transcript?session_id=…&limit=30&before=<cursor>`

1. 加 Journal 锁，取 `(epoch, head_seq, 进行中条目)`，然后解锁；
2. 读 timeline，按 `item_id` 构建已落盘条目，旧数据走 legacy 规则；
3. 按 `item_id` 合并。两边都有时取 Journal 的副本，因为它可能已经包含后到达的工具结果；
4. 排序：legacy 段按 timeline 顺序排在前面，其余按 `open_seq`。分页 cursor 用排序键，因此是稳定的；
5. 返回 `{epoch, seq: head_seq, items, pending, requests, has_more, cursor}`。

正确性性质（写成属性测试）：**对任意 k ≤ n，`快照(k)` 依次应用 `(k, n]` 的帧，结果等于 `快照(n)`。**

`/api/session/history` 已在阶段 4 删除；它的边界用例测试（委派、去重、回退、压缩、分页）已迁移到 `/api/session/transcript`。

### 3.6 跨重启

- Session 激活时生成新的 `epoch`。CW replay 读 timeline 时顺便取最大的 `seq`，Journal 从 `max+1` 开始，排序键因此跨重启单调。
- 客户端发现 `epoch` 变化，就丢弃本地状态，按快照重建。

### 3.7 服务端减负

- 删除转发器里逐 token 执行的 `refreshRequestWatchdog → resolveSession → configureRequestLifecycle()`（`chat_ws.go:671`、`chat_ws.go:911`）。watchdog 投影交给 Hub 已有的 1 秒 ticker（`hub.go:474`）。
- `SetWatchdog` 只在 phase 或终态变化时 `Notify`，不再因为 `LastProgressAt` 每个 token 都变而触发广播。
- `agent_streams` 继续供 status-ui 和委派卡片使用，但随 `state` 降到最高 4 Hz。

## 4. 前端设计

### 4.1 Store

新增 `web/src/stores/transcriptStore.ts`，替代 `chatStore.messages` 及其各种 mutation：

```ts
interface SessionTranscript {
  epoch: string | null
  headSeq: number
  items: Record<string, TranscriptItem>   // item_id → 条目
  order: string[]                         // 按 openSeq 升序；稳态下只追加
  pending: Record<string, PendingInput>   // 已发送、尚未提交（托盘）
  requests: Record<string, RequestState>  // started / route / end
  sync: 'idle' | 'loading' | 'live' | 'resyncing'
  hasMore: boolean
  cursor?: string
}

interface TranscriptItem {
  id: string
  kind: 'user' | 'assistant' | 'system' | 'error'
  requestId?: string
  iter?: number
  openSeq: number
  lastSeq: number
  ts: string
  segments: ChatSegment[]
  done: boolean
  files?: { name: string; path: string }[]
}
```

### 4.2 Reducer 规则

Reducer 是纯函数，全部用单测覆盖：

- `seq ≤ headSeq`：忽略（重放去重）；
- `seq > headSeq + 1`：判定为缺口，进入 `resyncing`；
- `epoch` 不一致：重同步；
- `input_committed`：移除 pending，打开 `in:` 条目，追加到 `order` 尾部；
- `item_delta`、`tool_started`：条目不存在就先打开，再追加 segment；
- `tool_finished`、`dispatch_terminal`：按 `item_id + call_id` 更新，**不再**全表按 callId 搜索；
- `request_end`：更新请求状态，带 error 时打开 `err:` 条目；
- `transcript_reset`：重同步。

每条 WS 消息（一个 `frames` 批次）只调用一次 `set`，减少渲染次数。

### 4.3 传输

- 进入会话：请求快照 → 写入 store → `subscribe(after_seq = snapshot.seq, epoch)`。
- 切换会话：保留最近几个会话的订阅或缓存。切回时如果 epoch 未变、ring 里还有对应帧，直接从 `headSeq` 续传，不重新拉快照。
- 断线重连：对每个已订阅会话用当前 `headSeq` 重新 `subscribe`；只有收到 `resync_required` 才重新拉快照。
- 窗口聚焦或变为可见：只确保连接存活，**不再 `loadHistory`**。
- 发送：生成 `request_id` → 写入 pending 托盘 → `chat_send` → 等待 `chat_ack`，超时报错。

### 4.4 渲染

- 列表由 `order` 映射成条目，同一 request 的连续助手条目合成一个气泡，复用现有的 `ChatMessageView` 和 segment 组件；
- pending 托盘放在列表末尾，显示"发送中 / 排队中 / 已取消"；
- `ChatPage` 和 `AssistantPage` 共用同一套 hook；
- 删除 `recoverInFlightMessages`、`registerExternalChannelChat`、`externalChatOwners`、`channelQueue*` 记账、`terminalDispatches` 缓存，以及 `useChatStream` 里按请求注册的 handler。`websocket.ts` 只保留连接、订阅和分发。

## 5. 问题与改法对照

| # | 问题 | 改法 |
|---|---|---|
| 1 | 气泡在首帧到达时追加到末尾 | 条目在首个事件的 `seq` 打开；输入在提交时打开，提交前留在托盘 |
| 2 | 实时和历史两套 ID，整表替换 | 统一 `item_id`；快照按 ID 合并；不再每轮重新加载 |
| 3 | `chat_done` 早于落盘 | `request_end` 在 `OnFinish` 发布；客户端也不再依赖重新加载 |
| 4 | 重连后断流 | 按会话订阅，用 `after_seq` 续传；超出 ring 时显式重同步 |
| 5 | 静默丢帧 | 拉取式 cursor，落后就 `resync_required` |
| 6 | 每个 token 一次全量 `state` | 去掉逐 token Notify；`state` 最高 4 Hz，只保留最新值 |
| 7 | 排队聚合丢失 request_id | `TemporalPart.RequestID`，每段一个 `in:` 条目 |
| 8 | 异步恢复的输出合并进旧气泡，显示在新消息上方 | 按 `seq` 成为新气泡，并标注"继续回复" |

## 6. 分阶段实施

每个阶段可以独立合并，测试通过后再进入下一阶段。

### 阶段 1：后端基础（UI 无变化）

- 新增 `internal/transcript`：Frame、Journal、Registry、ring，以及单测。
- sink 注入：在 `internal/iface` 定义接口；`Session.AskStream` 和 `/compact` 分支把 sink 放进 ctx；`Agent.emit` 按归属发布；hooks 和 Session 发布生命周期帧与合成输出。
- 落盘打标：`ctxwin.Message`、`timeline.MessagePayload` 增加 `ItemID`、`Seq`、`Iter`；`TemporalPart` 和 `TemporalPartPayload` 增加 `RequestID`；control 行增加 `Seq`；pushHook、最终回复、部分落盘写入这些字段。
- 激活时从 timeline 恢复最大 `seq`，生成 epoch。
- 新增 `GET /api/session/transcript`。
- 涉及文件：`internal/agent/stream.go`、`internal/memory/ctxwin/ctxwin.go`、`internal/memory/timeline/types.go`、`internal/session/{session,builder}.go`、`internal/server/{hub,session_handlers,server}.go`。
- 退出标准：属性测试 `快照(k) ⊕ 帧(k,n] == 快照(n)` 在以下 FakeLLM 脚本上全部通过——普通回答、多轮工具调用、运行中注入排队输入、continuePending、委派恢复、取消、超时部分落盘、`/compact`、`/clear`。

### 阶段 2：WS v2 投递

- `subscribe` / `unsubscribe`、按 cursor 拉取的写协程、批量帧、`resync_required`、`chat_ack`、`state` 合并与降频。
- 开发期用 `connected.protocol` 区分版本，v1 帧暂时保留给旧 UI。
- 涉及文件：`internal/server/{ws_handler,hub,chat_ws,request_registry,agent_handlers}.go`。
- 退出标准：
  - 重连后收到剩余的全部帧；
  - 两个客户端收到逐字节相同的帧序列；
  - 慢客户端收到 `resync_required`，而不是缺帧；
  - 流式期间 `state` 不超过 4 Hz。

### 阶段 3：前端 v2

- `transcriptStore`、reducer、传输层；`ChatPage` / `AssistantPage` 切换到新 store；分组渲染和 pending 托盘。
- 涉及文件：`web/src/stores/transcriptStore.ts`（新）、`web/src/lib/transcript/*`（新）、`web/src/lib/websocket.ts`、`web/src/hooks/useChatStream.ts`、`web/src/components/{ChatPage,AssistantPage,ChatMessage}.tsx`、`web/src/types/chat.ts`。
- 退出标准：诊断时的 4 个复现场景作为回归测试通过；§7 的手工清单全部通过。

### 阶段 4：清理

- 删除前后端的 v1 路径和 `/api/session/history` 的旧调用方，更新 `docs/architecture.md` 中的 WS 章节。

工作量粗估：阶段 1 最大（横跨 agent、session、ctxwin、timeline、server），阶段 3 其次，阶段 2 中等，阶段 4 较小。

## 7. 测试计划

**Go**

- Journal：并发发布下 seq 连续且唯一；ring 回绕后 `ReadAfter` 返回 `ErrLagged`；epoch 不一致返回 `ErrEpoch`。
- 归属：子 agent 和 `AskIsolated` 的事件不会进入父会话的 Journal。
- 打标：timeline 行带有正确的 `item_id`、`seq`、`iter`；排队聚合输入拆成多个 `in:` 条目。
- 不变量：所有客户端可见的 agent 事件都经过 `emit`。
- 属性测试：见阶段 1 的脚本列表。
- WS：中途断线后续传；多个客户端收到的帧一致；慢客户端触发重同步。

**前端（Vitest）**

- Reducer：重复帧、缺口、epoch 变化、快照与进行中条目的合并、legacy 条目排序、分组规则；
- 回归：首 token 前追问、`chat_done` 后的快照竞态、旧快照覆盖新快照、重连续传。

**手工验证（`make start`）**

1. 首个 token 之前发追问（L2 排队）；
2. L1 委派期间发新消息，观察恢复输出的位置；
3. 流式输出中用 DevTools 切到离线再恢复；
4. 两个标签页同时打开同一会话；
5. QQ/微信消息进入正在 Web 端查看的会话；
6. 刷新页面，顺序与刷新前一致。

## 8. 风险与对策

| 风险 | 对策 |
|---|---|
| `emit` 路径多出发布开销，影响 token 吞吐 | Journal 存结构体，写出时再编码；加 benchmark，确保发布开销远小于 token 间隔 |
| 有事件绕过 `emit` 直接写 channel | 加测试断言客户端可见的事件类型都经过 `emit`；纳入 code review 清单 |
| 大工具结果撑满 ring | ring 设字节上限；`tool_finished` 对超大结果截断，另提供取全量的 API |
| 同一迭代内 LLM 重试导致 delta 重复（同一条消息渲染两遍） | 不会发生：客户端只在尚未产出任何模型输出时才重试或重连，开始输出后失败直接上报错误，已显示的内容保留，不重放。该性质由 `internal/llm/deepseek/reconnect_test.go` 保护，因此不需要 `item_reset` 帧 |
| 旧会话没有 seq | 走 legacy 规则：排在最前，保持原 timeline 顺序 |
| 内存占用 | 空闲会话的 Journal 超时回收；已提交的条目过了宽限期就从 `items` 移除 |

## 9. 已确认的决策

1. **异步恢复的展示**：按产生顺序拆成新气泡，并标注"继续回复"。
2. **发送中和排队中的消息**：先放在底部托盘，提交后再进入时间线。
3. **发布策略**：开发期 v1/v2 并存，v1 已在阶段 4 删除（前后端打在同一个二进制里，不需要跨版本兼容）。

## 10. 实施记录与差异

- **Registry 归属**：没有使用进程级全局 Registry。`serve` 启动时创建一个 Registry，注入 `SessionManager`、`L2SessionStore` 和 `Mux`；未传入时 `Mux` 自建一个并注入它拿到的会话属主。没有 Registry 的 Session（cron 等无界面会话）既不发布帧也不给落盘行打标，因此 cron 运行不会出现在任何实时 Transcript 中。
- **压缩状态**：由拥有该上下文窗口的 Session 通过 `ctxwin.SetCompactionObserver` 发布，并且在 builder 的 hook 写完 timeline 行之后执行，保证覆盖该帧的快照一定能读到对应行。此前 builder 的 hook 固定按 `l1-session` 发布，cron 的 L1 压缩会出现在主 L1 会话中，这个问题随之修复。
- **确认消息**：`chat_send` 在路由之前就回 `chat_ack(accepted)`（分类可能耗时），被排队时再回 `chat_ack(queued)`。`chat_cancel_confirmed` 作为点对点回复保留；请求的结束只通过 `request_end` 帧传达。
- **协议版本**：开发期 v1/v2 并存，没有引入 `connected.protocol`；v1 删除后该标志不再需要。
- **发布开销**：`BenchmarkPublishContentDelta` 在 Apple M4 Pro 上约 0.45 µs/帧。Journal 的 View 用 `strings.Builder` 累积流式文本段，避免每个 token 复制整段文本；改动前在长输出下达到约 20 µs、600 KB/帧。
- **LLM 重试不会让同一条消息渲染两遍**：DeepSeek 客户端的 HTTP 重试和流重连只发生在尚未向消费者转发任何模型输出之前（`streamWithReconnect` 以 `emitted` 为界）；一旦开始输出，连接中断就作为错误上报，已显示的部分保留，不重放。ChatGPT 客户端和 supervised 包装层没有流内重试，上下文溢出时的压缩重试发生在请求发出之前并使用新的迭代序号。原先记为"已知限制"并计划增加的 `item_reset` 帧因此不需要实现。这条性质此前没有测试，现由 `reconnect_test.go` 的两个用例保护；去掉 `emitted` 守卫时，"开始输出后中断"的用例会得到 `"Hello Hello world"`。
- **手工验证（2026-11-25）**：§7 的六项清单已在浏览器中逐项验证，使用隔离的工作目录和端口，以及一个脚本化的假模型（按提示词里的指令控制首 token 延迟、流速、工具调用和报错），没有连接真实 Provider。
  - 通过：首 token 前追问（排队气泡始终在流式回复之下，回复结束后才按序提交）、L1 委派期间发新消息（恢复的输出成为新气泡并标注"继续回复"）、流式中途连接断开 5 秒后续传（无缺口、无重复）、两个标签页内容逐字节一致、刷新后顺序一致。
  - 第 5 项用第二个 WebSocket 客户端代替真实的 QQ/微信消息：验证了其他连接发起的请求和排队输入在所有标签页中的顺序，没有走渠道适配器路径。
  - 额外验证：流式中途刷新、Stop、LLM 报错、`/help`、`/compact`（成功与失败）、后端重启（epoch 变化后自动重同步，序号单调延续）、`/clear`。
  - 发现并修复一个问题：`/clear` 依次发布 `request_started`、`transcript_reset`、`request_end`，客户端因重置而重新加载快照，`request_end` 落在重同步的空隙里，发送方标签页的本地输入一直没有被清除，Stop 按钮卡住。现在 store 在每次加载快照时按快照对账本地输入（已提交、排队或已结束的请求都视为已处理），并在重置时清掉该会话的本地命令结果（如 `/help` 输出）。
  - 未覆盖：真实模型的输出形态（超长思考、并行工具调用）和渠道适配器路径。

