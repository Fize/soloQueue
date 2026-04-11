/**
 * 定时清理任务
 */

import cron from 'node-cron';
import { systemCleaner, teamCleaner, sessionCleaner } from '../cleaners/index.js';
import { Logger } from '../core.js';
import { safeLogSync } from '../safe-log.js';

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
  private logger: Logger;

  constructor() {
    this.logger = Logger.system({ enableConsole: true, enableFile: true });
  }

  /**
   * 启动定时清理任务
   * @param cronExpression 默认为每天凌晨 3 点
   */
  start(cronExpression: string = '0 3 * * *'): void {
    if (this.job) {
      this.logger.info({
        category: 'logger',
        message: 'CleanerTask already running',
      });
      return;
    }

    this.job = cron.schedule(cronExpression, async () => {
      await this.run();
    });

    this.logger.info({
      category: 'logger',
      message: 'CleanerTask started',
      context: { schedule: cronExpression },
    });
  }

  /**
   * 停止定时清理任务
   */
  stop(): void {
    if (this.job) {
      this.job.stop();
      this.job = null;
      this.logger.info({
        category: 'logger',
        message: 'CleanerTask stopped',
      });
    }
  }

  /**
   * 手动执行清理
   */
  async run(): Promise<string[]> {
    this.logger.info({
      category: 'logger',
      message: 'CleanerTask starting cleanup',
    });
    
    const allDeleted: string[] = [];

    try {
      // 清理 System 层
      const systemDeleted = await systemCleaner.clean();
      allDeleted.push(...systemDeleted);
      this.logger.debug({
        category: 'logger',
        message: 'System cleanup complete',
        context: { deleted: systemDeleted.length },
      });

      // 清理 Team 层
      const teamDeleted = await teamCleaner.clean();
      allDeleted.push(...teamDeleted);
      this.logger.debug({
        category: 'logger',
        message: 'Team cleanup complete',
        context: { deleted: teamDeleted.length },
      });

      // 清理 Session 层
      const sessionDeleted = await sessionCleaner.clean();
      allDeleted.push(...sessionDeleted);
      this.logger.debug({
        category: 'logger',
        message: 'Session cleanup complete',
        context: { deleted: sessionDeleted.length },
      });

      this.stats.lastRun = new Date();
      this.stats.lastDeleted = allDeleted;
      this.stats.lastError = null;

      this.logger.info({
        category: 'logger',
        message: 'Cleanup complete',
        context: { totalDeleted: allDeleted.length },
      });
    } catch (err) {
      const error = err instanceof Error ? err.message : String(err);
      this.stats.lastError = error;
      this.logger.error({
        category: 'logger',
        message: 'Cleanup failed',
        error: err instanceof Error
          ? { name: err.name, message: err.message, stack: err.stack }
          : { name: 'Unknown', message: String(err) },
      });
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
