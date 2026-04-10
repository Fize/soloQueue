/**
 * 定时清理任务
 */

import cron from 'node-cron';
import { systemCleaner, teamCleaner, sessionCleaner } from '../cleaners/index.js';

export interface CleanerTaskStats {
  lastRun: Date | null;
  lastDeleted: string[];
  lastError: string | null;
}

class CleanerTask {
  private job: cron.ScheduledTask | null = null;
  private stats: CleanerTaskStats = {
    lastRun: null,
    lastDeleted: [],
    lastError: null,
  };

  /**
   * 启动定时清理任务
   * @param cronExpression 默认为每天凌晨 3 点
   */
  start(cronExpression: string = '0 3 * * *'): void {
    if (this.job) {
      console.log('[CleanerTask] Already running');
      return;
    }

    this.job = cron.schedule(cronExpression, async () => {
      await this.run();
    });

    console.log(`[CleanerTask] Started with schedule: ${cronExpression}`);
  }

  /**
   * 停止定时清理任务
   */
  stop(): void {
    if (this.job) {
      this.job.stop();
      this.job = null;
      console.log('[CleanerTask] Stopped');
    }
  }

  /**
   * 手动执行清理
   */
  async run(): Promise<string[]> {
    console.log('[CleanerTask] Starting cleanup...');
    const allDeleted: string[] = [];

    try {
      // 清理 System 层
      const systemDeleted = await systemCleaner.clean();
      allDeleted.push(...systemDeleted);
      console.log(`[CleanerTask] System: deleted ${systemDeleted.length} files`);

      // 清理 Team 层
      const teamDeleted = await teamCleaner.clean();
      allDeleted.push(...teamDeleted);
      console.log(`[CleanerTask] Team: deleted ${teamDeleted.length} files`);

      // 清理 Session 层
      const sessionDeleted = await sessionCleaner.clean();
      allDeleted.push(...sessionDeleted);
      console.log(`[CleanerTask] Session: deleted ${sessionDeleted.length} files`);

      this.stats.lastRun = new Date();
      this.stats.lastDeleted = allDeleted;
      this.stats.lastError = null;

      console.log(`[CleanerTask] Cleanup complete: ${allDeleted.length} files deleted`);
    } catch (err) {
      const error = err instanceof Error ? err.message : String(err);
      this.stats.lastError = error;
      console.error('[CleanerTask] Cleanup failed:', error);
    }

    return allDeleted;
  }

  /**
   * 获取清理任务状态
   */
  getStats(): CleanerTaskStats {
    return { ...this.stats };
  }
}

export const cleanerTask = new CleanerTask();
