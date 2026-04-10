/**
 * Logger 核心类
 */

import { v4 as uuidv4 } from 'uuid';
import { LogLayer } from './layers.js';
import type { Transport, LogEntry, LogLevel } from './transports/index.js';
import { consoleTransport, fileTransport } from './transports/index.js';
import { ensureLogDir } from './utils/file.js';

/**
 * Logger 配置
 */
export interface LoggerOptions {
  layer: LogLayer;
  teamId?: string;
  sessionId?: string;
  enableConsole?: boolean;
  enableFile?: boolean;
  minLevel?: LogLevel;
}

/**
 * 创建 LogEntry 的选项
 */
export interface LogOptions {
  category: string;
  message: string;
  context?: Record<string, unknown>;
  traceId?: string;
  actorId?: string;
  duration?: number;
  error?: {
    name: string;
    message: string;
    stack?: string;
    code?: string;
  };
}

export class Logger {
  private layer: LogLayer;
  private teamId?: string;
  private sessionId?: string;
  private transports: Transport[];
  private minLevel: LogLevel;
  private traceId?: string;

  constructor(options: LoggerOptions) {
    this.layer = options.layer;
    this.teamId = options.teamId;
    this.sessionId = options.sessionId;
    this.minLevel = options.minLevel || 'info';
    this.transports = [];

    if (options.enableConsole !== false) {
      this.transports.push(consoleTransport);
    }

    if (options.enableFile !== false) {
      this.transports.push(fileTransport);
    }

    // 初始化日志目录
    ensureLogDir().catch(console.error);
  }

  /**
   * 设置 Trace ID
   */
  setTraceId(traceId: string): void {
    this.traceId = traceId;
  }

  /**
   * 生成新的 Trace ID
   */
  newTraceId(): string {
    this.traceId = uuidv4().slice(0, 8);
    return this.traceId;
  }

  /**
   * 创建子 Logger（继承当前配置）
   */
  child(options: Partial<LoggerOptions>): Logger {
    return new Logger({
      layer: options.layer || this.layer,
      teamId: options.teamId || this.teamId,
      sessionId: options.sessionId || this.sessionId,
      enableConsole: options.enableConsole !== undefined ? options.enableConsole : true,
      enableFile: options.enableFile !== undefined ? options.enableFile : true,
      minLevel: options.minLevel || this.minLevel,
    });
  }

  /**
   * 创建 System 层 Logger
   */
  static system(options?: Partial<Pick<LoggerOptions, 'enableConsole' | 'enableFile' | 'minLevel'>>): Logger {
    return new Logger({
      layer: LogLayer.SYSTEM,
      ...options,
    });
  }

  /**
   * 创建 Team 层 Logger
   */
  static team(teamId: string, options?: Partial<Pick<LoggerOptions, 'enableConsole' | 'enableFile' | 'minLevel'>>): Logger {
    return new Logger({
      layer: LogLayer.TEAM,
      teamId,
      ...options,
    });
  }

  /**
   * 创建 Session 层 Logger
   */
  static session(teamId: string, sessionId: string, options?: Partial<Pick<LoggerOptions, 'enableConsole' | 'enableFile' | 'minLevel'>>): Logger {
    return new Logger({
      layer: LogLayer.SESSION,
      teamId,
      sessionId,
      ...options,
    });
  }

  private buildEntry(options: LogOptions): LogEntry {
    return {
      timestamp: new Date().toISOString(),
      level: 'debug', // 临时占位
      category: options.category,
      layer: this.layer,
      message: options.message,
      context: options.context,
      traceId: options.traceId || this.traceId,
      actorId: options.actorId,
      teamId: this.teamId,
      sessionId: this.sessionId,
      duration: options.duration,
      error: options.error,
    };
  }

  private async write(level: LogLevel, options: LogOptions): Promise<void> {
    const entry = this.buildEntry(options);
    entry.level = level;

    const promises = this.transports.map((t) => t.log(entry));
    await Promise.allSettled(promises);
  }

  async debug(options: LogOptions): Promise<void> {
    await this.write('debug', options);
  }

  async info(options: LogOptions): Promise<void> {
    await this.write('info', options);
  }

  async warn(options: LogOptions): Promise<void> {
    await this.write('warn', options);
  }

  async error(options: LogOptions): Promise<void> {
    await this.write('error', options);
  }

  /**
   * 记录错误并格式化
   */
  async logError(
    category: string,
    message: string,
    error: Error | unknown,
    context?: Record<string, unknown>
  ): Promise<void> {
    const err = error instanceof Error
      ? {
          name: error.name,
          message: error.message,
          stack: error.stack,
        }
      : {
          name: 'UnknownError',
          message: String(error),
        };

    await this.error({
      category,
      message,
      context,
      error: err,
    });
  }

  /**
   * 记录带耗时的操作
   */
  async logDuration(
    category: string,
    message: string,
    fn: () => Promise<void>
  ): Promise<void> {
    const start = performance.now();

    try {
      await fn();
      const duration = performance.now() - start;

      await this.info({
        category,
        message,
        duration,
      });
    } catch (err) {
      const duration = performance.now() - start;

      await this.logError(category, message, err, { duration });
    }
  }

  /**
   * 刷新所有传输器
   */
  async flush(): Promise<void> {
    const promises = this.transports.map((t) => t.flush());
    await Promise.allSettled(promises);
  }

  /**
   * 关闭所有传输器
   */
  async close(): Promise<void> {
    await this.flush();
    const promises = this.transports.map((t) => t.close());
    await Promise.allSettled(promises);
  }
}
