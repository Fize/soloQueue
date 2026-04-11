/**
 * ============================================
 * Agent 工厂 - ChatAgent
 * ============================================
 *
 * 【用途】
 * 创建通用对话 Agent，从配置系统获取默认参数
 *
 * 【配置来源】
 * agent.defaults.roleDefaults.chat
 *
 */

import { createRoleFactory } from './role-factory.js';

/**
 * ChatAgent 工厂
 */
export const chatAgentFactory = createRoleFactory({
  kind: 'chat',
  roleKey: 'chat',
});
