/**
 * 日志系统类型定义
 */

export enum LogLevel {
  DEBUG = 'debug',
  INFO = 'info',
  WARN = 'warn',
  ERROR = 'error',
}

export enum LogCategory {
  APP = 'app',           // 应用生命周期
  ACTOR = 'actor',       // Actor 系统
  LLM = 'llm',          // LLM 调用
  HTTP = 'http',        // HTTP 请求
  WEBSOCKET = 'ws',     // WebSocket
  ERROR = 'error',      // 错误日志
  TEAM = 'team',        // Team 协作
  MESSAGES = 'messages', // 消息流
}

export interface LogEntry {
  timestamp: string;    // ISO 8601
  level: LogLevel;
  category: LogCategory;
  message: string;
  context?: Record<string, unknown>;  // 结构化上下文
  traceId?: string;     // 分布式追踪 ID
  actorId?: string;     // Actor 标识
  teamId?: string;      // Team 标识
  sessionId?: string;    // Session 标识
  duration?: number;    // 操作耗时（ms）
  error?: {
    name: string;
    message: string;
    stack?: string;
    code?: string;
  };
}

export interface LoggerConfig {
  level: LogLevel;
  console: boolean;     // 是否输出到控制台
  file: boolean;        // 是否写入文件
  logDir: string;       // 日志目录路径
  maxFileSize: number;  // 单文件最大大小（字节）
  maxDays: number;      // 保留天数
  categories: LogCategory[]; // 启用的日志分类
}

export interface LogStats {
  totalFiles: number;
  totalSize: number;    // bytes
  oldestFile: string;
  newestFile: string;
  errorCount: number;   // 错误日志数量
}
