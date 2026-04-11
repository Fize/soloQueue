# SoloQueue - AI 多智能体协作桌面应用

---

# 第一部分：整体架构

## 产品概述

SoloQueue 是一个基于 Actor 模型的 AI 多智能体协作桌面应用。每个 Agent 作为独立 Actor 运行，通过异步消息通信，支持动态创建/销毁、监督策略、状态持久化。

## 核心特性

- **Actor 模型架构**: 每个 Agent 是独立 Actor，无共享状态，通过消息通信
- **动态 Actor 池**: 运行时按需创建/销毁 Actor 实例，支持监督树
- **消息驱动**: 所有交互通过异步消息传递，支持请求-响应和发布-订阅模式
- **状态机驱动**: 使用 XState v5 定义 Agent 生命周期状态机
- **SQLite 持久化**: Actor 快照、消息日志、配置统一存储
- **流式 LLM**: 基于 Vercel AI SDK 的流式 AI 调用
- **桌面应用**: Tauri v2 轻量级跨平台桌面应用

## 用户场景

1. 用户创建一个 Team，定义多个 Agent（Actor）
2. 用户发起对话，Leader Actor 接收消息
3. Leader Actor 可以向子 Actor 发送任务消息（委托）
4. 子 Actor 处理完成后回复结果消息
5. 支持并行委托：同时向多个 Actor 发送消息
6. 所有 Actor 状态自动持久化到 SQLite

## 技术栈

| 层级 | 技术 | 版本 | 说明 |
| --- | --- | --- | --- |
| **桌面框架** | Tauri | v2.10.3 | Rust 核心，轻量安全 |
| **前端框架** | React | 19.2.4 | UI 组件化 |
| **状态管理** | XState | v5.x | Actor 系统 + 状态机 |
| **后端框架** | Fastify | 5.8.2 | HTTP/WebSocket 服务 |
| **LLM SDK** | Vercel AI SDK | 5.0.52 | 统一 LLM 接口 |
| **数据库** | better-sqlite3 | 11.x | 同步 SQLite API |
| **类型验证** | Zod | 4.3.6 | Schema 验证 |
| **CSS 框架** | TailwindCSS | v4.0 | 原子化 CSS |
| **UI 组件** | shadcn/ui | latest | 可定制组件库 |
| **构建工具** | Vite | 6.x | 快速构建 |

## Actor 系统架构

```
┌─────────────────────────────────────────────────────────────────┐
│                      Actor System                               │
│  ┌─────────────┐                                                 │
│  │ Supervisor  │ 监督者                                          │
│  └──────┬──────┘                                                 │
│         │                                                        │
│  ┌──────┴──────┬────────────────┬────────────────┐               │
│  │             │                │                │               │
│  ▼             ▼                ▼                ▼               │
│ Leader      Worker1          Worker2          WorkerN           │
│ Actor       Actor            Actor            Actor             │
│                                                        ┌────────┴────────┐
│  ┌──────────────────────────────────────────────────┐  │  System Actors   │
│  │              Agent Actors Pool                   │  │  ┌────────────┐  │
│  └──────────────────────────────────────────────────┘  │  │   Router   │  │
│                                                         │  │   Logger   │  │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐        │  │  Persister │  │
│  │  Message   │  │  Message   │  │  Message   │        │  └────────────┘  │
│  │   Queue    │  │   Queue    │  │   Queue    │        └────────┬────────┘
│  │    M1      │  │    M2      │  │    M3      │                 │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘                 │
│        │               │               │                        │
└────────┼───────────────┼───────────────┼────────────────────────┘
         │               │               │
         ▼               ▼               ▼
       Leader         Worker1         WorkerN
```

## Agent Actor 生命周期

```
  [*] ──────> Idle ──────> Processing ──────> WaitingLLM
                  │              │                │
                  │              │                ▼
                  │              │           Processing
                  │              │                │
                  │              ▼                │
                  │         Delegating            │
                  │              │                │
                  │              ▼                │
                  │         WaitingChild          │
                  │              │                │
                  │              ▼                │
                  └──────> Responding <───────────┘
                              │
                              ▼
                          Idle
```

## 消息流

```
用户 ──> 前端 ──> Router Actor ──> Leader Actor ──> Worker Actors (并行)
                      │                │                │
                      ▼                ▼                ▼
                  Persister        SQLite            结果回复
```

## 目录结构

```text
soloqueue/
├── src/                           # React 前端
│   ├── actors/                    # 前端 Actor
│   │   ├── ui-actor.ts           # UI 状态 Actor
│   │   └── connection-actor.ts   # WebSocket 连接 Actor
│   ├── components/
│   │   ├── ui/                   # shadcn/ui 组件
│   │   ├── chat/                 # 聊天界面
│   │   ├── agent/                # Agent 可视化
│   │   └── workspace/            # 工作区管理
│   ├── hooks/
│   ├── lib/
│   └── App.tsx
│
├── server/                        # 后端服务
│   ├── actors/                    # Actor 系统
│   │   ├── system.ts             # Actor 系统初始化
│   │   ├── supervisor.ts         # 监督者 Actor
│   │   ├── agent-actor.ts        # Agent Actor 定义
│   │   ├── router-actor.ts       # 消息路由 Actor
│   │   ├── logger-actor.ts       # 日志 Actor
│   │   └── persister-actor.ts    # 持久化 Actor
│   ├── machines/                  # XState 状态机
│   │   ├── agent-machine.ts      # Agent 生命周期状态机
│   │   ├── llm-machine.ts        # LLM 调用状态机
│   │   └── task-machine.ts       # 任务处理状态机
│   ├── messages/                  # 消息定义
│   │   ├── types.ts              # 消息类型定义
│   │   └── schemas.ts            # Zod Schema
│   ├── llm/                       # LLM 集成
│   │   ├── provider.ts           # AI SDK Provider
│   │   └── tools.ts              # 工具定义
│   ├── storage/                   # 存储层
│   │   ├── sqlite.ts             # SQLite 初始化
│   │   ├── actor-store.ts        # Actor 状态存储
│   │   ├── message-store.ts      # 消息日志存储
│   │   └── config-store.ts        # 配置存储
│   ├── routes/
│   └── websocket/
│
├── src-tauri/                     # Tauri Rust
│   ├── src/
│   │   ├── main.rs
│   │   └── commands.rs
│   └── Cargo.toml
│
├── config/                        # 配置文件
│   ├── agents/
│   ├── teams/
│   └── skills/
│
├── package.json
├── tsconfig.json
└── vite.config.ts
```

## 核心接口定义

### Actor 消息类型

```typescript
// server/messages/types.ts
export type ActorMessage =
  | { type: 'task'; from: string; content: string; taskId: string }
  | { type: 'delegate'; to: string; instruction: string; taskId: string }
  | { type: 'result'; taskId: string; data: string; from: string }
  | { type: 'error'; taskId: string; error: string }
  | { type: 'persist'; snapshot: ActorSnapshot }
  | { type: 'stop'; reason?: string };

export interface ActorSnapshot {
  id: string;
  name: string;
  state: string;
  context: Record<string, unknown>;
  mailbox: ActorMessage[];
  createdAt: number;
  updatedAt: number;
}
```

### Agent Actor 状态机

```typescript
// server/machines/agent-machine.ts
import { setup, assign } from 'xstate';

export const agentMachine = setup({
  types: {
    context: {} as {
      messages: Message[];
      currentTask: Task | null;
      children: Map<string, ActorRef>;
    },
    events: {} as ActorMessage
  },
  actions: {
    processMessage: assign({
      messages: ({ context, event }) => [...context.messages, event]
    }),
    addChild: assign({
      children: ({ context, event }) => {
        const newChildren = new Map(context.children);
        newChildren.set(event.taskId, event.childRef);
        return newChildren;
      }
    })
  }
}).createMachine({
  id: 'agent',
  initial: 'idle',
  states: {
    idle: { on: { task: 'processing' } },
    processing: {
      entry: 'processMessage',
      on: { delegate: 'delegating', result: 'responding' }
    },
    delegating: {
      entry: 'createChildActor',
      on: { result: 'processing' }
    },
    responding: {
      entry: 'sendResult',
      after: { 100: 'idle' }
    }
  }
});
```

### Actor 系统

```typescript
// server/actors/system.ts
export class ActorSystem {
  private actors: Map<string, ActorRef> = new Map();
  private supervisor: ActorRef;

  constructor(private db: Database) {
    this.supervisor = createActor(supervisorMachine);
    this.supervisor.start();
  }

  spawnAgent(name: string, config: AgentConfig): ActorRef { ... }
  stop(name: string) { ... }
  dispatch(target: string, message: ActorMessage) { ... }
}
```

## 性能策略

| 场景 | 策略 |
| --- | --- |
| Actor 创建 | 惰性初始化 + 对象池复用 |
| 消息传递 | 异步非阻塞，避免 Actor 阻塞 |
| 状态持久化 | 批量写入 + Write-Ahead Log |
| LLM 调用 | 流式响应，逐块发送给前端 |
| Actor 快照 | 定期 checkpoint + 增量保存 |
| SQLite 并发 | WAL 模式，读写分离 |

## 监督策略

- **One-For-One**: 单个 Actor 失败，只重启该 Actor
- **One-For-All**: 一个失败，重启所有子 Actor
- **Resume**: 保持状态，继续处理
- **Stop**: 停止 Actor，不重启

## 设计风格

采用 **Cyberpunk Dark Theme**，融合玻璃拟态效果，突出 AI/科技感。

## 页面规划

### 1. Dashboard 首页

- 顶部固定导航栏：Logo、搜索框、新建按钮、设置
- 左侧边栏：Teams 列表、快速导航
- 主内容区：统计卡片、Actor 活力图、最近会话、系统日志流

### 2. Chat 对话页

- 左侧：会话历史列表 + Actor 选择器
- 中间：消息流区域（用户消息、Agent 思考、工具调用卡片）
- 右侧：当前 Team Actor 状态面板
- 底部：输入框 + 目标 Actor 选择

### 3. Actor 管理页

- Actor 列表（卡片视图）、状态指示灯、状态机可视化
- 配置表单、消息历史、子 Actor 关系图

### 4. Settings 设置页

- LLM 配置、Actor 系统配置、监督策略、日志级别、数据库路径

---

# 第二部分：工作顺序

## Phase 1: 项目初始化

- [ ] T001 初始化 Tauri v2 项目 (`npm create tauri-app@latest`)
- [ ] T002 配置 React 19 + TypeScript + Vite
- [ ] T003 安装核心依赖 (XState v5, Fastify, Vercel AI SDK, better-sqlite3, Zod, shadcn/ui, TailwindCSS v4)
- [ ] T004 配置 ESLint + Prettier
- [ ] T005 验证 Tauri 桌面窗口运行

## Phase 2: 日志系统 [MVP]

- [x] T010 设计日志架构（Console + File 双通道）
- [x] T011 定义日志级别（DEBUG/INFO/WARN/ERROR）
- [x] T012 配置 Loguru + 结构化 JSONL 格式
- [x] T013 实现 Actor 消息追踪日志
- [x] T014 实现 LLM 调用日志
- [x] T015 WebSocket 通信日志
- [x] T016 日志轮转配置

## Phase 3: 存储层 (DAO) [MVP]

- [x] T020 SQLite 数据库初始化 (`server/storage/db.ts`)
- [x] T021 设计数据库 schema 框架（预留扩展表）
- [x] T022 实现基础 Repository 模式 (`server/storage/repositories/base.repository.ts`)
- [x] T023 实现 Team Repository (`server/storage/repositories/team.repository.ts`)
- [x] T024 实现 Config Repository (`server/storage/repositories/config.repository.ts`)
- [x] T025 实现 Agent Repository (`server/storage/repositories/agent.repository.ts`)
- [x] T026 数据库迁移机制 (`server/storage/migrations.ts`)

> **说明**: 日志文件由日志系统管理，不走 DAO 层

## Phase 4: 配置系统

- [x] T030 设计配置系统架构
- [x] T031 实现 ConfigService (`server/storage/config.service.ts`)
- [x] T032 实现 AgentService (`server/storage/agent.service.ts`)
- [x] T033 配置热加载和变更通知
- [x] T034 预留 Agent 扩展字段 (skills, mcp, hooks)

## Phase 5: 默认配置

- [x] T040 App 配置 (`app.theme`, `app.language`)
- [x] T041 Session 配置 (`session.timeout`, `session.maxHistory`, `session.autoSave`)
- [x] T042 默认团队初始化 (`teams` 表)
- [x] T043 默认 Agent 模型配置

## Phase 6: 状态机

- [x] T050 创建 Agent 状态机 (`server/machines/agent-machine.ts`)
- [x] T051 创建 LLM 调用状态机 (`server/machines/llm-machine.ts`)
- [x] T052 创建任务处理状态机 (`server/machines/task-machine.ts`)

## Phase 7: Actor 系统核心

- [ ] T060 创建 ActorSystem 类 (`server/actors/system.ts`)
- [ ] T061 实现 Supervisor Actor (`server/actors/supervisor.ts`)
- [ ] T062 实现 Agent Actor (`server/actors/agent-actor.ts`)
- [ ] T063 实现 Router Actor (`server/actors/router-actor.ts`)
- [ ] T064 实现 Logger Actor (`server/actors/logger-actor.ts`)
- [ ] T065 实现 Persister Actor (`server/actors/persister-actor.ts`)

## Phase 8: LLM 集成

- [ ] T070 配置 AI SDK Provider (`server/llm/provider.ts`)
- [ ] T071 定义工具调用 (`server/llm/tools.ts`)
- [ ] T072 实现流式响应处理

## Phase 9: 后端 API 层

- [ ] T080 Fastify 路由配置
- [ ] T081 WebSocket 端点 (`/ws/chat`)
- [ ] T082 REST API 端点

## Phase 10: 前端 UI 组件

- [ ] T090 配置 TailwindCSS v4 + 主题
- [ ] T091 安装 shadcn/ui 组件库
- [ ] T092 创建布局组件 (导航栏、侧边栏)

## Phase 11: 前端页面

- [ ] T100 Dashboard 首页
- [ ] T101 Chat 对话页
- [ ] T102 Actor 管理页
- [ ] T103 Settings 设置页

## Phase 12: 安全与监督

- [ ] T110 实现监督策略
- [ ] T111 文件写入审批机制
- [ ] T112 沙箱隔离

## Phase 13: 性能优化

- [ ] T120 Actor 对象池
- [ ] T121 批量写入 + WAL
- [ ] T122 Actor 快照机制
- [ ] T123 增量持久化

## Phase 14: 文档与测试

- [ ] T130 用户使用文档
- [ ] T131 API 文档
- [ ] T132 单元测试
- [ ] T133 集成测试
- [ ] T134 E2E 测试

---

## 任务标注说明

- **[P]**: 可并行运行（不同文件，无依赖）
- **[MVP]**: 最小可行产品必须项

## 执行顺序

1. **Phase 1 → 14 按顺序执行**
2. Phase 5-9 可部分并行（不同模块）
3. Phase 10-11 依赖后端完成
4. Phase 12-14 最后执行

**MVP 核心闭环**: Phase 1 → 2（日志）→ 3（DAO）→ 5（消息）→ 6（状态机）→ 7（Actor）

**设计原则**: 
- 日志 → 文件（JSONL）
- 业务数据 → SQLite（DAO）
- DAO 层先行，后续按需扩展表定义
