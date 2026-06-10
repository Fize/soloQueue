# Skills 系统技术方案

## 1. 概述

Skills 是 soloQueue 中受 Claude Code 启发的技能系统，允许用户通过编写 `SKILL.md` 文件来扩展 AI Agent 的行为。每个 Skill 是一个 Markdown 文件（带 YAML frontmatter），定义了一段可被 Agent 通过工具调用或斜杠命令触发的指令集。

系统支持三种加载来源（优先级从低到高）：
- **Builtin**：Go 代码内注册的内置技能（当前为 stub）
- **Plugin**：插件提供的技能（`~/.codebuddy/plugins/.../skills/`）
- **User**：用户安装的技能（`~/.soloqueue/skills/`）
- **Project**：项目级技能（`.codebuddy/skills/`）

后加载的同名 Skill 会覆盖先加载的。

---

## 2. 数据模型

### 2.1 SKILL.md 文件格式

```markdown
---
name: fullstack-dev
description: Fullstack development scenario-flow methodology
triggers:
  - 加功能
  - add feature
allowed-tools: Bash(npm:*), Edit(src/**/*.ts)
disable_model_invocation: false
user_invocable: true
context: fork
agent: Explore
upstream: https://github.com/xxx/skills.git
branch: main
subpath: skills/fullstack-dev
required_env:
  - ANTHROPIC_API_KEY
---

# Fullstack Development Skill
## Instructions
...
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 技能名称，缺省时用父目录名 |
| `description` | string | 给 LLM 的描述，缺省时取正文第一段 |
| `when_to_use` | string | 附加触发条件说明 |
| `allowed-tools` | string | 逗号分隔的工具白名单（`Bash(git:*)`、`mcp__server__tool`、glob 匹配） |
| `disable_model_invocation` | bool | `true` 时 Skill 工具描述中隐藏，只能通过 `/slash` 调用 |
| `user_invocable` | bool | `false` 时隐藏于 UI `/` 菜单，仅供内部/AI 使用 |
| `context` | string | `""`=内联执行，`"fork"`=派生子 Agent 执行 |
| `agent` | string | `context=fork` 时指定子 Agent 类型（`Explore`/`Plan` 等） |
| `triggers` | []string | 触发词列表，用于 UI 展示和匹配 |
| `upstream` | string | 远程 Git 仓库 URL（技能商店来源） |
| `branch` / `subpath` | string | 远程仓库的分支和子路径 |
| `required_env` | []string | 执行前检查的必需环境变量 |

### 2.2 内存数据结构

```go
// internal/skill/skill.go
type Skill struct {
    ID                     string
    Name                   string
    Description            string
    WhenToUse              string
    Instructions           string   // SKILL.md 正文（预处理前）
    AllowedTools           []string
    DisableModelInvocation bool
    UserInvocable          bool
    Context                string   // "" 或 "fork"
    Agent                  string
    Category               SkillCategory // builtin / user
    FilePath               string   // SKILL.md 绝对路径
    Dir                    string   // SKILL.md 所在目录（用于解析 @file 引用）
    Triggers               []string
    Disabled               bool     // .disabled 文件是否存在
    Upstream               string
    Branch                 string
    SubPath                string
    RequiredEnv            []string
}

type SkillRegistry struct {
    mu     sync.RWMutex
    skills map[string]*Skill
}
```

`SkillRegistry` 通过 `sync.RWMutex` 保证并发安全，所有读操作使用 `RLock`，写操作（`Register`/`Rebuild`）使用 `Lock`。

---

## 3. 加载机制

### 3.1 启动加载

入口：`internal/runtime/build.go` → `Build()` → `buildSkills()`

```
Build()
  └─ buildSkills(bc *buildContext)
       ├─ skill.SetPackageLogger(bc.log)
       ├─ skill.NewSkillRegistry() → 创建空注册表
       ├─ skill.RegisterBuiltinSkills(skillReg) → 注册内置技能（当前为空）
       ├─ skill.LoadSkillsFromDirs(skillDirs) → 从磁盘加载用户技能
       │    └─ 按 "plugin" → "user" → "project" 顺序加载，后者覆盖前者
       └─ bc.skillReg = skillReg → 存入 buildContext
            └─ Stack.SkillRegistry → 注入全局依赖容器
```

`LoadSkillsFromDir()` 的实现（`internal/skill/skill_md.go`）：
1. 检查目录是否存在，不存在则返回空 map（不报错）
2. 遍历目录下所有子目录，每个子目录视为一个 Skill
3. 读取 `{dir}/SKILL.md`，调用 `ParseSkillMD(path)` 解析
4. 检查 `{dir}/.disabled` 文件是否存在，设置 `Disabled` 字段
5. 单个 Skill 解析失败只记录 warning，不中断整体加载

### 3.2 热重载（Hot Reload）

入口：`internal/runtime/build.go` → `registerSkillHotReload()`

使用 `fsnotify` 监听 `~/.soloqueue/skills/` 目录，监听事件：`Write`、`Create`、`Rename`、`Remove`。

```
fsnotify 事件
  ├─ 去抖（debounce）：200ms 定时器，合并短时间内的多次事件
  └─ 触发后调用 reg.Rebuild(dirs)
       └─ LoadSkillsFromDirs(dirs) → 全量重新从磁盘加载
            └─ 替换 registry.skills map（写锁保护）
```

**注意**：热重载后，已运行的 Agent 持有的是旧版 `SkillRegistry` 的引用（Agent 创建时合并一次），新创建的 Agent 才会看到更新后的技能列表。

### 3.3 解析流程

`ParseSkillMD(path) (*Skill, error)` 解析步骤：

```
读取文件内容
  ├─ 查找开头的 "---"
  │   ├─ 无 "---" → 整个文件作为 Instructions，其余字段用默认值
  │   ├─ 有 "---" 但无结尾 "---" → 整个文件作为 Instructions
  │   └─ 有配对 "---" → 解析 YAML frontmatter + Markdown body
  ├─ yaml.Unmarshal → SkillMDConfig
  ├─ 默认值填充：
  │   ├─ Name: frontmatter.name → 父目录名
  │   ├─ Description: frontmatter.description → body 第一段
  │   ├─ UserInvocable: frontmatter 未设置 → true
  │   └─ Disabled: {dir}/.disabled 存在 → true
  └─ ParseAllowedTools(raw string) → 按逗号分割并 trim
```

---

## 4. 技能执行流程

### 4.1 Skill 作为 Tool 暴露给 LLM

`SkillTool`（`internal/skill/skill_tool.go`）实现了 `tools.Tool` 接口：

```go
type SkillTool struct {
    reg       *SkillRegistry
    forkSpawn SkillForkSpawnFn  // 由 agent factory 注入
}
```

- `Name()` 返回 `"Skill"`
- `Description()` **动态生成**：每次调用时遍历注册表，生成所有 `DisableModelInvocation == false` 的技能列表描述，附在工具描述末尾。这样 LLM 总能看到最新的可用技能列表。
- `Parameters()` 返回 JSON Schema：`{ skill: string, args: string }`

### 4.2 LLM 调用 Skill 的完整链路

```
LLM 输出 tool_call: Skill(skill="commit", args="-m 'fix bug'")
  ↓
Agent 执行 SkillTool.Execute(ctx, rawArgs)
  ↓
1. JSON 解析 rawArgs → skillName, args
2. registry.Lookup(skillName) → *Skill (未找到返回错误字符串给 LLM)
3. PreprocessContent(skill.Instructions, args, skill.Dir)
   ├─ 替换 $ARGUMENTS → args
   ├─ 执行 !`cmd` → shell 命令，stdout 内联
   └─ 解析 @filepath → 读取文件内容内联
4. 判断 skill.Context：
   ├─ "fork" → ExecuteFork()
   │    ├─ 创建子 Agent（system prompt = 预处理后的内容）
   │    ├─ 调用 child.AskStream(ctx, args)
   │    ├─ 流式累积响应
   │    └─ 返回子 Agent 的最终响应
   └─ "" (inline) → 返回预处理后的内容作为 tool_result
        └─ LLM 读取指令内容，继续执行后续 tool_calls
```

### 4.3 预处理管道（Preprocessing Pipeline）

`PreprocessContent()`（`internal/skill/preprocess.go`）按序执行三步：

| 步骤 | 语法 | 实现 |
|------|------|------|
| 参数替换 | `$ARGUMENTS` | `strings.ReplaceAll` 全局替换 |
| Shell 执行 | `` !`command` `` | 正则匹配 → `exec.CommandContext` 执行（10 分钟超时）→ stdout 替换原位置 |
| 文件引用 | `@filepath` | 正则匹配 → `os.ReadFile`（路径相对于 `skill.Dir`）→ 文件内容替换原位置 |

错误处理：Shell 执行失败 → 替换为空字符串；文件读取失败 → 替换为 `<file error: ...>`。

### 4.4 Fork 模式

`ExecuteFork()`（`internal/skill/fork.go`）：

```
SkillForkSpawnFn(ctx, skill, content, args) (Locatable, cleanup, error)
  ├─ 创建临时 Agent Definition（SystemPrompt = content）
  ├─ FilterTools(tools, skill.AllowedTools) → 按白名单过滤工具
  ├─ agent.NewAgent(def, tools...) → 创建子 Agent
  ├─ child.AskStream(ctx, args) → 发送用户参数，流式获取响应
  └─ 累积所有 response chunk → 返回完整结果
```

`ToolMatchesAllowed()` 支持三种匹配模式：
- 精确匹配：`"Bash"` 匹配 `"Bash"`
- MCP 前缀匹配：`"mcp__server"` 匹配 `"mcp__server__tool"`
- 括号模式：`"Bash(git:*)"` 匹配 `"Bash"`，且参数需符合 `git:*` 前缀

---

## 5. Agent 集成

### 5.1 Agent 创建时注入 Skills

`internal/agent/factory.go` → `DefaultFactory.Create()`：

```
Create(definition, opts):
  ├─ 合并全局 SkillRegistry + 项目级技能（.codebuddy/skills/）
  │   → mergedReg
  ├─ 如果 AgentTemplate.SkillIDs 非空：
  │   ├─ 从 mergedReg 中筛选指定的 Skills → sr (新 SkillRegistry)
  │   ├─ 创建 forkSpawn 闭包（注入 factory 的 buildAgentFunc）
  │   ├─ skill.NewSkillTool(sr, forkSpawn) → 创建 SkillTool 实例
  │   └─ 将 SkillTool 加入 allTools
  ├─ agent.NewAgent(...WithSkills(skills...)) → Agent 持有自己的 SkillRegistry
  └─ Agent 的 system prompt 中包含可用 Skill 列表（通过 SkillTool.Description()）
```

### 5.2 WithSkills Option

`internal/agent/agent.go`：

```go
func WithSkills(skills ...*skill.Skill) Option {
    return func(a *Agent) {
        if a.skills == nil {
            a.skills = skill.NewSkillRegistry()
        }
        for _, s := range skills {
            a.skills.Register(s)  // 重复注册 panic
        }
    }
}
```

Agent 持有独立的 `skills` 字段（`*SkillRegistry`），与 `tools`（`*tools.ToolRegistry`）分离。这使得可以在 Agent 级别精细控制哪些 Skill 可用。

---

## 6. HTTP API 设计

`internal/server/server.go` 注册路由，`internal/server/tool_skill_handlers.go` 实现 Handler。

| 方法 | 路径 | Handler | 说明 |
|------|------|---------|------|
| `GET` | `/api/skills` | `handleListSkills` | 列出所有已注册技能（含 builtin + user） |
| `POST` | `/api/skills` | `handleImportSkill` | 用户创建新技能（写入 `~/.soloqueue/skills/`） |
| `GET` | `/api/skills/store` | `handleListStoreSkills` | 列出技能商店目录（bundled + remote） |
| `POST` | `/api/skills/install` | `handleInstallSkill` | 从商店/GitHub/本地路径安装技能 |
| `GET` | `/api/skills/{id}` | `handleGetSkillDetail` | 获取单个技能详情（含 body） |
| `PUT` | `/api/skills/{id}` | `handleUpdateSkill` | 更新用户技能 |
| `DELETE` | `/api/skills/{id}` | `handleDeleteSkill` | 卸载用户技能 |
| `GET` | `/api/skills/{id}/files` | `handleGetSkillFiles` | 递归列出技能目录文件树 |
| `POST` | `/api/skills/{id}/toggle` | `handleToggleSkill` | 启用/禁用（创建/删除 `.disabled` 文件） |

写入操作（install/update/delete/toggle）后，Handler 调用 `m.skillReg.Rebuild(m.skillDirs)` 刷新内存中的注册表。

---

## 7. 前端管理界面

### 7.1 技术栈

- 路由：`react-router`，路径 `/settings/skills`
- 状态管理：Zustand（`toolsAndSkillsStore.ts`）
- UI 组件：`SkillsTab.tsx`（约 1195 行），含搜索、过滤、编辑、文件树预览
- 类型定义：`types/index.ts` → `SkillInfo`、`SkillListResponse`、`InstallSkillRequest`

### 7.2 功能

**Installed Skills 标签页**：
- 搜索（按 id/name/description/triggers）
- 分类过滤（All / Built-in / User Created）
- 技能卡片可展开：左侧文件树，右侧 Markdown 预览/编辑
- Enabled/Disabled 开关（调用 `/api/skills/{id}/toggle`）
- 创建、编辑、卸载技能

**Skill Store 标签页**：
- 从 `/api/skills/store` 获取可用技能目录
- 支持从 Git URL 或本地路径安装

### 7.3 当前限制

`ChatInput.tsx` 是纯文本框，**没有 slash command 自动补全**。用户需手动输入 `/skillname` 触发技能。triggers 字段仅在管理页面展示，未集成到 Chat 输入补全。

---

## 8. 关键设计决策

### 8.1 为什么 Skill 指令内容不作为 Tool Description 的一部分直接注入？

Skill 的 Instructions 可能很长（数百行）。如果直接放在 Tool Description 里，会浪费大量 context window。当前设计是：Tool Description 只放技能列表（名称 + 简短描述），LLM 通过调用 `Skill(skill="xxx")` 获取完整指令。这相当于一个**延迟加载**机制。

### 8.2 为什么用 fork 模式执行复杂 Skill？

某些 Skill（如 `fullstack-dev`）需要多轮工具调用和深度推理。Inline 模式下，Skill 的指令作为 tool_result 返回，LLM 需要在同一轮对话中继续处理，容易受到 context 窗口限制。Fork 模式派生子 Agent 专门处理，主 Agent 只拿到最终结果。

### 8.3 为什么 AllowedTools 要做白名单过滤？

防止 Skill 被滥用。例如一个只读分析 Skill 不应该有 `Edit` 或 `Bash(rm:*)` 权限。`allowed-tools` 字段让 Skill 作者可以声明最小权限。

### 8.4 `.disabled` 文件而非数据库字段？

技能启用状态存储在文件系统（`{skill_dir}/.disabled`），而非数据库。这样：
1. 不需要额外的数据库迁移
2. 用户可以直接 `touch .disabled` 手动禁用
3. 与文件系统监听（fsnotify）天然集成，修改即时生效

---

## 9. 错误处理

| 场景 | 处理方式 |
|------|---------|
| SKILL.md 解析失败 | 记录 warning，跳过该 Skill，继续加载其他 |
| 技能目录不存在 | `LoadSkillsFromDir` 返回空 map，不报错 |
| 注册重复 Skill ID | 返回 `ErrSkillAlreadyRegistered` |
| LLM 调用不存在的 Skill | 返回错误字符串（给 LLM，不 panic） |
| Fork 执行失败 | 返回错误字符串 |
| 预处理中 Shell 执行失败 | 替换为空字符串，不中断 |
| 预处理中文件读取失败 | 替换为 `<file error: ...>` |
| `forkSpawn == nil` | 降级为 inline 模式 |

---

## 10. 目录结构总览

```
skills/                          # bundled 技能（随仓库分发）
  agent-browser/SKILL.md
  fullstack-dev/SKILL.md
  ...

~/.soloqueue/skills/            # 用户安装技能
  my-custom-skill/SKILL.md
  my-custom-skill/.disabled     # 存在即禁用

internal/skill/
  skill.go                      # Skill 结构体、SkillRegistry
  skill_md.go                   # SKILL.md 解析
  skill_tool.go                 # SkillTool（实现 tools.Tool 接口）
  fork.go                       # Fork 模式执行、AllowedTools 过滤
  preprocess.go                 # $ARGUMENTS、!`cmd`、@file 预处理
  allowed_tools.go               # ParseAllowedTools
  management.go                 # Install/Uninstall/Update/Import
  builtin.go                    # RegisterBuiltinSkills（stub）

internal/server/
  tool_skill_handlers.go        # HTTP handlers
  server.go                     # 路由注册

web/src/
  components/settings/SkillsTab.tsx  # 前端管理界面
  stores/toolsAndSkillsStore.ts       # Zustand store
  lib/api.ts                         # API 函数
  types/index.ts                     # TypeScript 类型
```
