# SoloQueue 异步多智能体 Actor 架构完整方案

> 基于 Phase 1 代码分析，标注"已实现"与"待建设"，给出具体 Go 类型与文件规划。
> 本次不含 WebUI 相关工作。委托设计为 Skill 模式，先实现内置 Skill。

---

## 一、核心底盘：Skill 接入与控制流转

### 1.1 Skill 模型设计

**现状**：
- `Tool` 接口（`internal/agent/tool.go`）定义了 `Name/Description/Parameters/Execute`
- `ToolRegistry` 管理注册与查找，`Specs()` 返回 `[]llm.ToolDef`
- `tools.Build(cfg)`（`internal/tools/tools.go:128`）静态构建所有内置工具
- `AgentFrontmatter.Skills`（`internal/prompt/parser.go:20`）是 YAML 字段，但**运行时未使用**
- `main.go` 中 `agent.WithTools(toolList...)` 一次性注入所有工具，无按需加载

**目标**：引入 `Skill` 概念，作为 Agent 能力的组织单元。一个 Skill 可包含一个或多个 Tool，并带有描述和配置。委托（delegate）作为内置 Skill 实现。

#### 1.1.1 Skill 接口（新增）

```go
// internal/agent/skill.go（新文件）

// Skill 是 Agent 能力的组织单元
//
// 与 Tool 的区别：
//   - Tool 是最小粒度的可调用单元（1:1 映射 LLM function calling）
//   - Skill 是能力的组织单元，可包含 1~N 个 Tool
//   - Skill 有描述性元数据（Description），用于 LLM 理解能力范围
//   - Skill 有分类（Builtin / User），区分内置与外置
//
// 两种实现：
//   - BuiltinSkill：Go 代码注册，包含 Tool 实例列表
//     - 文件操作 Skill → file_read + write_file + replace + multi_replace + ...
//     - 委托 Skill   → delegate_dev + delegate_ops + ...（按可用 Team Leader 动态生成）
//   - UserSkill：从 .soloqueue/skills/ 目录加载（后续 Phase）
type Skill interface {
    // ID 返回 Skill 唯一标识（如 "fs", "web", "delegate"）
    ID() string

    // Description 给 LLM 看的自然语言描述
    Description() string

    // Category 返回 Skill 分类
    Category() SkillCategory

    // Tools 返回该 Skill 暴露的所有 Tool
    Tools() []Tool
}

// SkillCategory 区分内置与外置 Skill
type SkillCategory string

const (
    SkillBuiltin SkillCategory = "builtin" // 内置 Skill（Go 代码注册）
    SkillUser    SkillCategory = "user"    // 用户外置 Skill（.soloqueue/skills/ 目录）
)
```

#### 1.1.2 BuiltinSkill 实现（新增）

```go
// internal/agent/skill.go

// BuiltinSkill 内置 Skill 的标准实现
type BuiltinSkill struct {
    id          string
    description string
    tools       []Tool
}

func NewBuiltinSkill(id, description string, tools ...Tool) *BuiltinSkill {
    return &BuiltinSkill{id: id, description: description, tools: tools}
}

func (s *BuiltinSkill) ID() string          { return s.id }
func (s *BuiltinSkill) Description() string { return s.description }
func (s *BuiltinSkill) Category() SkillCategory { return SkillBuiltin }
func (s *BuiltinSkill) Tools() []Tool       { return s.tools }
```

#### 1.1.3 SkillRegistry（新增，组合 ToolRegistry）

```go
// internal/agent/skill.go

// SkillRegistry 管理 Skill 的注册与查找
//
// 组合现有 ToolRegistry —— 当 Skill 注册时，其所有 Tool 自动注册到底层 ToolRegistry。
// 对外暴露：
//   - Skill 维度：按 Skill ID 查找/列举
//   - Tool 维度：复用 ToolRegistry 的 Get/Specs/Len/Names
type SkillRegistry struct {
    skills map[string]Skill   // skillID → Skill
    tools  *ToolRegistry      // 组合，管理所有 Tool
    mu     sync.RWMutex
}

// Register 注册一个 Skill（其所有 Tool 自动注册到 tools）
func (r *SkillRegistry) Register(s Skill) error

// GetSkill 按 Skill ID 查找
func (r *SkillRegistry) GetSkill(id string) (Skill, bool)

// Skills 返回所有已注册 Skill 的快照
func (r *SkillRegistry) Skills() []Skill

// ToolSpecs 返回所有 Skill 暴露的 Tool 声明（给 LLM 用）
// 等价于底层 ToolRegistry.Specs()
func (r *SkillRegistry) ToolSpecs() []llm.ToolDef

// ToolRegistry 返回底层 ToolRegistry（供 Agent 执行 tool 时查找）
func (r *SkillRegistry) ToolRegistry() *ToolRegistry
```

#### 1.1.4 将现有 Tools 重构为 Skill 组织

**现有工具按功能域分组**：

| Skill ID | Description | 包含 Tool |
|----------|-------------|----------|
| `fs` | 文件系统读写操作 | file_read, write_file, replace, multi_replace, multi_write, glob |
| `search` | 内容搜索 | grep |
| `shell` | Shell 命令执行 | shell_exec |
| `web` | 网络与搜索 | http_fetch, web_search |
| `delegate` | 任务委托（内置） | delegate_*（动态生成） |

```go
// internal/tools/skills.go（新文件）

// BuildSkills 按功能域返回内置 Skill 列表
//
// 替代现有 Build(cfg) []agent.Tool，
// 改为 BuildSkills(cfg) []agent.Skill。
// Agent 构造时通过 SkillRegistry 注册。
func BuildSkills(cfg Config) []agent.Skill {
    return []agent.Skill{
        agent.NewBuiltinSkill("fs", "File system read/write operations",
            newFileReadTool(cfg), newGlobTool(cfg),
            newWriteFileTool(cfg), newReplaceTool(cfg),
            newMultiReplaceTool(cfg), newMultiWriteTool(cfg),
        ),
        agent.NewBuiltinSkill("search", "Content search within files",
            newGrepTool(cfg),
        ),
        agent.NewBuiltinSkill("shell", "Execute shell commands",
            newShellExecTool(cfg),
        ),
        agent.NewBuiltinSkill("web", "HTTP requests and web search",
            newHTTPFetchTool(cfg), newWebSearchTool(cfg),
        ),
    }
}
```

#### 1.1.5 delegate 内置 Skill（核心新增）

```go
// internal/agent/delegate_skill.go（新文件）

// DelegateSkill 是内置的委托 Skill
//
// 根据可用的 Team Leader 动态生成 delegate_* Tool。
// LLM 看到的就是一组普通工具（如 delegate_dev, delegate_ops），
// 但底层执行时框架负责：
//   1. 从 AgentLocator 找到目标 Agent
//   2. 向目标 Agent 投递任务
//   3. 异步等待结果
//   4. 将结果作为工具返回值注入回调用方上下文
//
// 对 LLM 而言，委托 = 调用一个工具，无需知道通信协议。
type DelegateSkill struct {
    leaders []LeaderInfo     // 可用的 Team Leader 列表
    locator AgentLocator    // 查找 Agent 实例
    timeout time.Duration   // 委托默认超时
}

func NewDelegateSkill(leaders []LeaderInfo, locator AgentLocator, timeout time.Duration) *DelegateSkill

func (ds *DelegateSkill) ID() string          { return "delegate" }
func (ds *DelegateSkill) Description() string { return "Delegate tasks to team leaders" }
func (ds *DelegateSkill) Category() SkillCategory { return SkillBuiltin }

// Tools 动态生成：每个 LeaderInfo → 一个 DelegateTool
func (ds *DelegateSkill) Tools() []Tool {
    var tools []Tool
    for _, l := range ds.leaders {
        tools = append(tools, &DelegateTool{
            leaderID: l.Name,
            desc:     l.Description,
            locator:  ds.locator,
            timeout:  ds.timeout,
        })
    }
    return tools
}
```

#### 1.1.6 DelegateTool 实现（新增）

```go
// internal/agent/delegate_tool.go（新文件）

// DelegateTool 是一个 Tool 实现，将任务委托给指定 Team Leader
//
// 实现 Tool 接口 → 可被 ToolRegistry 注册 → LLM 通过 function calling 调用
// 实现 Confirmable 接口 → 委托前可要求用户确认
type DelegateTool struct {
    leaderID string          // 目标 Agent 的标识（如 "dev"）
    desc     string          // Leader 描述（用于 Tool.Description）
    locator  AgentLocator    // 查找 Agent 实例
    timeout  time.Duration   // 委托超时
}

func (dt *DelegateTool) Name() string {
    return "delegate_" + dt.leaderID
}

func (dt *DelegateTool) Description() string {
    return fmt.Sprintf("Delegate a task to team leader '%s': %s", dt.leaderID, dt.desc)
}

func (dt *DelegateTool) Parameters() json.RawMessage {
    // {"type":"object","properties":{"task":{"type":"string","description":"Task description to delegate"}},"required":["task"]}
    return delegateParamsSchema
}

func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
    // 1. 解析 args → DelegateArgs{Task string}
    // 2. 从 locator 找到目标 Agent
    // 3. 构造委托 prompt
    // 4. 调用目标 Agent 的 Ask / AskStreamWithHistory
    // 5. 返回结果字符串（作为 tool result 注入 LLM 上下文）
    //
    // 超时处理：
    //   - 使用 context.WithTimeout 包裹 ctx
    //   - 超时后返回 "error: delegation to xxx timed out after Ys"
    //   - 不中断调用方的工具循环
}

// Confirmable 实现（可选，委托高危操作时需确认）
func (dt *DelegateTool) CheckConfirmation(args string) (bool, string)
func (dt *DelegateTool) ConfirmationOptions(args string) []string
func (dt *DelegateTool) ConfirmArgs(originalArgs string, choice ConfirmChoice) string
func (dt *DelegateTool) SupportsSessionWhitelist() bool
```

#### 1.1.7 AgentLocator 接口（新增）

```go
// internal/agent/locator.go（新文件）

// AgentLocator 按 ID 查找运行中的 Agent 实例
//
// 由现有 Registry 实现（签名兼容）。
type AgentLocator interface {
    Locate(id string) (*Agent, bool)
}

// Registry.Get 签名兼容，直接实现
var _ AgentLocator = (*Registry)(nil)

func (r *Registry) Locate(id string) (*Agent, bool) {
    return r.Get(id)
}
```

#### 1.1.8 Agent 集成 SkillRegistry（修改）

```go
// internal/agent/agent.go — 修改

type Agent struct {
    // 现有字段 ...
    // tools *ToolRegistry     ← 删除
    caps    *SkillRegistry    // ← 替换为 SkillRegistry

    // Phase 2 新增：委托追踪
    pending  map[string]*delegatedTask // correlationID → 等待中的委托任务
    pendMu   sync.RWMutex
}

// WithSkills 替代 WithTools
func WithSkills(skills ...Skill) Option {
    return func(a *Agent) {
        if a.caps == nil {
            a.caps = NewSkillRegistry()
        }
        for _, s := range skills {
            if err := a.caps.Register(s); err != nil {
                panic(fmt.Sprintf("agent: WithSkills: %v", err))
            }
        }
    }
}

// WithTools 保留兼容（内部包装为 BuiltinSkill）
func WithTools(tools ...Tool) Option {
    return func(a *Agent) {
        if len(tools) == 0 {
            return
        }
        // 将裸 Tool 包装为匿名 BuiltinSkill 注册
        if a.caps == nil {
            a.caps = NewSkillRegistry()
        }
        for _, t := range tools {
            s := NewBuiltinSkill("_tool_"+t.Name(), "Auto-wrapped tool", t)
            if err := a.caps.Register(s); err != nil {
                panic(fmt.Sprintf("agent: WithTools: %v", err))
            }
        }
    }
}

// ToolSpecs 从 SkillRegistry 获取
func (a *Agent) ToolSpecs() []llm.ToolDef {
    if a.caps == nil {
        return nil
    }
    return a.caps.ToolSpecs()
}
```

**涉及文件**：
- 新增 `internal/agent/skill.go` — Skill 接口 + BuiltinSkill + SkillRegistry
- 新增 `internal/agent/delegate_skill.go` — DelegateSkill
- 新增 `internal/agent/delegate_tool.go` — DelegateTool
- 新增 `internal/agent/locator.go` — AgentLocator
- 修改 `internal/agent/agent.go` — `tools` → `caps *SkillRegistry`，新增 `WithSkills` Option
- 修改 `internal/agent/tool.go` — `safeGet` 改为从 `SkillRegistry.ToolRegistry()` 查找
- 修改 `internal/agent/stream.go` — `execTools` 改为统一通过 `SkillRegistry` 查找 tool
- 新增 `internal/tools/skills.go` — `BuildSkills` 替代 `Build`
- 保留 `internal/tools/tools.go` — `Build` 标记 deprecated，内部改为调用 BuildSkills

---

### 1.2 控制权分离：委托与回复

**核心原则**：委托由 LLM 自主决策，回复由框架强接管。

#### 1.2.1 委托执行流

```
L1 Agent (AskStreamWithHistory)
  │
  ├─ LLM 返回 tool_calls: [delegate_dev(task="实现登录页")]
  │
  ├─ execToolStream 识别 tool name → 从 SkillRegistry 查找
  │   └─ 找到 DelegateTool{leaderID:"dev"}
  │       │
  │       ├─ 解析 args → {task: "实现登录页"}
  │       ├─ 从 AgentLocator 找到 L2 Agent 实例
  │       ├─ 构造 merged ctx（携带 correlationID）
  │       ├─ L2.AskStreamWithHistory(ctx, cw, taskPrompt)
  │       │   └─ L2 执行完成后返回 result
  │       │
  │       └─ result 作为 delegate_dev 的返回值
  │
  └─ 工具循环：result 塞回 LLM 上下文 → LLM 自主决定下一步
```

#### 1.2.2 回复注入（框架自动）

委托结果通过现有工具循环机制自动注入，无需 Agent 感知通信协议：

1. `DelegateTool.Execute()` 返回 `(result, nil)`
2. `execToolStream` 将 result 写入结果
3. 工具循环构造 `role=tool` 消息追加到 `msgs`
4. LLM 下一轮看到完整结果

**Agent 完全不需要知道"通信录"和"回复格式"**——对 LLM 而言，委托就是调用一个工具。

---

## 二、调度核心：三层 Agent 并发模型

**现状**：
- L1 单 Agent，mailbox 为 FIFO `chan job`（`internal/agent/run.go:18-49`）
- L2/L3 仅有 Prompt 层元数据（`AgentFrontmatter.IsLeader`/`SubAgents`，`internal/prompt/parser.go`）
- 无跨 Agent 协调、无 Stash、无 Factory

**目标**：实现 L1→L2→L3 的运行时并发调度。

### 2.1 Layer 1：全局主 Agent（异步非阻塞中枢）

**现状**：L1 的 `run()` 是单 goroutine 串行消费 mailbox。一个 Ask 在执行时，后续 job 必须排队。

**目标**：L1 派发委托后立刻释放 Goroutine，继续处理新消息。通过状态机维护等待中的 Correlation ID。

#### 2.1.1 优先级 Mailbox（新增）

```go
// internal/agent/mailbox.go（新文件）

// Priority 区分 mailbox 中的 job 优先级
type Priority int

const (
    PriorityNormal Priority = iota // 普通用户 Ask / Submit
    PriorityHigh                   // 超时事件 / 取消指令 / 委托回传
)

// prioritizedJob 带 Priority 的 job
type prioritizedJob struct {
    priority Priority
    job      job
}

// PriorityMailbox 支持优先级的 mailbox
//
// 核心设计：
//   - 两个 channel：highCh（容量 4）和 normalCh（容量 8）
//   - run goroutine 优先检查 highCh（超时事件优先处理）
//   - Stash 机制暂不实现（L2 逻辑阻塞的 stash 需求可后续迭代）
type PriorityMailbox struct {
    highCh   chan prioritizedJob
    normalCh chan prioritizedJob
}
```

#### 2.1.2 委托追踪状态（修改 Agent）

```go
// internal/agent/agent.go — Agent 新增字段

type Agent struct {
    // ... 现有字段 ...

    // Phase 2：委托追踪
    pending map[string]*delegatedTask // correlationID → 等待中的委托
    pendMu  sync.RWMutex
}

// delegatedTask 追踪一个等待中的委托任务
type delegatedTask struct {
    correlationID string
    targetAgentID string
    replyCh       chan delegateResult
    createdAt     time.Time
    timeout       time.Duration
}

type delegateResult struct {
    content string
    err     error
}
```

#### 2.1.3 异步委托流

```
L1 run goroutine
  │
  ├─ 收到 job（用户 Ask）
  │   └─ runOnceStreamWithHistory
  │       ├─ LLM 返回 delegate_dev(task="...")
  │       ├─ execToolStream → DelegateTool.Execute()
  │       │   ├─ 注册 pending[correlationID]
  │       │   ├─ 启动异步 goroutine：
  │       │   │   ├─ L2.AskStreamWithHistory(...)
  │       │   │   └─ 结果写入 replyCh
  │       │   ├─ 启动超时定时器（scheduleTimeout）
  │       │   └─ 阻塞等待 replyCh 或超时
  │       │       └─ 结果返回给 execToolStream
  │       └─ 结果塞回 LLM 上下文 → 继续工具循环
  │
  └─ 处理 mailbox 中的新 job
```

**涉及文件**：
- 新增 `internal/agent/mailbox.go`
- 修改 `internal/agent/agent.go` — 新增 `pending` 字段
- 修改 `internal/agent/run.go` — 支持 PriorityMailbox
- 修改 `internal/agent/ask.go` — `submit` 适配 PriorityMailbox

---

### 2.2 Layer 2：领域主管 Agent（逻辑阻塞 + 异步并发子线）

**现状**：`AgentFrontmatter.IsLeader`（`internal/prompt/parser.go:19`）和 `LeaderInfo`（`internal/prompt/types.go:3-10`）定义了 L2 元数据，`buildRoutingTable`（`internal/prompt/routing.go`）生成路由表注入 L1 prompt。但运行时无 L2 Agent 实例。

**目标**：L2 接收 L1 任务后，Fan-out 并发派发给多个 L3，Fan-in 聚合结果。

#### 2.2.1 Supervisor 结构（新增）

```go
// internal/agent/supervisor.go（新文件）

// Supervisor 是 L2 领域主管
//
// 嵌入 Agent 复用全部 Actor 基础设施（mailbox、lifecycle、stream），
// 额外增加子 Agent 管理能力。
type Supervisor struct {
    *Agent
    factory  AgentFactory     // 子 Agent 工厂
    children map[string]*childSlot // childID → 子 Agent 槽位
    childMu  sync.RWMutex
    locator  AgentLocator     // 用于注册/查找子 Agent
    registry *Registry        // 用于 Register/Unregister 子 Agent
}

// childSlot 追踪一个 L3 子 Agent
type childSlot struct {
    agent     *Agent
    replyCh   chan childResult
    createdAt time.Time
}

type childResult struct {
    content string
    err     error
}

// SpawnChild 基于 AgentTemplate 实例化一个 L3 子 Agent
func (s *Supervisor) SpawnChild(ctx context.Context, tmpl AgentTemplate, task string) (*Agent, error)

// WaitAll 等待所有子 Agent 完成，聚合结果（Fan-in）
func (s *Supervisor) WaitAll(ctx context.Context) []childResult

// CancelAllChildren 取消所有子 Agent（级联取消）
func (s *Supervisor) CancelAllChildren()

// ReapChild 回收一个已完成的 L3 子 Agent（Stop → Unregister → 删除）
func (s *Supervisor) ReapChild(childID string, timeout time.Duration) error
```

#### 2.2.2 AgentFactory 与 AgentTemplate（新增）

```go
// internal/agent/factory.go（新文件）

// AgentTemplate 是 Agent 的模板定义
//
// 来源于 ~/.soloqueue/agents/*.md 的 YAML frontmatter。
// AgentFrontmatter.SubAgents 引用其他 agent 的 name，
// 运行时从 agents 目录加载对应模板。
type AgentTemplate struct {
    ID            string
    Name          string
    ModelID       string
    ProviderID    string
    SystemPrompt  string  // markdown body
    Skills        []string // 需要加载的 Skill ID
    ReasoningEffort string
    IsLeader      bool
}

// AgentFactory 从模板实例化 Agent
type AgentFactory interface {
    Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error)
}
```

#### 2.2.3 L2 Fan-out/Fan-in 流

```
L2 Supervisor (AskStreamWithHistory)
  │
  ├─ LLM 分析任务，返回多个 tool_calls（每个对应一个子任务）
  │   [delegate_fe(task="实现登录表单"), delegate_be(task="实现登录API")]
  │
  ├─ execToolStream → 每个 DelegateTool.Execute()
  │   ├─ SpawnChild → L3-fe, L3-be
  │   ├─ 并行 L3.AskStreamWithHistory → replyCh
  │   └─ WaitAll → 聚合结果
  │
  ├─ 结果注入 L2 上下文，LLM 综合分析
  └─ DoneEvent → 结果回传 L1
```

**涉及文件**：
- 新增 `internal/agent/supervisor.go`
- 新增 `internal/agent/factory.go`
- 修改 `internal/prompt/parser.go` — `SubAgents` 关联到 `AgentTemplate` 加载

---

### 2.3 Layer 3：执行侧 Agent（轻量级、阅后即焚）

**目标**：L3 作为临时实例存在，处理完单一目标任务后立即被回收。

#### 2.3.1 WithEphemeral Option（新增）

```go
// internal/agent/agent.go — 新增 Option

// WithEphemeral 将 Agent 标记为阅后即焚
//
// 语义：
//   - MaxIterations 默认 3（L3 不需要多轮工具循环）
//   - MailboxCap 默认 1（只接收一个任务）
//   - 无 Timeline 持久化
//   - 执行完成后由 Supervisor.ReapChild 回收
func WithEphemeral() Option {
    return func(a *Agent) {
        a.ephemeral = true
        if a.mailboxCap <= 0 || a.mailboxCap == DefaultMailboxCap {
            a.mailboxCap = 1
        }
    }
}
```

#### 2.3.2 生命周期流

```
L2.Supervisor.SpawnChild
  ├─ factory.Create(template) → *Agent + *ContextWindow
  ├─ Registry.Register(child)  // 使 Locator 可查找
  ├─ Agent.Start(parentCtx)    // parentCtx = L2 的 ctx（级联取消）
  │
  ├─ L3.AskStreamWithHistory(ctx, cw, taskPrompt)
  │   └─ DoneEvent
  │
  ├─ 结果写入 childSlot.replyCh
  │
  └─ Supervisor.ReapChild → Stop → Unregister → 从 children 删除
```

**涉及文件**：
- 修改 `internal/agent/agent.go` — 新增 `ephemeral bool` + `WithEphemeral`
- 新增 `internal/agent/supervisor.go` — `SpawnChild`/`ReapChild`

---

## 三、容错机制：基于消息驱动的超时闭环

**现状**：
- 工具级超时已实现（`WithToolTimeout`，`agent.go:168-179`）
- 超时后格式化为 `"error: tool timeout after Xs"` 喂回 LLM（`stream.go:699-700`）
- 无跨 Agent 超时传播、无 Self-Messaging 模式

**目标**：超时抽象为 Actor 系统内的一等公民，通过 Self-Messaging 实现异步闭环。

### 3.1 全局时间约束

```go
// internal/agent/timeout.go（新文件）

const (
    TaskDefaultTimeout = 5 * time.Minute
    TaskMaxTimeout     = 15 * time.Minute
)

// TimeoutPolicy 超时后的处理策略
type TimeoutPolicy int

const (
    TimeoutCancel    TimeoutPolicy = iota // 直接取消（默认）
    TimeoutSummarize                      // 让 LLM 总结当前进度后重试
    TimeoutDelegate                       // 转委托给其他 Agent
)
```

### 3.2 委托超时实现

DelegateTool.Execute 中的超时处理：

```go
// internal/agent/delegate_tool.go

func (dt *DelegateTool) Execute(ctx context.Context, args string) (string, error) {
    // ... 解析 args，查找 target Agent ...

    timeout := dt.timeout
    if timeout <= 0 {
        timeout = TaskDefaultTimeout
    }
    if timeout > TaskMaxTimeout {
        timeout = TaskMaxTimeout
    }

    // 委托 ctx：在 caller ctx 基础上叠加超时
    delCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    // 同步等待委托结果（在 Agent 的 job goroutine 中执行）
    // 超时后 context 自动取消 → 目标 Agent 收到 ctx.Done
    result, err := targetAgent.Ask(delCtx, taskPrompt)
    if err != nil {
        if delCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
            return fmt.Sprintf("error: delegation to %s timed out after %s, task has been cancelled", dt.leaderID, timeout), nil
        }
        return "error: " + err.Error(), nil
    }
    return result, nil
}
```

### 3.3 级联取消

L2 的 parentCtx 被 cancel → 所有 L3 的 ctx 传播 cancel：

```
L1 委托超时
  ├─ delCtx cancel
  ├─ L2 Ask 收到 ctx.Done → 停止执行
  │   └─ Supervisor.CancelAllChildren
  │       ├─ L3-fe ctx.Done → 停止
  │       └─ L3-be ctx.Done → 停止
  └─ L1 收到超时错误 → LLM 自主决策
```

现有 `mergeCtx`（`internal/agent/helpers.go:64-78`）已经确保 caller ctx 和 agent ctx 任一取消都会传播。L3 的 parentCtx = L2 的 ctx，L2 的 ctx 又受 L1 的委托 ctx 控制，天然级联。

**涉及文件**：
- 新增 `internal/agent/timeout.go`
- 新增 `internal/agent/delegate_tool.go` — 超时处理逻辑
- 修改 `internal/agent/supervisor.go` — `CancelAllChildren`

---

## 四、可观测性：事件驱动遥测（后端部分，不含 WebUI）

**现状**：
- AgentEvent（9 种）通过 `out chan AgentEvent` 直接流向消费者
- 无 Event Bus、无 State Projector
- Timeline（`internal/timeline/`）是 JSONL append-only，`MessagePayload.AgentID` 已预留但未使用
- Logger 三层结构化日志（System/Team/Session），`trace_id` + `actor_id`

**目标**：建立后端旁路事件系统，为未来 UI 可视化留接口。本次不实现 WebUI 展示。

### 4.1 EventBus：Fire-and-forget 遥测

```go
// internal/eventbus/bus.go（新文件）

// Sink 事件消费端接口
type Sink interface {
    OnEvent(ctx context.Context, ev EnrichedEvent)
}

// EnrichedEvent 在 AgentEvent 基础上增加 Agent/Correlation 元数据
type EnrichedEvent struct {
    AgentID       string
    CorrelationID string
    Layer         int          // 1=L1, 2=L2, 3=L3
    Timestamp     time.Time
    AgentState    State
    Payload       AgentEvent
}

// EventBus 无阻塞的发布/订阅总线
type EventBus struct {
    ch      chan EnrichedEvent   // buffer=256
    sinks   []Sink
    sinkMu  sync.RWMutex
    dropped atomic.Int64
}

func (b *EventBus) Publish(ctx context.Context, ev EnrichedEvent)
func (b *EventBus) Subscribe(sink Sink)
```

### 4.2 Agent 集成 EventBus

```go
// internal/agent/agent.go — Agent 新增字段

type Agent struct {
    // ... 现有字段 ...
    eventBus *eventbus.EventBus // 可为 nil
}

func WithEventBus(bus *eventbus.EventBus) Option

// emit 时同步发布到 EventBus（fire-and-forget，不阻塞 Actor）
```

### 4.3 Timeline 扩展

```go
// internal/timeline/types.go — 新增

// DelegatePayload 委托事件载荷
type DelegatePayload struct {
    FromAgentID   string `json:"from"`
    ToAgentID     string `json:"to"`
    Task          string `json:"task"`
    CorrelationID string `json:"corr_id"`
    Result        string `json:"result,omitempty"`
    Err           string `json:"err,omitempty"`
}
```

**涉及文件**：
- 新增 `internal/eventbus/bus.go`
- 修改 `internal/agent/agent.go` — 新增 `eventBus` 字段 + `WithEventBus`
- 修改 `internal/agent/stream.go` — `emit` 时发布到 EventBus
- 修改 `internal/timeline/types.go` — 新增 `DelegatePayload`

---

## 五、实现分期

### Phase A：Skill 基座 + 委托 Skill（最小可用）

**目标**：L1 可以通过 `delegate_*` 工具将任务委托给 L2，结果自动注入 L1 上下文。

1. 新增 `internal/agent/skill.go` — Skill 接口 + BuiltinSkill + SkillRegistry
2. 新增 `internal/agent/delegate_skill.go` — DelegateSkill
3. 新增 `internal/agent/delegate_tool.go` — DelegateTool（同步委托，Ask 阻塞等待）
4. 新增 `internal/agent/locator.go` — AgentLocator + Registry 适配
5. 修改 `internal/agent/agent.go` — `tools` → `caps *SkillRegistry`，新增 `WithSkills`
6. 修改 `internal/agent/stream.go` — tool 查找改为从 SkillRegistry
7. 新增 `internal/tools/skills.go` — `BuildSkills`
8. 修改 `cmd/soloqueue/main.go` — factory 中注册 Skills

**验证**：L1 调用 `delegate_dev` → 任务发给 L2 Agent → L2 结果注入 L1 上下文 → LLM 继续。

### Phase B：三层并发

1. 新增 `internal/agent/supervisor.go` — Supervisor + Fan-out/Fan-in
2. 新增 `internal/agent/factory.go` — AgentFactory + AgentTemplate
3. 新增 `internal/agent/agent.go` — `WithEphemeral` Option
4. 新增 `internal/agent/mailbox.go` — PriorityMailbox
5. 修改 `internal/agent/run.go` — 支持优先级 mailbox
6. 修改 `internal/agent/agent.go` — `pending` 字段 + 委托追踪
7. 修改 `internal/prompt/parser.go` — SubAgents → AgentTemplate 加载

**验证**：L1 → L2 → 多 L3 并行 → L2 聚合 → L1 收到结果。

### Phase C：超时容错

1. 新增 `internal/agent/timeout.go` — 时间约束 + TimeoutPolicy
2. 修改 `internal/agent/delegate_tool.go` — 委托超时 + 级联取消
3. 修改 `internal/agent/supervisor.go` — CancelAllChildren

**验证**：委托超时 → 底层 L3 被级联取消 → L1 收到超时错误 → LLM 自主决策。

### Phase D：可观测性（后端）

1. 新增 `internal/eventbus/bus.go`
2. 修改 `internal/agent/agent.go` — EventBus 集成
3. 修改 `internal/agent/stream.go` — emit 发布遥测
4. 修改 `internal/timeline/types.go` — DelegatePayload

**验证**：EventBus 收到 EnrichedEvent，Timeline 记录委托事件。

---

## 六、关键类型关系图

```
┌─────────────────────────────────────────────────────────┐
│                      Agent (现有)                        │
│  - caps *SkillRegistry       (Phase A 替换 tools)       │
│  - pending map[string]*delegatedTask  (Phase B)         │
│  - eventBus *EventBus                     (Phase D)     │
│  - mailbox PriorityMailbox                (Phase B)     │
└──────────┬──────────────────────────────┬───────────────┘
           │ 嵌入                          │ 组合
           ▼                              ▼
┌──────────────────────┐    ┌──────────────────────────────┐
│   Supervisor (L2)    │    │    SkillRegistry             │
│  - factory           │    │  - skills map[string]Skill   │
│  - children map      │    │  - tools  *ToolRegistry      │
│  - SpawnChild()      │    │  - ToolSpecs() []llm.ToolDef │
│  - WaitAll()         │    │  - Register(Skill)           │
│  - CancelAllChildren │    └──────────┬───────────────────┘
│  - ReapChild()       │               │ 包含
└──────────┬───────────┘               ▼
           │ 创建           ┌──────────────────────────────┐
           ▼               │    Skill 接口                 │
┌──────────────────────┐   │  ├─ BuiltinSkill             │
│   L3 Agent (Ephemeral)│   │  │   ├─ "fs"  → 6 tools    │
│  - mailboxCap=1      │   │  │   ├─ "search" → grep     │
│  - MaxIterations=3   │   │  │   ├─ "shell" → shell_exec│
│  - 无 Timeline       │   │  │   ├─ "web"  → 2 tools    │
│  - 完成后 ReapChild  │   │  │   └─ "delegate" → N tools│
└──────────────────────┘   │  │       └─ DelegateSkill    │
                            │  │           └─ DelegateTool │
┌──────────────────────┐   │  └─ UserSkill (后续 Phase)   │
│   EventBus           │   └──────────────────────────────┘
│  - ch chan EnrichedEv│
│  - Publish()         │   ┌──────────────────────────────┐
│  - Subscribe(Sink)   │   │    AgentFactory              │
└──────────────────────┘   │  - Create(tmpl) → (*Agent,   │
                            │           *ContextWindow)     │
                            └──────────────────────────────┘
```

---

## 七、与现有代码的映射表

| 概念 | 现有文件 | 现有类型 | 改动类型 |
|------|---------|---------|---------|
| Agent Actor | `agent/agent.go` | `Agent` struct | 扩展字段 (caps, pending, eventBus) |
| Mailbox | `agent/run.go` | `mailbox chan job` | 替换为 PriorityMailbox |
| Tool | `agent/tool.go` | `Tool` interface | **不变**，被 Skill 包装 |
| ToolRegistry | `agent/tool.go` | `ToolRegistry` | 被 SkillRegistry 组合 |
| AgentEvent | `agent/events.go` | 9 种 sealed interface | 后续按需扩展 |
| ContextWindow | `ctxwin/ctxwin.go` | `ContextWindow` | 不变 |
| Timeline | `timeline/types.go` | `Event`/`MessagePayload` | 扩展 DelegatePayload |
| Prompt 模板 | `prompt/parser.go` | `AgentFrontmatter` | SubAgents → AgentTemplate |
| 路由表 | `prompt/routing.go` | `buildRoutingTable` | 生成 DelegateSkill 的 Tool |
| Registry | `agent/registry.go` | `Registry` | 实现 AgentLocator 接口 |
| Session | `session/session.go` | `Session`/`SessionManager` | 扩展以支持 Supervisor |
| Tools 构建 | `tools/tools.go` | `Build(cfg)` | 新增 `BuildSkills(cfg)` |
| main.go | `cmd/soloqueue/main.go` | factory 函数 | 使用 SkillRegistry 注册 |
