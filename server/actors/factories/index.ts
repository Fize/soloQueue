/**
 * ============================================
 * Agent 工厂 - 入口
 * ============================================
 *
 * 【用途】
 * 导出所有默认工厂，并提供注册函数
 *
 */

import type { ActorSystem } from '../actor-system.js';
import { chatAgentFactory } from './chat-agent.factory.js';
import { codeAgentFactory } from './code-agent.factory.js';
import { customAgentFactory } from './custom-agent.factory.js';
import type { AgentFactory } from '../types.js';

/**
 * 所有默认工厂
 */
export const DEFAULT_FACTORIES: AgentFactory[] = [
  chatAgentFactory,
  codeAgentFactory,
  customAgentFactory,
];

/**
 * 注册所有默认工厂
 */
export function registerDefaultFactories(system: ActorSystem): void {
  for (const factory of DEFAULT_FACTORIES) {
    system.registerFactory(factory);
  }
}

// 导出各个工厂供单独使用
export { chatAgentFactory } from './chat-agent.factory.js';
export { codeAgentFactory } from './code-agent.factory.js';
export { customAgentFactory } from './custom-agent.factory.js';
export { createRoleFactory } from './role-factory.js';
