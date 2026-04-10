/**
 * Session 层日志清理器
 */

import path from 'node:path';
import fs from 'node:fs/promises';
import { Cleaner } from './cleaner.interface.js';
import { LOG_DIR, ROTATE_CONFIG } from '../config.js';
import { deleteExpiredFiles, isFileExpired } from '../utils/file.js';

export class SessionCleaner implements Cleaner {
  private readonly sessionsDir: string;

  constructor(teamId?: string) {
    if (teamId) {
      this.sessionsDir = path.join(LOG_DIR, 'teams', teamId, 'sessions');
    } else {
      this.sessionsDir = path.join(LOG_DIR, 'teams', '**', 'sessions');
    }
  }

  async clean(teamId?: string): Promise<string[]> {
    const deleted: string[] = [];

    if (teamId) {
      return this.cleanTeamSessions(teamId);
    }

    // 清理所有 team 的 session
    const teamsDir = path.join(LOG_DIR, 'teams');

    try {
      const teams = await fs.readdir(teamsDir, { withFileTypes: true });

      for (const team of teams) {
        if (team.isDirectory()) {
          const teamDeleted = await this.cleanTeamSessions(team.name);
          deleted.push(...teamDeleted);
        }
      }
    } catch (err) {
      console.error('[SessionCleaner] Error:', err);
    }

    return deleted;
  }

  private async cleanTeamSessions(teamId: string): Promise<string[]> {
    const deleted: string[] = [];
    const sessionsDir = path.join(LOG_DIR, 'teams', teamId, 'sessions');

    try {
      const entries = await fs.readdir(sessionsDir, { withFileTypes: true });

      for (const entry of entries) {
        if (entry.isDirectory()) {
          const sessionDir = path.join(sessionsDir, entry.name);

          // 检查 session 目录是否过期
          const sessionExpired = await isFileExpired(sessionDir, ROTATE_CONFIG.maxDays);

          if (sessionExpired) {
            // 删除整个 session 目录
            await fs.rm(sessionDir, { recursive: true, force: true });
            deleted.push(sessionDir);
          } else {
            // 清理 session 目录内的过期文件
            const fileDeleted = await deleteExpiredFiles(sessionDir, ROTATE_CONFIG.maxDays);
            deleted.push(...fileDeleted);
          }
        }
      }
    } catch {
      // 目录不存在
    }

    return deleted;
  }
}

export const sessionCleaner = new SessionCleaner();
