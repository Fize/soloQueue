/**
 * Team Repository
 */

import { eq, and } from 'drizzle-orm';
import { v4 as uuidv4 } from 'uuid';
import { getDb } from '../db.js';
import { teams } from '../schema.js';
import type { Team, CreateTeamInput, UpdateTeamInput, TeamStatus } from '../types.js';
import type { Repository } from './base.repository.js';

export class TeamRepository implements Repository<Team> {
  /**
   * 根据 ID 查询
   */
  async findById(id: string): Promise<Team | null> {
    const db = getDb();
    const result = await db.select().from(teams).where(eq(teams.id, id)).limit(1);

    if (result.length === 0) return null;
    return this.mapRow(result[0]);
  }

  /**
   * 查询所有 Team
   */
  async findAll(): Promise<Team[]> {
    const db = getDb();
    const result = await db.select().from(teams);
    return result.map((row) => this.mapRow(row));
  }

  /**
   * 根据状态查询
   */
  async findByStatus(status: TeamStatus): Promise<Team[]> {
    const db = getDb();
    const result = await db.select().from(teams).where(eq(teams.status, status));
    return result.map((row) => this.mapRow(row));
  }

  /**
   * 创建 Team
   */
  async create(input: CreateTeamInput): Promise<Team> {
    const db = getDb();
    const now = new Date().toISOString();

    const team: Team = {
      id: uuidv4(),
      name: input.name,
      description: input.description || null,
      status: 'active',
      config: input.config || {},
      createdAt: now,
      updatedAt: now,
    };

    await db.insert(teams).values({
      id: team.id,
      name: team.name,
      description: team.description,
      status: team.status,
      config: JSON.stringify(team.config),
      createdAt: team.createdAt,
      updatedAt: team.updatedAt,
    });

    return team;
  }

  /**
   * 更新 Team
   */
  async update(id: string, input: UpdateTeamInput): Promise<Team | null> {
    const existing = await this.findById(id);
    if (!existing) return null;

    const updates: Partial<{
      name: string;
      description: string | null;
      status: string;
      config: string;
      updatedAt: string;
    }> = {
      updatedAt: new Date().toISOString(),
    };

    if (input.name !== undefined) updates.name = input.name;
    if (input.description !== undefined) updates.description = input.description || null;
    if (input.status !== undefined) updates.status = input.status;
    if (input.config !== undefined) updates.config = JSON.stringify(input.config);

    const db = getDb();
    await db.update(teams).set(updates).where(eq(teams.id, id));

    return this.findById(id);
  }

  /**
   * 删除 Team
   */
  async delete(id: string): Promise<boolean> {
    const db = getDb();
    await db.delete(teams).where(eq(teams.id, id));
    return true;
  }

  /**
   * 归档 Team
   */
  async archive(id: string): Promise<Team | null> {
    return this.update(id, { status: 'archived' });
  }

  /**
   * 激活 Team
   */
  async activate(id: string): Promise<Team | null> {
    return this.update(id, { status: 'active' });
  }

  /**
   * 映射数据库行到实体
   */
  private mapRow(row: typeof teams.$inferSelect): Team {
    return {
      id: row.id,
      name: row.name,
      description: row.description,
      status: row.status as TeamStatus,
      config: JSON.parse(row.config),
      createdAt: row.createdAt,
      updatedAt: row.updatedAt,
    };
  }
}

// 单例导出
export const teamRepository = new TeamRepository();
