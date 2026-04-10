/**
 * System 层日志清理器
 */

import path from 'node:path';
import { Cleaner } from './cleaner.interface.js';
import { LOG_DIR, ROTATE_CONFIG } from '../config.js';
import { deleteExpiredFiles, getDirSize } from '../utils/file.js';

export class SystemCleaner implements Cleaner {
  private readonly logDir: string;

  constructor() {
    this.logDir = path.join(LOG_DIR, 'system');
  }

  async clean(): Promise<string[]> {
    return deleteExpiredFiles(this.logDir, ROTATE_CONFIG.maxDays);
  }

  async getStats(): Promise<{ fileCount: number; totalSize: number }> {
    const { getLogFiles } = await import('../utils/file.js');
    const files = await getLogFiles(this.logDir);

    let totalSize = 0;
    for (const file of files) {
      const { getFileSize } = await import('../utils/file.js');
      totalSize += await getFileSize(file);
    }

    return { fileCount: files.length, totalSize };
  }
}

export const systemCleaner = new SystemCleaner();
