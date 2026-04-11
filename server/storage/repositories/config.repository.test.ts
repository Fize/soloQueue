/**
 * ConfigRepository 单元测试
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { ConfigRepository } from './config.repository.js';
import { setMemoryDb } from '../db.js';
import type { Database } from 'sql.js';

describe('ConfigRepository', () => {
  let repository: ConfigRepository;
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

    setMemoryDb(mockDb);
    repository = new ConfigRepository();
  });

  describe('create', () => {
    it('应该创建配置', async () => {
      const config = await repository.create({
        key: 'test.key',
        value: '"test-value"',
        type: 'string',
        category: 'test',
        description: 'Test config',
      });

      expect(config.id).toBeDefined();
      expect(config.key).toBe('test.key');
      expect(config.value).toBe('"test-value"');
      expect(config.type).toBe('string');
      expect(config.category).toBe('test');
    });

    it('应该创建不同类型的配置', async () => {
      const numberConfig = await repository.create({
        key: 'number.config',
        value: '123',
        type: 'number',
        category: 'test',
      });

      expect(numberConfig.type).toBe('number');
    });
  });

  describe('findById', () => {
    it('应该通过 ID 查询配置', async () => {
      const created = await repository.create({
        key: 'find.id',
        value: '"value"',
        type: 'string',
        category: 'test',
      });

      const found = await repository.findById(created.id.toString());
      expect(found).not.toBeNull();
      expect(found!.key).toBe('find.id');
    });

    it('应该返回 null 当 ID 不存在时', async () => {
      const found = await repository.findById('999999');
      expect(found).toBeNull();
    });
  });

  describe('findByKey', () => {
    it('应该通过 key 查询配置', async () => {
      await repository.create({
        key: 'unique.key',
        value: '"value"',
        type: 'string',
        category: 'test',
      });

      const found = await repository.findByKey('unique.key');
      expect(found).not.toBeNull();
      expect(found!.key).toBe('unique.key');
    });

    it('应该返回 null 当 key 不存在时', async () => {
      const found = await repository.findByKey('non.existent');
      expect(found).toBeNull();
    });
  });

  describe('findByCategory', () => {
    it('应该按分类查询配置', async () => {
      await repository.create({
        key: 'app.setting1',
        value: '"value1"',
        type: 'string',
        category: 'app',
      });
      await repository.create({
        key: 'app.setting2',
        value: '"value2"',
        type: 'string',
        category: 'app',
      });
      await repository.create({
        key: 'session.setting',
        value: '"value"',
        type: 'string',
        category: 'session',
      });

      const appConfigs = await repository.findByCategory('app');
      expect(appConfigs.length).toBe(2);
      expect(appConfigs.every(c => c.category === 'app')).toBe(true);
    });

    it('应该返回空数组当分类不存在时', async () => {
      const configs = await repository.findByCategory('non-existent');
      expect(configs).toEqual([]);
    });
  });

  describe('findAll', () => {
    it('应该查询所有配置', async () => {
      await repository.create({ key: 'key1', value: '"v1"', type: 'string', category: 'test' });
      await repository.create({ key: 'key2', value: '"v2"', type: 'string', category: 'test' });

      const configs = await repository.findAll();
      expect(configs.length).toBe(2);
    });
  });

  describe('update', () => {
    it('应该更新配置值', async () => {
      const created = await repository.create({
        key: 'update.test',
        value: '"old"',
        type: 'string',
        category: 'test',
      });

      const updated = await repository.update(created.id.toString(), '"new"');
      expect(updated).not.toBeNull();
      expect(updated!.value).toBe('"new"');
    });
  });

  describe('updateByKey', () => {
    it('应该通过 key 更新配置', async () => {
      await repository.create({
        key: 'update.bykey',
        value: '"old"',
        type: 'string',
        category: 'test',
      });

      const updated = await repository.updateByKey('update.bykey', '"updated"');
      expect(updated).not.toBeNull();
      expect(updated!.value).toBe('"updated"');
    });
  });

  describe('delete', () => {
    it('应该删除配置', async () => {
      const created = await repository.create({
        key: 'delete.test',
        value: '"value"',
        type: 'string',
        category: 'test',
      });

      const result = await repository.delete(created.id.toString());
      expect(result).toBe(true);
      expect(await repository.findById(created.id.toString())).toBeNull();
    });
  });

  describe('seedIfEmpty', () => {
    it('应该批量插入种子数据', async () => {
      const seeds = [
        { key: 'seed.1', value: '"v1"', type: 'string' as const, category: 'seed' },
        { key: 'seed.2', value: '"v2"', type: 'string' as const, category: 'seed' },
      ];

      await repository.seedIfEmpty(seeds);

      const configs = await repository.findAll();
      expect(configs.length).toBe(2);
    });

    it('不应该重复插入当已有时', async () => {
      // seedIfEmpty 在已有数据时不插入任何新数据
      await repository.create({ key: 'existing', value: '"v"', type: 'string', category: 'test' });
      
      const seeds = [
        { key: 'new.seed', value: '"v"', type: 'string' as const, category: 'test' },
      ];

      await repository.seedIfEmpty(seeds);

      // 只会保留已有的配置，不会插入新的
      const configs = await repository.findAll();
      expect(configs.length).toBe(1);
    });
  });
});
