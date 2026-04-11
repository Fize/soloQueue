/**
 * ============================================
 * Actor 系统核心 - 扩展点定义
 * ============================================
 *
 * 【扩展机制】
 * 1. Agent 工厂 - 用于注册新的 Agent 类型
 * 2. 路由策略 - 用于自定义消息路由逻辑
 * 3. 监督策略 - 内置于 Supervisor
 *
 */

import type { ActorSystem } from './actor-system.js';
import type { AgentDefinition, AgentKind, AgentInstance, TaskMessage } from './types.js';

// ============== Agent 工厂 ==============

/**
 * Agent 工厂接口 - 扩展点
 * 
 * 【用途】
 * 用于注册新的 Agent 类型，实现 create 方法即可定义新类型的 Agent 如何被创建
 * 
 * 【示例】
 * ```typescript
 * const myAgentFactory: AgentFactory = {
 *   kind: 'my_agent',
 *   create: (definition, system) => {
 *     // 创建逻辑
 *     return instance;
 *   },
 *   validate: (config) => {
 *     // 验证逻辑
 *     return isValid;
 *   }
 * };
 * system.registerFactory(myAgentFactory);
 * ```
 */
export interface AgentFactory {
  /** Agent 类型标识 */
  kind: AgentKind;
  
  /**
   * 创建 Agent 实例
   * @param definition Agent 定义
   * @param system ActorSystem 引用，用于访问系统功能
   */
  create(definition: AgentDefinition, system: ActorSystem): AgentInstance;
  
  /**
   * 可选: 验证配置
   */
  validate?(config: Partial<AgentDefinition>): boolean;
}

// ============== 路由策略 ==============

/**
 * 路由策略接口 - 扩展点
 * 
 * 【用途】
 * 用于自定义消息如何路由到合适的 Agent
 * 
 * 【示例】
 * ```typescript
 * const myRoutingStrategy: RoutingStrategy = {
 *   name: 'my_routing',
 *   select: (agents, message) => {
 *     // 选择逻辑
 *     return selectedAgent;
 *   }
 * };
 * system.setRoutingStrategy(myRoutingStrategy);
 * ```
 */
export interface RoutingStrategy {
  /** 策略名称 */
  name: string;
  
  /**
   * 选择目标 Agent
   * @param agents 可用的 Agent 列表
   * @param message 消息
   * @returns 选中的 Agent 或 null
   */
  select(agents: ActorInstance[], message: TaskMessage): ActorInstance | null;
}

// ============== Agent 创建参数 ==============

/**
 * 创建 Agent 时的参数
 */
export interface AgentCreateParams {
  id?: string;
  name: string;
  teamId: string;
  kind: AgentKind;
  modelId?: string;
  providerId?: string;
  systemPrompt?: string;
  capabilities?: string[];
  tools?: string[];
  supervision?: Partial<{
    strategy: 'one_for_one' | 'one_for_all' | 'all_for_one' | 'stop';
    maxRetries: number;
    retryInterval: number;
    exponentialBackoff: boolean;
    maxBackoff: number;
  }>;
}
