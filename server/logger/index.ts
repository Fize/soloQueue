/**
 * 日志系统主入口
 */

// 类型导出
export { LogLayer } from './layers.js';
export type { LayerConfig, SystemLayerConfig, TeamLayerConfig, SessionLayerConfig } from './layers.js';
export { LogLevel, LogCategory } from './types.js';
export type { LogEntry, LoggerConfig, LogStats } from './types.js';

// 核心
export { Logger } from './core.js';
export type { LoggerOptions, LogOptions } from './core.js';

// 配置
export { LOG_DIR, ROTATE_CONFIG, DEFAULT_LEVEL } from './config.js';

// 传输层
export { consoleTransport, fileTransport } from './transports/index.js';
export type { Transport, LogEntry as TransportLogEntry, LogLevel as TransportLogLevel, LogError } from './transports/index.js';

// 格式化器
export { jsonlFormatter } from './formatters/index.js';

// 清理器
export { systemCleaner, teamCleaner, sessionCleaner } from './cleaners/index.js';

// 定时任务
export { cleanerTask } from './tasks/index.js';

// 工具
export * from './utils/index.js';
