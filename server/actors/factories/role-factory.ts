/**
 * ============================================
 * Agent 工厂 - 通用工厂函数
 * ============================================
 *
 * 【用途】
 * 提供通用的 Agent 创建逻辑，所有 AgentFactory 都使用此函数
 *
 * 【设计】
 * - 统一 ActorInstance 创建逻辑
 * - 支持从配置系统获取默认参数
 * - 通过工厂选项区分不同 Agent 类型
 *
 */

import { createAgentActor } from '../machines/agent-machine.js';
import { llmConfigService } from '../../llm/index.js';
import type { AgentFactory, AgentDefinition, ActorInstance, ActorSystem, AgentKind } from '../types.js';

// ============== 工厂选项 ==============

export interface RoleFactoryOptions {
  /** Agent 类型 */
  kind: AgentKind;
  /** 角色配置键 (chat/code/planner/evaluator) */
  roleKey: 'chat' | 'code' | 'planner' | 'evaluator';
}

/**
 * 通用工厂创建函数
 */
export function createRoleFactory(options: RoleFactoryOptions): AgentFactory {
  const { kind, roleKey } = options;

  return {
    kind,

    create(definition: AgentDefinition, _system: ActorSystem): ActorInstance {
      // 从配置系统获取默认参数
      const defaults = llmConfigService.getRoleDefaultConfig(roleKey);

      const actorRef = createAgentActor({
        agentId: definition.id,
        teamId: definition.teamId,
        model: definition.modelId || defaults.modelId,
        systemPrompt: definition.systemPrompt,
        temperature: defaults.temperature,
        maxTokens: defaults.maxTokens,
      });

      actorRef.start();

      return {
        id: definition.id,
        kind,
        role: definition.role,
        ref: actorRef,
        children: new Set(),
        metadata: { definition },
      };
    },

    validate(config: Partial<AgentDefinition>): boolean {
      return config.teamId !== undefined;
    },
  };
}
