/**
 * 安全日志模块 - 避免循环依赖
 * 用于 logger 内部模块，避免与 core.ts 形成循环依赖
 */

import { LogLevel } from './types.js';

// 延迟获取 Logger（通过动态导入避免循环依赖）
let _logger: any = null;
let _initialized = false;

async function getLogger() {
  if (!_initialized) {
    _initialized = true;
    try {
      // 动态导入避免循环依赖
      const { Logger } = await import('./core.js');
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
  const logger = await getLogger();
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
  // 同步版本：静默失败，日志系统的初始化是异步的，
  // 同步调用时 logger 可能尚未初始化，这种情况下直接跳过
  try {
    if (level === 'debug') {
      // noop - avoid blocking
    } else if (level === 'info') {
      // noop
    } else if (level === 'warn') {
      // noop
    } else if (level === 'error') {
      // noop
    }
  } catch {
    // 静默忽略，避免日志错误影响主流程
  }
}
