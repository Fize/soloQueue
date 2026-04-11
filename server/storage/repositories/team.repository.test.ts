/**
 * TeamRepository 单元测试
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { TeamRepository } from './team.repository.js';
import { setMemoryDb } from '../db.js';
import type { Database } from 'sql.js';

describe('TeamRepository', () => {
  let repository: TeamRepository;
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

    setMemoryDb(mockDb);
    repository = new TeamRepository();
  });

  describe('create', () => {
    it('应该创建团队', async () => {
      const team = await repository.create({
        name: 'Test Team',
        description: 'Test Description',
        workspaces: ['/workspace1', '/workspace2'],
      });

      expect(team.id).toBeDefined();
      expect(team.name).toBe('Test Team');
      expect(team.description).toBe('Test Description');
      expect(team.workspaces).toEqual(['/workspace1', '/workspace2']);
      expect(team.isDefault).toBe(false);
    });

    it('应该使用默认工作空间', async () => {
      const team = await repository.create({
        name: 'Minimal Team',
      });

      expect(team.workspaces).toEqual(['~/.soloqueue']);
    });
  });

  describe('findById', () => {
    it('应该通过 ID 查询团队', async () => {
      const created = await repository.create({ name: 'Find Test' });
      const found = await repository.findById(created.id);

      expect(found).not.toBeNull();
      expect(found!.name).toBe('Find Test');
    });

    it('应该返回 null 当 ID 不存在时', async () => {
      const found = await repository.findById('non-existent');
      expect(found).toBeNull();
    });
  });

  describe('findByName', () => {
    it('应该通过名称查询团队', async () => {
      await repository.create({ name: 'Unique Name' });
      const found = await repository.findByName('Unique Name');

      expect(found).not.toBeNull();
      expect(found!.name).toBe('Unique Name');
    });

    it('应该返回 null 当名称不存在时', async () => {
      const found = await repository.findByName('Non Existent');
      expect(found).toBeNull();
    });
  });

  describe('findAll', () => {
    it('应该查询所有团队', async () => {
      await repository.create({ name: 'Team 1' });
      await repository.create({ name: 'Team 2' });

      const teams = await repository.findAll();
      expect(teams.length).toBe(2);
    });

    it('应该返回空数组当没有团队时', async () => {
      const teams = await repository.findAll();
      expect(teams).toEqual([]);
    });
  });

  describe('findDefault', () => {
    it('应该查询默认团队', async () => {
      const defaultTeam = await repository.ensureDefault();
      
      const found = await repository.findDefault();
      expect(found).not.toBeNull();
      expect(found!.id).toBe(defaultTeam.id);
      expect(found!.isDefault).toBe(true);
    });

    it('应该返回 null 当没有默认团队时', async () => {
      const found = await repository.findDefault();
      // 新数据库没有默认团队，应该返回 null
      expect(found).toBeNull();
    });
  });

  describe('update', () => {
    it('应该更新团队', async () => {
      const team = await repository.create({ name: 'Update Test' });
      
      const updated = await repository.update(team.id, {
        name: 'Updated Name',
        description: 'Updated Description',
      });

      expect(updated).not.toBeNull();
      expect(updated!.name).toBe('Updated Name');
      expect(updated!.description).toBe('Updated Description');
    });

    it('应该只更新指定字段', async () => {
      const team = await repository.create({ 
        name: 'Partial Update',
        description: 'Original Description',
      });
      
      const updated = await repository.update(team.id, {
        name: 'New Name',
      });

      expect(updated!.name).toBe('New Name');
      expect(updated!.description).toBe('Original Description');
    });

    it('应该更新工作空间', async () => {
      const team = await repository.create({ name: 'Workspaces Test' });
      
      const updated = await repository.update(team.id, {
        workspaces: ['/new/workspace'],
      });

      expect(updated!.workspaces).toEqual(['/new/workspace']);
    });

    it('应该返回 null 当团队不存在时', async () => {
      const updated = await repository.update('non-existent', { name: 'Test' });
      expect(updated).toBeNull();
    });

    it('应该返回原团队当没有更新内容时', async () => {
      const team = await repository.create({ name: 'No Changes' });
      
      const updated = await repository.update(team.id, {});
      expect(updated!.name).toBe('No Changes');
    });
  });

  describe('delete', () => {
    it('应该删除团队', async () => {
      const team = await repository.create({ name: 'Delete Test' });
      
      const result = await repository.delete(team.id);
      
      expect(result).toBe(true);
      expect(await repository.findById(team.id)).toBeNull();
    });

    it('不应该删除默认团队', async () => {
      const defaultTeam = await repository.ensureDefault();
      
      const result = await repository.delete(defaultTeam.id);
      
      expect(result).toBe(false);
    });

    it('应该返回 false 当团队不存在时', async () => {
      const result = await repository.delete('non-existent');
      expect(result).toBe(false);
    });
  });

  describe('ensureDefault', () => {
    it('应该创建默认团队当不存在时', async () => {
      const defaultTeam = await repository.ensureDefault();
      
      expect(defaultTeam.name).toBe('default');
      expect(defaultTeam.isDefault).toBe(true);
      expect(defaultTeam.workspaces).toEqual(['~/.soloqueue']);
    });

    it('应该返回已有默认团队当存在时', async () => {
      const first = await repository.ensureDefault();
      const second = await repository.ensureDefault();
      
      expect(first.id).toBe(second.id);
    });
  });
});
