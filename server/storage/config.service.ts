/**
 * ============================================
 * Config Service (配置服务)
 * ============================================
 *
 * 【职责】
 * - 提供配置的读取、写入、更新
 * - 配置缓存管理 (内存)
 * - 配置变更事件通知 (EventEmitter)
 * - 配置热加载支持
 *
 * 【架构】
 *
 *   ┌─────────────────────────────────────────────┐
 *   │              ConfigService                    │
 *   │  ┌─────────────────────────────────────────┐ │
 *   │  │           Cache (内存 Map)                │ │
 *   │  │  key → { value, type }                  │ │
 *   │  └─────────────────────────────────────────┘ │
 *   │                      ↑                        │
 *   │                      │                        │
 *   │  ┌─────────────────────────────────────────┐ │
 *   │  │       ConfigRepository (数据库)          │ │
 *   │  └─────────────────────────────────────────┘ │
 *   └─────────────────────────────────────────────┘
 *                         │
 *                         ▼
 *   ┌─────────────────────────────────────────────┐
 *   │              EventEmitter                     │
 *   │  触发 'change' 事件通知订阅者                  │
 *   └─────────────────────────────────────────────┘
 *
 * 【使用方式】
 *
 *   // 初始化 (启动时调用一次)
 *   await configService.initialize();
 *
 *   // 读取配置 (从缓存)
 *   const theme = configService.get('app.theme', 'dark');
 *
 *   // 更新配置 (写数据库 + 更新缓存 + 触发事件)
 *   await configService.set('app.theme', 'light');
 *
 *   // 监听变更
 *   configService.on('change', (event) => {
 *     console.log(`${event.key}: ${event.oldValue} → ${event.newValue}`);
 *   });
 *
 *   // 重新加载
 *   await configService.reload();
 *
 * 【日志分类】
 *
 *   category: 'config'
 *
 * ============================================
 */

import { EventEmitter } from 'events';
import { Logger } from '../logger/index.js';
import { configRepository } from './repositories/config.repository.js';
import { DEFAULT_CONFIGS, DEFAULT_LLM_CONFIGS } from './seeds.js';
import type { Config, ConfigType } from './types.js';

export interface ConfigChangeEvent {
  key: string;
  oldValue: unknown;
  newValue: unknown;
  timestamp: Date;
}

class ConfigService extends EventEmitter {
  private logger: Logger;
  private cache: Map<string, { value: unknown; type: ConfigType }> = new Map();
  private initialized = false;

  constructor() {
    super();
    this.logger = Logger.system();
  }

  /**
   * 初始化 - 加载种子数据 + 填充缓存
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    this.logger.info({
      category: 'config',
      message: 'Initializing config service',
    });

    // 初始化基础配置
    await configRepository.seedIfEmpty(DEFAULT_CONFIGS);

    // 初始化 LLM 配置
    await configRepository.seedIfEmpty(DEFAULT_LLM_CONFIGS);

    // 加载所有配置到缓存
    const configs = await configRepository.findAll();
    for (const config of configs) {
      const value = this.parseValue(config.value, config.type);
      this.cache.set(config.key, { value, type: config.type });
    }

    this.initialized = true;
    this.logger.info({
      category: 'config',
      message: 'Config service initialized',
      context: { count: configs.length },
    });
  }

  /**
   * 获取配置值
   */
  get<T>(key: string, defaultValue?: T): T | undefined {
    const cached = this.cache.get(key);
    if (cached) return cached.value as T;
    return defaultValue;
  }

  /**
   * 获取配置（带类型转换）
   */
  getString(key: string, defaultValue?: string): string | undefined {
    return this.get<string>(key, defaultValue);
  }

  getNumber(key: string, defaultValue?: number): number | undefined {
    return this.get<number>(key, defaultValue);
  }

  getBoolean(key: string, defaultValue?: boolean): boolean | undefined {
    return this.get<boolean>(key, defaultValue);
  }

  /**
   * 设置配置
   */
  async set<T>(key: string, value: T): Promise<void> {
    const cached = this.cache.get(key);
    const oldValue = cached?.value;
    const type = cached?.type || this.inferType(value);

    // 写入数据库
    const jsonValue = JSON.stringify(value);
    await configRepository.updateByKey(key, jsonValue);

    // 更新缓存
    this.cache.set(key, { value, type });

    // 记录日志
    this.logger.info({
      category: 'config',
      message: 'Config updated',
      context: { key, oldValue, newValue: value },
    });

    // 触发变更事件
    this.emit('change', {
      key,
      oldValue,
      newValue: value,
      timestamp: new Date(),
    } as ConfigChangeEvent);
  }

  /**
   * 批量获取配置
   */
  getAll(category?: string): Record<string, unknown> {
    const result: Record<string, unknown> = {};
    for (const [key, { value }] of this.cache) {
      if (!category || key.startsWith(`${category}.`)) {
        result[key] = value;
      }
    }
    return result;
  }

  /**
   * 获取所有配置项元数据
   */
  async getAllMetadata(): Promise<Config[]> {
    return configRepository.findAll();
  }

  /**
   * 获取分类下的所有配置
   */
  async getByCategory(category: string): Promise<Config[]> {
    return configRepository.findByCategory(category);
  }

  /**
   * 重新加载配置
   */
  async reload(): Promise<void> {
    this.cache.clear();
    this.initialized = false;
    await this.initialize();
  }

  /**
   * 检查是否已初始化
   */
  isInitialized(): boolean {
    return this.initialized;
  }

  /**
   * 推断值的类型
   */
  private inferType(value: unknown): ConfigType {
    if (typeof value === 'boolean') return 'boolean';
    if (typeof value === 'number') return 'number';
    if (typeof value === 'object') return 'json';
    return 'string';
  }

  /**
   * 解析配置值
   */
  private parseValue(value: string, type: ConfigType): unknown {
    try {
      const parsed = JSON.parse(value);
      return parsed;
    } catch {
      return value;
    }
  }
}

export const configService = new ConfigService();
