# 架构概览

[English: Architecture overview](../../architecture/overview.md)

我把 SoloQueue 构建成由 Go 运行时、React 桌面控制台和嵌入 Go 二进制的轻量
React Portal 组成的本地优先应用。

~~~text
Desktop / Portal
       │ HTTP + WebSocket
       ▼
HTTP Server 与认证边界
       │
       ▼
Session Manager
       │
       ├── Agent Actor 与 Supervisor
       │       ├── Task Router 与 Model Client
       │       ├── Native Tools、Skills、MCP/LSP
       │       └── 确认与项目范围
       ├── Workflow、Cron、Simulation
       ├── QQ / 微信渠道桥接
       └── Memory、Timeline、SQLite、日志
~~~

## 进程边界

我通过 soloqueue serve 启动服务端。启动时会构建一个 Runtime Stack，并把
LLM Client、配置服务、Team Store、Skill Registry、MCP Manager、Memory、
Workflow 和 Simulation 共享给 Session Manager 与 HTTP Handler。

我默认监听回环地址，让远程请求经过认证中间件，并有意让 localhost 使用不同路径。
详见[远程访问](../operations/remote-access.md)。

## 运行时分层

| 层 | 当前包区域 | 职责 |
| --- | --- | --- |
| Transport | internal/server | REST、WebSocket、认证和静态 Portal |
| Session | internal/session | 对话状态、请求串行化和历史 |
| Agent | internal/agent | Actor 生命周期、Mailbox、流式输出和委派 |
| Routing | internal/router、internal/tasktype | general/engineering/research 分类和模型选择 |
| Capability | internal/agenttools | Native Tool、Skill、MCP 和 LSP |
| Prompt/Team | internal/prompt、internal/team | Prompt 组装、Agent 模板和 Team 持久化 |
| Memory | internal/memory | 上下文压缩、摘要、长期搜索和 Timeline |
| Runtime | internal/runtime | 依赖构建和热加载 |
| Automation | internal/cron、internal/workflow、internal/simulation | 定时任务、YAML DAG 和 Simulation |
| Channel | internal/channel | QQ 与微信的消息契约 |

## Frontend

- desktop/ 是用于本地或远程连接的 Electron + React 控制台。
- portal/ 是构建到 internal/server/dist/ 并由 Go 服务的轻量 React Portal。

我让两个前端使用同一套后端契约。桌面开发服务默认期待 8765 端口的后端，
嵌入式 Portal 默认使用 57647。
