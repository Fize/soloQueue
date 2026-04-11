/**
 * File Utils 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import {
  ensureDir,
  ensureLogDir,
  getFileSize,
  getFileMtime,
  isFileExpired,
  deleteExpiredFiles,
  getLogFiles,
  getDirSize,
} from './file.js';

// Mock LOG_DIR
vi.mock('../config.js', () => ({
  LOG_DIR: '/mock/logs',
  ROTATE_CONFIG: { maxSize: 10 * 1024 * 1024, maxDays: 7 },
}));

describe('File Utils', () => {
  let tempDir: string;

  beforeEach(async () => {
    tempDir = path.join(os.tmpdir(), `logger-test-${Date.now()}`);
    await fs.mkdir(tempDir, { recursive: true });
  });

  afterEach(async () => {
    // 清理临时目录
    try {
      await fs.rm(tempDir, { recursive: true, force: true });
    } catch {
      // 忽略清理错误
    }
  });

  describe('ensureDir', () => {
    it('应该创建目录', async () => {
      const testDir = path.join(tempDir, 'subdir', 'nested');
      
      await ensureDir(testDir);
      
      const exists = await fs.access(testDir).then(() => true).catch(() => false);
      expect(exists).toBe(true);
    });

    it('应该处理已存在的目录', async () => {
      await ensureDir(tempDir);
      
      // 不应该报错
      expect(true).toBe(true);
    });
  });

  describe('ensureLogDir', () => {
    it('应该创建日志目录结构', async () => {
      const logBase = path.join(tempDir, 'logs');
      vi.doMock('../config.js', () => ({
        LOG_DIR: logBase,
        ROTATE_CONFIG: { maxSize: 10 * 1024 * 1024, maxDays: 7 },
      }));
      
      // 重新导入以使用 mock
      vi.resetModules();
      const { ensureLogDir: ensureLogDirMock } = await import('./file.js');
      
      await ensureLogDirMock();
      
      const systemDir = path.join(logBase, 'system');
      const teamsDir = path.join(logBase, 'teams');
      
      const systemExists = await fs.access(systemDir).then(() => true).catch(() => false);
      const teamsExists = await fs.access(teamsDir).then(() => true).catch(() => false);
      
      expect(systemExists).toBe(true);
      expect(teamsExists).toBe(true);
    });
  });

  describe('getFileSize', () => {
    it('应该返回文件大小', async () => {
      const testFile = path.join(tempDir, 'test.txt');
      await fs.writeFile(testFile, 'hello world');
      
      const size = await getFileSize(testFile);
      expect(size).toBe(11); // 'hello world' 的长度
    });

    it('文件不存在时应该返回 0', async () => {
      const size = await getFileSize(path.join(tempDir, 'nonexistent.txt'));
      expect(size).toBe(0);
    });
  });

  describe('getFileMtime', () => {
    it('应该返回文件修改时间', async () => {
      const testFile = path.join(tempDir, 'test.txt');
      await fs.writeFile(testFile, 'hello');
      
      const mtime = await getFileMtime(testFile);
      expect(mtime).toBeInstanceOf(Date);
    });

    it('文件不存在时应该返回 null', async () => {
      const mtime = await getFileMtime(path.join(tempDir, 'nonexistent.txt'));
      expect(mtime).toBeNull();
    });
  });

  describe('isFileExpired', () => {
    it('新文件不应该过期', async () => {
      const testFile = path.join(tempDir, 'test.txt');
      await fs.writeFile(testFile, 'hello');
      
      const expired = await isFileExpired(testFile, 7);
      expect(expired).toBe(false);
    });

    it('文件不存在时应该返回 true', async () => {
      const expired = await isFileExpired(path.join(tempDir, 'nonexistent.txt'), 7);
      expect(expired).toBe(true);
    });

    it('超过最大天数应该过期', async () => {
      const testFile = path.join(tempDir, 'old.txt');
      await fs.writeFile(testFile, 'old content');
      
      // 设置一个很旧的时间
      const oldDate = new Date();
      oldDate.setDate(oldDate.getDate() - 10);
      await fs.utimes(testFile, oldDate, oldDate);
      
      const expired = await isFileExpired(testFile, 7);
      expect(expired).toBe(true);
    });
  });

  describe('deleteExpiredFiles', () => {
    it('应该删除过期文件', async () => {
      const testFile = path.join(tempDir, 'expired.txt');
      await fs.writeFile(testFile, 'expired');
      
      // 设置为过期
      const oldDate = new Date();
      oldDate.setDate(oldDate.getDate() - 10);
      await fs.utimes(testFile, oldDate, oldDate);
      
      const deleted = await deleteExpiredFiles(tempDir, 7);
      
      expect(deleted.length).toBe(1);
      const exists = await fs.access(testFile).then(() => true).catch(() => false);
      expect(exists).toBe(false);
    });

    it('不应该删除未过期文件', async () => {
      const testFile = path.join(tempDir, 'recent.txt');
      await fs.writeFile(testFile, 'recent');
      
      const deleted = await deleteExpiredFiles(tempDir, 7);
      
      expect(deleted.length).toBe(0);
      const exists = await fs.access(testFile).then(() => true).catch(() => false);
      expect(exists).toBe(true);
    });

    it('应该递归处理子目录', async () => {
      const subDir = path.join(tempDir, 'subdir');
      await fs.mkdir(subDir);
      const testFile = path.join(subDir, 'expired.txt');
      await fs.writeFile(testFile, 'expired');
      
      const oldDate = new Date();
      oldDate.setDate(oldDate.getDate() - 10);
      await fs.utimes(testFile, oldDate, oldDate);
      
      const deleted = await deleteExpiredFiles(tempDir, 7);
      
      expect(deleted.some(d => d.includes('subdir'))).toBe(true);
    });

    it('目录不存在时应该返回空数组', async () => {
      // 创建一个明确不存在的路径
      const nonexistentDir = 'C:\\definitely_not_exists_' + Date.now();
      const deleted = await deleteExpiredFiles(nonexistentDir, 7);
      expect(deleted).toEqual([]);
    });
  });

  describe('getLogFiles', () => {
    it('应该返回匹配的日志文件', async () => {
      await fs.writeFile(path.join(tempDir, 'app.jsonl'), '{}');
      await fs.writeFile(path.join(tempDir, 'debug.jsonl'), '{}');
      await fs.writeFile(path.join(tempDir, 'readme.txt'), 'readme');
      
      const files = await getLogFiles(tempDir);
      
      expect(files.length).toBe(2);
      expect(files.every(f => f.endsWith('.jsonl'))).toBe(true);
    });

    it('应该使用自定义正则匹配', async () => {
      await fs.writeFile(path.join(tempDir, 'app.jsonl'), '{}');
      await fs.writeFile(path.join(tempDir, 'app.log'), 'log');
      
      const files = await getLogFiles(tempDir, /\.log$/);
      
      expect(files.length).toBe(1);
      expect(files[0]).toContain('.log');
    });

    it('目录不存在时应该返回空数组', async () => {
      const files = await getLogFiles(path.join(tempDir, 'nonexistent'));
      expect(files).toEqual([]);
    });
  });

  describe('getDirSize', () => {
    it('应该返回目录总大小', async () => {
      await fs.writeFile(path.join(tempDir, 'file1.txt'), 'hello'); // 5 bytes
      await fs.writeFile(path.join(tempDir, 'file2.txt'), 'world');  // 5 bytes
      
      const size = await getDirSize(tempDir);
      expect(size).toBe(10);
    });

    it('应该递归计算子目录大小', async () => {
      const subDir = path.join(tempDir, 'subdir');
      await fs.mkdir(subDir);
      await fs.writeFile(path.join(tempDir, 'file1.txt'), 'hello');      // 5 bytes
      await fs.writeFile(path.join(subDir, 'file2.txt'), 'world');         // 5 bytes
      
      const size = await getDirSize(tempDir);
      expect(size).toBe(10);
    });

    it('目录不存在时应该返回 0', async () => {
      const size = await getDirSize(path.join(tempDir, 'nonexistent'));
      expect(size).toBe(0);
    });
  });
});
