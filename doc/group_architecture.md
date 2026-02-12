# Agent 分组架构设计 (Group Architecture)

## 1. 核心理念
为了支持大规模多 Agent 协作系统，我们将扁平的 Agent 集合重构为层级化的 **Group (分组)** 结构。
这不仅仅是为了解决命名冲突，更是为了模拟真实世界中的组织架构（如“投资部”、“开发部”、“人事部”）。

## 2. 组织结构
### 2.1 层级模型
```
SoloQueue System
├── Global Context (MEMORY.md)
├── Group: Investment
│   ├── Context: 金融分析、市场研究
│   ├── Agent: Leader (Team Lead)
│   ├── Agent: Analyst
│   └── Agent: Trader
└── Group: Coding
    ├── Context: 软件开发、代码审查
    ├── Agent: Leader (Tech Lead)
    ├── Agent: Architect
    └── Agent: Developer
```

### 2.2 命名与寻址 (Addressing)
*   **Group ID**: 全局唯一，如 `investment`。
*   **Agent Name**: 组内唯一，如 `leader`。
*   **全局地址 (Global Address)**: `{group_id}.{agent_name}` (例如 `investment.leader`)。
    *   *优势*：允许不同组拥有同名角色（每个组都可以有自己的 Leader）。

## 3. 配置规范 (Configuration)

我们采用 **解耦设计**，将组定义与 Agent 定义分离。

## 3. 配置实现：目录扫描 (Directory Scanning)

为了保持简单性并支持动态扩展，系统将**直接扫描配置文件目录**，在内存中自动链接 Group 和 Agent。

### 3.1 目录结构
所有配置存储在项目根目录的 `config/` 文件夹下，分为两个扁平目录：

```bash
soloqueue/
├── config/
│   ├── groups/              # 分组定义目录
│   │   ├── investment.yaml
│   │   └── coding.yaml
│   │
│   └── agents/              # Agent 定义目录
│       ├── investment_leader.yaml
│       ├── investment_analyst.yaml
│       └── coding_leader.yaml
```

### 3.2 配置文件示例

**`config/agents/investment_leader.yaml`**
```yaml
name: "leader"
group: "investment"
is_leader: true     # 必填：标志该 Agent 为组长（每组只能有一个）
system_prompt: "..."
```

**`config/agents/investment_analyst.yaml`**
```yaml
name: "analyst"
group: "investment"
is_leader: false
system_prompt: "..."
skills:             # Optional: 显式配置技能
  - "web_search"
  - "financial_calculator"
```

## 4. 通信与路由 (Routing)

### 4.1 核心原则
1.  **单一领导原则**：每个组 **必须且只能** 有一个 `is_leader: true` 的 Agent。
2.  **层级通信原则**：跨组通信必须通过 Leader 进行。

### 4.2 路由逻辑
*   **组内通信 (Intra-Group) - 遵循 MVP 逻辑**：
    *   **严格层级 (Hierarchical)**：通信能力由 `delegate_to` 工具的配置决定。
    *   **Leader -> Member**: 允许。Leader 拥有 `delegate_to` 工具。
    *   **Member -> Member**: **默认禁止**。因为普通 Member 通常不配置 `delegate_to` 工具。它们完成任务后，直接将控制权返回给调用者 (Pop Stack)。
    *   **Member -> Leader**: 通过任务完成自动返回 (Return)。
*   **跨组通信 (Inter-Group)**：
    *   **限制**：仅允许 `Leader` -> `Leader`。
    *   **规则**：
        *   若发送者不是 Leader：**禁止**调用跨组 Agent。
        *   若目标不是 Leader：**禁止**被跨组调用。
    *   **示例**：
        *   `investment.leader` -> `coding.leader`：**允许** (Leader 对接)。
        *   `investment.analyst` -> `coding.leader`：**拒绝** (越级汇报)。
        *   `investment.leader` -> `coding.dev`：**拒绝** (微管跨组)。

### 4.3 寻址实现
`delegate_to` 工具将增加权限校验逻辑：
```python
def check_permission(source_agent, target_agent):
    if source_agent.group == target_agent.group:
        return True # 组内互通
    
    # 跨组：双方都必须是 Leader
    if source_agent.is_leader and target_agent.is_leader:
        return True
    
    return False
```

## 5. 工具与技能系统 (Tools & Skills)

### 5.1 概念区分
*   **内置工具 (Built-in Tools)**
    *   **定义**：SoloQueue 框架提供的基础设施能力，如 `delegate_to`, `read_memory`, `save_artifact`。
    *   **可用性**：对**所有 Agent 全局开放**，无需配置，无法禁用。
*   **技能 (Skills)**
    *   **定义**：具体的业务能力或外部集成，如 `web_search`, `shell_exec`, `docker_run`。
    *   **可用性**：**按需分配**。Agent 必须在 `skills` 列表中显式声明才能使用。

### 5.2 配置实现
`AgentConfig` 数据结构将包含 `skills: List[str]` 字段。

**加载逻辑**：
1.  Loader 初始化 Agent 时，自动注入所有 `BuiltInTools`。
2.  Loader 读取 `yaml` 中的 `skills` 列表。
3.  Loader 从 `SkillRegistry` 中查找对应的 Skill 实现，并绑定到 Agent。
4.  最终 Agent 的工具集 = `BuiltInTools + ConfiguredSkills`。

## 6. Implementation Status (2026-02-07)
1.  **Phase 1: Configuration** - Implemented `AgentConfig` and `GroupConfig` loading.
2.  **Phase 2: Orchestration** - Implemented `Orchestrator` to handle recursive routing and stack management.
3.  **Phase 3: State Isolation** - Implemented `TaskFrame` for isolated context.
4.  **Phase 4: Routing** - Enforced Leader-only inter-group communication rules via `Orchestrator` logic.
5.  **Phase 5: Skill System (Claude Code Style)** - Implemented dynamic skill loading, `!command` execution, and "Skill as Sub-Agent" architecture.

## 7. Skill System Architecture (New)

The Skill System treats skills as **Ephemeral Agents** (Contexts) rather than just functions.

### 7.1 Skill Structure
Located in `.claude/skills/<skill_name>/SKILL.md`:
```markdown
---
name: git-commit
description: Automate git commit with checks
allowed_tools: ["git_diff", "git_add"]
disable_model_invocation: false
---
You are a Git Expert.
Your goal is to commit changes with a conventional commit message.
User arguments: $ARGUMENTS

!git status
```

### 7.2 Execution Flow
1.  **Discovery**: `SkillLoader` scans `~/.claude/skills` and `./.claude/skills`.
2.  **Activation**: Agent calls logic tool `use_skill(name, args)` (or proxy tool).
3.  **Preprocessing**: `SkillPreprocessor` executes `!command` lines and injects output into the prompt. Substitutes `$ARGUMENTS`.
4.  **Context Creation**: `Orchestrator` creates a dynamic `TaskFrame` with the preprocessed prompt as System Prompt.
5.  **Execution**: A temporary agent (`skill__<name>`) runs within this frame, with access only to `allowed_tools`.
6.  **Return**: Result returns to the caller agent.
