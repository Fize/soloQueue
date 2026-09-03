# 架构与设计

[English](../architecture.md) | 简体中文

本文档提供 SoloQueue 内部架构、进程边界、记忆引擎、任务路由及平台集成的技术概览。

---

## 1. 进程边界与分层架构

SoloQueue 采用本地优先架构，由 Go 后端服务、完整浏览器 Web Console（`web/`）和独立只读状态页（`status-ui/`）组成。`internal/assets/` 只嵌入两个前端，Skills 作为外部包安装在工作目录中。

```text
Web Console / 状态页
       │ HTTP + WebSocket
       ▼
HTTP 服务与回环 CORS (internal/server)
       │
       ▼
Session Manager (internal/session)
       │
       ├── Agent Actor 循环与 Supervisor (internal/agent)
       │       ├── 任务路由与 Model Client (internal/router, internal/llm)
       │       ├── 原生工具、Skills、MCP/LSP (internal/agenttools)
       │       └── 确定性工具安全检查
       ├── Cron 与 Simulation 运行时 (internal/cron, internal/simulation)
       ├── 渠道桥接 (internal/channel/qq, internal/channel/wechat)
       └── 记忆、时间线、SQLite 数据库与日志 (internal/infra, internal/memory)
```

启动时服务构建统一的依赖容器（`runtime.Stack`），并将 LLM 客户端、工具注册表、记忆引擎、SQLite 数据库及渠道句柄注入到 Session Manager 和 HTTP 路由中。

---

## 2. 任务路由 (`internal/router`)

Prompt 被分类为工作性质类别（`general`、`engineering`、`research`），以选择最佳的模型配置：

1. **本地快速分类规则**：根据明确的结构特征（代码块、Stack Traceback、路径引用、终端命令）进行快速匹配。
2. **LLM Classifier 回退**：若特征匹配不确定，调用轻量级 LLM 进行分类。
3. **会话上下文连续性**：后续请求保留上一轮的任务分类上下文，避免对话过程中出现模型路由突变。

---

## 3. 上下文窗口与压缩 (`internal/memory/ctxwin`)

上下文管理器在保护上下文预算的同时保留关键信息：

- **Token 计数**：使用模型匹配的 Tokenizer 计算 Payload 大小。
- **双水位线压缩**：当 Token 消耗突破高水位线时，触发历史 Turns 的摘要生成。
- **Payload 修复与过滤**：在发送给外部 LLM API 之前自动清理孤立的 Tool-call/result 配对，避免触发 HTTP 400 错误。

---

## 4. 记忆子系统 (`internal/memory`)

SoloQueue 将短期上下文与长期搜索和审计日志分离开来：

- **短期对话记忆 (`internal/memory/conversation`)**：保存上下文压缩过程中生成的 LLM 驱动对话摘要。
- **长期记忆 (`internal/memory/engine`)**：纯 Go 实现的混合搜索引擎，结合 SQLite FTS5 BM25 全文检索与内存知识图谱。配置外部向量 Provider 时可融合向量检索（默认关闭，零外部依赖）。
- **时间线 (`internal/memory/timeline`)**：追加式 JSONL 事件流，记录具体的工具调用、会话状态变更、路由结果及 Agent 委派事件。过滤系统 Prompt 以保护隐私。

---

## 5. 渠道集成架构 (`internal/channel`)

渠道桥接器将外部消息协议规范化为统一的内部会话事件流：

- **QQ Bot (`internal/channel/qq`)**：实现腾讯 Bot Gateway 协议，处理被动回复窗口并维护主动发送限流队列。
- **微信 iLink (`internal/channel/wechat`)**：通过腾讯官方 iLink Bot API 连接，支持长轮询更新流、二维码配对及 typing 状态保持。
