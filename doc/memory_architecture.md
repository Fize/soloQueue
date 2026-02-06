# 记忆持久化架构设计（多 Agent 群组版）

## 1. 目标
使 SoloQueue 的多 Agent 系统能够：
1.  **断点续运**：在进程重启或中断后无需从头开始，能够恢复到上次的执行状态。
2.  **隔离回顾**：每个 Agent 维护自己私有的动作日志，避免上下文污染。
3.  **群组协作**：支持 **Group (分组)** 概念，解决命名冲突并支持大规模协作。
4.  **协作共享**：Agent 之间能够通过显式机制共享结论性信息。
5.  **遵循配置**：所有 Agent 共同尊重用户定义的全局指令。

## 2. 核心架构升级：Agent 分组 (Agent Grouping)

为了支持更大规模的协作并解决命名冲突，我们引入 **Group（组）** 的概念。
*   每个 Agent 必须属于且仅属于一个 Group。
*   **寻址方式**：`{group_id}.{agent_name}`。
*   **同名规则**：组内 Agent 名称唯一；不同组可以有同名 Agent（例如每个组都可以有 `leader`）。

## 3. 分层记忆模型 (Grid Storage)

```
┌─────────────────────────────────────────────────────────────┐
│                     用户记忆 (Global)                        │
│                     MEMORY.md                                │
└─────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┴───────────────────┐
          ▼                                       ▼
┌───────────────────────┐             ┌───────────────────────┐
│ Group: investment     │             │ Group: coding         │
│ .soloqueue/groups/    │             │ .soloqueue/groups/    │
│ investment/           │             │ coding/               │
│                       │             │                       │
│  ┌─────────────────┐  │             │  ┌─────────────────┐  │
│  │ Mem: leader     │  │             │  │ Mem: leader     │  │
│  │ Artifacts       │  │             │  │ Artifacts       │  │
│  └─────────────────┘  │             │  └─────────────────┘  │
│  ┌─────────────────┐  │             │  ┌─────────────────┐  │
│  │ Mem: analyst    │  │             │  │ Mem: dev        │  │
│  │ Artifacts       │  │             │  │ Artifacts       │  │
│  └─────────────────┘  │             │  └─────────────────┘  │
└───────────────────────┘             └───────────────────────┘
```

## 4. 存储规范 (Storage Spec)

### A. 目录结构
我们摒弃扁平结构，采用基于 Group 的层级结构：

```bash
.soloqueue/
├── state.db                          # 全局状态 (SQLite)
├── MEMORY.md                         # 全局用户指令
└── groups/
    ├── {group_id}/                   # 例如 "investment"
    │   ├── memories/                 # 私有情景记忆
    │   │   ├── {agent_name}/         # 例如 "leader"
    │   │   │   └── {date}.md
    │   │   └── ...
    │   └── artifacts/                # 共享工件
    │       ├── active/
    │       │   └── {agent_name}_{filename}.json
    │       └── archive/
    │           └── {date}/...
    └── {other_group}/
        └── ...
```

### B. 并发控制与文件名
利用目录隔离，我们大大降低了碰撞概率：
*   **私有记忆**：天然隔离在 `{agent_name}` 目录下。
*   **共享工件**：
    *   存储在 `group/artifacts/active/` 下。
    *   命名规范：`{agent_name}_{filename}.json`。
    *   **冲突解决**：
        1.  同组不同 Agent：`agent_name` 前缀保证不冲突。
        2.  同组同 Agent：同一个 Agent 自身负责逻辑串行，通常不会并发写同一个文件。如果需要，Agent 自己加 Timestamp 后缀。

### C. 详细数据流

#### 1. 状态持久化
*   LangGraph 依然使用全局 `state.db`，但在 State Schema 中增加 `group_id` 字段，确保路由正确。

#### 2. 私有情景记忆
*   **读取**：Agent 启动时，加载 `.soloqueue/groups/{my_group}/memories/{my_name}/{today}.md`。
*   **隔离**：Investment 组的 Leader 看不到 Coding 组 Leader 的日记。

#### 3. 共享工件
*   **组内共享 (Intra-group)**：默认可见。Investment Analyst 生成的报告，Investment Leader 可以直接读取。
*   **跨组共享 (Inter-group)**：(未来扩展) 需要通过显式路由或特殊的 "Public Artifacts" 区域。

## 5. 风险评估与对策 (Risk & Mitigation)

| 风险点         | 描述                       | 对策                                                                       |
| :------------- | :------------------------- | :------------------------------------------------------------------------- |
| **路径过深**   | 目录层级太深导致文件名超长 | 保持 Group ID 和 Agent Name 简洁。当前层级 (4层) 在 Linux/Windows 均安全。 |
| **跨组通信**   | 组 A 需要读取 组 B 的工件  | 目前 MVP 仅支持组内共享。未来可引入 `global_artifacts` 目录。              |
| **配置复杂度** | `agents.yaml` 变得复杂     | 提供 CLI 工具辅助生成配置模板。                                            |

## 6. 实施步骤更新
1.  **配置升级**：重构 `AgentLoader` 以支持 `groups` 嵌套结构。
2.  **核心**：更新 `MemoryManager` 和 `ArtifactStore`，构造函数需传入 `group_id` 和 `agent_name` 以确定根目录。
3.  **图**：更新 `builder.py`，遍历所有 Groups 注册节点。节点名称变更为 `{group_id}__{agent_name}` (双下划线分隔) 以适配 LangGraph 的扁平 Node ID。
