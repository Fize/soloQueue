# SoloQueue

一个纯文件驱动的递归式 AI Agent 网络系统，采用 "SRE + Unix Philosophy" 架构设计。它是一个多智能体协作平台，支持智能体之间的递归任务委派和协作。

## ✨ 特性

- **无数据库设计**：完全基于文件系统作为唯一数据源
- **分形架构**：递归的 Agent → Manager → Worker 结构
- **标准化配置**：兼容 Claude Code / Gemini 的 Agent 与 Skill 定义格式
- **纯文件驱动**：所有配置和状态都存储在文件中
- **多智能体协作**：支持投资分析团队等复杂协作场景
- **记忆系统**：分层记忆架构（工作记忆、情节记忆、语义记忆）
- **Web界面**：提供监控和交互界面
## 📜 Constitution

The project follows a formal constitution that defines its core principles, technical constraints, and development workflow. See [.specify/memory/constitution.md](.specify/memory/constitution.md) for details.

## 🚀 快速开始

### 1. 环境准备

```bash
# 克隆项目
git clone <repository>
cd soloQueue

# 安装依赖（推荐使用 uv）
uv sync

# 配置环境变量
cp .env.example .env
```

### 2. 环境变量配置

编辑 `.env` 文件，设置以下变量：

```bash
# LLM配置（支持DeepSeek或OpenAI）
OPENAI_API_KEY=sk-your-deepseek-api-key
OPENAI_BASE_URL=https://api.deepseek.com/v1
DEFAULT_MODEL=deepseek-reasoner

# 系统配置
LOG_LEVEL=INFO
REQUIRE_APPROVAL=true  # 危险操作需要用户批准

# Web服务器配置
SOLOQUEUE_WEB_HOST=0.0.0.0
SOLOQUEUE_WEB_PORT=45728
SOLOQUEUE_WEB_DEBUG=true
```

### 3. 启动系统

**方式一：使用CLI启动Web界面**

```bash
# 使用uv运行
uv run python -m soloqueue.cli

# 或直接运行
python -m soloqueue.cli
```

启动后访问：http://localhost:45728

**方式二：直接运行**

```bash
python main.py
```

## 📖 使用方式

### 基本使用流程

1. **配置Agent团队**：在 `config/` 目录下定义Agent和团队
2. **启动系统**：运行CLI或Web界面
3. **提交任务**：通过Web界面或API提交任务
4. **监控执行**：观察Agent协作和任务执行过程
5. **查看结果**：获取分析报告和执行结果

### 配置自定义Agent

SoloQueue使用Markdown + YAML frontmatter格式定义Agent：

1. 在 `config/agents/` 目录下创建 `.md` 文件
2. 使用YAML frontmatter定义Agent元数据
3. 在Markdown正文中定义系统提示词

**示例：投资团队领导者配置**

创建 `config/agents/leader.md`：

```markdown
---
name: leader
description: Investment Team Leader
group: investment
model: deepseek-reasoner
reasoning: true
is_leader: true
tools:
  - read_file
  - date-teller
sub_agents:
  - fundamental_analyst
  - technical_analyst
  - trader
---

## Identity
你是投资团队的领导者，负责协调分析师团队完成投资研究任务。

## Responsibilities
1. 理解用户的投资问题和需求
2. 将具体研究任务委派给合适的分析师
3. 整合分析结果形成综合报告
4. 管理团队的工作流程和进度

## Instructions
- 始终以专业、严谨的态度对待投资分析
- 确保所有分析都有数据支持
- 及时向用户汇报进展和发现
```

### 配置团队定义

在 `config/groups/` 目录下定义团队：

**示例：投资分析团队**

创建 `config/groups/investment.md`：

```markdown
---
name: investment
description: Investment analysis team
agents:
  - leader
  - fundamental_analyst
  - technical_analyst
  - trader
default_leader: leader
---

# 投资分析团队

这是一个完整的投资分析团队，包含领导者、基本面分析师、技术面分析师和交易员。
```

### 配置自定义技能

在 `config/skills/` 目录下创建技能：

**示例：日期查询技能**

创建 `config/skills/date-teller/SKILL.md`：

```markdown
---
name: date-teller
description: Tell the current date and time
---

## 功能
查询当前日期和时间

## 使用方式
直接调用即可获取当前日期时间信息
```

### Web界面使用

启动Web服务后，访问 http://localhost:45728：

1. **仪表板**：查看系统状态和运行中的Agent
2. **任务提交**：提交新任务给Agent团队
3. **执行监控**：实时查看任务执行进度
4. **结果查看**：浏览已完成任务的分析结果
5. **配置管理**：查看和编辑Agent配置

### API接口

SoloQueue提供REST API接口：

```bash
# 提交新任务
curl -X POST http://localhost:45728/api/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "prompt": "分析特斯拉股票的投资价值",
    "team": "investment"
  }'

# 获取任务状态
curl http://localhost:45728/api/tasks/{task_id}

# 获取Agent列表
curl http://localhost:45728/api/agents
```

### 投资分析示例

系统预置了完整的投资分析团队示例：

1. **领导者 (leader)**：协调整个分析过程
2. **基本面分析师 (fundamental_analyst)**：分析公司财务状况
3. **技术面分析师 (technical_analyst)**：分析股价走势和技术指标
4. **交易员 (trader)**：提供交易建议和风险管理

**使用示例**：

```bash
# 启动系统后，通过Web界面提交：
"请分析苹果公司(APPL)股票的投资价值，包括基本面分析、技术分析和交易建议"
```

## ⚙️ 配置说明

### 目录结构

```
config/
├── agents/          # Agent定义文件 (*.md)
├── groups/          # 团队定义文件 (*.md)
└── skills/          # 自定义技能目录
```

### Agent配置字段

| 字段 | 类型 | 说明 | 必填 |
|------|------|------|------|
| `name` | string | Agent唯一标识 | 是 |
| `description` | string | Agent描述 | 是 |
| `group` | string | 所属团队 | 是 |
| `model` | string | 使用的LLM模型 | 是 |
| `reasoning` | boolean | 是否启用推理模式 | 否 |
| `is_leader` | boolean | 是否为团队领导者 | 否 |
| `tools` | list | 可用工具列表 | 否 |
| `sub_agents` | list | 可委派的子Agent列表 | 否 |

### 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `OPENAI_API_KEY` | LLM API密钥 | 无 |
| `OPENAI_BASE_URL` | LLM API基础URL | https://api.deepseek.com/v1 |
| `DEFAULT_MODEL` | 默认模型 | deepseek-reasoner |
| `LOG_LEVEL` | 日志级别 | INFO |
| `REQUIRE_APPROVAL` | 危险操作需要批准 | true |
| `SOLOQUEUE_WEB_HOST` | Web服务绑定地址 | 0.0.0.0 |
| `SOLOQUEUE_WEB_PORT` | Web服务监听端口 | 45728 |
| `SOLOQUEUE_WEB_DEBUG` | 启用调试模式 | true |

## 🧪 示例

项目包含完整的示例配置：

- `config/agents/`：投资团队所有Agent定义
- `config/groups/investment.md`：投资团队定义
- `config/skills/date-teller/`：日期查询技能
- `examples/semantic_store_demo.py`：语义存储演示

运行示例：

```bash
# 启动Web界面
uv run python -m soloqueue.cli

# 或运行演示脚本
uv run python examples/semantic_store_demo.py
```

## 🛠️ 开发

### 项目结构

```
src/soloqueue/
├── cli.py              # CLI入口点
├── web/                # Web界面
│   ├── app.py         # FastAPI应用
│   └── config.py      # Web配置
├── orchestration/      # 编排引擎
│   ├── orchestrator.py # 核心编排器
│   ├── runner.py      # Agent运行器
│   └── state.py       # 状态管理
└── core/              # 核心模块
    ├── loaders/       # 配置加载器
    ├── memory/        # 记忆系统
    ├── context/       # 上下文管理
    └── logger.py      # 日志系统
```

### 开发环境设置

```bash
# 安装开发依赖
uv sync --dev

# 运行测试
uv run pytest

# 代码格式化
uv run ruff format

# 代码检查
uv run ruff check
```

### 添加新功能

1. **添加新Agent**：在 `config/agents/` 创建Markdown文件
2. **添加新技能**：在 `config/skills/` 创建技能目录
3. **修改核心逻辑**：编辑 `src/soloqueue/` 下的Python文件
4. **添加测试**：在 `tests/` 目录下添加测试文件

## 📚 文档

详细设计文档位于 `doc/` 目录：

- `doc/design.md`：主设计文档
- `doc/memory_architecture.md`：记忆架构
- `doc/roadmap.md`：路线图
- `doc/part1_infrastructure.md`：基础设施设计

## 🤝 贡献

欢迎提交Issue和Pull Request！

1. Fork项目
2. 创建功能分支
3. 提交更改
4. 推送到分支
5. 创建Pull Request

## 📄 许可证

[添加许可证信息]

## 📞 支持

如有问题，请提交Issue或参考文档。