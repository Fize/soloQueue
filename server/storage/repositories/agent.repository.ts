/**
 * ============================================
 * Agent Repository (AI Agent 数据访问)
 * ============================================
 *
 * 【职责】
 * - Agent 实体的数据库 CRUD 操作
 * - 提供按团队查询
 * - 管理 Agent 的模型配置
 *
 * 【数据关系】
 *
 *   Team (1) ──────< Agent (N)
 *   一个团队可以有多个 Agent
 *   一个 Agent 必须属于一个团队
 *
 * 【Agent 配置项】
 *
 *   ┌─────────────────────────────────────────┐
 *   │ LLM 配置                                │
 *   │   model          → 模型名称              │
 *   │   systemPrompt   → 系统提示词            │
 *   │   temperature    → 温度参数              │
 *   │   maxTokens      → 最大令牌数            │
 *   │   contextWindow  → 上下文窗口            │
 *   ├─────────────────────────────────────────┤
 *   │ 预留字段 (未来扩展)                      │
 *   │   skills         → 技能数组              │
 *   │   mcp            → MCP 配置数组          │
 *   │   hooks          → 钩子数组              │
 *   └─────────────────────────────────────────┘
 *
 * 【支持的模型】
 *
 *   - deepseek-chat (默认)
 *   - deepseek-coder
 *
 * 【特有方法】
 *
 *   findByTeamId(teamId)  → 按团队查找所有 Agent
 *
 * 【日志分类】
 *
 *   category: 'db.agent'
 *
 * ============================================
 */

import { v4 as uuidv4 } from 'uuid';
import { getDb, saveDb } from '../db.js';
import { Logger } from '../../logger/index.js';
import type { Agent, CreateAgentInput, UpdateAgentInput } from '../types.js';
import type { Repository } from './base.repository.js';

export class AgentRepository implements Repository<Agent> {
  private logger: Logger;

  constructor() {
    this.logger = Logger.system();
  }

  /**
   * 根据 ID 查询
   */
  async findById(id: string): Promise<Agent | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM agents WHERE id = ?`, [id]);

    if (result.length === 0 || result[0].values.length === 0) {
      this.logger.debug({
        category: 'db.agent',
        message: 'Agent not found',
        context: { id },
      });
      return null;
    }
    
    const agent = this.mapRow(result[0].columns, result[0].values[0]);
    this.logger.debug({
      category: 'db.agent',
      message: 'Agent found',
      context: { id, name: agent.name },
    });
    
    return agent;
  }

  /**
   * 根据团队查询
   */
  async findByTeamId(teamId: string): Promise<Agent[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM agents WHERE team_id = ?`, [teamId]);

    if (result.length === 0) {
      this.logger.debug({
        category: 'db.agent',
        message: 'No agents found by team',
        context: { teamId },
      });
      return [];
    }
    
    const agents = result[0].values.map((row) => this.mapRow(result[0].columns, row));
    this.logger.debug({
      category: 'db.agent',
      message: 'Agents found by team',
      context: { teamId, count: agents.length },
    });
    
    return agents;
  }

  /**
   * 查询所有
   */
  async findAll(): Promise<Agent[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM agents`);

    if (result.length === 0) {
      this.logger.debug({
        category: 'db.agent',
        message: 'No agents found',
      });
      return [];
    }
    
    const agents = result[0].values.map((row) => this.mapRow(result[0].columns, row));
    this.logger.debug({
      category: 'db.agent',
      message: 'Agents found',
      context: { count: agents.length },
    });
    
    return agents;
  }

  /**
   * 创建 Agent
   * 注意：需要外部传入 teamId，内部不做默认值处理
   */
  async create(input: CreateAgentInput): Promise<Agent> {
    if (!input.teamId) {
      throw new Error('teamId is required');
    }
    
    const db = getDb();
    const now = new Date().toISOString();
    const id = uuidv4();

    const agent: Agent = {
      id,
      teamId: input.teamId,
      name: input.name,
      model: input.model || 'deepseek-chat',
      systemPrompt: input.systemPrompt || '',
      temperature: input.temperature ?? 0.7,
      maxTokens: input.maxTokens ?? 2000,
      contextWindow: input.contextWindow ?? 64000,
      skills: input.skills || [],
      mcp: input.mcp || [],
      hooks: input.hooks || [],
      createdAt: now,
      updatedAt: now,
    };

    db.run(
      `INSERT INTO agents (id, team_id, name, model, system_prompt, temperature, max_tokens, context_window, skills, mcp, hooks, created_at, updated_at) 
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        agent.id,
        agent.teamId,
        agent.name,
        agent.model,
        agent.systemPrompt,
        agent.temperature,
        agent.maxTokens,
        agent.contextWindow,
        JSON.stringify(agent.skills),
        JSON.stringify(agent.mcp),
        JSON.stringify(agent.hooks),
        agent.createdAt,
        agent.updatedAt,
      ]
    );

    saveDb();
    
    this.logger.info({
      category: 'db.agent',
      message: 'Agent created',
      context: { id: agent.id, name: agent.name, teamId: agent.teamId },
    });
    
    return agent;
  }

  /**
   * 更新 Agent
   */
  async update(id: string, input: UpdateAgentInput): Promise<Agent | null> {
    const existing = await this.findById(id);
    if (!existing) {
      this.logger.warn({
        category: 'db.agent',
        message: 'Agent not found for update',
        context: { id },
      });
      return null;
    }

    const updates: string[] = [];
    const values: (string | number)[] = [];

    if (input.name !== undefined) {
      updates.push('name = ?');
      values.push(input.name);
    }
    if (input.model !== undefined) {
      updates.push('model = ?');
      values.push(input.model);
    }
    if (input.systemPrompt !== undefined) {
      updates.push('system_prompt = ?');
      values.push(input.systemPrompt);
    }
    if (input.temperature !== undefined) {
      updates.push('temperature = ?');
      values.push(input.temperature);
    }
    if (input.maxTokens !== undefined) {
      updates.push('max_tokens = ?');
      values.push(input.maxTokens);
    }
    if (input.contextWindow !== undefined) {
      updates.push('context_window = ?');
      values.push(input.contextWindow);
    }
    if (input.skills !== undefined) {
      updates.push('skills = ?');
      values.push(JSON.stringify(input.skills));
    }
    if (input.mcp !== undefined) {
      updates.push('mcp = ?');
      values.push(JSON.stringify(input.mcp));
    }
    if (input.hooks !== undefined) {
      updates.push('hooks = ?');
      values.push(JSON.stringify(input.hooks));
    }

    if (updates.length === 0) {
      this.logger.debug({
        category: 'db.agent',
        message: 'No changes to agent',
        context: { id },
      });
      return existing;
    }

    updates.push('updated_at = ?');
    values.push(new Date().toISOString());
    values.push(id);

    const db = getDb();
    db.run(`UPDATE agents SET ${updates.join(', ')} WHERE id = ?`, values);
    saveDb();

    const updated = await this.findById(id);
    
    this.logger.info({
      category: 'db.agent',
      message: 'Agent updated',
      context: { id, name: updated?.name },
    });
    
    return updated;
  }

  /**
   * 删除 Agent
   */
  async delete(id: string): Promise<boolean> {
    const agent = await this.findById(id);
    
    const db = getDb();
    db.run(`DELETE FROM agents WHERE id = ?`, [id]);
    saveDb();
    
    this.logger.info({
      category: 'db.agent',
      message: 'Agent deleted',
      context: { id, name: agent?.name },
    });
    
    return true;
  }

  /**
   * 映射数据库行到实体
   */
  private mapRow(columns: string[], values: (string | number | null | Uint8Array)[]): Agent {
    const row: Record<string, string | number | null> = {};
    columns.forEach((col, i) => {
      row[col] = values[i];
    });

    return {
      id: row['id'] as string,
      teamId: row['team_id'] as string,
      name: row['name'] as string,
      model: row['model'] as string,
      systemPrompt: row['system_prompt'] as string,
      temperature: row['temperature'] as number,
      maxTokens: row['max_tokens'] as number,
      contextWindow: row['context_window'] as number,
      skills: JSON.parse((row['skills'] as string) || '[]'),
      mcp: JSON.parse((row['mcp'] as string) || '[]'),
      hooks: JSON.parse((row['hooks'] as string) || '[]'),
      createdAt: row['created_at'] as string,
      updatedAt: row['updated_at'] as string,
    };
  }
}

export const agentRepository = new AgentRepository();
