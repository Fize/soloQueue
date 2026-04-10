/**
 * 日志传输层接口
 */

export interface Transport {
  log(entry: LogEntry): Promise<void>;
  flush(): Promise<void>;
  close(): Promise<void>;
}

export interface LogEntry {
  timestamp: string;
  level: LogLevel;
  category: string;
  layer: string;
  message: string;
  context?: Record<string, unknown>;
  traceId?: string;
  actorId?: string;
  teamId?: string;
  sessionId?: string;
  duration?: number;
  error?: LogError;
}

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface LogError {
  name: string;
  message: string;
  stack?: string;
  code?: string;
}
