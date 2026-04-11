/**
 * ============================================
 * Repository 层 - 数据访问入口
 * ============================================
 *
 * 【职责】
 * - 提供数据访问对象的统一导出
 * - 管理单例实例
 *
 * 【提供的 Repository】
 *
 *   ┌─────────────────────────────────────────┐
 *   │           Repository 单例                 │
 *   ├─────────────────────────────────────────┤
 *   │ teamRepository    → Team 数据访问        │
 *   │ configRepository  → Config 数据访问      │
 *   │ agentRepository   → Agent 数据访问       │
 *   └─────────────────────────────────────────┘
 *
 * 【日志规范】
 *
 *   每个 Repository 操作都使用 Logger.system() 记录:
 *   - DEBUG: 详细的 CRUD 操作
 *   - INFO: 创建、更新操作成功
 *   - WARN: 操作失败或异常情况
 *
 * 【使用示例】
 *
 *   import { teamRepository, configRepository, agentRepository } from './storage';
 *
 *   // Team 操作
 *   const team = await teamRepository.findById('xxx');
 *   const teams = await teamRepository.findAll();
 *
 *   // Config 操作
 *   const config = await configRepository.findByKey('app.theme');
 *
 *   // Agent 操作
 *   const agent = await agentRepository.create({ name: 'MyAgent' });
 *
 * ============================================
 */

// Repository 类和单例导出
export { TeamRepository, teamRepository } from './team.repository.js';
export { ConfigRepository, configRepository } from './config.repository.js';
export { AgentRepository, agentRepository } from './agent.repository.js';

// Repository 接口
export type { Repository } from './base.repository.js';
