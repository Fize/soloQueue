# Part 1: Infrastructure Design Document

**Version:** 1.1.0 (Enhanced)
**Status:** Approved
**Date:** 2026-02-05

---

## 1. Overview

基础设施层是 SoloQueue 的"物理法则"。

**Core Responsibilities:**
1.  **Fundamental Services:** Config, Logging, Workspace.
2.  **Built-in Primitives:** Agent 与系统交互的原子操作。
3.  **Loader System:** 动态加载 Agent 和 Skill 配置。
4.  **Security Mechanism:** 危险操作的用户审批机制。

---

## 2. Tech Stack

| 组件                | 选型              | 说明                 |
| ------------------- | ----------------- | -------------------- |
| **Runtime**         | Python 3.11+      | 类型提示、性能优化   |
| **Package Manager** | uv                | 快速依赖管理         |
| **Config Loader**   | pydantic-settings | 强类型配置管理       |
| **Logging**         | loguru            | 简单易用的结构化日志 |
| **Path Security**   | pathlib           | 安全路径处理         |

---

## 3. Fundamental Services

### 3.1 Workspace Manager

负责管理项目的根目录和文件沙盒。

```python
# src/core/workspace.py
from pathlib import Path

class WorkspaceError(Exception):
    pass

class WorkspaceManager:
    def __init__(self, root_dir: str | Path | None = None):
        # 默认使用当前工作目录，或从 env 获取
        self.root = Path(root_dir or os.getcwd()).resolve()
    
    def resolve_path(self, rel_path: str) -> Path:
        """
        解析相对路径，并确保其在沙盒内。
        
        Raises:
            WorkspaceError: 如果路径试图逃逸 (使用 ../)
        """
        target = (self.root / rel_path).resolve()
        if not str(target).startswith(str(self.root)):
            raise WorkspaceError(f"Path escape attempt: {rel_path}")
        return target
```

### 3.2 Configuration Manager

负责加载系统配置（Env + YAML）。

```python
# src/core/config.py
from pydantic_settings import BaseSettings

class Settings(BaseSettings):
    # System
    PROJECT_ROOT: str | None = None
    LOG_LEVEL: str = "INFO"
    REQUIRE_APPROVAL: bool = True
    
    # LLM
    OPENAI_API_KEY: str | None = None
    ANTHROPIC_API_KEY: str | None = None
    DEFAULT_MODEL: str = "claude-3-5-sonnet"
    
    # Feishu
    FEISHU_APP_ID: str | None = None
    FEISHU_APP_SECRET: str | None = None

    class Config:
        env_file = ".env"
        env_file_encoding = "utf-8"

# 全局单例
settings = Settings()
```

### 3.3 Structured Logging

提供带上下文的日志记录。

```python
# src/core/logger.py
from loguru import logger
import sys

def setup_logger():
    logger.remove()
    # Console output
    logger.add(sys.stderr, format="{time} | {level} | {extra[agent_id]} | {message}")
    # File output (JSON)
    logger.add("logs/soloqueue.jsonl", serialize=True, rotation="10 MB")

# Usage: logger.bind(agent_id="ceo").info("Thinking...")
```

### 3.4 LLM Service

负责初始化和管理 LLM 实例。MVP 阶段主要支持 OpenAI 兼容接口。

```python
# src/core/llm.py
from langchain_openai import ChatOpenAI
from soloqueue.core.config import settings

class LLMFactory:
    @staticmethod
    def get_llm(model: str | None = None) -> ChatOpenAI:
        """
        获取配置好的 ChatOpenAI 实例。
        
        Args:
            model: 指定模型名称。如果为 None，使用 Settings.DEFAULT_MODEL。
            
        Returns:
            LangChain ChatObject
        """
        return ChatOpenAI(
            model=model or settings.DEFAULT_MODEL,
            api_key=settings.OPENAI_API_KEY,
            base_url=settings.OPENAI_BASE_URL,
            temperature=0
        )
```

---

## 4. Built-in Primitives

### 4.1 统一接口与安全集成

所有文件操作 Primitive **必须** 调用 `WorkspaceManager.resolve_path`。

```python
# src/core/primitives/file_io.py

def read_file(path: str) -> str:
    try:
        # 使用 WorkspaceManager 确保安全
        safe_path = workspace.resolve_path(path)
        return safe_path.read_text(encoding="utf-8")
    except WorkspaceError as e:
        raise PermissionDenied(str(e))
    except FileNotFoundError:
        raise PrimitiveError(f"File not found: {path}")
```

### 4.2 Primitive 清单 (Updated)

| Primitive    | 安全控制                 | 依赖         |
| ------------ | ------------------------ | ------------ |
| `bash`       | 审批 + 命令白名单        | `subprocess` |
| `read_file`  | Workspace Sandbox        | `workspace`  |
| `write_file` | Workspace Sandbox + 审批 | `workspace`  |
| `grep`       | Workspace Sandbox        | `workspace`  |
| `glob`       | Workspace Sandbox        | `workspace`  |
| `find`       | Workspace Sandbox        | `workspace`  |
| `web_fetch`  | 无                       | `httpx`      |

### 4.3 Tool Registry (New)

为编排层提供统一的工具获取入口。

```python
# src/core/primitives/__init__.py
from langchain_core.tools import BaseTool, StructuredTool

def get_all_primitives() -> list[BaseTool]:
    """
    Return all Layer 1 primitives wrapped as LangChain StructuredTools.
    This creates the bridge between Python functions and the Agent Runtime.
    """
    return [
        StructuredTool.from_function(bash),
        StructuredTool.from_function(read_file),
        # ...
    ]
```

---

## 5. Loader System (Agent & Skills)

保持原设计，增加对 `pydantic` 的使用以验证 YAML Schema。

```python
# src/core/loaders/schema.py
from pydantic import BaseModel

class AgentSchema(BaseModel):
    name: str
    description: str
    model: str = "claude-3-5-sonnet"
    tools: list[str] = []
    sub_agents: list[str] = []
    memory: str | None = None

# Loader 在解析 frontmatter 后，使用 AgentSchema.model_validate(data) 校验
```

---

## 6. Security: User Approval

（保持原设计，增加对 Config `REQUIRE_APPROVAL` 开关的支持）

```python
if settings.REQUIRE_APPROVAL and not is_safe_command(cmd):
    if not approval_manager.request(...):
        raise PermissionDenied(...)
```

---

## 7. Directory Structure (Updated)

```text
soloQueue/
├── .env                      # Secrets
├── config/
│   ├── settings.yaml         # Project settings
│   └── agents/               # Agent Definitions
├── src/
│   └── soloqueue/
│       ├── core/
│       │   ├── config.py         # [NEW]
│       │   ├── workspace.py      # [NEW]
│       │   ├── logger.py         # [NEW]
│       │   ├── primitives/
│       │   ├── loaders/
│       │   └── security/
│       └── utils/
└── logs/                     # Log files
```

---

## 8. Dependencies

```toml
dependencies = [
    "python-frontmatter",
    "pydantic-settings",      # [NEW] Config
    "loguru",                 # [NEW] Logging
    "httpx",
    "html2text",
    "langchain-openai",       # [NEW] LLM
    "langchain-core",         # [NEW] LLM
]
```

---

## 9. Implementation Checklist

- [ ] 初始化 uv 项目 & 依赖
- [ ] 实现 `core/config.py`
- [ ] 实现 `core/logger.py`
- [ ] 实现 `core/workspace.py` (关键沙盒逻辑)
- [ ] 实现 `core/security/` (Approval & Allowlist)
- [ ] 实现 `core/primitives/` (集成 Workspace)
- [ ] 实现 `core/loaders/` (Loaders)
- [ ] 实现 `core/llm.py` (LLM Factory)
- [ ] 单元测试 (重点测试 Workspace 路径逃逸及 LLM Mock)
