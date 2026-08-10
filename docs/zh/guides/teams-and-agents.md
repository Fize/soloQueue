# 团队与 Agent

[English: Teams and agents](../../guides/teams-and-agents.md)

我把 Team 和 Agent 模板作为 SoloQueue 的可复用执行层。Team 按角色或项目
组织 Agent；Agent 模板定义会话创建 Agent 时使用的身份、模型、工具和指令。

## 在界面中管理

我打开 Settings → Agents 检查或编辑 Agent 定义和 Team。Chat 和 Workflow
编辑器使用同一个 Catalog，因此我在界面中选择的 Agent 必须存在于 Catalog。

## 在磁盘中管理

我默认在工作目录中维护：

- agents/：Agent 模板文件。
- groups/：Team/Group 定义。
- persona/：当前 Profile 和相关状态。

我使用 YAML frontmatter 和 Markdown 指令定义 Agent 模板。我依赖 SoloQueue 监听文件
变化，并在服务运行时触发 Prompt/Catalog 重建；批量编辑前我会保留备份。

## 委派

我可以让主会话把边界明确的子任务委派给其他 Agent。我让 Supervisor 跟踪子 Agent
生命周期，并把结果返回父会话。委派适合并行研究、审查或明确的实现步骤，
但我仍然会审查子 Agent 的输出。

编写 Agent 或 Team Prompt 时，我会写清楚目标、项目范围、输出证据、约束，以及
任务受阻时的处理方式。

## 常见问题

- Workflow 引用了不在 Catalog 中的 Agent ID。
- Agent 模板存在，但没有启用可用的 Provider/Model。
- 委派任务从父任务推断出了不同的工作目录。
- 文件已更新但服务尚未完成热加载；我会检查服务日志并刷新界面。
