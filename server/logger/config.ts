/**
 * 日志系统配置
 */

import path from 'node:path';
import os from 'node:os';

// 日志目录
export const LOG_DIR = path.join(os.homedir(), '.soloqueue', 'logs');

// 轮转配置
export const ROTATE_CONFIG = {
  maxSize: 50 * 1024 * 1024, // 50MB
  maxDays: 30,               // 保留 30 天
  maxFiles: 10,              // 最大文件数量
} as const;

// System 层日志分类
export const SYSTEM_CATEGORIES = {
  APP: 'app',
  ERROR: 'error',
  HTTP: 'http',
} as const;

// Team 层日志分类
export const TEAM_CATEGORIES = {
  TEAM: 'team',
} as const;

// Session 层日志分类
export const SESSION_CATEGORIES = {
  ACTOR: 'actor',
  LLM: 'llm',
  MESSAGES: 'messages',
} as const;

// 日志文件命名模板
export const LOG_FILE_PATTERNS = {
  system: '{category}-%DATE%.jsonl',
  team: 'team-%TEAM_ID%.jsonl',
  session: '{type}.jsonl',
} as const;

// 日期格式
export const DATE_FORMAT = 'YYYY-MM-DD';

// 默认日志级别
export const DEFAULT_LEVEL = 'info';
