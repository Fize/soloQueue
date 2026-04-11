/**
 * ConfigService 单元测试
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { configService } from './config.service.js';
import { setMemoryDb } from './db.js';
import type { Database } from 'sql.js';

describe('ConfigService', () => {
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
    configService.reload();
  });

  describe('initialize', () => {
    it('应该初始化并加载种子数据', async () => {
      await configService.initialize();
      
      expect(configService.isInitialized()).toBe(true);
      expect(configService.get('app.theme')).toBe('dark');
      expect(configService.get('app.language')).toBe('zh-CN');
    });

    it('不应该重复初始化', async () => {
      await configService.initialize();
      const configs1 = configService.getAll();
      
      await configService.initialize();
      const configs2 = configService.getAll();
      
      expect(configs1).toEqual(configs2);
    });
  });

  describe('get', () => {
    beforeEach(async () => {
      await configService.initialize();
    });

    it('应该返回配置值', () => {
      expect(configService.get('app.theme')).toBe('dark');
      expect(configService.get('session.timeout')).toBe(3600);
      expect(configService.get('session.autoSave')).toBe(true);
    });

    it('应该返回默认值当配置不存在时', () => {
      expect(configService.get('not.exist', 'default')).toBe('default');
      expect(configService.get('not.exist', 123)).toBe(123);
    });

    it('应该正确获取不同类型的配置', () => {
      expect(configService.getString('app.theme')).toBe('dark');
      expect(configService.getNumber('session.timeout')).toBe(3600);
      expect(configService.getBoolean('session.autoSave')).toBe(true);
    });
  });

  describe('set', () => {
    beforeEach(async () => {
      await configService.initialize();
    });

    it('应该设置配置值', async () => {
      await configService.set('app.theme', 'light');
      expect(configService.get('app.theme')).toBe('light');
    });

    it('应该触发 change 事件', async () => {
      const handler = vi.fn();
      configService.on('change', handler);
      
      await configService.set('app.theme', 'light');
      
      expect(handler).toHaveBeenCalledTimes(1);
      expect(handler).toHaveBeenCalledWith(
        expect.objectContaining({
          key: 'app.theme',
          oldValue: 'dark',
          newValue: 'light',
        })
      );
    });
  });

  describe('getAll', () => {
    beforeEach(async () => {
      await configService.initialize();
    });

    it('应该返回所有配置', () => {
      const all = configService.getAll();
      expect(all).toHaveProperty('app.theme');
      expect(all).toHaveProperty('session.timeout');
    });

    it('应该按分类返回配置', () => {
      const appConfig = configService.getAll('app');
      expect(appConfig).toHaveProperty('app.theme');
      expect(appConfig).toHaveProperty('app.language');
      expect(appConfig).not.toHaveProperty('session.timeout');
    });
  });

  describe('reload', () => {
    it('应该重新加载配置', async () => {
      await configService.initialize();
      await configService.set('app.theme', 'light');
      
      await configService.reload();
      
      expect(configService.get('app.theme')).toBe('light');
    });
  });
});
