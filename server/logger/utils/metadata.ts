/**
 * 元数据管理
 */

import fs from 'node:fs/promises';
import path from 'node:path';
import { LOG_DIR } from '../config.js';
import { getTeamMetadataPath, getRootMetadataPath } from './path.js';

export interface TeamMetadata {
  teamId: string;
  createdAt: string;
  updatedAt: string;
  sessionCount: number;
}

export interface RootMetadata {
  version: string;
  createdAt: string;
  updatedAt: string;
  teamCount: number;
}

/**
 * 加载根目录元数据
 */
export async function loadRootMetadata(): Promise<RootMetadata> {
  const metaPath = getRootMetadataPath();

  try {
    const content = await fs.readFile(metaPath, 'utf-8');
    return JSON.parse(content);
  } catch {
    // 返回默认元数据
    return {
      version: '1.0.0',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      teamCount: 0,
    };
  }
}

/**
 * 保存根目录元数据
 */
export async function saveRootMetadata(metadata: RootMetadata): Promise<void> {
  const metaPath = getRootMetadataPath();
  await fs.mkdir(path.dirname(metaPath), { recursive: true });
  await fs.writeFile(metaPath, JSON.stringify(metadata, null, 2));
}

/**
 * 加载 Team 元数据
 */
export async function loadTeamMetadata(teamId: string): Promise<TeamMetadata | null> {
  const metaPath = getTeamMetadataPath(teamId);

  try {
    const content = await fs.readFile(metaPath, 'utf-8');
    return JSON.parse(content);
  } catch {
    return null;
  }
}

/**
 * 保存 Team 元数据
 */
export async function saveTeamMetadata(metadata: TeamMetadata): Promise<void> {
  const metaPath = getTeamMetadataPath(metadata.teamId);
  await fs.mkdir(path.dirname(metaPath), { recursive: true });
  await fs.writeFile(metaPath, JSON.stringify(metadata, null, 2));
}

/**
 * 创建新的 Team 元数据
 */
export async function createTeamMetadata(teamId: string): Promise<TeamMetadata> {
  const metadata: TeamMetadata = {
    teamId,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    sessionCount: 0,
  };

  await saveTeamMetadata(metadata);

  // 更新根目录元数据
  const rootMeta = await loadRootMetadata();
  rootMeta.updatedAt = new Date().toISOString();
  rootMeta.teamCount += 1;
  await saveRootMetadata(rootMeta);

  return metadata;
}

/**
 * 更新 Team 元数据
 */
export async function updateTeamMetadata(
  teamId: string,
  updates: Partial<Omit<TeamMetadata, 'teamId'>>
): Promise<TeamMetadata | null> {
  const metadata = await loadTeamMetadata(teamId);
  if (!metadata) return null;

  Object.assign(metadata, updates, { updatedAt: new Date().toISOString() });
  await saveTeamMetadata(metadata);

  return metadata;
}

/**
 * 删除 Team 元数据
 */
export async function deleteTeamMetadata(teamId: string): Promise<void> {
  const metaPath = getTeamMetadataPath(teamId);

  try {
    await fs.unlink(metaPath);

    // 更新根目录元数据
    const rootMeta = await loadRootMetadata();
    rootMeta.updatedAt = new Date().toISOString();
    rootMeta.teamCount = Math.max(0, rootMeta.teamCount - 1);
    await saveRootMetadata(rootMeta);
  } catch {
    // 文件不存在
  }
}
