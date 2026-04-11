/**
 * ============================================
 * 数据库迁移 (Migrations)
 * ============================================
 *
 * 【职责】
 * - 确保数据库表结构符合最新 Schema
 * - 添加新列 (ALTER TABLE ADD COLUMN)
 * - 创建新表 (CREATE TABLE IF NOT EXISTS)
 * - 渐进式迁移，不破坏现有数据
 *
 * 【迁移策略】
 *
 *   1. 检查现有表结构
 *      ↓
 *   2. 对比目标 Schema
 *      ↓
 *   3. 执行 ALTER/CREATE 语句
 *      ↓
 *   4. 保存数据库
 *
 * 【已实现的迁移】
 *
 *   v1.0 → v1.1
 *   - teams 表: 添加 is_default 列
 *
 *   v1.1 → v1.2
 *   - 创建 configs 表 (如不存在)
 *   - 创建 agents 表 (如不存在)
 *
 * 【注意事项】
 *
 *   ⚠️ SQLite 不支持 DROP COLUMN
 *   ⚠️ SQLite 不支持 RENAME COLUMN (需要重建表)
 *   ⚠️ 本迁移系统为增量式，不支持回滚
 *
 * 【最佳实践】
 *
 *   - 每次添加列前先检查是否存在
 *   - 使用 IF NOT EXISTS 避免重复创建
 *   - 迁移后立即保存数据库
 *
 * ============================================
 */

import { getDb } from './db.js';
import { saveDb } from './db.js';

/**
 * 执行数据库迁移
 * 确保所有必要的表和列都存在
 */
export async function runMigrations(): Promise<void> {
  const db = getDb();

  // 1. 检查 teams 表是否有 is_default 列
  const teamColumns = db.exec("PRAGMA table_info(teams)");
  const teamColumnNames = teamColumns.length > 0 
    ? teamColumns[0].values.map(v => v[1] as string)
    : [];

  // 添加 is_default 列
  if (!teamColumnNames.includes('is_default')) {
    db.run(`ALTER TABLE teams ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`);
  }

  // 2. 确保存在 configs 表
  const configsExists = db.exec(`
    SELECT name FROM sqlite_master WHERE type='table' AND name='configs'
  `);
  
  if (configsExists.length === 0 || configsExists[0].values.length === 0) {
    db.run(`
      CREATE TABLE IF NOT EXISTS configs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        key TEXT NOT NULL UNIQUE,
        value TEXT NOT NULL,
        type TEXT NOT NULL DEFAULT 'string',
        description TEXT,
        category TEXT NOT NULL,
        editable INTEGER NOT NULL DEFAULT 1,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    `);
  }

  // 3. 确保存在 agents 表
  const agentsExists = db.exec(`
    SELECT name FROM sqlite_master WHERE type='table' AND name='agents'
  `);

  if (agentsExists.length === 0 || agentsExists[0].values.length === 0) {
    db.run(`
      CREATE TABLE IF NOT EXISTS agents (
        id TEXT PRIMARY KEY,
        team_id TEXT NOT NULL REFERENCES teams(id),
        name TEXT NOT NULL,
        model TEXT NOT NULL DEFAULT 'deepseek-chat',
        system_prompt TEXT NOT NULL DEFAULT '',
        temperature REAL NOT NULL DEFAULT 0.7,
        max_tokens INTEGER NOT NULL DEFAULT 2000,
        context_window INTEGER NOT NULL DEFAULT 64000,
        skills TEXT NOT NULL DEFAULT '[]',
        mcp TEXT NOT NULL DEFAULT '[]',
        hooks TEXT NOT NULL DEFAULT '[]',
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
      )
    `);
  }

  saveDb();
}
