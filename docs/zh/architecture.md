# 架构与设计

[English](../architecture.md) | 简体中文

本文档提供 SoloQueue 内部架构、进程边界、记忆引擎、任务路由及平台集成的技术概览。

本文描述的 `main` 不包含模拟功能。剥离前的仓库状态保留在
`experimental/simulation` 分支。已有的 `simulation.db` 文件及其 `-wal` 或
`-shm` 附属文件会保留，但 `main` 不会打开或初始化它们。运行该分支时应使用独立的
`SOLOQUEUE_WORK_DIR`，避免其设置影响 `main` 使用的工作目录。

---

## 1. 进程边界与分层架构

SoloQueue 由 Go 后端服务、浏览器 Web Console（`web/`）和独立只读状态页
（`status-ui/`）组成。`internal/assets/` 嵌入两个前端的浏览器资源，Skills
作为外部包安装在工作目录中。

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
       ├── Cron 运行时 (internal/cron)
       ├── 渠道桥接 (internal/channel/qq, internal/channel/wechat, internal/channel/telegram)
       └── 记忆、时间线、SQLite 数据库与日志 (internal/infra, internal/memory)
```

启动时服务构建统一的依赖容器（`runtime.Stack`），并将 LLM 客户端、工具注册表、记忆引擎、SQLite 数据库及渠道句柄注入到 Session Manager 和 HTTP 路由中。

---

## 2. 任务路由 (`internal/router`)

Prompt 被分类为工作性质类别（`general`、`engineering`、`research`），并映射到已配置的模型路由：

1. **本地快速分类规则**：匹配代码块、Stack Traceback、路径引用和终端命令等结构特征。
2. **LLM Classifier 回退**：若特征匹配不确定，调用已配置的 Classifier 模型进行分类。
3. **会话上下文连续性**：分类后续请求时会传入上一轮的任务类别。

---

## 3. 上下文窗口与压缩 (`internal/memory/ctxwin`)

上下文管理器统计 Payload Token，并在达到配置阈值时压缩历史内容：

- **Token 计数**：使用模型匹配的 Tokenizer 计算 Payload 大小。
- **双水位线压缩**：当 Token 消耗突破高水位线时，触发历史 Turns 的摘要生成。
- **Payload 修复与过滤**：在发送给外部 LLM API 之前清理孤立的 Tool-call/result 配对。

---

## 4. 记忆子系统 (`internal/memory`)

SoloQueue 将短期上下文与长期搜索和审计日志分离开来：

- **短期对话记忆 (`internal/memory/conversation`)**：保存上下文压缩过程中生成的 LLM 驱动对话摘要。
- **长期记忆 (`internal/memory/engine`)**：纯 Go 实现的混合搜索引擎，结合 SQLite FTS5 BM25 全文检索与内存知识图谱。配置外部 Embedding Provider 时启用向量检索，默认配置为关闭。
- **时间线 (`internal/memory/timeline`)**：追加式 JSONL 事件流，记录工具调用、会话状态变更、路由结果及 Agent 委派事件。系统 Prompt 不写入时间线。

---

## 5. 渠道集成架构 (`internal/channel`)

渠道桥接器将外部消息协议规范化为统一的内部会话事件流：

- **QQ Bot (`internal/channel/qq`)**：实现腾讯 Bot Gateway 协议，处理被动回复窗口并维护主动发送限流队列。
- **微信 iLink (`internal/channel/wechat`)**：通过腾讯官方 iLink Bot API 连接，支持长轮询更新流、二维码配对及 typing 状态保持。
- **Telegram (`internal/channel/telegram`)**：连接 Telegram Bot API，使用长轮询接收更新，并支持文本和媒体投递。
