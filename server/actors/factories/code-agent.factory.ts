/**
 * ============================================
 * Agent 工厂 - CodeAgent
 * ============================================
 *
 * 【用途】
 * 创建代码生成 Agent，从配置系统获取默认参数
 *
 * 【配置来源】
 * agent.defaults.roleDefaults.code
 *
 */

import { createRoleFactory } from './role-factory.js';

/**
 * CodeAgent 工厂
 */
export const codeAgentFactory = createRoleFactory({
  kind: 'code',
  roleKey: 'code',
});
