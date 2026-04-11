/**
 * ============================================
 * 存储层 (Storage Layer) - 模块主入口
 * ============================================
 *
 * 【模块职责】
 * 存储层负责所有数据的持久化操作，采用 Repository 模式实现数据访问。
 *
 * 【架构分层】
 *
 *   ┌─────────────────────────────────────────────┐
 *   │              Service 层                      │
 *   │  configService  │  agentService  │  teamService │
 *   └─────────────────┴────────────────┴────────────┘
 *                         │
 *                         ▼
 *   ┌─────────────────────────────────────────────┐
 *   │           Repository 层                      │
 *   │  configRepository │ agentRepository │ teamRepository │
 *   └─────────────────────┴────────────────┴──────────┘
 *                         │
 *                         ▼
 *   ┌─────────────────────────────────────────────┐
 *   │             Infrastructure 层                 │
 *   │           db.ts │ migrations.ts              │
 *   └─────────────────────────────────────────────┘
 *                         │
 *                         ▼
 *   ┌─────────────────────────────────────────────┐
 *   │              SQLite Database                 │
 *   └─────────────────────────────────────────────┘
 *
 * 【模块结构】
 *
 *   server/storage/
 *   ├── db.ts              # 数据库连接 (sql.js)
 *   ├── schema.ts          # Drizzle ORM Schema 定义
 *   ├── migrations.ts      # 数据库迁移
 *   ├── types.ts           # 领域模型类型定义
 *   ├── seeds.ts           # 种子数据
 *   ├── index.ts           # 模块入口 (本文件)
 *   │
 *   ├── config.service.ts  # 配置服务 (热加载 + 缓存 + 变更通知)
 *   ├── agent.service.ts    # Agent 服务 (CRUD + 事件通知)
 *   │
 *   └── repositories/       # Repository 层 (数据访问)
 *       ├── base.repository.ts    # Repository 基类
 *       ├── team.repository.ts    # Team 数据访问
 *       ├── config.repository.ts  # Config 数据访问
 *       └── agent.repository.ts   # Agent 数据访问
 *
 * 【数据模型】
 *
 *   Team (团队)
 *   ├── id: string (UUID)
 *   ├── name: string
 *   ├── description: string
 *   ├── workspaces: string[]
 *   ├── isDefault: boolean
 *   │
 *   └── Agents[] (一对多)
 *
 *   Agent (AI 代理)
 *   ├── id: string (UUID)
 *   ├── teamId: string (FK → Team)
 *   ├── name: string
 *   ├── model: string
 *   ├── systemPrompt: string
 *   ├── temperature: number
 *   ├── maxTokens: number
 *   ├── contextWindow: number
 *   ├── skills: string[] (预留)
 *   ├── mcp: string[] (预留)
 *   └── hooks: string[] (预留)
 *
 *   Config (配置项)
 *   ├── id: number (自增)
 *   ├── key: string (唯一)
 *   ├── value: string (JSON 格式)
 *   ├── type: 'string' | 'number' | 'boolean' | 'json'
 *   ├── category: string
 *   ├── description: string
 *   └── editable: boolean
 *
 * 【日志集成】
 *
 *   所有数据库操作使用 Logger.system() 记录:
 *   - CRUD 操作 (DEBUG 级别)
 *   - 初始化操作 (INFO 级别)
 *   - 错误和异常 (ERROR 级别)
 *
 * 【使用示例】
 *
 *   import { initDb, configService, agentService } from './storage';
 *
 *   // 初始化
 *   await initDb();
 *   await configService.initialize();
 *   await agentService.initialize();
 *
 *   // 使用配置
 *   const theme = configService.get('app.theme', 'dark');
 *   await configService.set('app.theme', 'light');
 *
 *   // 使用 Agent
 *   const agent = await agentService.create({ name: 'MyAgent' });
 *   const agents = await agentService.getByTeam(teamId);
 *
 * ============================================
 */

// 数据库
export { initDb, closeDb, saveDb, getDb, getDbPath, isDbInitialized, setMemoryDb, resetDb } from './db.js';
export { runMigrations } from './migrations.js';

// 类型
export * from './types.js';

// Repository
export { teamRepository } from './repositories/team.repository.js';
export { configRepository } from './repositories/config.repository.js';
export { agentRepository } from './repositories/agent.repository.js';
export type { Repository } from './repositories/base.repository.js';

// Service
export { configService } from './config.service.js';
export type { ConfigChangeEvent } from './config.service.js';
export { agentService } from './agent.service.js';

// Seeds
export { DEFAULT_TEAM, DEFAULT_CONFIGS } from './seeds.js';
