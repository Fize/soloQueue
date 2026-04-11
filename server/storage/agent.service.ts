/**
 * ============================================
 * Agent Service (Agent 服务)
 * ============================================
 *
 * 【职责】
 * - Agent 实体的业务逻辑封装
 * - 默认团队初始化管理
 * - Agent CRUD 操作
 * - Agent 变更事件通知
 *
 * 【架构】
 *
 *   ┌─────────────────────────────────────────────┐
 *   │              AgentService                     │
 *   │                                              │
 *   │  ┌─────────────────────────────────────────┐ │
 *   │  │      初始化流程                            │ │
 *   │  │  1. 确保默认团队存在                      │ │
 *   │  │  2. 缓存默认团队 ID                       │ │
 *   │  └─────────────────────────────────────────┘ │
 *   │                                              │
 *   │  ┌─────────────────────────────────────────┐ │
 *   │  │      CRUD 操作                            │ │
 *   │  │  create → AgentRepository.create        │ │
 *   │  │  update → AgentRepository.update        │ │
 *   │  │  delete → AgentRepository.delete        │ │
 *   │  └─────────────────────────────────────────┘ │
 *   └─────────────────────────────────────────────┘
 *
 * 【事件通知】
 *
 *   agent:created  → Agent 创建后触发
 *   agent:updated  → Agent 更新后触发
 *   agent:deleted  → Agent 删除后触发
 *
 * 【默认值处理】
 *
 *   创建 Agent 时:
 *   - 如未指定 teamId，自动使用默认团队 ID
 *   - model 默认为 'deepseek-chat'
 *   - temperature 默认为 0.7
 *
 * 【日志分类】
 *
 *   category: 'agent'
 *
 * ============================================
 */

import { EventEmitter } from 'events';
import { Logger } from '../logger/index.js';
import { agentRepository } from './repositories/agent.repository.js';
import { teamRepository } from './repositories/team.repository.js';
import type { Agent, CreateAgentInput, UpdateAgentInput } from './types.js';

class AgentService extends EventEmitter {
  private logger: Logger;
  private initialized = false;
  private defaultTeamId?: string;

  constructor() {
    super();
    this.logger = Logger.system();
  }

  /**
   * 初始化 - 确保默认团队存在
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    this.logger.info({
      category: 'agent',
      message: 'Initializing agent service',
    });

    // 确保默认团队存在
    const defaultTeam = await teamRepository.ensureDefault();
    this.defaultTeamId = defaultTeam.id;

    this.initialized = true;
    this.logger.info({
      category: 'agent',
      message: 'Agent service initialized',
      context: { defaultTeamId: this.defaultTeamId },
    });
  }

  /**
   * 获取默认团队 ID
   */
  getDefaultTeamId(): string {
    if (!this.defaultTeamId) {
      throw new Error('Agent service not initialized');
    }
    return this.defaultTeamId;
  }

  /**
   * 创建 Agent
   */
  async create(input: CreateAgentInput): Promise<Agent> {
    const agent = await agentRepository.create({
      ...input,
      teamId: input.teamId || this.getDefaultTeamId(),
    });

    this.logger.info({
      category: 'agent',
      message: 'Agent created',
      context: { id: agent.id, name: agent.name, teamId: agent.teamId },
    });

    this.emit('agent:created', agent);
    return agent;
  }

  /**
   * 获取 Agent
   */
  async get(id: string): Promise<Agent | null> {
    return agentRepository.findById(id);
  }

  /**
   * 获取团队的所有 Agent
   */
  async getByTeam(teamId: string): Promise<Agent[]> {
    return agentRepository.findByTeamId(teamId);
  }

  /**
   * 获取所有 Agent
   */
  async getAll(): Promise<Agent[]> {
    return agentRepository.findAll();
  }

  /**
   * 更新 Agent
   */
  async update(id: string, input: UpdateAgentInput): Promise<Agent | null> {
    const agent = await agentRepository.update(id, input);

    if (agent) {
      this.logger.info({
        category: 'agent',
        message: 'Agent updated',
        context: { id: agent.id, name: agent.name },
      });
      this.emit('agent:updated', agent);
    }

    return agent;
  }

  /**
   * 删除 Agent
   */
  async delete(id: string): Promise<boolean> {
    const agent = await agentRepository.findById(id);
    if (!agent) return false;

    const result = await agentRepository.delete(id);

    if (result) {
      this.logger.info({
        category: 'agent',
        message: 'Agent deleted',
        context: { id, name: agent.name },
      });
      this.emit('agent:deleted', agent);
    }

    return result;
  }

  /**
   * 检查是否已初始化
   */
  isInitialized(): boolean {
    return this.initialized;
  }
}

export const agentService = new AgentService();
