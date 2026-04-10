/**
 * 文件操作工具
 */

import fs from 'node:fs/promises';
import path from 'node:path';
import { LOG_DIR, ROTATE_CONFIG } from '../config.js';

/**
 * 确保目录存在
 */
export async function ensureDir(dirPath: string): Promise<void> {
  await fs.mkdir(dirPath, { recursive: true });
}

/**
 * 确保日志根目录存在
 */
export async function ensureLogDir(): Promise<void> {
  await ensureDir(LOG_DIR);
  await ensureDir(path.join(LOG_DIR, 'system'));
  await ensureDir(path.join(LOG_DIR, 'teams'));
}

/**
 * 获取文件大小（字节）
 */
export async function getFileSize(filePath: string): Promise<number> {
  try {
    const stat = await fs.stat(filePath);
    return stat.size;
  } catch {
    return 0;
  }
}

/**
 * 获取文件修改时间
 */
export async function getFileMtime(filePath: string): Promise<Date | null> {
  try {
    const stat = await fs.stat(filePath);
    return stat.mtime;
  } catch {
    return null;
  }
}

/**
 * 检查文件是否过期
 */
export async function isFileExpired(filePath: string, maxDays: number = ROTATE_CONFIG.maxDays): Promise<boolean> {
  const mtime = await getFileMtime(filePath);
  if (!mtime) return true;

  const now = new Date();
  const diffDays = (now.getTime() - mtime.getTime()) / (1000 * 60 * 60 * 24);
  return diffDays > maxDays;
}

/**
 * 删除过期文件
 */
export async function deleteExpiredFiles(
  dirPath: string,
  maxDays: number = ROTATE_CONFIG.maxDays
): Promise<string[]> {
  const deleted: string[] = [];

  try {
    const entries = await fs.readdir(dirPath, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);

      if (entry.isFile()) {
        if (await isFileExpired(fullPath, maxDays)) {
          await fs.unlink(fullPath);
          deleted.push(fullPath);
        }
      } else if (entry.isDirectory()) {
        // 递归处理子目录
        const subDeleted = await deleteExpiredFiles(fullPath, maxDays);
        deleted.push(...subDeleted);
      }
    }
  } catch (err) {
    console.error(`[file.utils] Error deleting expired files in ${dirPath}:`, err);
  }

  return deleted;
}

/**
 * 获取目录下的所有日志文件
 */
export async function getLogFiles(dirPath: string, pattern: RegExp = /\.jsonl$/): Promise<string[]> {
  const files: string[] = [];

  try {
    const entries = await fs.readdir(dirPath, { withFileTypes: true });

    for (const entry of entries) {
      if (entry.isFile() && pattern.test(entry.name)) {
        files.push(path.join(dirPath, entry.name));
      }
    }
  } catch {
    // 目录不存在
  }

  return files;
}

/**
 * 获取目录大小
 */
export async function getDirSize(dirPath: string): Promise<number> {
  let size = 0;

  try {
    const entries = await fs.readdir(dirPath, { withFileTypes: true });

    for (const entry of entries) {
      const fullPath = path.join(dirPath, entry.name);

      if (entry.isFile()) {
        size += await getFileSize(fullPath);
      } else if (entry.isDirectory()) {
        size += await getDirSize(fullPath);
      }
    }
  } catch {
    // 目录不存在
  }

  return size;
}
