# Phase B：三层并发实施计划

> 基于 Phase A 实际代码状态（Skill/Tool 已解耦），重新设计 Phase B。

---

## 目标

实现 L1 → L2 → 多 L3 并行 → L2 聚合 → L1 收到结果的完整委托链路。

核心设计原则：**L1 异步等待，L2/L3 同步阻塞等待**。

- L1 是用户交互层，不能因为等待委托而卡住 run goroutine
- L2 是领域主管，可以阻塞等待 L3 结果（用户不直接与 L2 交互）
- L3 是执行单元，同步执行单一任务

---

## 一、L1 异步委托核心机制

### 1.1 问题分析

当前 DelegateTool.Execute 同步调用 `target.Ask()`，在 L1 的 run goroutine 中阻塞。
后果：L1 在等待 L2 期间无法处理任何新消息（mailbox 中的 job 排队等待）。

### 1.2 解决方案：异步委托 + 暂停/恢复 tool loop

核心思路：当 L1 遇到 `delegate_*` tool_call 时，不在 Execute 内阻塞等待，
而是将委托任务异步派发，保存当前 tool loop 状态，释放 L1 的 run goroutine。

#### 执行流

```
L1 run goroutine
  │
  ├─ 收到 job（用户 Ask）
  │   └─ runOnceStreamWithHistory
  │       ├─ LLM 返回 tool_calls: [delegate_dev(task="实现登录")]
  │       ├─ execTools → execToolStream → DelegateTool.Execute()
  │       │   ├─ 注册 pending[corrID] = delegatedTask{...}
  │       │   ├─ 启动异步 goroutine: go L2.Ask(task)
  │       │   └─ 返回 errAsyncPending（特殊哨兵错误）
  │       │
  │       ├─ execToolStream 检测到 errAsyncPending：
  │       │   ├─ 发射 DelegationStartedEvent{CorrelationID, TargetAgentID}
  │       │   └─ 返回空字符串（占位）
  │       │
  │       ├─ runOnceStreamWithHistory 检测到有异步委托：
  │       │   ├─ assistant(tool_calls) 已 push 到 cw（正常流程已有）
  │       │   ├─ tool result 不 push（等结果回来再 push）
  │       │   ├─ 不 close(out)（事件通道保持打开）
  │       │   └─ return from runOnceStreamWithHistory
  │       │       → job 执行完毕 → L1 run goroutine 回到 mailbox select
  │       │
  ├─ L1 现在是 idle 状态，可处理新消息
  │
  ├─ [L2 异步执行任务，可能同步阻塞等 L3]
  │
  ├─ L2 结果到达 → replyCh 收到 delegateResult
  │   ├─ 异步 goroutine 将结果投递为高优先级 job：
  │   │   job: resumeDelegation(corrID, result)
  │   │
  └─ L1 run goroutine 取到高优先级 job：
      ├─ 从 pending[corrID] 取出 delegatedTask（含 out, cw, toolCalls 等）
      ├─ 将 tool result push 到 cw
      ├─ 调用 LLM 继续下一轮迭代
      └─ 正常工具循环继续
```

### 1.3 关键类型

```go
// internal/agent/delegate_async.go（新文件）

// delegatedTask 追踪一个等待中的异步委托
type delegatedTask struct {
    correlationID string
    targetAgentID string
    replyCh       chan delegateResult
    createdAt     time.Time
    timeout       time.Duration

    // tool loop 恢复状态
    out       chan<- AgentEvent    // 原始事件通道（保持打开）
    cw        *ctxwin.ContextWindow
    iter      int                  // 当前迭代号
    toolCalls []llm.ToolCall       // 本轮所有 tool_calls
    asyncIdx  int                  // 哪个 tool_call 是异步的
    syncResults []string           // 同步 tool 的结果（如有）
    callerCtx context.Context      // caller 的 ctx（用于 mergeCtx）
}

type delegateResult struct {
    content string
    err     error
}

// errAsyncPending 是 DelegateTool 返回的特殊哨兵错误
// execToolStream 检测到此错误时，不格式化为 "error: ..."，
// 而是触发异步委托流程
var errAsyncPending = errors.New("async delegation pending")
```

### 1.4 DelegateTool 异步模式

```go
// internal/tools/delegate.go — 修改

type DelegateTool struct {
    LeaderID string
    Desc     string
    Locator  AgentLocator
    Timeout  time.Duration
    Async    bool           // 新增：true 时 L1 异步委托模式
}

func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
    // ... 解析 args，查找 target Agent ...

    if dt.Async {
        // 异步模式：
        // 1. 从 ctx 中获取 caller 的 Agent 引用（通过 context value）
        // 2. 创建 replyCh，注册 pending
        // 3. 启动 goroutine: go func() { result := target.Ask(...); replyCh <- result }()
        // 4. 启动超时定时器
        // 5. 返回 ("", errAsyncPending)
    }

    // 同步模式（L2/L3 使用）：
    // 现有逻辑不变，阻塞调用 target.Ask()
}
```

**Agent 引用传递问题**：DelegateTool 需要知道自己是哪个 Agent 调用的（才能注册 pending）。
方案：在 `execToolStream` 中，将 `*Agent` 通过 `context.Value` 注入 ctx：

```go
// internal/agent/agent.go
type agentCtxKey struct{}

func ctxWithAgent(ctx context.Context, a *Agent) context.Context {
    return context.WithValue(ctx, agentCtxKey{}, a)
}

func agentFromCtx(ctx context.Context) *Agent {
    a, _ := ctx.Value(agentCtxKey{}).(*Agent)
    return a
}
```

在 `execToolStream` 的 tool.Execute 调用前：
```go
execCtx = ctxWithAgent(execCtx, a)
```

### 1.5 Agent 新增字段

```go
// internal/agent/agent.go — Agent struct 新增

type Agent struct {
    // ... 现有字段 ...

    // 异步委托追踪（L1 专用）
    pendMu   sync.RWMutex
    pending  map[string]*delegatedTask  // correlationID → 等待中的委托
}
```

### 1.6 runOnceStreamWithHistory 修改

在 `execTools` 返回后，检测是否有异步委托：

```go
// runOnceStreamWithHistory 中，execTools 之后的逻辑：

results := a.execTools(ctx, iter, toolCalls, out)

// 检查是否有异步结果（errAsyncPending → 空字符串占位）
hasAsync := false
for i, r := range results {
    if r == "" && a.isAsyncPending(toolCalls[i].ID) {
        hasAsync = true
        break
    }
}

if hasAsync {
    // 异步路径：不 push tool result，保存状态，return
    // out 不 close（由 resume 逻辑最终 close）
    a.saveAsyncState(ctx, out, cw, iter, toolCalls, results)
    return  // ← 释放 L1 run goroutine
}

// 同步路径（原有逻辑）
for i, tc := range toolCalls {
    cw.Push(ctxwin.RoleTool, results[i], ...)
}
```

### 1.7 恢复逻辑

```go
// internal/agent/delegate_async.go

// resumeDelegation 由高优先级 job 调用
func (a *Agent) resumeDelegation(corrID string, result delegateResult) {
    a.pendMu.Lock()
    task := a.pending[corrID]
    delete(a.pending, corrID)
    a.pendMu.Unlock()

    if task == nil {
        return // 已被清理（超时/取消）
    }

    // 构造 tool result
    toolResult := result.content
    if result.err != nil {
        toolResult = "error: " + result.err.Error()
    }

    // push tool result 到 cw
    tc := task.toolCalls[task.asyncIdx]
    task.cw.Push(ctxwin.RoleTool, toolResult,
        ctxwin.WithToolCallID(tc.ID),
        ctxwin.WithToolName(tc.Function.Name),
        ctxwin.WithEphemeral(true),
    )

    // 如果还有其他同步 tool results 没 push，一起 push
    for i, r := range task.syncResults {
        if i == task.asyncIdx {
            continue // 异步的已经 push 了
        }
        task.cw.Push(ctxwin.RoleTool, r,
            ctxwin.WithToolCallID(task.toolCalls[i].ID),
            ctxwin.WithToolName(task.toolCalls[i].Function.Name),
            ctxwin.WithEphemeral(true),
        )
    }

    // 从 task.cw 构建消息，调用 LLM 继续工具循环
    a.continueToolLoop(task.callerCtx, task.out, task.cw, task.iter+1)
}
```

---

## 二、PriorityMailbox

### 2.1 设计

L1 需要 PriorityMailbox 来确保委托结果高优先级投递，
避免排在普通用户消息后面等待。

```go
// internal/agent/mailbox.go（新文件）

type Priority int

const (
    PriorityNormal Priority = iota // 普通用户 Ask / Submit
    PriorityHigh                   // 委托回传 / 超时事件
)

type prioritizedJob struct {
    priority Priority
    job      job
}

type PriorityMailbox struct {
    highCh   chan prioritizedJob  // cap=4
    normalCh chan prioritizedJob  // cap=8
}
```

### 2.2 run goroutine 适配

```go
func (a *Agent) run(ctx context.Context, pm *PriorityMailbox, done chan<- struct{}) {
    for {
        // 优先检查 highCh
        select {
        case <-ctx.Done():
            // ... drain + return
        case pj := <-pm.highCh:
            a.state.Store(int32(StateProcessing))
            pj.job(ctx)
            a.state.Store(int32(StateIdle))
        default:
            // highCh 无消息时，同时等 highCh + normalCh
            select {
            case <-ctx.Done():
                // ...
            case pj := <-pm.highCh:
                a.state.Store(int32(StateProcessing))
                pj.job(ctx)
                a.state.Store(int32(StateIdle))
            case pj := <-pm.normalCh:
                a.state.Store(int32(StateProcessing))
                pj.job(ctx)
                a.state.Store(int32(StateIdle))
            }
        }
    }
}
```

### 2.3 与现有 mailbox 的兼容

- Agent 新增 `priorityMailbox *PriorityMailbox` 字段
- `WithPriorityMailbox()` Option 启用
- 未启用时保持 `chan job`（L2/L3 不需要优先级）
- submit 方法适配：有 PriorityMailbox 时投递到 pm，否则投递到 chan job

---

## 三、AgentTemplate + AgentFactory

### 3.1 AgentTemplate

```go
// internal/agent/factory.go

type AgentTemplate struct {
    ID             string
    Name           string
    Description    string
    SystemPrompt   string            // markdown body
    ModelID        string
    Reasoning      bool
    Skills         []string
    SubAgents      []string          // 子 Agent 名称列表（L2 专用）
    IsLeader       bool
    Ephemeral      bool
}
```

### 3.2 AgentFactory 接口

```go
type AgentFactory interface {
    Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error)
}
```

### 3.3 DefaultFactory 实现

```go
type DefaultFactory struct {
    registry   *Registry
    llm        LLMClient
    toolsCfg   tools.Config
    skillDir   string
    log        *logger.Logger
}

func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error) {
    // 1. 构建 Definition（KindCustom, systemPrompt from tmpl）
    // 2. 构建 tools（Build(toolsCfg)）
    // 3. 如果 tmpl.SubAgents 非空，为每个 SubAgent 创建同步模式 DelegateTool
    // 4. 加载 skills（LoadSkillsFromDir）
    // 5. 创建 Agent（WithTools, WithSkills）
    // 6. 如果 tmpl.Ephemeral → WithEphemeral()
    // 7. 如果 tmpl.IsLeader → WithPriorityMailbox()（L2 也可能收到回传）
    // 8. 创建 ContextWindow，push system prompt + skill catalog
    // 9. Register 到 registry
    // 10. Start Agent
    // 11. 返回 (agent, cw, nil)
}
```

---

## 四、Supervisor（L2 领域主管）

### 4.1 设计

L2 本质是普通 Agent + 子 Agent 生命周期管理。Supervisor 组合 Agent。

L2 → L3 的委托是**同步阻塞**的（通过现有 DelegateTool），
因为用户不直接与 L2 交互，阻塞等待 L3 结果是可接受的。

### 4.2 Supervisor 结构

```go
// internal/agent/supervisor.go

type Supervisor struct {
    agent    *Agent
    factory  AgentFactory
    children map[string]*childSlot
    childMu  sync.RWMutex
    log      *logger.Logger
}

type childSlot struct {
    agent     *Agent
    cw        *ctxwin.ContextWindow
    createdAt time.Time
}

func NewSupervisor(agent *Agent, factory AgentFactory, log *logger.Logger) *Supervisor
```

### 4.3 核心方法

```go
// SpawnChild 基于 AgentTemplate 实例化一个 L3 子 Agent
func (s *Supervisor) SpawnChild(ctx context.Context, tmpl AgentTemplate) (*Agent, error)

// ReapChild 回收一个子 Agent（Stop → Unregister → 删除 slot）
func (s *Supervisor) ReapChild(childID string, timeout time.Duration) error

// ReapAll 回收所有子 Agent
func (s *Supervisor) ReapAll(timeout time.Duration) []error

// Children 返回当前所有子 Agent 的快照
func (s *Supervisor) Children() []*Agent
```

### 4.4 Fan-out/Fan-in 机制

L2 的 Fan-out/Fan-in **复用现有 execTools 并行机制**：

1. L2 的 LLM 返回多个 `delegate_*` tool_calls
2. `execTools` 并行执行每个 DelegateTool（`WithParallelTools(true)`）
3. 每个 DelegateTool.Execute 同步阻塞调用 L3.Ask()
4. 全部完成后结果注入 L2 上下文

Supervisor 的核心价值是**生命周期管理**（Spawn/Reap），而非并发调度。

---

## 五、WithEphemeral Option

```go
// internal/agent/agent.go — 新增

func WithEphemeral() Option {
    return func(a *Agent) {
        a.ephemeral = true
        if a.mailboxCap <= 0 || a.mailboxCap == DefaultMailboxCap {
            a.mailboxCap = 1
        }
    }
}
```

---

## 六、SubAgents → AgentTemplate 加载

### 6.1 修改 prompt/parser.go

保留 `LoadLeaders`（兼容），新增：

```go
// LoadAgentFiles 扫描 agents 目录，返回所有解析后的 AgentFile
func LoadAgentFiles(agentsDir string) ([]AgentFile, error)
```

### 6.2 main.go 改造

当前流程：
1. `LoadLeaders` → L2 列表
2. 为每个 L2 创建 `DelegateTool`（异步模式）
3. 只创建 L1 主 Agent

改造后：
1. `LoadAgentFiles` → 所有 agent 定义
2. 用 `DefaultFactory` 创建 L2 Agent（IsLeader=true 的）
3. L1 的 `DelegateTool` 指向已注册的 L2（异步模式）
4. L2 的 `DelegateTool` 指向 L3 模板名（同步模式，运行时由 factory 创建 L3 实例）
5. 创建 `Supervisor` 管理 L2 → L3 的生命周期

---

## 七、新事件类型

```go
// internal/agent/events.go — 新增

// DelegationStartedEvent L1 异步委托已启动
type DelegationStartedEvent struct {
    Iter           int
    CorrelationID  string
    TargetAgentID  string
    Task           string
}

func (DelegationStartedEvent) agentEvent() {}

// DelegationCompletedEvent L1 异步委托已完成，结果已注入
type DelegationCompletedEvent struct {
    Iter          int
    CorrelationID string
    TargetAgentID string
    Result        string
}

func (DelegationCompletedEvent) agentEvent() {}
```

---

## 八、实施步骤（按顺序）

### Step 1：基础能力
- 新增 `internal/agent/factory.go` — AgentTemplate + AgentFactory + DefaultFactory
- 修改 `internal/agent/agent.go` — ephemeral 字段 + WithEphemeral + pending 字段 + agentCtxKey
- 修改 `internal/prompt/parser.go` — LoadAgentFiles 函数

### Step 2：PriorityMailbox
- 新增 `internal/agent/mailbox.go` — PriorityMailbox
- 修改 `internal/agent/agent.go` — priorityMailbox 字段 + WithPriorityMailbox
- 修改 `internal/agent/run.go` — 支持 PriorityMailbox
- 修改 `internal/agent/ask.go` — submit 适配 PriorityMailbox

### Step 3：L1 异步委托
- 新增 `internal/agent/delegate_async.go` — delegatedTask + resumeDelegation
- 修改 `internal/tools/delegate.go` — Async 字段 + 异步 Execute 路径
- 修改 `internal/agent/stream.go` — 异步检测 + 暂停/恢复 tool loop
- 修改 `internal/agent/events.go` — DelegationStartedEvent + DelegationCompletedEvent

### Step 4：Supervisor
- 新增 `internal/agent/supervisor.go` — Supervisor + Spawn/Reap

### Step 5：main.go 改造
- 修改 `cmd/soloqueue/main.go` — factory 改造

### Step 6：集成测试
- 端到端验证 L1 → L2 → L3 异步链路
- 确保 L3 完成后被正确回收
- 验证 L1 在等待委托期间可响应新消息

---

## 九、关键文件清单

| 操作 | 文件 | 说明 |
|------|------|------|
| 新增 | `internal/agent/factory.go` | AgentTemplate + AgentFactory + DefaultFactory |
| 新增 | `internal/agent/supervisor.go` | Supervisor 生命周期管理 |
| 新增 | `internal/agent/mailbox.go` | PriorityMailbox |
| 新增 | `internal/agent/delegate_async.go` | 异步委托追踪 + 恢复 |
| 修改 | `internal/agent/agent.go` | ephemeral + pending + priorityMailbox + agentCtxKey |
| 修改 | `internal/agent/run.go` | PriorityMailbox 支持 |
| 修改 | `internal/agent/ask.go` | submit 适配 PriorityMailbox |
| 修改 | `internal/agent/stream.go` | 异步委托暂停/恢复 |
| 修改 | `internal/agent/events.go` | Delegation 事件 |
| 修改 | `internal/tools/delegate.go` | Async 模式 |
| 修改 | `internal/prompt/parser.go` | LoadAgentFiles |
| 修改 | `cmd/soloqueue/main.go` | factory 改造 |

---

## 十、与 plan.md 原设计的对应

| plan.md 原设计 | 本计划实现 | 状态 |
|---------------|-----------|------|
| Supervisor + Fan-out/Fan-in | Supervisor 组合 Agent，复用 execTools | 包含 |
| AgentFactory + AgentTemplate | DefaultFactory + AgentTemplate | 包含 |
| WithEphemeral | WithEphemeral Option | 包含 |
| PriorityMailbox | PriorityMailbox（双 channel 优先级） | 包含 |
| pending 委托追踪 | delegatedTask + replyCh + pending map | 包含 |
| 修改 run.go | PriorityMailbox 适配 | 包含 |
| SubAgents → AgentTemplate | LoadAgentFiles + DefaultFactory | 包含 |

本计划完整覆盖 plan.md 的 Phase B 全部内容，并补充了 plan.md 未涉及的：
- L1 异步委托的暂停/恢复机制
- DelegateTool 双模式（异步/同步）
- 新事件类型（DelegationStarted/Completed）
