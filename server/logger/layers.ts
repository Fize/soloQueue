/**
 * 日志层级
 */
export enum LogLayer {
  SYSTEM = 'system',
  TEAM = 'team',
  SESSION = 'session',
}

/**
 * 日志层级配置
 */
export interface LayerConfig {
  layer: LogLayer;
  enabled: boolean;
  maxSize: number;
  maxDays: number;
  categories?: string[];
}

/**
 * 系统层日志配置
 */
export interface SystemLayerConfig extends LayerConfig {
  layer: LogLayer.SYSTEM;
}

/**
 * Team 层日志配置
 */
export interface TeamLayerConfig extends LayerConfig {
  layer: LogLayer.TEAM;
  teamId: string;
}

/**
 * Session 层日志配置
 */
export interface SessionLayerConfig extends LayerConfig {
  layer: LogLayer.SESSION;
  teamId: string;
  sessionId: string;
}
