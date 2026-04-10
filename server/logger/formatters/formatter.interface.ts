/**
 * 格式化器接口
 */

export interface Formatter {
  format(entry: LogEntry): string;
}

export interface LogEntry {
  timestamp: string;
  level: string;
  category: string;
  layer: string;
  message: string;
  context?: Record<string, unknown>;
  traceId?: string;
  actorId?: string;
  teamId?: string;
  sessionId?: string;
  duration?: number;
  error?: {
    name: string;
    message: string;
    stack?: string;
    code?: string;
  };
}
