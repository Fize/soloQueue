# 参考手册

[English](../reference.md) | 简体中文

本手册记录配置参数 (`settings.yaml`)、CLI 命令行工具使用、数据库存储、备份步骤及安全策略。

---

## 1. 配置参考 (`settings.yaml`)

配置项从工作目录下的 `settings.yaml` 读取（默认位于 `~/.soloqueue/`；可通过 `SOLOQUEUE_WORK_DIR` 覆盖）。修改支持热加载的参数会在保存后实时生效。

### 最小配置示例

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

### 主要配置区段

| 区段 | 用途说明 |
| --- | --- |
| `providers` | OpenAI 兼容 API Endpoint 定义与重试参数 |
| `models` | 模型定义、上下文窗口、生成参数及思考 (thinking) 参数 |
| `model_routes` | 任务类型路由映射 (`general`, `engineering`, `research`, `classifier`, `fallback`) |
| `tools` | 文件路径限制、Shell 正则安全过滤、HTTP Host 白名单及输出限制 |
| `agent` | 内置工具与 MCP Server 授权许可 |
| `qqbots` / `wechat_bots` | QQ / 微信渠道的凭据、绑定设置及用户白名单 |
| `lspmcp` | 语言服务器二进制路径、参数及语言/扩展名绑定 |
| `embedding` | 向量 Embedding Provider 及模型设置（可选） |

---

## 2. CLI 命令行参考

主执行文件为 `soloqueue`。运行 `soloqueue --help` 查看完整子命令。

### `soloqueue serve`
在 `127.0.0.1` 启动 HTTP REST、WebSocket 及 Agent 运行时服务：
- `--port, -p`：监听端口（默认 `57647`；`0` 表示随机端口）。
- `--verbose, -v`：启用详细的 stderr 日志输出。

### `soloqueue start`
在一个 `127.0.0.1` 监听器中启动后端运行时、`/` 下的 Web Console 和 `/status/` 下的状态页。支持与 `serve` 相同的 port 和 verbose 参数。

### `soloqueue web`
只启动独立 Web Console。使用 `--backend` 设置后端地址，默认值为 `http://127.0.0.1:57647`。

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

SoloQueue 从 `${SOLOQUEUE_WORK_DIR:-$HOME/.soloqueue}/skills/` 发现全局 `SKILL.md` 包，也会从 `<project>/.claude/skills/` 发现兼容的项目级技能。全局 `SKILL.md` 定义会在技能目录或受支持的入口文件变化时热加载，项目级技能在 Agent 创建时加载；其他辅助文件的变化不会触发全局 Skill 注册表重建。Web Console 只提供只读查看。不要使用 `openclaw` 或 SoloQueue 的管理接口处理生命周期，应使用独立的 `clawhub` 命令。

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

## 3. 数据目录与安全备份

应用数据全部保存在 `~/.soloqueue/` 下（或由 `SOLOQUEUE_WORK_DIR` 指定）：

| 路径 | 内容描述 |
| --- | --- |
| `settings.yaml` | 应用配置与当前活动设置 |
| `mcp.json` | 外部 MCP Server 定义 |
| `soloqueue.db` | 共享 SQLite 数据库（团队、Cron、记忆） |
| `logs/` | HTTP、应用日志、时间线 JSONL 及定时任务日志 |
| `agents/` / `groups/` | 用户 Agent 模板与团队定义 |
| `skills/` | 已安装的自定义 Skills |

### 安全备份步骤

1. 停止 `soloqueue` 服务进程。
2. 将整个工作目录（`~/.soloqueue/`）复制到安全的备份位置。
3. 重启服务进程。

> **注意**：备份前务必先停止服务，以确保 SQLite WAL checkpoint 完成且时间线 JSONL 日志已完全刷盘。
