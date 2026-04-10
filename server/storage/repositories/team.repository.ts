/**
 * Team Repository
 */

import { v4 as uuidv4 } from 'uuid';
import { getDb } from '../db.js';
import type { Team, CreateTeamInput, UpdateTeamInput, TeamStatus } from '../types.js';
import type { Repository } from './base.repository.js';

export class TeamRepository implements Repository<Team> {
  /**
   * 根据 ID 查询
   */
  async findById(id: string): Promise<Team | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams WHERE id = ?`, [id]);

    if (result.length === 0 || result[0].values.length === 0) return null;
    return this.mapRow(result[0].columns, result[0].values[0]);
  }

  /**
   * 查询所有 Team
   */
  async findAll(): Promise<Team[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams`);

    if (result.length === 0) return [];
    return result[0].values.map((row) => this.mapRow(result[0].columns, row));
  }

  /**
   * 根据状态查询
   */
  async findByStatus(status: TeamStatus): Promise<Team[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams WHERE status = ?`, [status]);

    if (result.length === 0) return [];
    return result[0].values.map((row) => this.mapRow(result[0].columns, row));
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

    db.run(
      `INSERT INTO teams (id, name, description, status, config, created_at, updated_at) 
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
      [
        team.id,
        team.name,
        team.description,
        team.status,
        JSON.stringify(team.config),
        team.createdAt,
        team.updatedAt,
      ]
    );

    return team;
  }

  /**
   * 更新 Team
   */
  async update(id: string, input: UpdateTeamInput): Promise<Team | null> {
    const existing = await this.findById(id);
    if (!existing) return null;

    const updates: string[] = [];
    const values: (string | null)[] = [];

    if (input.name !== undefined) {
      updates.push('name = ?');
      values.push(input.name);
    }
    if (input.description !== undefined) {
      updates.push('description = ?');
      values.push(input.description);
    }
    if (input.status !== undefined) {
      updates.push('status = ?');
      values.push(input.status);
    }
    if (input.config !== undefined) {
      updates.push('config = ?');
      values.push(JSON.stringify(input.config));
    }

    updates.push('updated_at = ?');
    values.push(new Date().toISOString());

    values.push(id);

    const db = getDb();
    db.run(`UPDATE teams SET ${updates.join(', ')} WHERE id = ?`, values);

    return this.findById(id);
  }

  /**
   * 删除 Team
   */
  async delete(id: string): Promise<boolean> {
    const db = getDb();
    db.run(`DELETE FROM teams WHERE id = ?`, [id]);
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
  private mapRow(columns: string[], values: (string | number | null | Uint8Array)[]): Team {
    const row: Record<string, string | null> = {};
    columns.forEach((col, i) => {
      row[col] = values[i] as string | null;
    });

    return {
      id: row['id']!,
      name: row['name']!,
      description: row['description'],
      status: row['status'] as TeamStatus,
      config: JSON.parse(row['config'] || '{}'),
      createdAt: row['created_at']!,
      updatedAt: row['updated_at']!,
    };
  }
}

// 单例导出
export const teamRepository = new TeamRepository();
