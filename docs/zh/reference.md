# 参考手册

[English](../reference.md) | 简体中文

本手册记录配置参数 (`settings.yaml`)、CLI 命令行工具使用、数据库存储、备份步骤及安全策略。

---

## 1. 配置参考 (`settings.yaml`)

配置项从工作目录下的 `settings.yaml` 读取（默认位于 `~/.soloqueue/`；可通过 `SOLOQUEUE_WORK_DIR` 覆盖）。修改支持热加载的参数会在保存后实时生效。

### 配置示例

```yaml
providers:
  - id: deepseek
    name: DeepSeek
    base_url: https://api.deepseek.com/v1
    api_key_env: DEEPSEEK_API_KEY
    enabled: true
    is_default: true

models:
  - id: deepseek-v4-flash-thinking
    provider_id: deepseek
    name: DeepSeek V4 Flash (Thinking)
    context_window: 1048576
    enabled: true
    generation:
      temperature: 0
      max_tokens: 16384
    thinking:
      enabled: true
      reasoning_effort: high

model_routes:
  general: deepseek:deepseek-v4-flash-thinking
  engineering: deepseek:deepseek-v4-flash-thinking
  research: deepseek:deepseek-v4-flash-thinking
  classifier: deepseek:deepseek-v4-flash-thinking
  fallback: deepseek:deepseek-v4-flash-thinking
```

### 配置区段

| 区段 | 用途说明 |
| --- | --- |
| `providers` | OpenAI 兼容 API Endpoint 定义与重试参数 |
| `models` | 模型定义、上下文窗口、生成参数、思考参数及视觉能力 |
| `model_routes` | 任务路由 (`general`, `engineering`, `research`) 及辅助路由 `classifier`、`vision`、`fallback` |
| `tools` | 文件路径、Shell 过滤、HTTP Host 白名单、输出限制及图像生成模型 (`image_models`) |
| `agent` | 内置工具与 MCP Server 授权许可 |
| `qqbots` / `wechat_bots` / `telegram_bots` / `wecom_bots` | QQ、微信、Telegram 和企业微信渠道的凭据及 Session 绑定 |
| `lspmcp` | 语言服务器二进制路径、参数及语言/扩展名绑定 |
| `embedding` | 长期记忆检索及嵌入模型设置 |
| `speech` | 基于 whisper.cpp 的可选本地语音转写设置 |
| `storage` | 分别管理运行日志、会话历史、执行详情和定时任务的保留策略 |

### 配置字段详解

下表中的字段名使用 YAML 格式。省略字段时使用程序内置默认值。Provider API Key 可直接填写，也可通过环境变量读取；为避免密钥保存在 `settings.yaml` 中，建议使用环境变量。

#### Provider 与 Model

| YAML 字段 | 说明 |
| --- | --- |
| `providers[].id`、`providers[].name` | Provider 标识和显示名称；Model 与路由通过 `id` 引用 Provider。 |
| `providers[].base_url` | OpenAI 兼容 API 的基础 URL。 |
| `providers[].api_key`、`providers[].api_key_env` | API Key 或保存 Key 的环境变量名。直接填写的 Key 优先。 |
| `providers[].enabled`、`providers[].is_default` | 启用 Provider，并将其设为默认 Provider。 |
| `providers[].timeout_ms` | Provider 请求超时时间，单位为毫秒。 |
| `providers[].headers` | 请求 Provider 时附加的 HTTP Header。 |
| `providers[].retry.max_retries` | 最大重试次数（默认 `3`）。 |
| `providers[].retry.initial_delay_ms`、`providers[].retry.max_delay_ms` | 初始和最大重试间隔（默认分别为 `1000`、`30000` 毫秒）。 |
| `providers[].retry.backoff_multiplier` | 重试间隔递增系数（默认 `2`）。 |
| `models[].id`、`models[].provider_id`、`models[].name` | Model 标识、所属 Provider ID 和显示名称。 |
| `models[].api_model` | 可选的上游模型名称；未设置时使用 Model ID。 |
| `models[].context_window` | 模型上下文窗口大小，单位为 Token。 |
| `models[].enabled`、`models[].vision` | 启用模型；模型支持图像输入时设置 `vision: true`。 |
| `models[].generation.temperature`、`models[].generation.max_tokens` | 采样温度和最大生成 Token 数。 |
| `models[].thinking.enabled` | 启用 Provider 的推理/思考模式。 |
| `models[].thinking.reasoning_effort` | Provider 支持的推理强度，例如 `high` 或 `max`。 |
| `models[].thinking.thinking_type` | 某些 API 使用的思考类型参数值。 |

#### 模型路由

路由值格式为 `provider-id:model-id`，必须对应已启用的 Provider 和 Model。

| YAML 字段 | 说明 |
| --- | --- |
| `model_routes.general` | 对话、写作、翻译等通用任务使用的模型。 |
| `model_routes.engineering` | 编码、调试等工程任务使用的模型。 |
| `model_routes.research` | 调研与信息查询任务使用的模型。 |
| `model_routes.classifier` | 可选的任务分类模型。 |
| `model_routes.vision` | 可选的图像模型；对应的 Model 需设置 `vision: true`。 |
| `model_routes.fallback` | 任务路由无法解析时使用的回退模型。 |

#### 执行详情与存储保留

已采集的 Web 对话会在助手回复下方提供 **执行详情**：本轮 LLM 逻辑调用次数、总耗时、每次调用的 provider/模型、用途、状态、Token 及持久化委派过程。展开一次调用可查看当时采集的 system/developer 指令；provider 使用合并指令时展示相应的实际表示。网络请求尝试和重试与逻辑调用分开统计，未报告尝试次数的 provider 明确显示未知。并行调用的耗时之和不等于本轮总耗时。

已完成的委派卡片同时提供 **结果** 和 **执行过程**。历史根据请求、dispatch 和工具调用 ID 关联，不通过显示名称猜测。缺少快照的旧调用显示未记录，已过期或部分缺失的详情也会明确说明。定时任务历史也会区分“详情已过期”和“没有已记录的事件”，继续展示可用的结果摘要、最终输出及执行状态，并提供对应的保留设置链接。重新打开页面不能还原从未采集的内容。

在 **设置 → 存储与保留** 查看文件与数据库用量、分别调整策略、检查最近清理结果与下次检查时间，或预览手动清理。轮转大小与记录数放在高级设置中。收紧策略前须预览并确认；预览凭据有有效期，执行删除前服务端会重新检查运行中及受保护记录。历史遗留文件不参与自动清理，须在手动预览中明确勾选。

| `storage` 分类 | 默认策略 |
| --- | --- |
| `runtime_logs` | `max_file_mb: 50`、`retention_days: 15`、`max_files: 30`、`max_total_mb: 1024`；HTTP 默认继承，设置 `http_override: true` 后单独配置 |
| `session_history` | `max_file_mb: 50`、`retention_days: 0`、`warning_mb: 1024`；默认长期保留会话历史 |
| `execution_details` | `max_file_mb: 50`、`retention_days: 30`、`max_files: 0`、`max_total_mb: 1024`、`max_turn_mb: 50` |
| `cron_history` | `max_file_mb: 50`、`retention_days: 30`、`max_files: 0`、`max_total_mb: 512`、`max_per_task: 0` |

容量单位均为 MiB。天数、记录数和容量设为 `0` 表示不限制；单文件及每轮正文大小范围为 1–1024 MiB。运行日志的 `max_files` 是每个前缀保留的历史文件数，不含当前文件；执行详情按已结束的完整详情单元计数，不会任意裁掉恢复日志的片段。保留天数包含当天。HTTP 覆盖字段为 `http_max_file_mb`、`http_retention_days` 和 `http_max_files`。

会话历史支持显式保留期限和用量提醒，不支持按文件数或总容量自动删除。提醒阈值不是磁盘硬上限。其他分类达到保留限制后会淘汰符合条件的旧详情，受保护的恢复记录可能继续保留。额度不足时可选详细正文会被截断，执行状态仍保留；有限额度无法保证无限期保存完整历史。

会话回放继续使用 timeline JSONL。System 指令以私有、去重快照单独保存，不写入回放时间线；LLM 用量指标继续保存在共享 SQLite 数据库中。委派状态及其重启恢复仍由 dispatch 管理器维护。清理会保护运行中的任务，并保留仍需恢复的 dispatch 状态。需要同时保留对话和可用执行详情时，请备份整个工作目录。

#### 会话、日志与工具

| YAML 字段 | 说明与默认值 |
| --- | --- |
| `session.timeline_max_file_mb` | 兼容旧配置的时间线轮转大小，单位 MiB（默认 `50`）；显式配置 `storage.session_history.max_file_mb` 时以新字段为准。 |
| `log.level` | 日志级别（默认 `info`）。 |
| `log.console`、`log.file` | 是否输出到控制台和文件（默认分别为 `false`、`true`）。 |
| `tools.max_file_size`、`tools.max_write_size` | 单次读取或写入文件的字节数上限（默认均为 `1048576`）。 |
| `tools.max_matches`、`tools.max_line_len`、`tools.max_glob_items` | 搜索匹配数、返回行长度及 Glob 结果数量上限（默认 `100`、`500`、`1000`）。 |
| `tools.max_multi_write_bytes`、`tools.max_multi_write_files`、`tools.max_replace_edits` | 多文件写入总字节数、文件数及替换编辑数上限（默认 `10485760`、`50`、`50`）。 |
| `tools.http_allowed_hosts` | WebFetch 可访问主机的可选白名单；为空时不限制主机，但仍受其他安全检查约束。 |
| `tools.http_max_body`、`tools.http_timeout_ms` | WebFetch 响应体大小与超时时间上限（默认 `5242880` 字节、`600000` 毫秒）。 |
| `tools.http_block_private` | 是否阻止访问私有、回环和链路本地地址（默认 `true`）。 |
| `tools.shell_block_regexes` | Shell 命令正则黑名单；默认空列表，即不通过此列表拦截命令。 |
| `tools.shell_max_output` | Shell 输出字节数上限（默认 `262144`）。 |
| `tools.web_search_timeout_ms` | Web 搜索超时时间（默认 `600000` 毫秒）。 |
| `tools.tavily_api_key`、`tools.tavily_api_key_env` | Tavily Key 或对应环境变量名；未提供 Key 时使用 DuckDuckGo。 |
| `tools.image_models[]` | 图像生成模型列表。每项包含 `id`、`name`、`provider`、`enabled`、`is_default`，以及按 Provider 需要填写的 `secret_id`、`secret_id_env`、`secret_key`、`secret_key_env`、`api_key`、`api_key_env`、`api_base_host` 和 `region`。 |

#### Agent 与集成

| YAML 字段 | 说明 |
| --- | --- |
| `agent.builtin_mcp_servers`、`external_mcp_servers` | 内置与外部 MCP Server 白名单。省略表示加载全部；设置为 `[]` 表示不加载。 |
| `qqbots[]` | QQ 账号字段：`id`、`name`、`enabled`、`app_id`、`app_secret`、`intents`、`sandbox`、`bind_type`、`bind_agent`、`whitelist_enabled`、`whitelist`。 |
| `wechat_bots[]` | 微信 iLink 账号字段：`id`、`name`、`enabled`、`bot_token`、`bot_id`、`base_url`、`bot_agent`、`bind_type`、`bind_agent`、`whitelist_enabled`、`whitelist`。 |
| `telegram_bots[]` | Telegram 账号字段：`id`、`name`、`enabled`、`bot_token`、`bot_id`、`username`、`bind_type`、`bind_agent`、`whitelist_enabled`、`whitelist`。 |
| `wecom_bots[]` | 企业微信智能机器人账号字段：`id`、`name`、`enabled`、`bot_id`、`secret`、`bind_type`、`bind_agent`、`whitelist_enabled`、`whitelist`。 |
| `embedding.enabled`、`embedding.min_similarity`、`embedding.provider`、`embedding.model_name` | 控制长期记忆（默认关闭）、设置相似度阈值（默认 `0.65`）、嵌入来源（`none`：仅 BM25 + 知识图谱；`onnx`：增加本地向量；`openai`：增加远程向量）及远程模型名称。 |
| `embedding.providers[]` | Embedding Provider 字段：`id`、`name`、`base_url`、`api_key`、`api_key_env`、`enabled`。 |
| `embedding.models[]` | Embedding Model 字段：`id`、`provider_id`、`name`、`dimension`、`batch_size`、`normalize`、`enabled`、`is_default`。 |
| `lspmcp.servers[]` | 按 `id` 覆盖内置 LSP Server；每项包含 `command`、`args`、`languages`、`extensions`、`disabled`。列表为空时使用全部内置 Server。 |
| `speech.enabled`、`speech.model`、`speech.model_dir` | 启用本地语音转写、选择 `tiny`、`base`、`small` 或 `medium` 模型，并指定模型目录（默认 `<工作目录>/models`，通常为 `~/.soloqueue/models`）。默认关闭。 |

QQ、微信、Telegram 和企业微信的凭据及渠道绑定也可以通过 Web Console 的**设置 → Channels** 管理。在 L1 配置或团队 Agent 的渠道设置中选择账号，并指定接收定时任务结果的通知渠道。L1 使用全局会话，L2 按渠道／账号共享会话。通知需要先由白名单允许的用户发送消息建立收件人路由，并受平台权限和投递限制约束。

企业微信需创建智能机器人并选择 **API 模式 → 使用长连接**，然后在 **WeCom · 企业微信** 中填写 Bot ID 和 Secret。Core 主动连接 `wss://openws.work.weixin.qq.com`，无需公网回调地址。等待状态显示 **Connected** 后，可单聊发送文本或在群内 @ 机器人。首期支持文本，包括原位更新的流式回复和主动通知；收到附件会提示暂不支持，生成的文件可在 Web Console 查看。API 不返回 Secret，编辑时留空会保留已保存的值。修改 Bot ID 时必须填写新 Secret，并发送消息建立新的通知路由。渠道凭据保存在 `settings.yaml` 中，请妥善保护该文件。

外部 MCP Server 定义保存在独立的 `mcp.json` 中，不属于 `settings.yaml`。

---

## 2. CLI 命令行参考

主执行文件为 `soloqueue`。运行 `soloqueue --help` 列出已注册的子命令和参数。

### `soloqueue serve`
在 `127.0.0.1` 启动 HTTP REST、WebSocket 及 Agent 运行时服务：
- `--host`：监听主机（默认 `127.0.0.1`）。
- `--port, -p`：监听端口（默认 `57689`；`0` 表示随机端口）。
- `--verbose, -v`：启用详细的 stderr 日志输出。

### `soloqueue start`
在一个监听器中启动后端运行时、`/` 下的 Web Console 和 `/status/` 下的状态页。支持与 `serve` 相同的 host、port 和 verbose 参数。

### `soloqueue web`
只启动独立 Web Console。使用 `--backend` 设置后端地址，默认值为 `http://127.0.0.1:57689`。

### `soloqueue version`
打印应用程序版本字符串。

### `soloqueue skills report`
生成关于已安装 Skill 的 JSON 治理报告，显示调用频率和 Prompt 开销指标：
```bash
soloqueue skills report --days 30
```

### 使用 ClawHub 管理 Skill

Skill 包不再嵌入 SoloQueue，也不通过 SoloQueue 的 HTTP API 管理。请使用独立的 [ClawHub](https://github.com/openclaw/clawhub) CLI，并指向 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}`。如需使用其他目录，请先设置 `SOLOQUEUE_WORK_DIR`：

```bash
SOLOQUEUE_HOME="${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills list
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills search "calendar"
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills inspect @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills install @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update @owner/slug
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills update --all
clawhub --workdir "$SOLOQUEUE_HOME" --dir skills uninstall slug
```

SoloQueue 从 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` 发现全局 `SKILL.md` 包，也会从 `<project>/.claude/skills/` 发现兼容的项目级技能。发现支持 `skills/@user/skill/SKILL.md` 这样的分组目录，最多递归六层；目录中找到受支持的入口文件后不再向下扫描。`@user` 只用于组织文件，Skill ID 来自 frontmatter 的 `name`，未填写时使用技能目录名。同一根目录出现相同 ID 时，浅层路径优先，再按路径字典序选择。全局 `SKILL.md` 定义会在技能目录或受支持的入口文件变化时热加载，项目级技能在 Agent 创建时加载；其他辅助文件的变化不会触发全局 Skill 注册表重建。Web Console 只提供只读查看。不要使用 `openclaw` 或 SoloQueue 的管理接口处理生命周期，应使用独立的 `clawhub` 命令。

### `soloqueue memory`
检查或清理长期记忆：
```bash
soloqueue memory audit [--db path]
soloqueue memory cleanup --project-root /path/to/project [--apply]
```

### `soloqueue wechat login`
触发微信 iLink 二维码授权流程：
```bash
soloqueue wechat login --id personal --name "Personal WeChat" [--bind-type l1|l2]
```

---

## 3. 数据目录与备份

应用数据全部保存在 `~/.soloqueue/` 下（或由 `SOLOQUEUE_WORK_DIR` 指定）：

| 路径 | 内容描述 |
| --- | --- |
| `settings.yaml` | 应用配置与当前活动设置 |
| `mcp.json` | 外部 MCP Server 定义 |
| `soloqueue.db` | 共享 SQLite 数据库（团队、Cron、记忆） |
| `logs/` | HTTP、应用日志、时间线 JSONL 及定时任务日志 |
| `agents/` / `groups/` | 用户 Agent 模板与团队定义 |
| `skills/` | 已安装的自定义 Skills |

### 一致性备份步骤

1. 停止 `soloqueue` 服务进程。
2. 将整个工作目录（`~/.soloqueue/`）复制到备份位置。
3. 重启服务进程。

> **注意**：复制前停止服务，使 SQLite WAL checkpoint 和时间线 JSONL 写入在复制文件前完成。
