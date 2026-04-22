# SoloQueue - AI 多智能体协作应用

---

# 第一部分：整体架构

## 产品概述

SoloQueue 是一个基于 Actor 模型的 AI 多智能体协作应用。每个 Agent 作为独立 Actor 运行，通过异步消息通信，支持动态创建/销毁、监督策略、状态持久化。

## 核心特性

- **Actor 模型架构**: 每个 Agent 是独立 Actor，无共享状态，通过消息通信
- **Go 后端**: 高性能 Go 语言后端，提供 REST + WebSocket 接口
- **流式 LLM**: 基于 DeepSeek 的流式 AI 调用
- **桌面 + Web**: Electron 桌面端 + Web 端双模式
- **配置热加载**: 运行时配置变更自动生效
- **沙箱工具**: 文件读写、Shell 执行、HTTP 请求等工具均在沙箱内运行

## 用户场景

1. 用户通过 Web 或 Electron 桌面端访问 SoloQueue
2. 创建会话（Session），与 AI Agent 进行对话
3. Agent 可调用工具（文件读写、Shell 执行、HTTP 请求等）完成任务
4. 支持流式响应，实时查看 Agent 的思考和执行过程
5. 所有对话历史自动持久化

## 技术栈

| 层级 | 技术 | 说明 |
| --- | --- | --- |
| **后端语言** | Go | 高性能、并发友好 |
| **后端框架** | net/http + cobra | HTTP 服务 + CLI 命令 |
| **LLM 客户端** | DeepSeek | 流式 AI 调用 |
| **前端框架** | React 19 | UI 组件化 |
| **CSS 框架** | TailwindCSS v4 | 原子化 CSS |
| **构建工具** | Vite | 快速构建 |
| **桌面框架** | Electron | 跨平台桌面应用 |

## 系统架构

```
┌─────────────────────────────────────────────────────┐
│                 Frontend (React)                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐          │
│  │ Dashboard │  │   Chat   │  │ Settings │          │
│  └─────┬────┘  └─────┬────┘  └─────┬────┘          │
│        └──────────────┼─────────────┘               │
│                       │ REST / WebSocket             │
└───────────────────────┼─────────────────────────────┘
                        │
┌───────────────────────┼─────────────────────────────┐
│                 Backend (Go)                         │
│                       │                              │
│  ┌────────────────────┴────────────────────┐        │
│  │           HTTP Server (net/http)         │        │
│  │  POST /v1/sessions                      │        │
│  │  DELETE /v1/sessions/{id}               │        │
│  │  GET /v1/sessions/{id}/history          │        │
│  │  GET /v1/sessions/{id}/stream (WS)      │        │
│  └────────────────────┬────────────────────┘        │
│                       │                              │
│  ┌──────────────┐  ┌──┴───┐  ┌──────────────┐      │
│  │ Session Mgr  │  │Agent │  │   Tools      │      │
│  │              │  │      │  │ - file_read  │      │
│  │ - Create     │  │ - LLM│  │ - write_file │      │
│  │ - Delete     │  │ -    │  │ - replace    │      │
│  │ - History    │  │ Tool │  │ - grep       │      │
│  │ - Reap       │  │ Loop │  │ - glob       │      │
│  └──────────────┘  └──────┘  │ - shell_exec │      │
│                                │ - http_fetch  │      │
│  ┌──────────────┐  ┌───────┐  │ - web_search  │      │
│  │   Config     │  │Logger │  └──────────────┘      │
│  │ (hot-reload) │  │(JSONL)│                         │
│  └──────────────┘  └───────┘                         │
└──────────────────────────────────────────────────────┘
```

## 目录结构

```text
soloqueue/
├── backend/                       # Go 后端
│   ├── cmd/soloqueue/main.go     # 入口（serve / version 命令）
│   └── internal/
│       ├── agent/                 # Agent 核心（LLM 调用 + Tool Loop）
│       ├── config/                # 配置系统（热加载）
│       ├── llm/                   # LLM 客户端（DeepSeek）
│       ├── logger/                # 日志系统（JSONL + 轮转）
│       ├── server/                # HTTP/WS 路由
│       ├── session/               # 会话管理
│       └── tools/                 # 工具集（文件、Shell、HTTP 等）
│
├── frontend/                      # React 前端
│   ├── src/
│   │   ├── components/
│   │   │   ├── ui/                # 基础 UI 组件
│   │   │   ├── chat/              # 聊天界面
│   │   │   ├── agent/             # Agent 可视化
│   │   │   └── workspace/        # 工作区管理
│   │   ├── hooks/
│   │   ├── lib/
│   │   └── App.tsx
│   ├── public/                    # 静态资源
│   ├── package.json               # 前端依赖
│   ├── vite.config.ts             # Vite 配置
│   ├── tsconfig.json              # TypeScript 配置
│   └── eslint.config.ts           # ESLint 配置
```

## 性能策略

| 场景 | 策略 |
| --- | --- |
| LLM 调用 | 流式响应，逐块发送给前端 |
| 配置热加载 | fsnotify 监听 + 回调通知 |
| 日志轮转 | 按大小/日期自动轮转 |
| Session 回收 | 定期清理过期 Session |
| 工具超时 | 每个工具独立超时控制 |

---

# 第二部分：工作顺序

## Phase 1: 后端核心 ✅

- [x] T001 Go 后端项目初始化（cobra CLI + net/http）
- [x] T002 配置系统（settings.json + 热加载）
- [x] T003 日志系统（JSONL + 轮转 + 分层）
- [x] T004 LLM 客户端（DeepSeek + SSE 流式 + 重试）
- [x] T005 Agent 核心（Tool Loop + 流式事件）
- [x] T006 工具集（file_read, write_file, replace, grep, glob, shell_exec, http_fetch, web_search）
- [x] T007 Session 管理（创建/删除/历史/回收）
- [x] T008 HTTP/WS 服务（REST + WebSocket 流式）

## Phase 2: 前端 UI

- [ ] T010 配置 TailwindCSS v4 + Cyberpunk 主题
- [ ] T011 安装 shadcn/ui 组件库
- [ ] T012 创建布局组件（导航栏、侧边栏）
- [ ] T013 实现前端 WebSocket 连接管理

## Phase 3: 前端页面

- [ ] T020 Dashboard 首页
- [ ] T021 Chat 对话页（流式消息 + 工具调用展示）
- [ ] T022 Agent 管理页
- [ ] T023 Settings 设置页

## Phase 4: Electron 桌面端

- [ ] T030 集成 Electron
- [ ] T031 配置双模式构建（Web + Electron）
- [ ] T032 系统托盘与原生功能

## Phase 5: 安全与优化

- [ ] T040 文件写入审批机制
- [ ] T041 沙箱隔离增强
- [ ] T042 Agent 并行工具调用优化
- [ ] T043 Session 快照与恢复

## Phase 6: 文档与测试

- [ ] T050 用户使用文档
- [ ] T051 API 文档
- [ ] T052 后端单元测试完善
- [ ] T053 前端组件测试
- [ ] T054 E2E 测试

---

## 任务标注说明

- **[P]**: 可并行运行（不同文件，无依赖）
- **[MVP]**: 最小可行产品必须项

## 执行顺序

1. Phase 1 ✅ 已完成
2. Phase 2 → 3 前端开发
3. Phase 4 Electron 桌面端
4. Phase 5 安全与优化
5. Phase 6 文档与测试

**MVP 核心闭环**: Phase 1（后端 ✅）→ Phase 2（前端 UI）→ Phase 3（页面）

**设计原则**: 
- 日志 → 文件（JSONL）
- 配置 → settings.json（热加载）
- Go 后端专注核心逻辑，前端专注交互体验
