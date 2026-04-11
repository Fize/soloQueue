/**
 * ============================================
 * Team Repository (团队数据访问)
 * ============================================
 *
 * 【职责】
 * - Team 实体的数据库 CRUD 操作
 * - 提供按 ID、名称、默认状态查询
 * - 管理默认团队的初始化
 *
 * 【数据约束】
 *
 *   1. name 唯一约束
 *   2. 默认团队 (is_default=1) 只能有一个
 *   3. 默认团队不可删除
 *   4. workspaces 存储为 JSON 数组
 *
 * 【特有方法】
 *
 *   findByName(name)   → 按名称查找
 *   findDefault()      → 查找默认团队
 *   ensureDefault()     → 确保默认团队存在
 *
 * 【日志分类】
 *
 *   category: 'db.team'
 *
 * ============================================
 */

import { v4 as uuidv4 } from 'uuid';
import { getDb, saveDb } from '../db.js';
import { Logger } from '../../logger/index.js';
import type { Team, CreateTeamInput, UpdateTeamInput } from '../types.js';
import type { Repository } from './base.repository.js';
import { DEFAULT_TEAM } from '../seeds.js';

export class TeamRepository implements Repository<Team> {
  private logger: Logger;

  constructor() {
    this.logger = Logger.system();
  }

  /**
   * 根据 ID 查询
   */
  async findById(id: string): Promise<Team | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams WHERE id = ?`, [id]);

    if (result.length === 0 || result[0].values.length === 0) {
      this.logger.debug({
        category: 'db.team',
        message: 'Team not found',
        context: { id },
      });
      return null;
    }
    
    const team = this.mapRow(result[0].columns, result[0].values[0]);
    this.logger.debug({
      category: 'db.team',
      message: 'Team found',
      context: { id, name: team.name },
    });
    
    return team;
  }

  /**
   * 根据名称查询
   */
  async findByName(name: string): Promise<Team | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams WHERE name = ?`, [name]);

    if (result.length === 0 || result[0].values.length === 0) {
      this.logger.debug({
        category: 'db.team',
        message: 'Team not found by name',
        context: { name },
      });
      return null;
    }
    
    const team = this.mapRow(result[0].columns, result[0].values[0]);
    this.logger.debug({
      category: 'db.team',
      message: 'Team found by name',
      context: { name, id: team.id },
    });
    
    return team;
  }

  /**
   * 查询所有 Team
   */
  async findAll(): Promise<Team[]> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams`);

    if (result.length === 0) {
      this.logger.debug({
        category: 'db.team',
        message: 'No teams found',
      });
      return [];
    }
    
    const teams = result[0].values.map((row) => this.mapRow(result[0].columns, row));
    this.logger.debug({
      category: 'db.team',
      message: 'Teams found',
      context: { count: teams.length },
    });
    
    return teams;
  }

  /**
   * 查询默认团队
   */
  async findDefault(): Promise<Team | null> {
    const db = getDb();
    const result = db.exec(`SELECT * FROM teams WHERE is_default = 1`);

    if (result.length === 0 || result[0].values.length === 0) {
      this.logger.debug({
        category: 'db.team',
        message: 'Default team not found',
      });
      return null;
    }
    
    const team = this.mapRow(result[0].columns, result[0].values[0]);
    this.logger.debug({
      category: 'db.team',
      message: 'Default team found',
      context: { id: team.id },
    });
    
    return team;
  }

  /**
   * 创建 Team
   */
  async create(input: CreateTeamInput): Promise<Team> {
    const db = getDb();
    const now = new Date().toISOString();
    const id = uuidv4();

    const team: Team = {
      id,
      name: input.name,
      description: input.description ?? '',
      workspaces: input.workspaces || ['~/.soloqueue'],
      isDefault: false,
      createdAt: now,
      updatedAt: now,
    };

    db.run(
      `INSERT INTO teams (id, name, description, workspaces, is_default, created_at, updated_at) 
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
      [
        team.id,
        team.name,
        team.description,
        JSON.stringify(team.workspaces),
        0,
        team.createdAt,
        team.updatedAt,
      ]
    );

    saveDb();
    
    this.logger.info({
      category: 'db.team',
      message: 'Team created',
      context: { id: team.id, name: team.name },
    });
    
    return team;
  }

  /**
   * 更新 Team
   */
  async update(id: string, input: UpdateTeamInput): Promise<Team | null> {
    const existing = await this.findById(id);
    if (!existing) {
      this.logger.warn({
        category: 'db.team',
        message: 'Team not found for update',
        context: { id },
      });
      return null;
    }

    const updates: string[] = [];
    const values: (string | null)[] = [];

    if (input.name !== undefined) {
      updates.push('name = ?');
      values.push(input.name);
    }
    if (input.description !== undefined) {
      updates.push('description = ?');
      values.push(input.description ?? '');
    }
    if (input.workspaces !== undefined) {
      updates.push('workspaces = ?');
      values.push(JSON.stringify(input.workspaces));
    }

    if (updates.length === 0) {
      this.logger.debug({
        category: 'db.team',
        message: 'No changes to team',
        context: { id },
      });
      return existing;
    }

    updates.push('updated_at = ?');
    values.push(new Date().toISOString());
    values.push(id);

    const db = getDb();
    db.run(`UPDATE teams SET ${updates.join(', ')} WHERE id = ?`, values);
    saveDb();

    const updated = await this.findById(id);
    
    this.logger.info({
      category: 'db.team',
      message: 'Team updated',
      context: { id, name: updated?.name },
    });
    
    return updated;
  }

  /**
   * 删除 Team（不能删除默认团队）
   */
  async delete(id: string): Promise<boolean> {
    const team = await this.findById(id);
    if (!team) {
      this.logger.warn({
        category: 'db.team',
        message: 'Team not found for deletion',
        context: { id },
      });
      return false;
    }
    
    if (team.isDefault) {
      this.logger.warn({
        category: 'db.team',
        message: 'Cannot delete default team',
        context: { id },
      });
      return false;
    }

    const db = getDb();
    db.run(`DELETE FROM teams WHERE id = ?`, [id]);
    saveDb();
    
    this.logger.info({
      category: 'db.team',
      message: 'Team deleted',
      context: { id, name: team.name },
    });
    
    return true;
  }

  /**
   * 初始化默认团队（如果不存在）
   */
  async ensureDefault(): Promise<Team> {
    const existing = await this.findDefault();
    if (existing) {
      this.logger.debug({
        category: 'db.team',
        message: 'Default team already exists',
        context: { id: existing.id },
      });
      return existing;
    }

    const db = getDb();
    const now = new Date().toISOString();
    const id = uuidv4();

    const team: Team = {
      id,
      name: DEFAULT_TEAM.name,
      description: DEFAULT_TEAM.description,
      workspaces: DEFAULT_TEAM.workspaces,
      isDefault: true,
      createdAt: now,
      updatedAt: now,
    };

    db.run(
      `INSERT INTO teams (id, name, description, workspaces, is_default, created_at, updated_at) 
       VALUES (?, ?, ?, ?, ?, ?, ?)`,
      [
        team.id,
        team.name,
        team.description,
        JSON.stringify(team.workspaces),
        1,
        team.createdAt,
        team.updatedAt,
      ]
    );

    saveDb();
    
    this.logger.info({
      category: 'db.team',
      message: 'Default team created',
      context: { id: team.id, name: team.name },
    });
    
    return team;
  }

  /**
   * 映射数据库行到实体
   */
  private mapRow(columns: string[], values: (string | number | null | Uint8Array)[]): Team {
    const row: Record<string, string | number | null> = {};
    columns.forEach((col, i) => {
      row[col] = values[i];
    });

    return {
      id: row['id'] as string,
      name: row['name'] as string,
      description: row['description'] as string | null,
      workspaces: JSON.parse((row['workspaces'] as string) || '["~/.soloqueue"]'),
      isDefault: Boolean(row['is_default']),
      createdAt: row['created_at'] as string,
      updatedAt: row['updated_at'] as string,
    };
  }
}

export const teamRepository = new TeamRepository();
