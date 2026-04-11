/**
 * Path Utils 单元测试
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock LOG_DIR
vi.mock('../config.js', () => ({
  LOG_DIR: '/mock/logs',
}));

describe('Path Utils', () => {
  beforeEach(() => {
    vi.resetModules();
  });

  describe('getSystemLogPath', () => {
    it('应该返回正确的系统日志路径', async () => {
      const { getSystemLogPath } = await import('./path.js');
      
      const logPath = getSystemLogPath('logger');
      expect(logPath).toContain('system');
      expect(logPath).toContain('logger.jsonl');
    });
  });

  describe('getTeamLogPath', () => {
    it('应该返回正确的团队日志路径', async () => {
      const { getTeamLogPath } = await import('./path.js');
      
      const logPath = getTeamLogPath('team-123');
      expect(logPath).toContain('teams');
      expect(logPath).toContain('team-123');
      expect(logPath).toContain('team.jsonl');
    });
  });

  describe('getSessionLogPath', () => {
    it('应该返回正确的会话日志路径', async () => {
      const { getSessionLogPath } = await import('./path.js');
      
      const logPath = getSessionLogPath('team-123', 'session-456', 'message');
      expect(logPath).toContain('teams');
      expect(logPath).toContain('team-123');
      expect(logPath).toContain('sessions');
      expect(logPath).toContain('session-456');
      expect(logPath).toContain('message.jsonl');
    });
  });

  describe('getSessionMetadataPath', () => {
    it('应该返回正确的会话元数据路径', async () => {
      const { getSessionMetadataPath } = await import('./path.js');
      
      const metaPath = getSessionMetadataPath('team-123', 'session-456');
      expect(metaPath).toContain('teams');
      expect(metaPath).toContain('session-456');
      expect(metaPath).toContain('metadata.json');
    });
  });

  describe('getTeamMetadataPath', () => {
    it('应该返回正确的团队元数据路径', async () => {
      const { getTeamMetadataPath } = await import('./path.js');
      
      const metaPath = getTeamMetadataPath('team-123');
      expect(metaPath).toContain('teams');
      expect(metaPath).toContain('team-123');
      expect(metaPath).toContain('metadata.json');
    });
  });

  describe('getRootMetadataPath', () => {
    it('应该返回根目录元数据路径', async () => {
      const { getRootMetadataPath } = await import('./path.js');
      
      const metaPath = getRootMetadataPath();
      expect(metaPath).toContain('metadata.json');
    });
  });

  describe('normalizeTeamId', () => {
    it('应该返回规范化后的 teamId', async () => {
      const { normalizeTeamId } = await import('./path.js');
      
      expect(normalizeTeamId('team-123')).toBe('team-123');
    });

    it('应该替换特殊字符', async () => {
      const { normalizeTeamId } = await import('./path.js');
      
      expect(normalizeTeamId('team@123#!')).toBe('team_123__');
    });

    it('应该保留字母数字和连字符', async () => {
      const { normalizeTeamId } = await import('./path.js');
      
      expect(normalizeTeamId('my-team_v2')).toBe('my-team_v2');
    });
  });

  describe('normalizeSessionId', () => {
    it('应该返回规范化后的 sessionId', async () => {
      const { normalizeSessionId } = await import('./path.js');
      
      expect(normalizeSessionId('session-456')).toBe('session-456');
    });

    it('应该替换特殊字符', async () => {
      const { normalizeSessionId } = await import('./path.js');
      
      expect(normalizeSessionId('session@456!')).toBe('session_456_');
    });
  });
});
