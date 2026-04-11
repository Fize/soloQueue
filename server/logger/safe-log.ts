/**
 * 安全日志模块 - 避免循环依赖
 * 用于 logger 内部模块，避免与 core.ts 形成循环依赖
 */

import { LogLevel, LogCategory } from './types.js';

// 日志级别映射
const LEVELS = ['debug', 'info', 'warn', 'error'];

interface SafeLogEntry {
  timestamp: string;
  level: LogLevel;
  category: string;
  message: string;
  context?: Record<string, unknown>;
  error?: {
    name: string;
    message: string;
    stack?: string;
  };
}

// 延迟获取 Logger（通过全局实例避免循环依赖）
let _logger: any = null;
let _initialized = false;

function getLogger() {
  if (!_initialized) {
    _initialized = true;
    try {
      // 动态导入避免循环依赖
      const { Logger } = require('./core.js');
      _logger = Logger.system({ enableConsole: false, enableFile: true });
    } catch {
      // 日志系统未初始化
    }
  }
  return _logger;
}

/**
 * 安全记录日志（静默失败，不抛出异常）
 */
export async function safeLog(
  level: LogLevel,
  category: string,
  message: string,
  context?: Record<string, unknown>,
  error?: { name: string; message: string; stack?: string }
): Promise<void> {
  const logger = getLogger();
  if (!logger) return;

  try {
    if (level === 'debug') {
      await logger.debug({ category, message, context });
    } else if (level === 'info') {
      await logger.info({ category, message, context });
    } else if (level === 'warn') {
      await logger.warn({ category, message, context });
    } else if (level === 'error') {
      await logger.error({ category, message, context, error });
    }
  } catch {
    // 静默忽略日志错误
  }
}

/**
 * 安全记录日志（同步版本，用于 catch 块）
 */
export function safeLogSync(
  level: LogLevel,
  category: string,
  message: string,
  context?: Record<string, unknown>,
  error?: { name: string; message: string; stack?: string }
): void {
  const logger = getLogger();
  if (!logger) return;

  try {
    const entry: SafeLogEntry = {
      timestamp: new Date().toISOString(),
      level,
      category,
      message,
      context,
      error,
    };

    // 写入到控制台作为后备
    if (level === 'error') {
      console.error(`[${category}] ${message}`, context || '', error ? `\n${error.stack || error.message}` : '');
    } else if (level === 'warn') {
      console.warn(`[${category}] ${message}`, context || '');
    } else {
      console.log(`[${category}] ${message}`, context || '');
    }
  } catch {
    // 静默忽略
  }
}
