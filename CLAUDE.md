# soloQueue Development Guidelines

递归式多智能体协作平台，采用 "SRE + Unix Philosophy" 设计理念。通过文件驱动配置，支持 Agent 间递归任务委派（串行/并行）与协作。

## Tech Stack

- **Language**: Python 3.11+
- **Web**: FastAPI + Uvicorn + Jinja2 + WebSocket
- **LLM**: LangChain/LangGraph + OpenAI API (支持 DeepSeek/Kimi 适配器)
- **向量存储**: ChromaDB (语义记忆)
- **配置验证**: Pydantic / Pydantic Settings
- **日志**: Loguru (结构化 JSONL)
- **Lint/Format**: Ruff
- **测试**: pytest + Playwright (E2E)
- **包管理**: uv + hatchling

## Project Structure

```text
src/soloqueue/
├── core/                          # 基础设施层
│   ├── adapters/                  # LLM 适配器 (OpenAI/DeepSeek/Kimi)
│   ├── context/                   # 上下文构建 (token 预算管理)
│   ├── loaders/                   # 配置加载 (Markdown + YAML Frontmatter)
│   ├── memory/                    # 4层记忆系统 (L1-Working/L2-Episodic/L3-Semantic/L4-Artifact)
│   ├── primitives/                # 内置工具 (bash/read_file/write_file/grep/glob/web_fetch)
│   ├── security/                  # 安全审批 (WebUI/Terminal 审批门控)
│   ├── skills/                    # Skill 加载
│   ├── state/                     # 状态管理
│   ├── tools/                     # 制品工具 (artifact CRUD)
│   ├── config.py                  # Pydantic Settings 配置
│   ├── embedding.py               # 嵌入模型 (OpenAI/Ollama/OpenAI-compatible)
│   ├── logger.py                  # 双通道日志 (Console + JSONL)
│   ├── registry.py                # 全局单例注册表 (Agent/Group/Skill)
│   └── workspace.py               # 文件系统沙箱
├── orchestration/                 # 编排引擎层
│   ├── orchestrator.py            # 核心: async run() + TaskFrame 栈 + 信号驱动事件循环
│   ├── runner.py                  # Agent 单步执行: 上下文构建 → LLM 调用 → 信号解析
│   ├── frame.py                   # 栈帧抽象 (隔离的消息历史/任务状态)
│   ├── signals.py                 # 控制信号 (CONTINUE/DELEGATE/DELEGATE_PARALLEL/RETURN/ERROR/USE_SKILL)
│   └── tools.py                   # 工具解析 (原语 + Skill代理 + delegate_to + delegate_parallel)
└── web/                           # Web 层
    ├── app.py                     # FastAPI 应用 (WebSocket /ws/chat, REST API)
    ├── templates/                 # Jinja2 模板
    └── static/                    # 静态资源 (CSS/JS)

config/                            # 文件驱动配置 (Markdown + YAML Frontmatter)
├── agents/                        # Agent 定义 (leader.md, fundamental_analyst.md, ...)
├── groups/                        # 团队定义 (investment.md)
└── skills/                        # 技能定义 (date-teller/SKILL.md)

tests/                             # 测试 (pythonpath=src, testpaths=tests)
├── core/                          # 单元测试
├── orchestration/                 # 编排层测试
├── web/                           # Web 层测试
├── contract/                      # 契约测试
├── integration/                   # 集成测试
└── e2e/                           # E2E 测试 (Playwright)
```

## Commands

```bash
uv sync                              # 安装依赖
uv run python main.py                # 启动 Web 服务
uv run pytest                        # 运行测试
uv run ruff check .                  # Lint 检查
uv run ruff format .                 # 格式化
```

## Key Architecture Patterns

- **递归调用栈**: Orchestrator 维护 TaskFrame 栈，Agent 委派时 push，完成时 pop
- **信号驱动事件循环**: AgentRunner.step() 返回 ControlSignal，Orchestrator 据此决定下一步
- **并行委派**: DELEGATE_PARALLEL 信号通过 asyncio.gather + run_in_executor 并发执行多个子 Agent
- **适配器工厂**: ModelAdapterFactory 根据模型名前缀自动匹配 LLM 适配器
- **单例注册表**: Registry 全局管理所有 Agent/Group/Skill 配置
- **Skill 代理工具**: Skill 生成代理工具，返回 `__USE_SKILL__:` 信号触发临时子 Agent
- **上下文预算**: ContextBuilder 基于 token 预算优先级截断 (95% 安全边际 + 4096 响应缓冲)
- **大输出卸载**: 工具输出超 2000 字符自动存为 L4 Artifact，替换为摘要 + 引用
- **异步编排**: Orchestrator.run() 为 async，通过 asyncio.create_task 驱动 WebSocket 流式输出

## Code Style

- Python: 遵循 Ruff 规范，使用 type hints
- 配置文件: Markdown + YAML Frontmatter (通过 python-frontmatter 解析)
- 环境变量: 通过 `.env` 文件 + Pydantic Settings 加载

<!-- MANUAL ADDITIONS START -->
<!-- MANUAL ADDITIONS END -->
