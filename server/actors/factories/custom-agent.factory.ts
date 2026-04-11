/**
 * ============================================
 * Agent 工厂 - CustomAgent
 * ============================================
 *
 * 【用途】
 * 创建自定义 Agent，允许通过配置完全自定义行为
 *
 */

import { createAgentActor } from '../machines/agent-machine.js';
import type { AgentFactory, AgentDefinition, ActorInstance, ActorSystem } from '../types.js';
import { llmConfigService } from '../../llm/index.js';

/**
 * CustomAgent 工厂
 * 允许用户通过 AgentDefinition.tools 字段传递自定义配置
 * 
 * 【自定义配置方式】
 * 通过 definition.tools 数组传递自定义参数，格式：
 * tools: ['tool-a', 'tool-b']  → 标准工具列表
 * 
 * 【参数来源】
 * - model: definition.modelId
 * - temperature/maxTokens: 从 llmConfigService.getAgentDefaults() 获取
 */
export const customAgentFactory: AgentFactory = {
  kind: 'custom',

  create(definition: AgentDefinition, _system: ActorSystem): ActorInstance {
    // 从配置服务获取默认值，而非硬编码
    const agentDefaults = llmConfigService.getAgentDefaults();
    const chatDefaults = agentDefaults.roleDefaults.chat;

    const actorRef = createAgentActor({
      agentId: definition.id,
      teamId: definition.teamId,
      model: definition.modelId,
      systemPrompt: definition.systemPrompt,
      temperature: chatDefaults.temperature,
      maxTokens: chatDefaults.maxTokens,
    });

    actorRef.start();

    return {
      id: definition.id,
      kind: 'custom',
      role: definition.role,
      ref: actorRef,
      children: new Set(),
      metadata: { definition },
    };
  },

  validate(config: Partial<AgentDefinition>): boolean {
    // 自定义 Agent 只需要基本配置
    return !!config.id && !!config.teamId;
  },
};
