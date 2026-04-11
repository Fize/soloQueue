/**
 * Database 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { resetDb, getDbPath, isDbInitialized, isMemoryMode } from './db.js';

describe('Database', () => {
  beforeEach(() => {
    resetDb();
  });

  afterEach(() => {
    resetDb();
  });

  describe('getDbPath', () => {
    it('应该返回数据库路径', () => {
      const dbPath = getDbPath();
      expect(dbPath).toContain('soloqueue.db');
    });

    it('路径应该包含用户目录', () => {
      const dbPath = getDbPath();
      expect(dbPath).toContain('.soloqueue');
    });
  });

  describe('isDbInitialized', () => {
    it('初始状态应该为 false', () => {
      expect(isDbInitialized()).toBe(false);
    });

    it('resetDb 后应该为 false', () => {
      // 即使之前初始化过，resetDb 后也应该为 false
      resetDb();
      expect(isDbInitialized()).toBe(false);
    });
  });

  describe('isMemoryMode', () => {
    it('初始状态应该为 false', () => {
      expect(isMemoryMode()).toBe(false);
    });

    it('resetDb 后应该为 false', () => {
      resetDb();
      expect(isMemoryMode()).toBe(false);
    });
  });

  describe('resetDb', () => {
    it('应该重置数据库状态', () => {
      resetDb();
      
      expect(isDbInitialized()).toBe(false);
      expect(isMemoryMode()).toBe(false);
    });

    it('多次重置不应该报错', () => {
      expect(() => {
        resetDb();
        resetDb();
        resetDb();
      }).not.toThrow();
    });
  });
});
