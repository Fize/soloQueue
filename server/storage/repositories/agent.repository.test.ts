/**
 * AgentRepository 单元测试
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { AgentRepository } from './agent.repository.js';
import { TeamRepository } from './team.repository.js';
import { setMemoryDb } from '../db.js';
import type { Database } from 'sql.js';

describe('AgentRepository', () => {
  let repository: AgentRepository;
  let teamRepository: TeamRepository;
  let mockDb: Database;
  let defaultTeamId: string;

  beforeEach(async () => {
    // 创建内存数据库
    const initSqlJs = (await import('sql.js')).default;
    const SQL = await initSqlJs();
    mockDb = new SQL.Database();
    
    // 创建表结构
    mockDb.run(`
      CREATE TABLE IF NOT EXISTS teams (
        id TEXT PRIMARY KEY,
        name TEXT NOT NULL UNIQUE,
        description TEXT NOT NULL DEFAULT '',
        workspaces TEXT NOT NULL DEFAULT '["~/.soloqueue"]',
        is_default INTEGER NOT NULL DEFAULT 0,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      );
    `);
    
    mockDb.run(`
      CREATE TABLE IF NOT EXISTS agents (
        id TEXT PRIMARY KEY,
        team_id TEXT NOT NULL REFERENCES teams(id),
        name TEXT NOT NULL,
        model TEXT NOT NULL DEFAULT 'deepseek-chat',
        system_prompt TEXT NOT NULL DEFAULT '',
        temperature REAL NOT NULL DEFAULT 0.7,
        max_tokens INTEGER NOT NULL DEFAULT 2000,
        context_window INTEGER NOT NULL DEFAULT 64000,
        skills TEXT NOT NULL DEFAULT '[]',
        mcp TEXT NOT NULL DEFAULT '[]',
        hooks TEXT NOT NULL DEFAULT '[]',
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      );
    `);

    setMemoryDb(mockDb);
    teamRepository = new TeamRepository();
    
    // 创建默认团队
    const defaultTeam = await teamRepository.ensureDefault();
    defaultTeamId = defaultTeam.id;
    
    repository = new AgentRepository();
  });

  describe('create', () => {
    it('应该创建 Agent', async () => {
      const agent = await repository.create({
        teamId: defaultTeamId,
        name: 'Test Agent',
        model: 'deepseek-chat',
        systemPrompt: 'You are helpful.',
      });

      expect(agent.id).toBeDefined();
      expect(agent.name).toBe('Test Agent');
      expect(agent.model).toBe('deepseek-chat');
      expect(agent.systemPrompt).toBe('You are helpful.');
    });

    it('应该使用默认模型', async () => {
      const agent = await repository.create({
        teamId: defaultTeamId,
        name: 'Default Model Agent',
      });

      expect(agent.model).toBe('deepseek-chat');
    });

    it('应该使用指定团队', async () => {
      const otherTeam = await teamRepository.create({ name: 'Other Team' });
      
      const agent = await repository.create({
        teamId: otherTeam.id,
        name: 'Other Team Agent',
      });

      expect(agent.teamId).toBe(otherTeam.id);
    });

    it('应该使用默认 LLM 参数', async () => {
      const agent = await repository.create({
        teamId: defaultTeamId,
        name: 'Params Agent',
      });

      expect(agent.temperature).toBe(0.7);
      expect(agent.maxTokens).toBe(2000);
      expect(agent.contextWindow).toBe(64000);
    });

    it('应该支持预留字段', async () => {
      const agent = await repository.create({
        teamId: defaultTeamId,
        name: 'Extended Agent',
        skills: ['coding', 'debugging'],
        mcp: ['mcp-server'],
        hooks: ['before-request'],
      });

      expect(agent.skills).toEqual(['coding', 'debugging']);
      expect(agent.mcp).toEqual(['mcp-server']);
      expect(agent.hooks).toEqual(['before-request']);
    });

    it('应该抛出错误当没有 teamId 时', async () => {
      await expect(
        repository.create({ name: 'No Team Agent' } as any)
      ).rejects.toThrow('teamId is required');
    });
  });

  describe('findById', () => {
    it('应该通过 ID 查询 Agent', async () => {
      const created = await repository.create({ teamId: defaultTeamId, name: 'Find Test' });
      const found = await repository.findById(created.id);

      expect(found).not.toBeNull();
      expect(found!.name).toBe('Find Test');
    });

    it('应该返回 null 当 ID 不存在时', async () => {
      const found = await repository.findById('non-existent');
      expect(found).toBeNull();
    });
  });

  describe('findByTeamId', () => {
    it('应该查询团队的所有 Agent', async () => {
      await repository.create({ teamId: defaultTeamId, name: 'Team Agent 1' });
      await repository.create({ teamId: defaultTeamId, name: 'Team Agent 2' });

      const agents = await repository.findByTeamId(defaultTeamId);
      expect(agents.length).toBe(2);
    });

    it('应该返回空数组当团队没有 Agent 时', async () => {
      const otherTeam = await teamRepository.create({ name: 'Empty Team' });
      
      const agents = await repository.findByTeamId(otherTeam.id);
      expect(agents).toEqual([]);
    });
  });

  describe('findAll', () => {
    it('应该查询所有 Agent', async () => {
      await repository.create({ teamId: defaultTeamId, name: 'Agent 1' });
      await repository.create({ teamId: defaultTeamId, name: 'Agent 2' });

      const agents = await repository.findAll();
      expect(agents.length).toBe(2);
    });

    it('应该返回空数组当没有 Agent 时', async () => {
      const agents = await repository.findAll();
      expect(agents).toEqual([]);
    });
  });

  describe('update', () => {
    it('应该更新 Agent', async () => {
      const agent = await repository.create({ teamId: defaultTeamId, name: 'Update Test' });
      
      const updated = await repository.update(agent.id, {
        name: 'Updated Name',
        model: 'deepseek-v3',
        temperature: 0.5,
      });

      expect(updated).not.toBeNull();
      expect(updated!.name).toBe('Updated Name');
      expect(updated!.model).toBe('deepseek-v3');
      expect(updated!.temperature).toBe(0.5);
    });

    it('应该更新预留字段', async () => {
      const agent = await repository.create({ teamId: defaultTeamId, name: 'Extended Update' });
      
      const updated = await repository.update(agent.id, {
        skills: ['new-skill'],
        mcp: ['new-mcp'],
        hooks: ['new-hook'],
      });

      expect(updated!.skills).toEqual(['new-skill']);
      expect(updated!.mcp).toEqual(['new-mcp']);
      expect(updated!.hooks).toEqual(['new-hook']);
    });

    it('应该返回 null 当 Agent 不存在时', async () => {
      const updated = await repository.update('non-existent', { name: 'Test' });
      expect(updated).toBeNull();
    });

    it('应该返回原 Agent 当没有更新内容时', async () => {
      const agent = await repository.create({ teamId: defaultTeamId, name: 'No Changes' });
      
      const updated = await repository.update(agent.id, {});
      expect(updated!.name).toBe('No Changes');
    });
  });

  describe('delete', () => {
    it('应该删除 Agent', async () => {
      const agent = await repository.create({ teamId: defaultTeamId, name: 'Delete Test' });
      
      const result = await repository.delete(agent.id);
      
      expect(result).toBe(true);
      expect(await repository.findById(agent.id)).toBeNull();
    });
  });
});
