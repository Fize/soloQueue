/**
 * Metadata Utils 单元测试
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';

// Mock LOG_DIR
vi.mock('../config.js', () => ({
  LOG_DIR: '/mock/logs',
}));

describe('Metadata Utils', () => {
  let tempDir: string;
  let mockLogDir: string;

  beforeEach(async () => {
    tempDir = path.join(os.tmpdir(), `metadata-test-${Date.now()}`);
    mockLogDir = path.join(tempDir, 'logs');
    await fs.mkdir(mockLogDir, { recursive: true });
    
    // 重置模块以使用新的 mock
    vi.resetModules();
    vi.doMock('../config.js', () => ({
      LOG_DIR: mockLogDir,
    }));
  });

  afterEach(async () => {
    try {
      await fs.rm(tempDir, { recursive: true, force: true });
    } catch {
      // 忽略清理错误
    }
  });

  describe('loadRootMetadata', () => {
    it('应该加载存在的根目录元数据', async () => {
      const { loadRootMetadata } = await import('./metadata.js');
      
      const metaPath = path.join(mockLogDir, 'metadata.json');
      await fs.writeFile(metaPath, JSON.stringify({
        version: '2.0.0',
        createdAt: '2024-01-01T00:00:00.000Z',
        updatedAt: '2024-01-02T00:00:00.000Z',
        teamCount: 5,
      }));
      
      const metadata = await loadRootMetadata();
      
      expect(metadata.version).toBe('2.0.0');
      expect(metadata.teamCount).toBe(5);
    });

    it('文件不存在时应该返回默认元数据', async () => {
      const { loadRootMetadata } = await import('./metadata.js');
      
      const metadata = await loadRootMetadata();
      
      expect(metadata.version).toBe('1.0.0');
      expect(metadata.teamCount).toBe(0);
      expect(metadata.createdAt).toBeTruthy();
      expect(metadata.updatedAt).toBeTruthy();
    });

    it('JSON 解析失败时应该返回默认元数据', async () => {
      const { loadRootMetadata } = await import('./metadata.js');
      
      const metaPath = path.join(mockLogDir, 'metadata.json');
      await fs.writeFile(metaPath, 'invalid json');
      
      const metadata = await loadRootMetadata();
      
      expect(metadata.version).toBe('1.0.0');
      expect(metadata.teamCount).toBe(0);
    });
  });

  describe('saveRootMetadata', () => {
    it('应该保存根目录元数据', async () => {
      const { saveRootMetadata, loadRootMetadata } = await import('./metadata.js');
      
      const metadata = {
        version: '3.0.0',
        createdAt: '2024-01-01T00:00:00.000Z',
        updatedAt: '2024-01-02T00:00:00.000Z',
        teamCount: 10,
      };
      
      await saveRootMetadata(metadata);
      
      const loaded = await loadRootMetadata();
      expect(loaded.version).toBe('3.0.0');
      expect(loaded.teamCount).toBe(10);
    });
  });

  describe('loadTeamMetadata', () => {
    it('应该加载存在的团队元数据', async () => {
      const { loadTeamMetadata } = await import('./metadata.js');
      
      const teamDir = path.join(mockLogDir, 'teams', 'team-123');
      await fs.mkdir(teamDir, { recursive: true });
      await fs.writeFile(path.join(teamDir, 'metadata.json'), JSON.stringify({
        teamId: 'team-123',
        createdAt: '2024-01-01T00:00:00.000Z',
        updatedAt: '2024-01-02T00:00:00.000Z',
        sessionCount: 3,
      }));
      
      const metadata = await loadTeamMetadata('team-123');
      
      expect(metadata?.teamId).toBe('team-123');
      expect(metadata?.sessionCount).toBe(3);
    });

    it('文件不存在时应该返回 null', async () => {
      const { loadTeamMetadata } = await import('./metadata.js');
      
      const metadata = await loadTeamMetadata('nonexistent');
      
      expect(metadata).toBeNull();
    });
  });

  describe('saveTeamMetadata', () => {
    it('应该保存团队元数据', async () => {
      const { saveTeamMetadata, loadTeamMetadata } = await import('./metadata.js');
      
      const metadata = {
        teamId: 'team-new',
        createdAt: '2024-01-01T00:00:00.000Z',
        updatedAt: '2024-01-02T00:00:00.000Z',
        sessionCount: 1,
      };
      
      await saveTeamMetadata(metadata);
      
      const loaded = await loadTeamMetadata('team-new');
      expect(loaded?.teamId).toBe('team-new');
      expect(loaded?.sessionCount).toBe(1);
    });
  });

  describe('createTeamMetadata', () => {
    it('应该创建新的团队元数据', async () => {
      const { createTeamMetadata } = await import('./metadata.js');
      
      const metadata = await createTeamMetadata('team-new');
      
      expect(metadata.teamId).toBe('team-new');
      expect(metadata.sessionCount).toBe(0);
      expect(metadata.createdAt).toBeTruthy();
    });

    it('应该更新根目录元数据的 teamCount', async () => {
      const { createTeamMetadata, loadRootMetadata } = await import('./metadata.js');
      
      // 先创建第一个团队
      await createTeamMetadata('team-1');
      
      const rootBefore = await loadRootMetadata();
      expect(rootBefore.teamCount).toBe(1);
      
      // 再创建第二个团队
      await createTeamMetadata('team-2');
      
      const rootAfter = await loadRootMetadata();
      expect(rootAfter.teamCount).toBe(2);
    });
  });

  describe('updateTeamMetadata', () => {
    it('应该更新团队元数据', async () => {
      const { createTeamMetadata, updateTeamMetadata, loadTeamMetadata } = await import('./metadata.js');
      
      await createTeamMetadata('team-123');
      
      const updated = await updateTeamMetadata('team-123', {
        sessionCount: 5,
      });
      
      expect(updated?.sessionCount).toBe(5);
      expect(updated?.updatedAt).toBeTruthy();
    });

    it('团队不存在时应该返回 null', async () => {
      const { updateTeamMetadata } = await import('./metadata.js');
      
      const result = await updateTeamMetadata('nonexistent', {
        sessionCount: 1,
      });
      
      expect(result).toBeNull();
    });
  });

  describe('deleteTeamMetadata', () => {
    it('应该删除团队元数据', async () => {
      const { createTeamMetadata, deleteTeamMetadata, loadTeamMetadata } = await import('./metadata.js');
      
      await createTeamMetadata('team-123');
      
      await deleteTeamMetadata('team-123');
      
      const metadata = await loadTeamMetadata('team-123');
      expect(metadata).toBeNull();
    });

    it('应该更新根目录元数据减少 teamCount', async () => {
      const { createTeamMetadata, deleteTeamMetadata, loadRootMetadata } = await import('./metadata.js');
      
      await createTeamMetadata('team-1');
      await createTeamMetadata('team-2');
      
      await deleteTeamMetadata('team-1');
      
      const root = await loadRootMetadata();
      expect(root.teamCount).toBe(1);
    });

    it('teamCount 不应该变成负数', async () => {
      const { deleteTeamMetadata, loadRootMetadata } = await import('./metadata.js');
      
      await deleteTeamMetadata('nonexistent');
      
      const root = await loadRootMetadata();
      expect(root.teamCount).toBeGreaterThanOrEqual(0);
    });

    it('文件不存在时不应该报错', async () => {
      const { deleteTeamMetadata } = await import('./metadata.js');
      
      // 不应该抛出错误
      await expect(deleteTeamMetadata('nonexistent')).resolves.not.toThrow();
    });
  });
});
