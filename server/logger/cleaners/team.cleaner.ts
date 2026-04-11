/**
 * Team 层日志清理器
 */

import path from 'node:path';
import fs from 'node:fs/promises';
import { Cleaner } from './cleaner.interface.js';
import { LOG_DIR, ROTATE_CONFIG } from '../config.js';
import { deleteExpiredFiles } from '../utils/file.js';
import { deleteTeamMetadata } from '../utils/metadata.js';
import { safeLogSync } from '../safe-log.js';

export class TeamCleaner implements Cleaner {
  private readonly teamsDir: string;

  constructor() {
    this.teamsDir = path.join(LOG_DIR, 'teams');
  }

  async clean(): Promise<string[]> {
    const deleted: string[] = [];

    try {
      const entries = await fs.readdir(this.teamsDir, { withFileTypes: true });

      for (const entry of entries) {
        if (entry.isDirectory()) {
          const teamDir = path.join(this.teamsDir, entry.name);

          // 清理过期的 session 目录
          const sessionDeleted = await deleteExpiredFiles(teamDir, ROTATE_CONFIG.maxDays);
          deleted.push(...sessionDeleted);

          // 检查 team 目录是否为空
          const teamFiles = await fs.readdir(teamDir);
          if (teamFiles.length === 0) {
            await fs.rmdir(teamDir);
            deleted.push(teamDir);
            await deleteTeamMetadata(entry.name);
          }
        }
      }
    } catch (err) {
      safeLogSync(
        'error',
        'logger',
        'TeamCleaner error',
        undefined,
        err instanceof Error
          ? { name: err.name, message: err.message, stack: err.stack }
          : { name: 'Unknown', message: String(err) }
      );
    }

    return deleted;
  }

  async getTeams(): Promise<string[]> {
    try {
      const entries = await fs.readdir(this.teamsDir, { withFileTypes: true });
      return entries.filter((e) => e.isDirectory()).map((e) => e.name);
    } catch {
      return [];
    }
  }
}

export const teamCleaner = new TeamCleaner();
