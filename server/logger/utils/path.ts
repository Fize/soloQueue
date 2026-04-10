/**
 * 路径工具
 */

import path from 'node:path';
import { LOG_DIR } from '../config.js';

/**
 * 获取 System 层日志路径
 */
export function getSystemLogPath(category: string): string {
  return path.join(LOG_DIR, 'system', `${category}.jsonl`);
}

/**
 * 获取 Team 层日志路径
 */
export function getTeamLogPath(teamId: string): string {
  return path.join(LOG_DIR, 'teams', teamId, 'team.jsonl');
}

/**
 * 获取 Session 层日志路径
 */
export function getSessionLogPath(
  teamId: string,
  sessionId: string,
  type: string
): string {
  return path.join(
    LOG_DIR,
    'teams',
    teamId,
    'sessions',
    sessionId,
    `${type}.jsonl`
  );
}

/**
 * 获取 Session 元数据路径
 */
export function getSessionMetadataPath(teamId: string, sessionId: string): string {
  return path.join(
    LOG_DIR,
    'teams',
    teamId,
    'sessions',
    sessionId,
    'metadata.json'
  );
}

/**
 * 获取 Team 元数据路径
 */
export function getTeamMetadataPath(teamId: string): string {
  return path.join(LOG_DIR, 'teams', teamId, 'metadata.json');
}

/**
 * 获取根目录元数据路径
 */
export function getRootMetadataPath(): string {
  return path.join(LOG_DIR, 'metadata.json');
}

/**
 * 规范化 teamId
 */
export function normalizeTeamId(teamId: string): string {
  return teamId.replace(/[^a-zA-Z0-9-_]/g, '_');
}

/**
 * 规范化 sessionId
 */
export function normalizeSessionId(sessionId: string): string {
  return sessionId.replace(/[^a-zA-Z0-9-_]/g, '_');
}
