/**
 * ============================================
 * Actor 系统核心 - 入口
 * ============================================
 *
 * 【模块组成】
 * - ActorSystem: 核心管理器
 * - Supervisor: 监督者
 * - Router: 路由器
 * - types: 类型定义
 * - extensions: 扩展点
 * - factories: Agent 工厂
 *
 * 【使用示例】
 * ```typescript
 * import { ActorSystem } from './actors';
 * import { registerDefaultFactories } from './actors/factories';
 * 
 * const system = new ActorSystem();
 * registerDefaultFactories(system);
 * await system.start();
 * 
 * // 创建 Agent
 * const agent = await system.createAgent({
 *   name: 'My Agent',
 *   teamId: 'my-team',
 *   kind: 'chat',
 *   model: 'deepseek-chat',
 * });
 * 
 * // 发送消息
 * system.dispatch({
 *   type: 'task',
 *   taskId: 'task-1',
 *   content: 'Hello!',
 *   from: 'user',
 * });
 * 
 * // 停止
 * await system.stop();
 * ```
 *
 */

// 导出核心类
export { ActorSystem } from './actor-system.js';
export { Supervisor } from './supervisor.js';
export { Router } from './router.js';

// 导出类型
export type {
  AgentDefinition,
  AgentKind,
  AgentRole,
  ActorMessage,
  TaskMessage,
  DelegateMessage,
  ResultMessage,
  ErrorMessage,
  SystemMessage,
  SupervisionMessage,
  SupervisionConfig,
  SupervisionStrategy,
  ActorInstance,
  ActorLifecycle,
  SystemStatus,
  ActorSystemEvent,
} from './types.js';

// 导出扩展点
export type {
  AgentFactory,
  RoutingStrategy,
  AgentCreateParams,
} from './extensions.ts';

// 导出工厂
export { registerDefaultFactories } from './factories/index.js';
export { chatAgentFactory, codeAgentFactory, customAgentFactory } from './factories/index.js';

// 导出预定义系统 Agent
export { SYSTEM_AGENTS } from './types.js';

// 导出路由策略
export {
  RoundRobinStrategy,
  LeastLoadStrategy,
  TypeMatchStrategy,
  AffinityStrategy,
} from './router.js';
