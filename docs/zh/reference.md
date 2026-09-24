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
| `qqbots` / `wechat_bots` / `telegram_bots` | QQ、微信和 Telegram 渠道的凭据及 Session 绑定 |
| `lspmcp` | 语言服务器二进制路径、参数及语言/扩展名绑定 |
| `embedding` | 向量 Embedding Provider 及模型设置（可选） |
| `speech` | 基于 whisper.cpp 的可选本地语音转写设置 |

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

#### 会话、日志与工具

| YAML 字段 | 说明与默认值 |
| --- | --- |
| `session.timeline_max_file_mb` | 单个时间线文件的大小上限，单位 MiB（默认 `50`）。 |
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
| `embedding.enabled`、`embedding.min_similarity`、`embedding.provider`、`embedding.model_name` | 启用向量 Embedding、设置相似度阈值（默认 `0.65`）、Provider 类型（`none` 或 `openai`）及模型名称。默认关闭。 |
| `embedding.providers[]` | Embedding Provider 字段：`id`、`name`、`base_url`、`api_key`、`api_key_env`、`enabled`。 |
| `embedding.models[]` | Embedding Model 字段：`id`、`provider_id`、`name`、`dimension`、`batch_size`、`normalize`、`enabled`、`is_default`。 |
| `lspmcp.servers[]` | 按 `id` 覆盖内置 LSP Server；每项包含 `command`、`args`、`languages`、`extensions`、`disabled`。列表为空时使用全部内置 Server。 |
| `speech.enabled`、`speech.model`、`speech.model_dir` | 启用本地语音转写、选择 `tiny`、`base`、`small` 或 `medium` 模型，并指定模型目录（默认 `<工作目录>/models`，通常为 `~/.soloqueue/models`）。默认关闭。 |

QQ、微信和 Telegram 的凭据及渠道绑定也可以通过 Web Console 管理。外部 MCP Server 定义保存在独立的 `mcp.json` 中，不属于 `settings.yaml`。

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
