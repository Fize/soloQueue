# Skill System Architecture

## 概览

Skill 系统位于 raw tool 之上，提供“可复用任务配方”这一层抽象。它并不直接增加新的底层能力，而是把已有能力组织成可发现、可复用、可隔离执行的技能定义。

关键特性：

- `Skill` 是不可变的数据定义
- `SkillTool` 把全部 skill 暴露成一个统一的 function-calling 入口
- 支持 `inline` 与 `fork` 两种执行模式
- 支持 `$ARGUMENTS`、``!`command` ``、`@file` 预处理
- 支持 `allowed-tools` 对 fork 子 agent 的工具集做裁剪

## 代码设计

这个包是数据驱动设计。`Skill` 只负责表达 skill 的静态形态；真正的执行行为由 `SkillTool`、预处理管道和 `ExecuteFork(...)` 实现。

它没有把每个 skill 都注册成一个独立 tool，而是统一通过一个名为 `Skill` 的 dispatch tool 暴露给 LLM。这样即便 skill 数量变化，provider 侧看到的 tool surface 仍然稳定。

Fork 模式通过注入 `SkillForkSpawnFn` 与 `iface.Locatable` 工作，不直接依赖具体 `agent` 构造逻辑，从而保持 skill 层与 agent 层解耦。

## 核心类型

### `Skill`

定义在 [internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go#L36)，关键字段包括：

- `ID`
- `Description`
- `Instructions`
- `AllowedTools`
- `DisableModelInvocation`
- `UserInvocable`
- `Context`
- `Agent`
- `Category`、`FilePath`、`Dir`

### `SkillRegistry`

[internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go#L143) 中的 `SkillRegistry` 是并发安全的 `id -> *Skill` 注册表，主要负责：

- 注册和去重
- 通过 ID 查询
- 为 `SkillTool.Description()` 提供有序快照

## SkillTool：从 Skill 到 Tool 的适配层

[internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go#L12) 中的 `SkillTool` 是技能系统最核心的执行适配器。

它对 LLM 暴露一个统一 schema：

- `skill`
- `args`

执行步骤：

1. 解析参数
2. 从 `SkillRegistry` 找到对应 skill
3. 执行预处理
4. 根据 `skill.Context` 选择 `inline` 或 `fork`

`Description()` 会动态拼接所有可被模型调用的 skill 列表，因此 skill 的 discoverability 依赖 registry 的快照结果。

## 执行模式

### Inline

Inline 是默认模式。在 [internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go#L99) 中，这个分支会直接返回预处理后的 skill 内容作为 tool result。

然后由当前 agent 在后续 LLM 轮次继续消费这段内容并调用普通工具。

### Fork

Fork 模式通过 [internal/skill/fork.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/fork.go#L25) 中的 `ExecuteFork(...)` 实现。

执行流程：

1. 调用 `spawnFn` 创建临时子 agent
2. 用 skill 内容作为子 agent 的 system prompt
3. 把 `args` 或默认 prompt 发给子 agent
4. 消费子 agent 的流式输出并累积结果
5. 执行 cleanup

这种模式适合需要隔离上下文或收窄工具权限的 skill。

## 预处理管道

预处理逻辑在 [internal/skill/preprocess.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/preprocess.go)。

`PreprocessContent(...)` 的顺序是：

1. `$ARGUMENTS` 替换
2. ``!`command` `` shell 执行
3. `@filepath` 文件内容展开

### Shell Expansion

`expandShellCommands(...)` 会在带超时的 context 中运行 shell 命令。失败时不会整个 skill 报错，而是返回空字符串替换。

### File Expansion

`expandFileRefs(...)` 会相对 `skill.Dir` 解析路径，并把文件内容内联到 skill 文本中。读取失败时会输出占位错误文本。

## SKILL.md 加载

外部 skill 通过 [internal/skill/skill_md.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_md.go) 加载。

支持的 frontmatter 字段包括：

- `name`
- `description`
- `allowed-tools`
- `disable-model-invocation`
- `user-invocable`
- `context`
- `agent`

加载入口有两类：

- `LoadSkillsFromDir(...)`：加载单个目录下的一层 `SKILL.md`
- `LoadSkillsFromDirs(...)`：按 `plugin -> user -> project` 的优先级合并多个目录

这说明 skill 系统本身支持分层来源与覆盖，而不需要调用方自己做 merge。

## Allowed Tools 过滤

[internal/skill/fork.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/fork.go#L63) 中的 `FilterTools(...)` 用于根据 `allowed-tools` 白名单筛选 fork 子 agent 的工具。

支持的模式主要是：

- 精确 tool 名
- 带括号的约束表达式，如 `Bash(git:*)`
- MCP server 前缀，如 `mcp__server`

当前过滤粒度主要还是基于 tool 名，不是完整的策略引擎。

## 与运行时的集成

### Factory 集成

[internal/agent/factory.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/factory.go) 会：

- 从 skill 目录加载 skills
- 注册到 `SkillRegistry`
- 在有 skill 时追加 `SkillTool`
- 为 fork 模式注入 `forkSpawn` 闭包

### Session Agent 集成

[cmd/soloqueue/main.go](/Users/xiaobaitu/github.com/soloQueue/cmd/soloqueue/main.go#L426) 中的 session builder 也会给会话级 agent 注册 `SkillTool`，因此用户直接对话的 agent 同样具备 skill 能力。

## 与 Tool 的关系

可以把 skill 理解为“建立在 tool 之上的任务配方层”：

- tool 是最小能力原语
- skill 是能力组合、提示模板和执行模式封装

因此 skill 依赖 tool，但 tool 不依赖 skill。

## 关键文件

- [internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go)
- [internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go)
- [internal/skill/fork.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/fork.go)
- [internal/skill/skill_md.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_md.go)
- [internal/skill/preprocess.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/preprocess.go)
