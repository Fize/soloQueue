/**
 * 数据库连接
 */

import Database from 'better-sqlite3';
import { drizzle } from 'drizzle-orm/better-sqlite3';
import path from 'node:path';
import fs from 'node:fs/promises';
import os from 'node:os';
import * as schema from './schema.js';

// 数据库路径
const DB_DIR = path.join(os.homedir(), '.soloqueue', 'data');
const DB_PATH = path.join(DB_DIR, 'soloqueue.db');

// 数据库实例
let _db: ReturnType<typeof drizzle> | null = null;

/**
 * 获取数据库实例
 */
export function getDb() {
  if (!_db) {
    throw new Error('Database not initialized. Call initDb() first.');
  }
  return _db;
}

/**
 * 初始化数据库
 */
export async function initDb(): Promise<void> {
  // 确保目录存在
  await fs.mkdir(DB_DIR, { recursive: true });

  // 创建数据库连接
  const sqlite = new Database(DB_PATH);
  sqlite.pragma('journal_mode = WAL');

  // 创建 drizzle 实例
  _db = drizzle(sqlite, { schema });

  // 创建表（如果不存在）
  createTables(sqlite);

  console.log(`[DB] Initialized at ${DB_PATH}`);
}

/**
 * 创建表（简化版本，不用迁移）
 */
function createTables(sqlite: Database.Database): void {
  sqlite.exec(`
    CREATE TABLE IF NOT EXISTS teams (
      id TEXT PRIMARY KEY,
      name TEXT NOT NULL,
      description TEXT,
      status TEXT NOT NULL DEFAULT 'active',
      config TEXT NOT NULL DEFAULT '{}',
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );
  `);
}

/**
 * 关闭数据库连接
 */
export async function closeDb(): Promise<void> {
  if (_db) {
    // SQLite 不需要显式关闭，但 drizzle 没有 close 方法
    _db = null;
    console.log('[DB] Closed');
  }
}

/**
 * 获取数据库路径
 */
export function getDbPath(): string {
  return DB_PATH;
}
