# 核心功能指南

[English](../features.md) | 简体中文

本文档详细说明 SoloQueue 的核心能力：项目与会话管理、团队与 Agent 模版、模型路由、定时任务、消息渠道以及 Skills/MCP 扩展。

---

## 1. 项目与会话

SoloQueue 将全局运行时工作目录（`~/.soloqueue/`）与具体的项目执行范围分离开来：

- **项目 (Projects)**：指向代码库的绝对路径。项目路径确立了工具执行的作用域，确保 Agent 的写入和 Shell 命令被限制在允许的目录中。
- **会话 (Sessions)**：Chat 会话通过 WebSocket 实时推送 Agent 的推理与工具执行状态。会话状态和历史消息在服务重启后持久化保存。

---

## 2. 团队与 Agent 模板

Agent 的执行依赖于工作目录中的 Agent 模板与团队定义：

- `agents/`：包含 YAML frontmatter 的 Markdown 文件，定义 Agent 的身份、模型路由、可用工具及系统指令。
- `groups/`：团队定义，用于将多个 Agent 按角色或项目分组。
- **任务委派 (Delegation)**：主会话可以把边界明确的子任务委派给专门的子 Agent，Supervisor 会跟踪子任务执行并将结果汇总返回给父会话。

---

## 3. 模型与任务路由

请求按照工作性质（而不是人工划分的难度阶梯）进行分类路由：

| 任务类型 | 工作性质 |
| --- | --- |
| `general` | 对话、文本写作、翻译、摘要 |
| `engineering` | 代码检查、仓库修改、调试、单元测试、部署 |
| `research` | Web 搜索、文档查阅、时效信息检索 |

分类优先使用本地快速规则（识别代码块、Traceback、路径、终端命令）。输入不明确时使用配置的 Classifier 模型进行解析。模型路由映射配置在 `settings.yaml` 的 `model_routes` 中。

---

## 4. 定时任务 (Cron)

Cron 任务通过标准的会话、路由和确定性安全策略执行周期性或一次性 Prompt：

- 通过 **Scheduled tasks** 界面管理定时任务。
- 任务可绑定指定的 Agent 模板及可选的项目路径范围。
- 历史运行记录、输出和状态保存在 SQLite 中并在 UI 中可查。

---

## 5. 消息渠道

SoloQueue 将 Agent 运行时桥接到外部消息平台，复用同一套会话与记忆系统：

- **QQ Bot**：通过腾讯 Bot Gateway 连接，支持 App ID / App Secret 配置，将私聊、群聊和 Guild 消息规范化为会话输入。
- **微信 iLink**：通过二维码流程授权（`soloqueue wechat login --id personal`），支持长轮询文本接收及运行期间的 typing 状态保持。

定时任务的渠道通知投递遵循尽力而为原则，Web UI 记录为权威依据。

---

## 6. Skills、MCP 与 LSP 扩展

在不改变核心代码的前提下扩展内置工具层：

- **Skills**：全局技能安装在 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/<skill-name>/SKILL.md`；Agent 创建时也会从 `<project>/.claude/skills/<skill-name>/SKILL.md` 加载兼容的项目级技能。技能定义带指令、脚本及参考资料的可复用工作流，全局 `SKILL.md` 定义会在技能目录或受支持的入口文件变化时热加载。SoloQueue 只负责发现、执行和展示已安装内容，不内置目录，也不修改技能文件。
- **技能生命周期**：使用独立的 [ClawHub](https://github.com/openclaw/clawhub) CLI，并通过 `--workdir "$SOLOQUEUE_HOME" --dir skills` 指向工作目录，其中 `SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"`。`inspect` 和 `install` 使用 `@owner/slug`，更新使用 `update @owner/slug` 或 `update --all`，卸载使用已安装技能的 slug。搜索和查看是只读操作；安装、更新和卸载必须有明确意图，且只由 L1 Agent 直接执行。L2/L3 只使用已安装技能，缺少技能时向 L1 报告 Skill ID。
- **MCP Server**：在 `~/.soloqueue/mcp.json` 中使用标准 `mcpServers` Map 结构配置，支持 `stdio` 传输机制。
- **LSP 工具**：在 `settings.yaml` 的 `lspmcp` 下配置语言服务器二进制路径与语言绑定，提供补全、定义跳转等代码智能工具。
