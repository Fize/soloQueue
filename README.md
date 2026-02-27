# SoloQueue

递归式多智能体协作平台。通过 Markdown 文件定义 Agent 团队，支持串行/并行任务委派、4 层记忆系统和 WebSocket 实时交互。

## 快速开始

```bash
# 安装
git clone <repository> && cd soloQueue
uv sync

# 配置（填入 LLM API Key）
cp .env.example .env

# 启动
uv run python main.py
```

访问 http://localhost:45728 打开 Web 界面。

## 核心能力

- **多 Agent 编排** — Leader 通过 `delegate_to` / `delegate_parallel` 向子 Agent 委派任务
- **文件驱动配置** — Markdown + YAML Frontmatter 定义 Agent、团队、技能
- **4 层记忆** — 工作记忆 / 情节日志 / 语义检索(ChromaDB) / 制品存储
- **内置工具** — bash、read_file、write_file、grep、glob、web_fetch
- **安全审批** — 危险操作需用户确认，支持 WebUI / Terminal 双通道
- **Web UI** — Dashboard、Agent/Team/Skill 管理、WebSocket 流式聊天

## 配置

### 环境变量（`.env`）

```bash
OPENAI_API_KEY=sk-your-api-key
OPENAI_BASE_URL=https://api.deepseek.com/v1   # 或留空用 OpenAI
DEFAULT_MODEL=deepseek-reasoner
REQUIRE_APPROVAL=true
```

完整配置参考 `.env.example`（含 Embedding 多提供商配置）。

### 目录结构

```
config/
├── agents/     # Agent 定义 (*.md)
├── groups/     # 团队定义 (*.md)
└── skills/     # 技能定义 (SKILL.md)
```

### Agent 配置示例

```yaml
---
name: leader
description: Team Leader
group: my-team
model: deepseek-reasoner
is_leader: true
sub_agents: [worker_a, worker_b]
---
```

## 开发

```bash
uv sync --dev          # 安装开发依赖
uv run pytest          # 运行测试
uv run ruff check .    # Lint
uv run ruff format .   # 格式化
```

## 技术栈

Python 3.11+ / FastAPI / LangChain / ChromaDB / Loguru / uv

## 文档

详细设计文档见 `doc/` 目录。
