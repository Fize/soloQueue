/**
 * ============================================
 * Config Repository (配置数据访问)
 * ============================================
 *
 * 【职责】
 * - Config 实体的数据库 CRUD 操作
 * - 提供按 key、category 查询
 * - 管理种子数据的初始化
 *
 * 【数据约束】
 *
 *   1. key 唯一约束
 *   2. value 存储为 JSON 字符串格式
 *   3. 支持类型: string, number, boolean, json
 *   4. 按 category 分组管理
 *
 * 【配置分类 (category)】
 *
 *   ┌─────────────────────────────────────────┐
 *   │ app          → 应用级配置               │
 *   │ session      → 会话配置                │
 *   │ team         → 团队配置 (预留)          │
 *   │ agent        → Agent 配置 (预留)       │
 *   └─────────────────────────────────────────┘
 *
 * 【特有方法】
 *
 *   findByKey(key)      → 按配置键查找
 *   findByCategory()    → 按分类查找
 *   updateByKey()       → 按键更新
 *   seedIfEmpty()       → 批量插入种子数据
 *
 * 【日志分类】
 *
 *   category: 'db.config'
 *
 * ============================================
 */

import { v4 as uuidv4 } from 'uuid';
import { getDb, saveDb } from '../db.js';
import { Logger } from '../../logger/index.js';
import type { Config, CreateConfigInput } from '../types.js';
import type { Repository } from './base.repository.js';

export class ConfigRepository implements Repository<Config> {
  private logger: Logger;

  constructor() {
    this.logger = Logger.system();
  }

  /**
   * 根据 ID 查询
   */
  async findById(id: string): Promise<Config | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM configs WHERE id = ?`, [id]);

    if (result.length === 0 || result[0].values.length === 0) {
      this.logger.debug({
        category: 'db.config',
        message: 'Config not found',
        context: { id },
      });
      return null;
    }
    
    const config = this.mapRow(result[0].columns, result[0].values[0]);
    this.logger.debug({
      category: 'db.config',
      message: 'Config found',
      context: { id, key: config.key },
    });
    
    return config;
  }

  /**
   * 根据 key 查询
   */
  async findByKey(key: string): Promise<Config | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM configs WHERE key = ?`, [key]);

    if (result.length === 0 || result[0].values.length === 0) {
      this.logger.debug({
        category: 'db.config',
        message: 'Config not found by key',
        context: { key },
      });
      return null;
    }
    
    const config = this.mapRow(result[0].columns, result[0].values[0]);
    this.logger.debug({
      category: 'db.config',
      message: 'Config found by key',
      context: { key, id: config.id },
    });
    
    return config;
  }

  /**
   * 根据 category 查询
   */
  async findByCategory(category: string): Promise<Config[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM configs WHERE category = ?`, [category]);

    if (result.length === 0) {
      this.logger.debug({
        category: 'db.config',
        message: 'No configs found by category',
        context: { category },
      });
      return [];
    }
    
    const configs = result[0].values.map((row) => this.mapRow(result[0].columns, row));
    this.logger.debug({
      category: 'db.config',
      message: 'Configs found by category',
      context: { category, count: configs.length },
    });
    
    return configs;
  }

  /**
   * 查询所有
   */
  async findAll(): Promise<Config[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM configs`);

    if (result.length === 0) {
      this.logger.debug({
        category: 'db.config',
        message: 'No configs found',
      });
      return [];
    }
    
    const configs = result[0].values.map((row) => this.mapRow(result[0].columns, row));
    this.logger.debug({
      category: 'db.config',
      message: 'Configs found',
      context: { count: configs.length },
    });
    
    return configs;
  }

  /**
   * 创建配置
   */
  async create(input: CreateConfigInput): Promise<Config> {
    const db = getDb();
    const now = new Date().toISOString();

    const config: Config = {
      id: parseInt(uuidv4().replace(/-/g, '').slice(0, 8), 16),
      key: input.key,
      value: input.value,
      type: input.type,
      description: input.description || null,
      category: input.category,
      editable: input.editable ?? true,
      createdAt: now,
      updatedAt: now,
    };

    db.run(
      `INSERT INTO configs (id, key, value, type, description, category, editable, created_at, updated_at) 
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [
        config.id,
        config.key,
        config.value,
        config.type,
        config.description,
        config.category,
        config.editable ? 1 : 0,
        config.createdAt,
        config.updatedAt,
      ]
    );

    saveDb();
    
    this.logger.info({
      category: 'db.config',
      message: 'Config created',
      context: { id: config.id, key: config.key },
    });
    
    return config;
  }

  /**
   * 更新配置
   */
  async update(id: string, value: string): Promise<Config | null> {
    const db = getDb();
    const now = new Date().toISOString();

    db.run(`UPDATE configs SET value = ?, updated_at = ? WHERE id = ?`, [value, now, id]);

    const updated = await this.findById(id);
    
    if (updated) {
      this.logger.info({
        category: 'db.config',
        message: 'Config updated',
        context: { id, key: updated.key },
      });
    }
    
    return updated;
  }

  /**
   * 根据 key 更新配置
   */
  async updateByKey(key: string, value: string): Promise<Config | null> {
    const db = getDb();
    const now = new Date().toISOString();

    db.run(`UPDATE configs SET value = ?, updated_at = ? WHERE key = ?`, [value, now, key]);

    const updated = await this.findByKey(key);
    
    if (updated) {
      this.logger.info({
        category: 'db.config',
        message: 'Config updated by key',
        context: { key, id: updated.id },
      });
    }
    
    return updated;
  }

  /**
   * 删除配置
   */
  async delete(id: string): Promise<boolean> {
    const config = await this.findById(id);
    
    const db = getDb();
    db.run(`DELETE FROM configs WHERE id = ?`, [id]);
    saveDb();
    
    this.logger.info({
      category: 'db.config',
      message: 'Config deleted',
      context: { id, key: config?.key },
    });
    
    return true;
  }

  /**
   * 批量插入种子数据
   */
  async seedIfEmpty(configs: CreateConfigInput[]): Promise<void> {
    const existing = await this.findAll();
    if (existing.length > 0) {
      this.logger.debug({
        category: 'db.config',
        message: 'Configs already seeded',
        context: { count: existing.length },
      });
      return;
    }

    this.logger.info({
      category: 'db.config',
      message: 'Seeding configs',
      context: { count: configs.length },
    });
    
    for (const config of configs) {
      await this.create(config);
    }
  }

  /**
   * 映射数据库行到实体
   */
  private mapRow(columns: string[], values: (string | number | null | Uint8Array)[]): Config {
    const row: Record<string, string | number | null> = {};
    columns.forEach((col, i) => {
      row[col] = values[i];
    });

    return {
      id: row['id'] as number,
      key: row['key'] as string,
      value: row['value'] as string,
      type: row['type'] as Config['type'],
      description: row['description'] as string | null,
      category: row['category'] as string,
      editable: Boolean(row['editable']),
      createdAt: row['created_at'] as string,
      updatedAt: row['updated_at'] as string,
    };
  }
}

export const configRepository = new ConfigRepository();
