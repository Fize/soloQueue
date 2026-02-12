# Implementation Plan - Web UI (FastAPI + Jinja2)

本计划旨在构建 SoloQueue 的轻量级 Web 界面，使用 Python 原生技术栈 (FastAPI, Jinja2) 和无构建前端 (Bootstrap 5, Alpine.js)。

## Phase 1: 基础框架与只读浏览
**目标**: 搭建 Web 服务骨架，能够浏览现有的 Teams, Agents, Skills 配置。

- [ ] **1.1 项目结构初始化**
  - 创建 `src/soloqueue/web/` 目录结构 (templates, static, routers)
  - 创建 `src/soloqueue/web/app.py` (FastAPI 入口)
  - 配置静态文件挂载和 Jinja2 模板环境
- [ ] **1.2 基础布局实现**
  - 创建 `base.html` (Bootstrap 5 导航栏, 侧边栏)
  - 创建 `dashboard.html` (系统状态概览 - 暂用 Mock 数据)
- [ ] **1.3 列表页面实现**
  - 后端: 实现 `GET /api/teams`, `GET /api/agents` (基于现有 Loader.load_all)
  - 前端: 创建 `teams.html`, `agents.html` (Jinja2 渲染列表)
- [ ] **1.4 详情页面实现 (只读)**
  - 后端: 实现 `GET /api/agents/{name}`, `GET /api/teams/{id}`
  - 前端: 创建 `agent_detail.html`, `team_detail.html`

## Phase 2: 配置编辑能力
**目标**: 实现 Agent 和 Team 配置的修改与保存。

- [ ] **2.1 Loader 增强**
  - 扩展 `AgentLoader` 支持 `save(schema)` (写回 Markdown)
  - 扩展 `GroupLoader` 支持 `save(schema)`
- [ ] **2.2 编辑 API 实现**
  - 实现 `PUT /api/agents/{name}`
  - 实现 `PUT /api/teams/{id}`
- [ ] **2.3 前端编辑交互**
  - 使用 Alpine.js 实现表单提交
  - 集成简易 Markdown 编辑器 (如 SimpleMDE 或仅 Textarea)

## Phase 3: 实时交互与监控
**目标**: 实现聊天调试窗口和实时状态监控。

- [ ] **3.1 WebSocket 基础**
  - 实现 `/ws/system` 端点
  - 前端 `useWebSocket` 封装 (Alpine.js store)
- [ ] **3.2 聊天界面**
  - 创建 `chat.html` 或嵌入式聊天组件
  - 实现消息列表渲染和输入框
- [ ] **3.3 Orchestrator 改造**
  - 重构 `Orchestrator.run` 支持 `yield` 事件流
  - 对接 WebSocket 推送 Token 流和工具调用状态

## Phase 4: 可视化与优化
**目标**: 展示团队拓扑图，优化用户体验。

- [ ] **4.1 拓扑图集成**
  - 集成 Vis.js
  - 实现 `GET /api/topology/{team_id}`
  - 渲染团队内部 Agent 关系图
- [ ] **4.2 系统集成**
  - 将 Web 服务集成到 `soloqueue serve` CLI 命令中
