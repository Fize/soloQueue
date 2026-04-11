/**
 * ============================================
 * Actor 系统核心 - Router (路由器)
 * ============================================
 *
 * 【核心职责】
 * 1. 根据路由策略选择合适的 Agent
 * 2. 支持多种路由策略 (轮询、负载均衡、类型匹配)
 * 3. 扩展点：自定义路由策略
 *
 * 【设计原则】
 * 1. 策略模式 - 路由逻辑可替换
 * 2. 可扩展 - 支持自定义路由策略
 *
 */

import type { ActorSystem } from './actor-system.js';
import type { ActorInstance, TaskMessage } from './types.js';
import type { RoutingStrategy } from './extensions.js';

// ============== 路由策略实现 ==============

/**
 * 轮询策略
 */
export class RoundRobinStrategy implements RoutingStrategy {
  name = 'round_robin';
  private index = 0;

  select(agents: ActorInstance[]): ActorInstance | null {
    if (agents.length === 0) return null;

    const selected = agents[this.index % agents.length];
    this.index++;

    return selected;
  }
}

/**
 * 负载均衡策略 - 选择负载最低的 Agent
 */
export class LeastLoadStrategy implements RoutingStrategy {
  name = 'least_load';

  select(agents: ActorInstance[]): ActorInstance | null {
    if (agents.length === 0) return null;

    // 选择负载最低的 (currentTasks 最少的)
    return agents.reduce((min, current) => {
      const minTasks = (min.metadata.currentTasks as number) || 0;
      const currentTasks = (current.metadata.currentTasks as number) || 0;
      return currentTasks < minTasks ? current : min;
    });
  }
}

/**
 * 类型匹配策略 - 根据任务内容匹配 Agent 类型
 */
export class TypeMatchStrategy implements RoutingStrategy {
  name = 'type_match';

  select(agents: ActorInstance[], message: TaskMessage): ActorInstance | null {
    const requiredKind = this.inferKind(message);

    // 尝试找匹配类型的 Agent
    const matched = agents.find(a => a.kind === requiredKind);

    // 如果找到匹配的，返回
    if (matched) return matched;

    // 否则返回任意一个 chat 类型的 Agent
    const chatAgent = agents.find(a => a.kind === 'chat');
    return chatAgent || agents[0] || null;
  }

  private inferKind(message: TaskMessage): string {
    const content = message.content.toLowerCase();

    if (content.includes('code') || content.includes('function') || content.includes('implement')) {
      return 'code';
    }
    if (content.includes('search') || content.includes('find') || content.includes('query')) {
      return 'tool';
    }
    if (content.includes('plan') || content.includes('schedule') || content.includes('organize')) {
      return 'planner';
    }

    return 'chat';
  }
}

/**
 * 亲和性策略 - 优先选择之前处理过同类任务的 Agent
 */
export class AffinityStrategy implements RoutingStrategy {
  name = 'affinity';
  private affinityMap = new Map<string, string>(); // taskPattern -> agentId

  select(agents: ActorInstance[], message: TaskMessage): ActorInstance | null {
    if (agents.length === 0) return null;

    // 生成任务模式
    const pattern = this.extractPattern(message.content);

    // 检查亲和性
    const preferredId = this.affinityMap.get(pattern);
    if (preferredId) {
      const preferred = agents.find(a => a.id === preferredId);
      if (preferred) {
        return preferred;
      }
    }

    // 选择第一个
    const selected = agents[0];

    // 更新亲和性
    this.affinityMap.set(pattern, selected.id);

    return selected;
  }

  private extractPattern(content: string): string {
    // 简化：取前 50 个字符作为模式
    return content.substring(0, 50).toLowerCase();
  }
}

// ============== Router ==============

export class Router {
  private strategy: RoutingStrategy;

  constructor(
    private system: ActorSystem,
    strategy?: RoutingStrategy
  ) {
    this.strategy = strategy || new RoundRobinStrategy();
  }

  /**
   * 设置路由策略
   */
  setStrategy(strategy: RoutingStrategy): void {
    this.strategy = strategy;
  }

  /**
   * 路由消息到合适的 Agent
   */
  route(message: TaskMessage): ActorInstance | null {
    // 获取所有可用的用户 Agent
    const agents = this.system.getAgentsByRole('user').filter(a => a.role !== 'system');

    if (agents.length === 0) {
      return null;
    }

    // 使用策略选择
    return this.strategy.select(agents, message);
  }

  /**
   * 获取路由统计
   */
  getStats(): {
    strategy: string;
    agentCount: number;
  } {
    return {
      strategy: this.strategy.name,
      agentCount: this.system.getAgentsByRole('user').length,
    };
  }
}
