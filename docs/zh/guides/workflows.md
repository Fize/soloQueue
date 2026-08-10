# 工作流

[English: Workflows](../../guides/workflows.md)

我把可重复的多智能体任务写成 YAML 定义的有向图。我使用桌面编辑器查看可视化视图，
并把 YAML 作为便于迁移和审查的事实来源。

## 创建并运行 Workflow

1. 我打开 Workflows，选择 New workflow。
2. 我为第一个节点选择已有 Agent 模板。
3. 我在编辑器中添加节点、Prompt、输出和结果边。
4. 我在保存前验证定义。
5. 我使用明确的项目路径和任务输入启动 Run。
6. 我在 Run 详情中检查节点状态、输出、确认、重试和审计事件。

## 定义结构

~~~yaml
name: docs-check
description: Review documentation and report actionable gaps
version: "1"
defaults:
  node_timeout: 20m
  workflow_timeout: 45m
  max_node_runs: 3
  max_output_bytes: 200000
agents:
  reviewer:
    template: reviewer
    model: ""
entry:
  - inspect
nodes:
  - id: inspect
    agent: reviewer
    prompt: Review the selected project and report concrete documentation gaps.
    outputs:
      completed:
        to: []
        terminal_status: completed
~~~

我为每个节点指定 Agent 和 Prompt，并把输出路由到其他节点、声明终态或有界
循环。我设置错误策略，让 Run 失败或重试节点，并设置最大尝试次数；Join 节点由我
配置为等待列出的所有前置节点。

## 运行边界

我会为 Workflow Run 提供结构化任务输入：目标、验收标准、约束和明确的项目
工作目录。可能修改文件的开发 Run 应使用独立项目目录。

我不会让 Workflow 图本身提交、推送或创建 Pull Request，除非我为 Run 明确附加了
delivery action。我会把这些外部副作用当作需要单独审查的操作。

## 审计

我把 Run 状态和事件保存到 SQLite，并在 Run 详情页区分节点完成、等待确认、失败、
重试和取消。即使 Workflow 成功，我也会检查输出和项目 diff，而不会默认建议或修改正确。
