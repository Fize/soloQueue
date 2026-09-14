# 功能说明

[English](../features.md) | 简体中文

本文档说明项目工作区、会话、Team 与 Agent、模型路由、定时任务、消息渠道、
Skills、MCP 和 LSP 集成。

---

## 1. 项目与会话

SoloQueue 将全局运行时工作目录（`~/.soloqueue/`）与具体的项目执行范围分离开来：

- **项目 (Projects)**：指向代码库的绝对路径。选择项目后，该路径成为项目 Agent 工具的默认工作目录。
- **会话 (Sessions)**：Chat 会话通过 WebSocket 实时推送 Agent 的推理与工具执行状态。会话状态和历史消息在服务重启后持久化保存。

---

## 2. 团队与 Agent 模板

Agent 的执行依赖于工作目录中的 Agent 模板与团队定义：

- `agents/`：包含 YAML frontmatter 的 Markdown 文件，记录身份、Team 归属、Leader 状态、模型、MCP Server、渠道绑定和通知渠道；Markdown 正文作为 Agent System Prompt。
- `groups/`：Team 定义，记录 Team 名称、共享 Skill 配置和 Markdown 描述。
- **任务委派 (Delegation)**：主会话可以把边界明确的子任务委派给 Team Agent，Supervisor 会跟踪执行并将结果汇总返回给父会话。
- **管理方式**：Web Console 和 REST API 读写同一组定义。L1 Prompt 不包含内置 Team Schema；用户明确要求管理 Team 或 Agent 时，L1 会先读取现有 `groups/*.md` 和 `agents/*.md` 并沿用其当前格式。

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

Cron 任务在每次执行时创建的临时 L1 或 L2 Session 中运行周期性或一次性 Prompt：

- 通过 **Scheduled tasks** 界面管理定时任务。
- 任务可绑定指定的 Agent 模板及可选的项目路径范围。
- 历史运行记录、输出和状态保存在 SQLite 中并在 UI 中可查。

---

## 5. 消息渠道

渠道适配器将平台消息规范化后提交到配置绑定的 L1 或 L2 Session：

- **QQ Bot**：通过腾讯 Bot Gateway 连接，支持 App ID / App Secret 配置，将私聊、群聊和 Guild 消息规范化为会话输入。
- **微信 iLink**：通过二维码流程授权（`soloqueue wechat login --id personal`），支持长轮询文本接收及运行期间的 typing 状态保持。

定时任务的渠道通知依赖已注册的渠道发送方和平台投递结果，执行历史保存在 Web UI 中。

---

## 6. Skills、MCP 与 LSP 扩展

SoloQueue 通过 Agent 工具层加载 Skills，并连接 MCP 与 LSP Server：

- **Skills**：全局技能安装在 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/`；支持 `skills/@user/skill/SKILL.md` 这样的分组目录，最多递归六层，找到入口文件后停止向下扫描。Agent 创建时也会从 `<project>/.claude/skills/` 加载兼容的项目级技能。技能定义带指令、脚本及参考资料的可复用工作流，全局 `SKILL.md` 定义会在技能目录或受支持的入口文件变化时热加载。SoloQueue 只负责发现、执行和展示已安装内容，不内置目录，也不修改技能文件。
- **技能生命周期**：使用独立的 [ClawHub](https://github.com/openclaw/clawhub) CLI，并通过 `--workdir "$SOLOQUEUE_HOME" --dir skills` 指向工作目录，其中 `SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"`。`inspect` 和 `install` 使用 `@owner/slug`，更新使用 `update @owner/slug` 或 `update --all`，卸载使用已安装技能的 slug。搜索和查看是只读操作；安装、更新和卸载必须有明确意图，且只由 L1 Agent 直接执行。L2/L3 只使用已安装技能，缺少技能时向 L1 报告 Skill ID。
- **MCP Server**：在 `~/.soloqueue/mcp.json` 中使用标准 `mcpServers` Map 结构配置，支持 `stdio` 传输机制。
- **LSP 工具**：在 `settings.yaml` 的 `lspmcp` 下配置语言服务器二进制路径与语言绑定，提供补全、定义跳转等代码智能工具。
