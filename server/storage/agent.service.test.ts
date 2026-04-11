/**
 * AgentService 单元测试
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { agentService } from './agent.service.js';
import { configService } from './config.service.js';
import { setMemoryDb } from './db.js';
import type { Database } from 'sql.js';

describe('AgentService', () => {
  let mockDb: Database;

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
      CREATE TABLE IF NOT EXISTS configs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        key TEXT NOT NULL UNIQUE,
        value TEXT NOT NULL,
        type TEXT NOT NULL DEFAULT 'string',
        description TEXT,
        category TEXT NOT NULL,
        editable INTEGER NOT NULL DEFAULT 1,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      );
    `);
    
    mockDb.run(`
      CREATE TABLE IF NOT EXISTS agents (
        id TEXT PRIMARY KEY,
        team_id TEXT NOT NULL REFERENCES teams(id),
        name TEXT NOT NULL,
        model_id TEXT NOT NULL DEFAULT 'deepseek-chat',
        provider_id TEXT NOT NULL DEFAULT 'deepseek',
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
    
    // 重置服务状态
    await agentService.initialize();
  });

  describe('initialize', () => {
    it('应该创建默认团队', async () => {
      await agentService.initialize();
      
      const defaultTeamId = agentService.getDefaultTeamId();
      expect(defaultTeamId).toBeDefined();
      expect(defaultTeamId.length).toBeGreaterThan(0);
    });
  });

  describe('create', () => {
    beforeEach(async () => {
      await agentService.initialize();
    });

    it('应该创建 Agent', async () => {
      const agent = await agentService.create({
        name: 'Test Agent',
        modelId: 'deepseek-chat',
        systemPrompt: 'You are a helpful assistant.',
      });

      expect(agent.id).toBeDefined();
      expect(agent.name).toBe('Test Agent');
      expect(agent.modelId).toBe('deepseek-chat');
      expect(agent.systemPrompt).toBe('You are a helpful assistant.');
      expect(agent.temperature).toBe(0.7);
      expect(agent.maxTokens).toBe(2000);
      expect(agent.contextWindow).toBe(64000);
      expect(agent.teamId).toBe(agentService.getDefaultTeamId());
    });

    it('应该使用默认模型', async () => {
      const agent = await agentService.create({
        name: 'Test Agent',
      });

      expect(agent.modelId).toBe('deepseek-chat');
    });

    it('应该触发 agent:created 事件', async () => {
      const handler = vi.fn();
      agentService.on('agent:created', handler);

      const agent = await agentService.create({ name: 'Test Agent' });

      expect(handler).toHaveBeenCalledTimes(1);
      expect(handler).toHaveBeenCalledWith(agent);
    });
  });

  describe('get', () => {
    beforeEach(async () => {
      await agentService.initialize();
    });

    it('应该获取 Agent', async () => {
      const created = await agentService.create({ name: 'Test Agent' });
      const retrieved = await agentService.get(created.id);

      expect(retrieved).not.toBeNull();
      expect(retrieved!.name).toBe('Test Agent');
    });

    it('应该返回 null 当 Agent 不存在时', async () => {
      const agent = await agentService.get('non-existent-id');
      expect(agent).toBeNull();
    });
  });

  describe('getAll', () => {
    beforeEach(async () => {
      await agentService.initialize();
    });

    it('应该获取所有 Agent', async () => {
      await agentService.create({ name: 'Agent 1' });
      await agentService.create({ name: 'Agent 2' });

      const agents = await agentService.getAll();
      expect(agents.length).toBe(2);
    });
  });

  describe('update', () => {
    beforeEach(async () => {
      await agentService.initialize();
    });

    it('应该更新 Agent', async () => {
      const agent = await agentService.create({ name: 'Test Agent' });
      
      const updated = await agentService.update(agent.id, {
        name: 'Updated Agent',
        temperature: 0.5,
      });

      expect(updated).not.toBeNull();
      expect(updated!.name).toBe('Updated Agent');
      expect(updated!.temperature).toBe(0.5);
    });

    it('应该触发 agent:updated 事件', async () => {
      const agent = await agentService.create({ name: 'Test Agent' });
      const handler = vi.fn();
      agentService.on('agent:updated', handler);

      await agentService.update(agent.id, { name: 'Updated' });

      expect(handler).toHaveBeenCalledTimes(1);
    });
  });

  describe('delete', () => {
    beforeEach(async () => {
      await agentService.initialize();
    });

    it('应该删除 Agent', async () => {
      const agent = await agentService.create({ name: 'Test Agent' });
      
      const result = await agentService.delete(agent.id);
      
      expect(result).toBe(true);
      expect(await agentService.get(agent.id)).toBeNull();
    });

    it('应该触发 agent:deleted 事件', async () => {
      const agent = await agentService.create({ name: 'Test Agent' });
      const handler = vi.fn();
      agentService.on('agent:deleted', handler);

      await agentService.delete(agent.id);

      expect(handler).toHaveBeenCalledTimes(1);
      expect(handler).toHaveBeenCalledWith(
        expect.objectContaining({ id: agent.id })
      );
    });

    it('返回 false 当 Agent 不存在时', async () => {
      const result = await agentService.delete('non-existent-id');
      expect(result).toBe(false);
    });
  });

  describe('Agent 预留字段', () => {
    beforeEach(async () => {
      await agentService.initialize();
    });

    it('应该支持 skills 数组', async () => {
      const agent = await agentService.create({
        name: 'Test Agent',
        skills: ['coding', 'debugging'],
      });

      expect(agent.skills).toEqual(['coding', 'debugging']);
    });

    it('应该支持 mcp 数组', async () => {
      const agent = await agentService.create({
        name: 'Test Agent',
        mcp: ['mcp-server-1', 'mcp-server-2'],
      });

      expect(agent.mcp).toEqual(['mcp-server-1', 'mcp-server-2']);
    });

    it('应该支持 hooks 数组', async () => {
      const agent = await agentService.create({
        name: 'Test Agent',
        hooks: ['before-request', 'after-response'],
      });

      expect(agent.hooks).toEqual(['before-request', 'after-response']);
    });
  });
});
