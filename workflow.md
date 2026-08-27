# Workflow System Design — Historical Design Note

> This repository-level document records an earlier design conversation. It is
> not the current user documentation. Use [docs/README.md](docs/README.md) for
> the current product and [docs/guides/workflows.md](docs/guides/workflows.md)
> for the supported workflow user path.

## 1. 结论与 v1 定位

为 soloQueue 增加一个由用户手写 YAML 定义、由 L1 agent 调用的同步工作流执行器。

v1 是一个**有界有向工作流图执行器**，不是严格 DAG：普通边必须无环，显式 `loop: true` 的边允许形成有限循环。它支持：

- 精确 outcome 路由
- fan-out 并行执行
- fan-in 汇合
- 有界自动循环
- 节点级错误重试
- 超时、取消、确认转发和 agent 清理

v1 明确不支持：

- 等待用户补充信息后恢复
- 服务重启后的状态恢复
- 异步运行、`workflow_status`、`workflow_resume`
- LLM 自动创建或修改 workflow
- Web UI DAG 编辑与可视化
- workflow timeline 持久化

因此，v1 中的所有循环都是一次 `workflow_run` 调用内的自动循环。需要用户参与的暂停/恢复属于 v2。

---

## 2. 核心设计决策

### 2.1 outcome 采用精确匹配

agent 调用 `workflow_handoff(outcome, content)` 后，引擎只匹配同名 output。

不根据 `"failed"`、`"needs_work"` 等字符串猜测成功或失败，也不提供 `on_success/on_failure/always`。业务 outcome 与系统执行错误是两套独立机制：

- 业务结果：`outputs.<outcome>`
- 系统错误：节点超时、LLM 错误、无 handoff、非法 handoff、工具失败等，由 `on_error` 处理

### 2.2 节点定义与节点执行实例分离

同一个节点可能被循环多次执行，因此不能只维护 `map[nodeID]NodeState`。

每次执行都创建独立的 `NodeRun`：

```go
type NodeRun struct {
    ID           string
    NodeID       string
    Attempt      int
    ActivationID string
    State        NodeRunState
    Inputs       []NodeInput
    Result       *HandoffData
    Error        error
    StartedAt    time.Time
    FinishedAt   time.Time
}
```

状态机：

```text
QUEUED -> RUNNING -> SUCCEEDED
                  -> FAILED
                  -> CANCELLED
                  -> TIMED_OUT
```

成功的 loop edge 会创建目标节点的新 `NodeRun`，而不是把旧节点保持在 `RUNNING`。

### 2.3 token 驱动，不维护全图永久 PENDING 状态

workflow 启动时，为每个入口节点创建 activation token。节点输出沿选中的 transition 传递 token 和数据。

没有被路由选中的节点不进入运行态，也不需要永久停留在 `PENDING`。

这可以避免条件分支未选中时造成全局终止判断卡死。

### 2.4 循环必须显式且有界

任何构成环的边必须同时声明：

```yaml
loop: true
max_traversals: 3
```

规则：

- `max_traversals` 必须大于 0。
- 按 `edge + activation` 计数。
- `max_traversals` 表示该 loop edge 最多可成功通过的次数，不包含进入循环前的首次节点执行。
- 达到限制后，workflow 以 `LOOP_LIMIT_EXCEEDED` 失败。
- v1 不支持无限循环。
- v1 禁止 loop edge 进入 `join: all` 节点，避免跨迭代 fan-in 语义不明确。
- loop edge 的目标必须能通过普通边再次到达 loop edge 的起点；普通边移除所有 loop edge 后必须构成 DAG。

### 2.5 fan-in 使用显式 join 语义

默认节点收到一个 activation 即执行一次。

需要汇合多个上游时，显式声明：

```yaml
join:
  mode: all
  from: [fetch_react, fetch_vue, fetch_svelte]
```

同一次 fan-out 产生的子 token 共享 `ActivationID`。`join: all` 按 `ActivationID` 收集每个指定来源的一份输入，全部到齐后只执行一次。

v1 仅支持 `join.mode: all`；普通节点等价于单输入触发。条件分支汇合应直接把各分支路由到普通节点，保证一次运行只选择其中一条路径。

### 2.6 一个 handoff 结束当前 agent turn

`workflow_handoff` 是 terminal tool：

- 每个 `NodeRun` 必须且只能成功 handoff 一次。
- outcome 必须在该节点 `outputs` 中声明。
- handoff 成功后立即结束当前 agent turn，不允许继续调用其他工具。
- agent 正常结束但没有 handoff，视为 `HANDOFF_MISSING`。
- 重复 handoff 视为 `HANDOFF_DUPLICATE`。

为避免在 agent stream 中硬编码工具名，给 tools 增加可选接口：

```go
type TurnTerminator interface {
    Tool
    TerminatesTurn(result string, err error) bool
}
```

agent tool loop 在 terminal tool 成功后完成当前 turn。其他现有 tool 行为不变。

---

## 3. YAML Schema

### 3.1 完整示例

```yaml
name: bug-fix-pipeline
description: Reproduce, diagnose, fix, test and review with bounded feedback loops
version: "1"

defaults:
  node_timeout: 20m
  workflow_timeout: 45m
  max_node_runs: 30
  max_output_bytes: 131072

agents:
  debugger:
    template: debugger
  fixer:
    template: backend-dev
    model: deepseek-v4-pro-max
  tester:
    template: tester
  reviewer:
    template: code-reviewer

entry: [reproduce]

nodes:
  - id: reproduce
    agent: debugger
    prompt: |
      尝试复现问题。
      成功时 handoff outcome=reproduced。
      无法复现时 handoff outcome=not_reproduced，并说明已尝试的步骤。
    outputs:
      reproduced:
        to: [diagnose]
      not_reproduced:
        to: [reproduce]
        loop: true
        max_traversals: 2
    on_error:
      strategy: fail

  - id: diagnose
    agent: debugger
    prompt: |
      根据上游复现结果分析根因。
      完成时 handoff outcome=diagnosed。
    outputs:
      diagnosed:
        to: [fix]

  - id: fix
    agent: fixer
    prompt: |
      根据诊断或测试反馈实施修复。
      完成时 handoff outcome=fixed。
      无法完成时 handoff outcome=fix_failed。
    outputs:
      fixed:
        to: [test]
      fix_failed:
        to: [fix]
        loop: true
        max_traversals: 2
    on_error:
      strategy: retry
      max_attempts: 2

  - id: test
    agent: tester
    prompt: |
      验证修复并运行相关测试。
      通过时 handoff outcome=passed。
      失败时 handoff outcome=failed，并给出失败详情。
    outputs:
      passed:
        to: [verify]
      failed:
        to: [fix]
        loop: true
        max_traversals: 3

  - id: verify
    agent: reviewer
    prompt: |
      审查修复的正确性、安全性和完整性。
      通过时 handoff outcome=approved。
      需要修改时 handoff outcome=needs_work。
    outputs:
      approved:
        to: []
      needs_work:
        to: [fix]
        loop: true
        max_traversals: 2
```

`to: []` 表示当前 activation 分支终止，并把该 handoff 记录为 terminal output。

### 3.2 字段定义

```yaml
# WorkflowDef
name: string
description: string
version: "1"
defaults: Defaults
agents: map<string, AgentRef>
entry: []string
nodes: []NodeDef

# Defaults
node_timeout: duration
workflow_timeout: duration
max_node_runs: int
max_output_bytes: int

# AgentRef
template: string
model: string

# NodeDef
id: string
agent: string
prompt: string
timeout: duration
join: JoinDef
outputs: map<string, OutputDef>
on_error: ErrorPolicy

# JoinDef
mode: all
from: []string

# OutputDef
to: []string
loop: bool
max_traversals: int

# ErrorPolicy
strategy: fail | retry
max_attempts: int
```

默认值：

```yaml
defaults:
  node_timeout: 20m
  workflow_timeout: 45m
  max_node_runs: 50
  max_output_bytes: 131072

on_error:
  strategy: fail
```

约束：

- `name`、agent key、node ID、outcome 使用 `^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`。
- `version` 只能是受支持的字符串版本。
- `entry` 非空，且引用存在节点。
- `agents` 非空；每个 node 的 `agent` 必须引用其中一个 key。
- 每个 node 至少声明一个 output。
- `to` 中所有节点必须存在。
- terminal output 使用空数组，不使用空字符串。
- `join.from` 必须与该节点的普通入边来源一致，且至少包含两个节点。
- 参与环的边必须显式 `loop: true` 并设置正数限制。
- 非 loop 边组成的图必须无环。
- `on_error.max_attempts` 表示包含首次执行在内的最大总尝试次数，使用 `retry` 时必须至少为 2。
- 总 node 数、edge 数、YAML 文件大小必须设置上限。

解析使用严格模式，拒绝未知字段、重复 key、非法 duration 和不支持的 version。

---

## 4. 执行语义

### 4.1 启动

```text
workflow_run(name, input, work_dir)
  -> Store.Load(name)
  -> Parse + Validate
  -> 校验 work_dir
  -> 创建 RunState
  -> 创建一个根 ActivationID
  -> 为所有 entry 节点创建共享该 ActivationID 的初始 NodeRun
```

`work_dir` 必须是已存在的绝对目录。所有节点共享同一个项目目录；v1 不允许节点自行覆盖目录。

### 4.2 NodeRun 输入

节点 prompt 由结构化片段构成：

```text
<workflow_context>
workflow: bug-fix-pipeline
run_id: ...
node: fix
attempt: 2
</workflow_context>

<workflow_input>
初始用户输入
</workflow_input>

<upstream_inputs>
- from: diagnose
  outcome: diagnosed
  content: ...
- from: test
  outcome: failed
  content: ...
</upstream_inputs>

<node_instruction>
节点 YAML 中的 prompt
</node_instruction>

必须调用 workflow_handoff 结束当前节点。
```

所有插入内容按数据处理，不允许上游 content 覆盖 system prompt。输入和输出都受大小限制，超限返回明确错误，不静默截断关键控制字段。

### 4.3 调度

Engine 维护：

```go
type RunState struct {
    ID             string
    Workflow       *ParsedWorkflow
    Status         RunStatus
    NodeRuns       map[string]*NodeRun
    ReadyQueue     []string
    Running        map[string]context.CancelFunc
    JoinBuckets    map[JoinKey]*JoinBucket
    LoopCounters   map[LoopKey]int
    TerminalOutput []TerminalOutput
    StartedAt      time.Time
    FinishedAt     time.Time
}
```

`RunState` 采用单写者模型：只有 Engine 的调度 goroutine 可以修改 ready queue、join bucket、loop counter 和 NodeRun 状态。并行 NodeExecutor 只通过结果 channel 回传不可变结果，禁止直接写共享 map。

调度循环：

```text
1. 从 ReadyQueue 取出可运行节点。
2. 在并发上限内执行 NodeRun。
3. 成功 handoff：
   a. 精确查找 output。
   b. terminal output -> 结束当前 activation 分支。
   c. 普通 edge -> 派生下游 activation。
   d. loop edge -> 递增计数，未超限则派生新 NodeRun。
4. 系统错误：
   a. on_error=retry 且未超限 -> 为同节点创建新 NodeRun。
   b. 否则 workflow fail-fast，取消所有运行节点。
5. 无 READY、无 RUNNING 时：
   a. 有未满足 join -> UNSATISFIED_JOIN。
   b. 至少一个 terminal output -> COMPLETED。
   c. 否则 -> NO_TERMINAL_PATH。
```

并行执行必须设置 `max_parallel_nodes` 内部上限，默认建议为 4。对共享工作区并行写入可能冲突，文档需提示 workflow 作者只对可安全并行的节点做 fan-out。

服务端硬上限建议固定为：

```go
type EngineLimits struct {
    MaxYAMLBytes       int64         // 1 MiB
    MaxNodes           int           // 64
    MaxEdges           int           // 256
    MaxParallelNodes   int           // 4
    MaxNodeRuns        int           // 100
    MaxWorkflowTimeout time.Duration // 60m
    MaxNodeTimeout     time.Duration // 30m
    MaxOutputBytes     int           // 256 KiB
}
```

YAML 只能在硬上限内收紧配置，不能扩大服务端限制。

### 4.4 终止规则

workflow 完成条件不是“任意节点走到 `to: []`”，而是：

- 所有已激活分支都已结束；
- 没有 READY/RUNNING NodeRun；
- 没有未满足的 join bucket；
- 至少有一个 terminal output。

多个并行 terminal output 全部进入最终结果。

### 4.5 错误与取消

错误码至少包括：

```text
WORKFLOW_NOT_FOUND
WORKFLOW_INVALID
WORK_DIR_INVALID
WORKFLOW_TIMEOUT
NODE_TIMEOUT
NODE_EXECUTION_FAILED
HANDOFF_MISSING
HANDOFF_DUPLICATE
HANDOFF_OUTCOME_UNKNOWN
LOOP_LIMIT_EXCEEDED
MAX_NODE_RUNS_EXCEEDED
OUTPUT_TOO_LARGE
UNSATISFIED_JOIN
NO_TERMINAL_PATH
CANCELLED
```

策略：

- schema/引用错误：运行前失败，不创建 agent。
- 单节点系统错误：按 `on_error` 执行。
- loop limit、全局运行次数、workflow timeout：workflow fail-fast。
- 用户 Stop、WebSocket 断开或父 context 取消：取消全部 NodeRun，停止并清理所有子 agent。
- 并行节点中任一不可恢复错误：取消同一 workflow 的其他节点。

---

## 5. Agent 执行与现有契约

### 5.1 每个 NodeRun 使用临时 agent

v1 每次 NodeRun 创建一个临时 agent，不跨尝试复用隐式对话历史。重试所需信息通过结构化 upstream inputs 显式传入。

优点：

- 循环语义清晰
- 不会把前一次失败的 tool call 历史带入下一次
- ContextWindow 生命周期简单
- 每次执行都使用最新热更新配置

### 5.2 扩展 DefaultFactory

保留现有 `Create`，新增兼容入口：

```go
type CreateOptions struct {
    ExtraSystemPrompt string
    ExtraTools        []tools.Tool
}

func (f *DefaultFactory) CreateWithOptions(
    ctx context.Context,
    tmpl AgentTemplate,
    workDir string,
    opts CreateOptions,
) (*Agent, *ctxwin.ContextWindow, error)
```

现有 `Create` 调用 `CreateWithOptions(..., CreateOptions{})`。

workflow agent 在创建前注入：

- workflow 专用 system prompt
- 当前 NodeRun 独占的 `workflow_handoff`

Engine 不保存 `llmClient` 或 `tools.Config` 快照；工厂在每次创建时读取热更新后的值。

### 5.3 template 与 model 解析

运行时通过并发安全的 `TemplateResolver` 获取最新模板：

```go
type TemplateResolver interface {
    ResolveTemplate(name string) (agent.AgentTemplate, error)
}
```

若 `AgentRef.model` 非空，只覆盖解析后模板的 `ModelID`，仍通过现有 ModelResolver 校验 provider、enabled 状态和上下文配置。

### 5.4 事件和确认转发

NodeExecutor 必须消费 child `AskStream` 的全部事件，并复用现有 delegation 契约：

- 将 `ToolNeedsConfirmEvent` 转发到父 L1 event channel。
- 将用户确认路由回正确 child agent。
- 并行 child 的确认 ID 必须使用 `runID/nodeRunID/callID` 命名空间，避免 call ID 冲突。
- 父 context 取消时停止事件转发并取消 child。

建议把 DelegateTool 当前的 child event 消费、确认代理和结果收集逻辑抽成公共 helper，由 DelegateTool 和 Workflow NodeExecutor 共同使用。

### 5.5 生命周期清理

每个临时 agent 必须执行：

```go
defer func() {
    _ = child.Stop(5 * time.Second)
    registry.Unregister(child.InstanceID)
}()
```

以下路径都必须覆盖清理：

- 成功
- handoff 缺失或非法
- node timeout
- workflow timeout
- 用户取消
- 并行 sibling 失败
- panic recovery

---

## 6. 包结构与依赖方向

不要把 workflow tool 直接放入 `internal/tools` 并让 core workflow 反向依赖它，否则容易形成：

```text
workflow -> agent -> tools -> workflow
```

按 workflow feature 聚合，在 `internal/workflow/` 下拆分子包：

```text
internal/workflow/
  schema.go
  validate.go
  graph.go
  engine.go
  state.go
  store.go

  agentexec/
    executor.go
    handoff_tool.go
    event_relay.go
    cleanup.go

  tool/
    run.go
    list.go
```

依赖方向：

```text
workflow/agentexec -> workflow
workflow/tool      -> workflow

workflow/agentexec -> agent -> tools
workflow/agentexec ---------> tools
workflow/tool      ---------> tools

runtime -> workflow + workflow/agentexec
session -> runtime + workflow/tool
```

其中：

- `workflow` 是不依赖 agent/runtime/session 的 schema、验证、调度和 Store 核心，通过 `NodeExecutor` 接口执行节点。
- `workflow/agentexec` 实现 `NodeExecutor`，负责创建 agent、注入 handoff、事件转发和清理。
- `workflow/tool` 实现 L1 的 `workflow_run`、`workflow_list`。
- 根 `workflow` 包不得反向导入 `workflow/agentexec` 或 `workflow/tool`。
- `workflow/agentexec` 不得依赖整个 `runtime.Stack`，所有 factory、template resolver、registry 和 logger 依赖都通过构造参数注入。
- Go 子包没有父包私有访问权限；子包只使用根 `workflow` 包有意暴露的最小 API。

包声明：

```go
package workflow  // internal/workflow
package agentexec // internal/workflow/agentexec
package tool      // internal/workflow/tool
```

调用方为避免与现有 `internal/tools` 混淆，应使用明确别名：

```go
workflowtool "github.com/xiaobaitu/soloqueue/internal/workflow/tool"
```

核心接口：

```go
type NodeExecutor interface {
    Execute(ctx context.Context, req NodeRunRequest) (NodeRunResult, error)
}

type Engine struct {
    executor NodeExecutor
    limits   EngineLimits
    log      *logger.Logger
}
```

---

## 7. Store 与安全边界

默认目录：

```text
~/.soloqueue/workflows/
```

Store 自己负责 `MkdirAll`，不修改 `config.DefaultWorkDir()` 去创建某个具体子目录。

API：

```go
type Store struct {
    Dir          string
    MaxFileBytes int64
}

func (s *Store) Load(name string) (*ParsedWorkflow, error)
func (s *Store) List() ([]WorkflowMeta, error)
```

安全要求：

- `Load` 只接受合法 workflow name，不接受路径。
- 使用 `<name>.yaml` 精确定位，拒绝 `..`、分隔符和绝对路径。
- 验证解析后的 `WorkflowDef.Name` 与请求名、文件名一致。
- 拒绝越过 Store.Dir 的 symlink。
- 文件大小、node 数、edge 数、prompt 长度均有限制。
- `List` 对单个坏文件返回带错误的 metadata，不应让全部列表失败。
- workflow YAML 是本机受信配置，但仍不能绕过现有 tool confirmation 和 model enablement。

---

## 8. L1 tools

### 8.1 workflow_run

参数：

```json
{
  "name": "bug-fix-pipeline",
  "input": "修复登录偶发 500",
  "work_dir": "/absolute/project/path"
}
```

行为：

- 同步执行直到完成、失败或取消。
- 实现 `PreferredTimeout()`，其值略大于允许的最大 `workflow_timeout`。
- 实际 workflow deadline 始终由 YAML/defaults 和服务端硬上限共同决定，不能仅信任 YAML。
- 返回有大小上限的结构化 JSON。

结果：

```json
{
  "run_id": "wf_...",
  "workflow": "bug-fix-pipeline",
  "status": "completed",
  "duration_ms": 12345,
  "terminal_outputs": [
    {
      "node": "verify",
      "outcome": "approved",
      "content": "..."
    }
  ],
  "node_runs": [
    {
      "node": "fix",
      "attempt": 1,
      "status": "succeeded",
      "outcome": "fixed"
    }
  ]
}
```

### 8.2 workflow_list

返回：

```json
[
  {
    "name": "bug-fix-pipeline",
    "description": "...",
    "version": "1",
    "valid": true
  }
]
```

v1 只把两个 tool 注入 L1，不注入 L2/L3：

- `workflow_list`
- `workflow_run`

---

## 9. Runtime 与 Prompt 集成

`runtime.Stack` 增加：

```go
WorkflowStore  *workflow.Store
WorkflowEngine *workflow.Engine
```

构建顺序：

```text
shared DB/config
-> LLM
-> prompt/memory/skills
-> agent infra
-> workflow store + template resolver + node executor + engine
-> assemble Stack
```

L1 session builder 在创建 session tools 时注入 `workflow/tool`。workflow tool 使用当前 session logger，并从执行 context 获取父事件 channel 和 confirmation forwarder。

L1 system prompt 增加简短能力说明：

```text
当用户明确指定 workflow，或已有 workflow 与任务高度匹配时：
1. 先调用 workflow_list 确认名称。
2. 调用 workflow_run，并传入用户任务和绝对 work_dir。
3. 不得自动创建、修改或猜测不存在的 workflow。
4. workflow_run 失败时向用户报告结构化错误，不要自动无限重试。
```

System prompt 仍遵守现有 invariant：不得写入 timeline。

---

## 10. 实现阶段

### Phase 1：Schema、严格解析与验证

- WorkflowDef、ParsedWorkflow、Node、Edge
- 严格 YAML parser
- 精确 outcome map
- entry、terminal、引用完整性
- SCC 环检测与 loop 标记验证
- join 验证
- 全局静态限制

### Phase 2：纯 Engine

使用 fake `NodeExecutor` 实现：

- activation token
- NodeRun 状态
- fan-out
- join bucket
- 有界 loop
- on_error retry
- fail-fast cancellation
- terminal aggregation

Phase 2 不依赖真实 agent。

### Phase 3：Store

- 安全名称解析
- 文件大小限制
- Load/List
- symlink 和文件名一致性检查

### Phase 4：Agent terminal tool 支持

- `TurnTerminator` 可选接口
- agent stream 成功执行 terminal tool 后结束 turn
- 既有 tools 回归测试

### Phase 5：Workflow Agent Executor

- `CreateWithOptions`
- template/model 动态解析
- workflow_handoff 注入
- child stream 消费
- confirmation/event relay
- timeout/cancel
- Stop/Unregister 清理

### Phase 6：L1 tools 与 Runtime

- workflow_run/list
- PreferredTimeout
- Stack 构建
- Session Builder 注入
- L1 prompt 能力说明

### Phase 7：端到端验证

- FakeLLM 完整 workflow
- L1 tool 调用
- 写文件确认路径
- Stop/断连取消
- registry 无泄漏
- race test

---

## 11. 测试矩阵

### Schema/Store

- 合法 YAML
- 未知字段、重复 key、非法 duration
- name/filename 不一致
- 路径穿越与 symlink
- 缺失 agent/node/output/entry
- 非法 template/model
- 普通边成环
- 环边未声明 loop
- loop 无限制或限制为 0
- loop 指向 join:all
- 无 terminal path

### Engine

- 单节点 terminal
- 两节点线性流程
- 精确 outcome 分支
- fan-out 并发
- fan-in all
- 多 terminal output 聚合
- 自循环
- 多节点循环
- loop limit exhausted
- on_error retry 成功与耗尽
- max_node_runs
- workflow timeout
- node timeout
- parent cancellation
- sibling fail-fast cancellation
- unsatisfied join

### Agent integration

- handoff 正常结束 turn
- handoff missing
- duplicate handoff
- unknown outcome
- output too large
- child confirmation 转发
- 并行 child confirmation ID 隔离
- model override 校验
- work_dir 正确传递
- system prompt 不进入 timeline
- 所有成功/失败/取消路径 registry 无泄漏

### 命令

```bash
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/workflow -count=1
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/workflow/agentexec -count=1
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/workflow/tool -count=1
GOCACHE=/tmp/soloqueue-go-cache go test ./internal/agent/... -count=1
GOCACHE=/tmp/soloqueue-go-cache go test -race ./internal/workflow ./internal/workflow/agentexec -count=1
```

---

## 12. v1 验收标准

只有同时满足以下条件才算完成：

1. 文档中的三个示例都能通过严格 schema 校验。
2. 精确 outcome 路由不存在字符串猜测。
3. 任意环都有显式正数上限，达到上限能确定失败。
4. fan-out/fan-in 不会因未选择分支永久卡住。
5. workflow、node、tool 三层 timeout 均明确且可取消。
6. 用户 Stop 或断连后，没有遗留运行 goroutine 和 registry agent。
7. 节点写操作的确认能到达 UI，并准确回到对应 child。
8. workflow 始终在传入的绝对 `work_dir` 中运行。
9. 热更新后的 template、model、tools 配置用于后续 NodeRun。
10. `go test -race` 覆盖并行状态更新且无数据竞争。
11. 非法 YAML、非法 outcome、无 handoff、循环耗尽都有稳定错误码。
12. 不引入 workflow 状态持久化、异步 API、UI 或自动创建等 v2 能力。

---

## 13. 典型场景

### 13.1 Fan-out / Fan-in

```yaml
name: frontend-research
description: Research three frameworks in parallel and synthesize
version: "1"

agents:
  researcher:
    template: researcher
  analyst:
    template: analyst

entry: [dispatch]

nodes:
  - id: dispatch
    agent: researcher
    prompt: 创建研究任务，handoff outcome=ready。
    outputs:
      ready:
        to: [fetch_react, fetch_vue, fetch_svelte]

  - id: fetch_react
    agent: researcher
    prompt: 调研 React，handoff outcome=data。
    outputs:
      data:
        to: [synthesize]

  - id: fetch_vue
    agent: researcher
    prompt: 调研 Vue，handoff outcome=data。
    outputs:
      data:
        to: [synthesize]

  - id: fetch_svelte
    agent: researcher
    prompt: 调研 Svelte，handoff outcome=data。
    outputs:
      data:
        to: [synthesize]

  - id: synthesize
    agent: analyst
    join:
      mode: all
      from: [fetch_react, fetch_vue, fetch_svelte]
    prompt: 汇总三份输入，handoff outcome=report。
    outputs:
      report:
        to: []
```

### 13.2 条件分支

```yaml
name: reproduce-check
description: Route based on exact reproduction result
version: "1"

agents:
  debugger:
    template: debugger
  analyst:
    template: analyst

entry: [reproduce]

nodes:
  - id: reproduce
    agent: debugger
    prompt: 复现问题，handoff outcome=reproduced 或 not_reproduced。
    outputs:
      reproduced:
        to: [diagnose]
      not_reproduced:
        to: [report]

  - id: diagnose
    agent: analyst
    prompt: 分析根因，handoff outcome=done。
    outputs:
      done:
        to: []

  - id: report
    agent: analyst
    prompt: 汇总无法复现的证据，handoff outcome=done。
    outputs:
      done:
        to: []
```

### 13.3 有界反馈循环

```yaml
name: write-review
description: Write and review with bounded revision
version: "1"

agents:
  writer:
    template: writer
  reviewer:
    template: reviewer

entry: [write]

nodes:
  - id: write
    agent: writer
    prompt: 根据初始要求或审查反馈写作，handoff outcome=draft。
    outputs:
      draft:
        to: [review]

  - id: review
    agent: reviewer
    prompt: 审查内容，handoff outcome=approved 或 needs_revision。
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
        max_traversals: 2
```
