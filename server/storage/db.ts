/**
 * 数据库连接 - 使用 sql.js (纯 JavaScript SQLite)
 * 适用于 Tauri 桌面应用，无需编译原生模块
 */

import initSqlJs, { type Database as SqlJsDatabase } from 'sql.js';
import path from 'node:path';
import fs from 'node:fs/promises';
import os from 'node:os';

// 数据库路径
const DB_DIR = path.join(os.homedir(), '.soloqueue', 'data');
const DB_PATH = path.join(DB_DIR, 'soloqueue.db');

// 数据库实例（单例）
let _db: SqlJsDatabase | null = null;
let _initialized = false;

/**
 * 获取数据库实例
 */
export function getDb(): SqlJsDatabase {
  if (!_db) {
    throw new Error('Database not initialized. Call initDb() first.');
  }
  return _db;
}

/**
 * 检查是否已初始化
 */
export function isDbInitialized(): boolean {
  return _initialized;
}

/**
 * 初始化数据库
 */
export async function initDb(): Promise<void> {
  if (_initialized && _db) return;

  // 确保目录存在
  await fs.mkdir(DB_DIR, { recursive: true });

  // 初始化 sql.js
  const SQL = await initSqlJs();

  // 尝试加载已有数据库
  try {
    const fileBuffer = await fs.readFile(DB_PATH);
    _db = new SQL.Database(fileBuffer);
  } catch {
    // 数据库不存在，创建新的
    _db = new SQL.Database();
  }

  // 创建表（如果不存在）
  _db.run(`
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

  // 保存数据库到文件
  saveDb();

  _initialized = true;
  console.log(`[DB] Initialized at ${DB_PATH}`);
}

/**
 * 保存数据库到文件
 */
export function saveDb(): void {
  if (!_db) return;

  const data = _db.export();
  const buffer = Buffer.from(data);
  fs.writeFileSync(DB_PATH, buffer);
}

/**
 * 关闭数据库连接
 */
export async function closeDb(): Promise<void> {
  if (_db) {
    saveDb();
    _db.close();
    _db = null;
    _initialized = false;
    console.log('[DB] Closed');
  }
}

/**
 * 获取数据库路径
 */
export function getDbPath(): string {
  return DB_PATH;
}

/**
 * 重置数据库（用于测试）
 */
export function resetDb(): void {
  if (_db) {
    _db.close();
  }
  _db = null;
  _initialized = false;
}

/**
 * 设置内存数据库（用于测试）
 */
export function setMemoryDb(db: SqlJsDatabase): void {
  _db = db;
  _initialized = true;
}
