/**
 * Cleaner Task 单元测试
 */

import { describe, it, expect, vi } from 'vitest';

// Mock node-cron
const mockStop = vi.fn();
const mockSchedule = vi.fn(() => ({
  stop: mockStop,
}));

vi.mock('node-cron', () => ({
  default: {
    schedule: mockSchedule,
  },
}));

// Mock cleaners
const mockSystemClean = vi.fn();
const mockTeamClean = vi.fn();
const mockSessionClean = vi.fn();

vi.mock('../cleaners/index.js', () => ({
  systemCleaner: { clean: mockSystemClean },
  teamCleaner: { clean: mockTeamClean },
  sessionCleaner: { clean: mockSessionClean },
}));

describe('CleanerTask', () => {
  let cleanerTask: any;
  let consoleLogSpy: ReturnType<typeof vi.spyOn>;
  let consoleErrorSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(async () => {
    // 清除所有 mock
    vi.clearAllMocks();
    mockSystemClean.mockResolvedValue(['file1.jsonl', 'file2.jsonl']);
    mockTeamClean.mockResolvedValue(['team1.jsonl']);
    mockSessionClean.mockResolvedValue(['session1.jsonl']);
    
    consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {});
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    
    // 重新导入获取新的实例
    vi.resetModules();
    const module = await import('./cleaner.task.js');
    cleanerTask = module.cleanerTask;
  });

  afterEach(() => {
    consoleLogSpy.mockRestore();
    consoleErrorSpy.mockRestore();
  });

  describe('start', () => {
    it('应该调用 cron.schedule', () => {
      cleanerTask.start('0 3 * * *');
      
      expect(mockSchedule).toHaveBeenCalledWith('0 3 * * *', expect.any(Function));
    });

    it('应该打印启动信息', () => {
      cleanerTask.start('0 3 * * *');
      
      expect(consoleLogSpy).toHaveBeenCalledWith(
        expect.stringContaining('[CleanerTask] Started')
      );
    });
  });

  describe('stop', () => {
    it('应该调用 job.stop', () => {
      cleanerTask.start();
      cleanerTask.stop();
      
      expect(mockStop).toHaveBeenCalled();
    });

    it('应该打印停止信息', () => {
      cleanerTask.start();
      consoleLogSpy.mockClear();
      
      cleanerTask.stop();
      
      expect(consoleLogSpy).toHaveBeenCalledWith('[CleanerTask] Stopped');
    });
  });

  describe('run', () => {
    it('应该调用所有 cleaner', async () => {
      await cleanerTask.run();
      
      expect(mockSystemClean).toHaveBeenCalled();
      expect(mockTeamClean).toHaveBeenCalled();
      expect(mockSessionClean).toHaveBeenCalled();
    });

    it('应该返回所有删除的文件', async () => {
      const deleted = await cleanerTask.run();
      
      expect(deleted).toContain('file1.jsonl');
      expect(deleted).toContain('file2.jsonl');
      expect(deleted).toContain('team1.jsonl');
      expect(deleted).toContain('session1.jsonl');
      expect(deleted.length).toBe(4);
    });

    it('清理失败时应该记录错误', async () => {
      mockSystemClean.mockRejectedValueOnce(new Error('Cleanup failed'));
      
      await cleanerTask.run();
      
      // 检查是否记录了错误 (console.error 被调用了多次)
      expect(consoleErrorSpy).toHaveBeenCalled();
      expect(consoleErrorSpy.mock.calls.some((call: any[]) => 
        call[0]?.includes('[CleanerTask] Cleanup failed')
      )).toBe(true);
    });
  });

  describe('getStats', () => {
    it('应该返回 stats 对象', () => {
      const stats = cleanerTask.getStats();
      
      expect(stats).toHaveProperty('lastRun');
      expect(stats).toHaveProperty('lastDeleted');
      expect(stats).toHaveProperty('lastError');
    });

    it('run 后应该更新 stats', async () => {
      await cleanerTask.run();
      
      const stats = cleanerTask.getStats();
      expect(stats.lastRun).toBeTruthy();
      expect(stats.lastDeleted.length).toBeGreaterThan(0);
      expect(stats.lastError).toBeNull();
    });

    it('run 失败后应该记录错误', async () => {
      mockSystemClean.mockRejectedValueOnce(new Error('Test error'));
      
      await cleanerTask.run();
      
      const stats = cleanerTask.getStats();
      expect(stats.lastError).toBeTruthy();
    });
  });
});
