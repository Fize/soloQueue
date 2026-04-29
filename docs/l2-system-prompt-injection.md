# L2 Supervisor 三段式 System Prompt 动态注入方案

## 目标

在 `DefaultFactory.Create` 中为 L2 Supervisor 实现三段式 Prompt 拼接：
- **Segment 1 (用户定义区)**: 用户的业务 Role + System Prompt
- **Segment 2 (动态能力区)**: Sub-Agents 目录 + MCP Servers + Skills
- **Segment 3 (框架强制区)**: 不可篡改的底层契约

利用"近因效应"：Segment 3 在最末，优先级最高，防止用户越权。

## 需要修改的文件

| 文件 | 改动 |
|------|------|
| `internal/prompt/parser.go` | `AgentFrontmatter` 增加 `MCPServers` 字段 |
| `internal/agent/factory.go` | `AgentTemplate` 增加 `MCPServers`；`DefaultFactory` 增加 `templates` 索引；新增 `buildL2SystemPrompt()`；改造 `Create` 的 ContextWindow 初始化 |
| `cmd/soloqueue/main.go` | 将 `allTemplates` 传入 `NewDefaultFactory`；`LoadAgentTemplates` 映射新字段 |

## 详细改动

### 1. `internal/agent/factory.go` — 核心改动

#### 1.1 `AgentTemplate` 增加 `MCPServers`

```go
type AgentTemplate struct {
    // ... 现有字段 ...
    MCPServers  []string // MCP Server 名称列表
}
```

#### 1.2 `DefaultFactory` 增加 `templates` 索引

```go
type DefaultFactory struct {
    registry  *Registry
    llm       LLMClient
    toolsCfg  tools.Config
    skillDir  string
    log       *logger.Logger
    templates map[string]AgentTemplate // 新增：按 ID 索引的全量模板，供 buildL2SystemPrompt 查找子 agent 描述
}
```

`NewDefaultFactory` 增加可选参数 `allTemplates []AgentTemplate`，内部转为 map。
选择**函数式选项**而非修改签名（不破坏现有调用方）：

```go
type FactoryOption func(*DefaultFactory)

func WithTemplates(templates []AgentTemplate) FactoryOption {
    return func(f *DefaultFactory) {
        f.templates = make(map[string]AgentTemplate, len(templates))
        for _, t := range templates {
            f.templates[t.ID] = t
        }
    }
}
```

#### 1.3 新增 `buildL2SystemPrompt()`

独立函数，接收 `tmpl AgentTemplate` + `templates map[string]AgentTemplate`，返回拼接后的字符串。

```go
// l2EnforcedDirectives 是 Segment 3 框架强制区常量
const l2EnforcedDirectives = `
========================================
SYSTEM ENFORCED EXECUTION RULES
========================================
You are operating as a Layer 2 Supervisor. The following rules are ABSOLUTE and override any previous instructions.

# 1. Sandbox Awareness
You must delegate tasks to Layer 3 Workers. These Workers run in isolated, ephemeral sandboxes. They have NO memory, NO context of the overall project, and NO shared state. You MUST pass all necessary context, absolute file paths, and dependencies in your task description.

# 2. Atomic Delegation
Tasks MUST be deterministic and executable.
BAD: "Fix the bug in the backend."
GOOD: "Read /workspace/main.go, find the panic on line 42, fix it, and return the diff."

# 3. Autonomous Retry
If a Worker returns an error, DO NOT immediately report back to the orchestrator. You must analyze the error, adjust your delegation prompt, and retry.
`

func buildL2SystemPrompt(tmpl AgentTemplate, templates map[string]AgentTemplate) string {
    var b strings.Builder

    // ── Segment 1: 用户定义区 ──────────────────────────────
    // tmpl.SystemPrompt 是 markdown body，已包含用户自定义的完整 role 定义
    // tmpl.Description 仅在 SystemPrompt 为空时作为兜底
    if tmpl.SystemPrompt != "" {
        b.WriteString(tmpl.SystemPrompt)
        b.WriteString("\n\n")
    } else if tmpl.Description != "" {
        b.WriteString("# Role\n")
        b.WriteString(tmpl.Description)
        b.WriteString("\n\n")
    }

    // ── Segment 2: 动态能力区 ──────────────────────────────
    // 2a. Sub-Agents 目录
    if len(tmpl.SubAgents) > 0 {
        b.WriteString("# Available Sub-Agents\n\n")
        b.WriteString("You can delegate tasks to the following workers:\n\n")
        for _, subName := range tmpl.SubAgents {
            desc := "no description"
            if sub, ok := templates[subName]; ok && sub.Description != "" {
                desc = sub.Description
            }
            fmt.Fprintf(&b, "- **%s**: %s\n", subName, desc)
        }
        b.WriteString("\n")
    }

    // 2b. MCP Servers
    if len(tmpl.MCPServers) > 0 {
        b.WriteString("# Available MCP Servers\n\n")
        for _, name := range tmpl.MCPServers {
            fmt.Fprintf(&b, "- %s\n", name)
        }
        b.WriteString("\n")
    }

    // ── Segment 3: 框架强制区 ──────────────────────────────
    b.WriteString(l2EnforcedDirectives)

    return b.String()
}
```

**关于 Description vs SystemPrompt**：查看实际 agent md 文件（如 `dev.md`），body 和 description 经常重复。所以 Segment 1 优先用 `SystemPrompt`（body），`Description` 仅作兜底，避免重复。

**关于 Skills**：Skills 已通过 `SkillCatalog()` 作为**第二条** system message 独立注入（`factory.go:153`），不需要在 Prompt 正文中重复。保持现有的两步注入机制。

#### 1.4 改造 `Create` 方法

在 step 1（构建 Definition）和 step 7（ContextWindow 初始化）之间插入三段式拼接逻辑：

```go
func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error) {
    // 1. 构建最终 SystemPrompt
    var finalPrompt string
    if tmpl.IsLeader {
        finalPrompt = buildL2SystemPrompt(tmpl, f.templates)
    } else {
        // L3: 直接使用用户的 SystemPrompt（或 Description 兜底）
        if tmpl.SystemPrompt != "" {
            finalPrompt = tmpl.SystemPrompt
        } else if tmpl.Description != "" {
            finalPrompt = tmpl.Description
        }
    }

    // 2. 构建 Definition（使用 finalPrompt）
    def := Definition{
        ID:           tmpl.ID,
        Name:         tmpl.Name,
        Role:         RoleUser,
        Kind:         KindCustom,
        ModelID:      tmpl.ModelID,
        SystemPrompt: finalPrompt,
    }

    // 3-6. 不变（tools、skills、opts、agent 创建）

    // 7. 创建 ContextWindow
    cw := ctxwin.NewContextWindow(DefaultContextWindow, 2000, ctxwin.NewTokenizer())
    if def.SystemPrompt != "" {
        cw.Push(ctxwin.RoleSystem, def.SystemPrompt)
    }
    if catalog := a.SkillCatalog(); catalog != "" {
        cw.Push(ctxwin.RoleSystem, catalog)
    }

    // 8-9. 不变
}
```

### 2. `internal/prompt/parser.go`

`AgentFrontmatter` 增加 `MCPServers []string \`yaml:"mcp_servers"\``。

### 3. `cmd/soloqueue/main.go` — 传入模板索引

```go
// buildRuntimeStack 中，创建 factory 时传入 allTemplates
agentFactory := agent.NewDefaultFactory(
    agentRegistry, llmClient, toolsCfg,
    filepath.Join(workDir, "skills"), log,
    agent.WithTemplates(allTemplates),  // 新增
)
```

同时 `LoadAgentTemplates` 中映射 `MCPServers`：

```go
tmpl := AgentTemplate{
    // ... 现有字段 ...
    MCPServers: fm.MCPServers,
}
```

### 4. `DelegateTool.Desc` 优化

当前 `DelegateTool.Desc` 在 `factory.go:112` 使用硬编码的 `fmt.Sprintf("sub-agent '%s'", subName)`。
在 `buildL2SystemPrompt` 已经将子 agent 描述注入 system prompt 后，`DelegateTool.Desc` 也可以改为从 `templates` 索引获取真实描述：

```go
for _, subName := range tmpl.SubAgents {
    desc := fmt.Sprintf("sub-agent '%s'", subName)
    if sub, ok := f.templates[subName]; ok && sub.Description != "" {
        desc = sub.Description
    }
    dt := &tools.DelegateTool{
        LeaderID: subName,
        Desc:     desc,
        // ...
    }
}
```

## 改动总结

```
internal/prompt/parser.go    --- AgentFrontmatter +MCPServers
internal/agent/factory.go    --- AgentTemplate +MCPServers; DefaultFactory +templates; +FactoryOption; +buildL2SystemPrompt; +l2EnforcedDirectives; Create 改造
cmd/soloqueue/main.go        --- LoadAgentTemplates 映射 MCPServers; NewDefaultFactory 传入 WithTemplates
```

## 不改动的部分

- **Skill 注入**: 保持现有 `SkillCatalog()` 作为第二条 system message 的机制，不在 Prompt 正文中重复
- **L1 System Prompt**: 不涉及，L1 的 prompt 组装由 `prompt` 包独立处理
- **L3 Worker**: 只做 `SystemPrompt` / `Description` 兜底，不注入框架强制契约
- **`Definition` 结构体**: 不变，`SystemPrompt` 字段仍存储最终拼好的字符串
