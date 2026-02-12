# SoloQueue Web UI 设计文档 (轻量级版)

**Version**: 3.0  
**Status**: Design  
**Date**: 2026-02-10

---

## 1. 概述

### 1.1 目标

构建一个轻量级、零构建、易于部署的 Web 界面，作为 SoloQueue 的**辅助控制台**。
核心功能：
- **配置管理**: 可视化查看和编辑 Team/Agent/Skill 配置
- **状态监控**: 实时查看 Agent 运行状态、Token 消耗
- **调试聊天**: 提供内置聊天窗口用于调试 Agent 行为
- **组织可视化**: 展示团队和 Agent 的拓扑关系

### 1.2 技术选型 (Python Native)

| 层级          | 技术                | 理由                                      |
| ------------- | ------------------- | ----------------------------------------- |
| **后端**      | FastAPI             | 高性能、异步、WebSocket 支持              |
| **模板引擎**  | Jinja2              | Python 原生，服务端渲染 (SSR)             |
| **UI 框架**   | Bootstrap 5 (CDN)   | 成熟、响应式、无需构建                    |
| **交互逻辑**  | Alpine.js (CDN)     | 轻量级 (15KB)，声明式交互，替代 Vue/React |
| **图标库**    | Bootstrap Icons     | 即使                                      |
| **图表/拓扑** | Vis.js / Mermaid.js | 轻量级拓扑图可视化                        |
| **实时通信**  | WebSocket           | 原生 JS 实现                              |

**核心优势**: 
- **零 Node.js 依赖**: 用户无需安装 npm/yarn
- **单文件部署**: 所有静态资源可打包在 Python 包内或通过 CDN 加载
- **启动即用**: `soloqueue serve` 即可启动

---

## 2. 核心概念与模型

*(与后端代码完全对齐)*

### 2.1 目录结构映射

```
config/
  ├── groups/
  │   └── {team_id}.md           → 团队配置 (Frontmatter: name, description)
  │                                (正文: shared_context 自动注入)
  ├── agents/
  │   └── {agent_name}.md        → Agent 配置
  └── skills/
      └── {skill_id}/SKILL.md    → Skill 配置
```

### 2.2 Agent 模型 (Web UI 展示字段)

```yaml
Agent:
  name: str
  description: str
  group: str
  model: str
  reasoning: bool
  is_leader: bool
  tools: [str]          # 仅展示自定义 Skill (原生工具自动隐藏)
  sub_agents: [str]
  memory: str | null
  system_prompt: str    # Markdown 正文
```

### 2.3 Skill 模型 (只读)

```yaml
Skill:
  name: str
  description: str
  allowed_tools: [str]
  prompt_template: str  # Markdown 正文
```

---

## 3. 页面设计

### 3.1 整体布局 (`base.html`)

```html
<!-- 顶部导航栏 -->
<nav class="navbar navbar-expand-lg navbar-dark bg-dark">
  <div class="container-fluid">
    <a class="navbar-brand" href="/">SoloQueue</a>
    <div class="collapse navbar-collapse">
      <ul class="navbar-nav me-auto">
        <li class="nav-item"><a class="nav-link" href="/">仪表盘</a></li>
        <li class="nav-item"><a class="nav-link" href="/teams">团队</a></li>
        <li class="nav-item"><a class="nav-link" href="/agents">Agents</a></li>
        <li class="nav-item"><a class="nav-link" href="/skills">Skills</a></li>
      </ul>
      <!-- 状态指示器 -->
      <span class="navbar-text">
        <span class="badge bg-success" id="connection-status">Online</span>
      </span>
    </div>
  </div>
</nav>

<!-- 主内容区 -->
<div class="container-fluid mt-3">
  <div class="row">
    <!-- 左侧快捷栏 (可选) -->
    <div class="col-md-2 d-none d-md-block bg-light sidebar">...</div>
    
    <!-- 内容视口 -->
    <main class="col-md-9 ms-sm-auto col-lg-10 px-md-4">
      {% block content %}{% endblock %}
    </main>
  </div>
</div>
```

### 3.2 仪表盘 (`dashboard.html`)

**路由**: `/`

- **系统状态卡片**:
  - 🟢 系统健康状态
  - 👥 在线团队数 / Agent 数
  - ⚡ 活跃任务数
- **最近活动日志**: 实时滚动的简要日志流

### 3.3 团队管理 (`teams.html`, `team_detail.html`)

**路由**: `/teams`, `/teams/{team_id}`

**功能**:
- **团队列表**: 卡片式展示 (Name, Description, Leader, Members)
- **团队详情**:
  - 基本信息编辑 (Name, Description)
  - 成员列表 (只读展示，链接到 Agent 详情)
  - **调试聊天窗口**: (类似 ChatUI，右下角或独立区域)
    - 发送消息给 Team Leader
    - 查看实时流式响应
    - 查看工具调用过程 (折叠/展开)
  - **拓扑图**: 使用 Vis.js 展示团队内部 Agent 关系 (Leader -> SubAgents)

### 3.4 Agent 管理 (`agents.html`, `agent_detail.html`)

**路由**: `/agents`, `/agents/{agent_name}`

**功能**:
- **Agent 列表**: 表格展示
  - Name, Group, Role (Leader?), Model, Status
- **Agent 编辑器**:
  - 表单编辑 Frontmatter 字段 (Group, Model, Tools, etc.)
  - Monaco Editor (或简单 Textarea) 编辑 System Prompt
  - 保存按钮 (调用 PUT API)

### 3.5 Skill/Model 浏览 (`skills.html`, `models.html`)

**路由**: `/skills`, `/models`

**功能**:
- **只读列表**: 展示系统中所有可用的 Skill 和 Model
- **详情弹窗**: 查看 Skill 的 Prompt 模板，Model 的适配器能力

---

## 4. 后端 API 设计

### 4.1 核心 API

```yaml
# 页面渲染 (返回 HTML)
GET /                   -> dashboard.html
GET /teams              -> teams.html
GET /teams/{id}         -> team_detail.html
GET /agents             -> agents.html
GET /agents/{name}      -> agent_detail.html

# 数据 API (JSON)
GET /api/teams
GET /api/agents
GET /api/skills
GET /api/models

# 操作 API
POST /api/chat/{team_id}      # 发送消息
PUT  /api/agents/{name}       # 更新配置
PUT  /api/teams/{id}          # 更新配置
```

### 4.2 WebSocket 协议

**Endpoint**: `/ws/system`

**消息类型**:
- `status_update`: 系统/Agent 状态变更
- `log_entry`: 实时日志
- `chat_stream`: 聊天内容流 (token by token)
- `tool_event`: 工具调用开始/结束

---

## 5. 项目结构 (src/soloqueue/web)

```
src/soloqueue/
  web/
    __init__.py
    app.py              # FastAPI App 定义
    router.py           # 页面路由
    api.py              # JSON API 路由
    
    templates/          # Jinja2 模板
      base.html
      dashboard.html
      teams.html
      team_detail.html
      agents.html
      agent_detail.html
      components/
        chat_box.html
        topology_graph.html
        
    static/             # 静态资源
      css/
        main.css
      js/
        app.js          # 全局逻辑 (WebSocket, Alpine store)
        chat.js         # 聊天逻辑
        topology.js     # Vis.js 逻辑
```

## 6. 实现路线图

1. **基础架构**: 
   - 设置 FastAPI + Jinja2 + Static Files
   - 创建 `base.html` 布局
2. **只读浏览**:
   - 实现 Team/Agent/Skill 的列表和详情页 (只读)
3. **配置编辑**:
   - Agent 编辑表单 +后端保存逻辑 (`config_sync.py`)
4. **实时交互**:
   - WebSocket 设好
   - 实现 Team 聊天窗口 (调试用)
   - 实现实时日志流
5. **可视化**: 
   - 集成 Vis.js 展示团队拓扑
