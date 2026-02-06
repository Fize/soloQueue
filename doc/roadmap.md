# SoloQueue 未来路线图 (Roadmap)

本文档规划了 SoloQueue 项目在 MVP 之后的演进方向，旨在将项目从一个基础的原型提升为成熟的、生产级的多 Agent 协作平台。

## 第一阶段：记忆持久化 (Memory Persistence)
**目标**：赋予 Agent 长期记忆和断点续运能力，使其能够处理长时间跨度的任务而不丢失上下文。

### 1.1 混合记忆架构 (Hybrid Memory Architecture)
*   **状态持久化 (State Persistence)**:
    *   使用 `SQLite` + `LangGraph Checkpointer`。
    *   **功能**：保存精确的程序状态（调用栈、变量、下一步操作）。
    *   **效果**：实现“硬中断恢复”，进程崩溃后可无损重启。
*   **情景记忆 (Episodic Memory)**:
    *   使用 **Markdown 日志**，按天、按 Agent 隔离存储。
    *   **功能**：Agent 启动前读取自己的今日日志。
    *   **效果**：避免重复劳动，Agent 知道自己“今天做了什么”。
*   **用户记忆 (User Memory)**:
    *   使用 `MEMORY.md`。
    *   **功能**：用户手动维护的全局指令（如 API Key、偏好）。

### 1.2 协作工件共享 (Artifact Sharing) - 持久化设计
*   **文件级存储**：工件写入 `.soloqueue/artifacts/active/` 目录，不驻留内存。
*   **按日归档**：每日或会话结束时，自动将工件迁移到 `archive/YYYY-MM-DD/`。
*   **RAG 长期记忆**：
    *   根据策略（时间/大小/重要性）触发。
    *   将归档工件通过 Embedding 模型转为向量，存入 `rag/vectors.db`。
    *   Agent 可通过语义搜索查询历史工件（如"上次关于茅台的分析"）。

---

## 第二阶段：并行执行 (Parallel Execution)
**目标**：打破串行执行的瓶颈，显著提升复杂任务的处理效率。

### 2.1 Map-Reduce 模式
*   **Leader** 可以同时向多个 Sub-Agent (如 `FundamentalAnalyst` 和 `TechnicalAnalyst`) 委派任务。
*   **并发执行**：基于 `asyncio` 实现真正的非阻塞并发。
*   **结果聚合**：Leader 等待所有 Sub-Agent 返回结果后，进行综合分析（Reduce）。

### 2.2 异步工具调用
*   允许 Agent 在等待长时工具（如深度搜索、复杂计算）返回时，挂起或处理其他任务。

---

## 第三阶段：工具增强 (Advanced Tooling)
**目标**：用专业级数据源替代简易工具，提升分析的深度和可信度。

### 3.1 金融数据增强
*   接入 **Yahoo Finance / Alpha Vantage / Bloomberg API**。
*   提供实时行情、历史K线、财务报表等结构化数据，而非依赖模糊的网页搜索。

### 3.2 浏览器增强
*   集成 **Headless Browser (如 Playwright)**。
*   支持抓取动态渲染的网页（SPA），支持截图和视觉分析。

### 3.3 代码执行沙箱
*   从本地执行转向 **Docker 沙箱** 执行 Python 代码，提高安全性。

---

## 第四阶段：用户界面 (Web UI)
**目标**：降低使用门槛，提供更直观的交互和监控体验。

### 4.1 交互式前端
*   使用 **Streamlit** 或 **Next.js** 构建 Web 界面。
*   **功能**：
    *   聊天窗口：与 Leader 对话。
    *   拓扑视图：实时可视化 Agent 之间的委派关系（类似 LangGraph Studio）。
    *   状态监控：查看当前运行的 Agent、调用栈和 Token 消耗。

### 4.2 人在回路 (Human-in-the-Loop) 增强
*   在 Web 界面上实现更友好的审批流（Approval Workflow）。
*   允许用户在任务执行中途暂停、修改变量或直接干预。
