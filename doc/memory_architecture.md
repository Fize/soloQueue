# SoloQueue 记忆架构 (生产环境规范)

## 1. 目标
为 SoloQueue 多智能体框架构建一个健壮、可扩展且分层的记忆系统。该系统超越了简单的文件 I/O，提供了一个生产级的**知识管理系统 (Knowledge Management System)**，支持：
1.  **完全持久化**：记录并可恢复每一次交互、思考过程 (思维链) 和工具执行。
2.  **分层检索**：根据访问频率和成本组织信息 (热/温/冷)。
3.  **上下文效率**：智能构建上下文，在 Token 预算内最大化 LLM 性能。
4.  **生命周期管理**：针对临时数据自动执行垃圾回收，防止存储膨胀。

---

## 2. 架构概览

### 2.1 "大脑"堆栈 (分层记忆)

| 分层   | 名称                           | 技术栈              | 生命周期     | 描述                                                   |
| :----- | :----------------------------- | :------------------ | :----------- | :----------------------------------------------------- |
| **L1** | **工作记忆 (Working Memory)**  | RAM / 上下文窗口    | 任务持续期间 | 传递给 LLM 的即时上下文。高度过滤和优化。              |
| **L2** | **情节记忆 (Episodic Memory)** | 本地文件 (JSONL/MD) | 会话持续期间 | 当前交互会话的完整日志。用于调试和追溯。               |
| **L3** | **语义记忆 (Semantic Memory)** | 向量数据库 (未来)   | 长期         | 用于检索以往解决方案和成功模式的知识库。               |
| **L4** | **制品库 (Artifact Store)**    | 文件系统 + 索引     | 永久/临时    | L1 引用的结构化大容量数据存储 (文件、报告、巨型日志)。 |

### 2.2 组织结构
```text
SoloQueue 系统
├── .soloqueue/                       # 根存储目录
│   ├── state.db                      # 全局状态 (SQLite: 队列、智能体状态)
│   ├── artifacts.db                  # L4: 制品元数据索引 (SQLite)
│   ├── artifacts/                    # L4: 制品仓库
│   │   └── blobs/                    # 内容寻址存储 (CAS)
│   │       └── 2026/02/08/           # 日期前缀 (时间局部性)
│   │           └── ab/cd/            # 哈希分片 (Git 风格)
│   │               └── abcdef123...  # SHA256 内容哈希
│   ├── groups/
│   │   ├── {group_id}/
│   │   │   ├── sessions/             # L2: 会话日志
│   │   │   │   └── {agent_name}/
│   │   │   │       ├── {session_id}.jsonl  # 机器可读
│   │   │   │       └── {session_id}.md     # 人类可读
│   │   │   └── kv_store/             # 智能体偏好设置
```

---

## 3. 上下文管理 (L1)

上下文管理模块充当**工作记忆的守门人**。它通过平衡相关性、时效性和 Token 预算，为 LLM 构建最优提示词 (Prompt)。

#### 3.1 两级上下文策略
1.  **即时上下文 (L1)**：由 `ContextBuilder` 管理。
    *   **组成部分**：系统提示词 (System Prompt) + 最近消息历史。
    *   **安全余量 (Safety Margin)**：预留上下文窗口的 **5-10%**，以应对分词器评估误差 (例如 GPT-4 与 Claude 的差异)。
    *   **截断策略**：当 `总 Token 数 > (最大上下文 - 回复保留 - 安全余量)` 时，截断优先级 1 的历史记录 (从旧到新)。

2.  **上下文卸载 (Context Offloading) (L4)**：由 `AgentRunner` 管理。
    *   **问题**：巨大的工具输出 (例如 `cat` 一个 500 行的文件) 会淹没上下文。
    *   **解决方案**：**带恢复功能的自动卸载**。
    *   **机制**：
        *   拦截器检查工具输出长度。
        *   如果 `长度 > 2000 字符`：
            1.  将内容保存到制品库 (`sys:ephemeral` 标签)。
            2.  **生成摘要**：提取前 500 字符 + 后 200 字符作为预览。
            3.  在 L1 中用富引用替换内容：
                `[输出过大 (15KB)。已保存为制品: {id}。预览: {summary}... 使用 read_artifact('{id}') 查看完整内容。]`
        *   **体验优势**：防止出现“悬空引用”，避免用户完全不知道被删除的制品包含什么内容。

#### 3.2 上下文构建逻辑 (伪代码)
```python
def build_context(self, system_prompt, history, model_limit):
    # 0. 安全余量 (95% 规则)
    safe_limit = int(model_limit * 0.95)
    
    # 1. 预留回复缓冲区
    budget = safe_limit - self.response_buffer
    
    # 2. 系统提示词 (优先级 0 - 始终包含)
    sys_tokens = self.count(system_prompt)
    remaining_budget = budget - sys_tokens
    if remaining_budget < 0:
        return [SystemMessage(system_prompt)] # 紧急回退
    
    # 3. 历史记录 (优先级 1 - 从最新开始的滑动窗口)
    selected_msgs = []
    for msg in reversed(history):
        msg_tokens = self.count(msg)
        if remaining_budget - msg_tokens < 0:
            break # 如果预览预算已满则停止
        selected_msgs.insert(0, msg)
        remaining_budget -= msg_tokens
        
    return [SystemMessage(system_prompt)] + selected_msgs
```

---

## 4. 垃圾回收 (维护)

为了确保系统的长期运行，采用严格的垃圾回收 (GC) 机制来清理上下文卸载过程中产生的临时数据。

### 4.1 制品生命周期
制品通过 `tags` 元数据进行分类，以决定其生命周期：

| 分类                  | 标签              | 来源                                 | 策略                                           |
| :-------------------- | :---------------- | :----------------------------------- | :--------------------------------------------- |
| **临时 (Ephemeral)**  | `sys:ephemeral`   | 自动卸载的工具输出 (日志、原始 HTML) | **一次性**。N 天后清理 (默认：3 天)。          |
| **持久 (Persistent)** | `user:persistent` | 用户/智能体创建的文件 (代码、报告)   | **永久**。系统永不自动删除，仅由用户手动决定。 |

#### 4.2 GC 架构 (两阶段清理)
为解决索引膨胀和僵尸文件问题，我们采用**两阶段策略**：

*   **阶段 1：元数据清理 (快速)**：
    *   **目标**：`artifacts.db` (SQLite)。
    *   **动作**：执行 `DELETE FROM artifacts WHERE tag='sys:ephemeral' AND created_at < ?`。
    *   **结果**：元数据立即移除，索引保持精简。
*   **阶段 2：孤立文件扫描 (深度清理)**：
    *   **目标**：`artifacts/blobs/` 中的物理文件。
    *   **触发频率**：较低 (例如每周)。
    *   **动作**：遍历所有文件。如果 `file_id` 不在 `artifacts.db` 中，则删除该文件。
    *   **优势**：清理阶段 1 崩溃或磁盘写入成功但数据库写入失败时留下的文件。

#### 4.3 进程安全
*   **数据库并发**：SQLite 采用 **WAL 模式**，允许并发执行多读一写。
*   **全局锁**：在 `.gc.lock` 上使用 `fcntl`，确保同一时间只有一个 **GC 进程** 运行 (阶段 2 是 I/O 密集型任务)。

---

## 5. 存储规范

### 5.1 目录结构 (混合策略：CAS + 日期布局)
```bash
.soloqueue/
├── state.db                          # 全局状态 (队列)
├── artifacts.db                      # 制品元数据索引 (SQLite)
├── .gc.lock                          # GC 进程锁
├── .gc_state                         # 上次 GC 执行的时间戳
├── artifacts/
│   └── blobs/                        # 内容寻址存储 (CAS)
│       └── 2026/02/08/               # 日期前缀 (时间局部性)
│           └── ab/cd/                # 哈希前缀分片
│               └── abcdef123...      # 完整 SHA256 哈希 (内容)
├── groups/
│   ├── {group_id}/
│   │   ├── sessions/                 # L2 日志
│   │   │   └── {agent_name}/
│   │   │       ├── {session_id}.jsonl
│   │   │       └── {session_id}.md
│   │   └── kv_store/                 # 智能体偏好设置
```

**设计逻辑 (混合方式)**：
- **日期前缀** (`2026/02/08/`)：支持高效的时间维度操作 (例如“删除早于 X 天的所有制品”)。
- **哈希分片** (`ab/cd/`)：防止单目录 Inode 耗尽 (Git 风格，支持数百万个文件)。
- **内容哈希文件名** (`abcdef123...`)：自动去重 (相同内容 = 相同文件)。

### 5.2 制品元数据架构 (SQLite 表)
```sql
CREATE TABLE artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,  -- 唯一元数据 ID
    content_hash TEXT NOT NULL,            -- SHA256 (链接到 blob 文件)
    group_id TEXT,
    title TEXT,
    tags TEXT,                             -- JSON 列表: ["sys:ephemeral"]
    author TEXT,
    created_at TIMESTAMP,
    path TEXT,                             -- 相对路径: artifacts/blobs/.../hash
    size INTEGER,
    mime TEXT
);
CREATE INDEX idx_content_hash ON artifacts(content_hash);
CREATE INDEX idx_tags ON artifacts(tags);
CREATE INDEX idx_created ON artifacts(created_at);
```

**架构设计逻辑**：
- **自增 ID (AUTO_INCREMENT)**：允许同一内容对应多个元数据条目 (例如不同 group 引用同一份日志)。
- **content_hash 索引**：快速执行去重检查和 Blob 查找。
- **tags 索引**：高效执行 GC 查询 (`WHERE tags LIKE '%sys:ephemeral%'`)。

---

## 6. 实施路线图

### 阶段 1：核心持久化 ✅
- [x] MemoryManager 与目录结构。
- [x] SessionLogger (JSONL/MD)。
- [x] 基础版 ArtifactStore。

### 阶段 2：生产环境强化 (进行中)
- [x] **步骤 2.1**：**ArtifactStore (SQLite)**：实现 CAS + 日期混合架构的 `artifacts.db`。
- [x] **步骤 2.2**：**GarbageCollector**：实现两阶段清理 (SQL + 孤立扫描)。
- [x] **步骤 2.3**：**ContextBuilder**：实现带 95% 安全余量的 Token 计数。
- [ ] **步骤 2.4**：**AgentRunner 集成**：
    - [ ] 带摘要生成的上下文卸载拦截器
    - [ ] `save_artifact` 与 `read_artifact` 工具
    - [ ] SessionLogger 集成

### 阶段 3：高级智能 (未来)
- [ ] 向量数据库集成 (L3)。
- [ ] 语义搜索工具。
- [ ] 自动化会话摘要。
