/**
 * TeamRepository 单元测试
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import initSqlJs from 'sql.js';
import { TeamRepository } from './repositories/team.repository.js';
import { setMemoryDb, resetDb } from './db.js';

describe('TeamRepository', () => {
  let repository: TeamRepository;

  beforeEach(async () => {
    // 创建内存数据库
    const SQL = await initSqlJs();
    const db = new SQL.Database();

    // 创建表
    db.run(`
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

    // 设置内存数据库
    setMemoryDb(db);

    repository = new TeamRepository();
  });

  afterEach(() => {
    resetDb();
  });

  describe('create()', () => {
    it('should create a team with generated id', async () => {
      const team = await repository.create({
        name: 'Test Team',
        description: 'A test team',
      });

      expect(team.id).toBeDefined();
      expect(team.name).toBe('Test Team');
      expect(team.description).toBe('A test team');
      expect(team.status).toBe('active');
      expect(team.config).toEqual({});
      expect(team.createdAt).toBeDefined();
      expect(team.updatedAt).toBeDefined();
    });

    it('should create team without description', async () => {
      const team = await repository.create({
        name: 'Team Without Description',
      });

      expect(team.name).toBe('Team Without Description');
      expect(team.description).toBeNull();
    });

    it('should create team with config', async () => {
      const team = await repository.create({
        name: 'Team With Config',
        config: { theme: 'dark', language: 'en' },
      });

      expect(team.config).toEqual({ theme: 'dark', language: 'en' });
    });

    it('should generate unique ids', async () => {
      const team1 = await repository.create({ name: 'Team 1' });
      const team2 = await repository.create({ name: 'Team 2' });

      expect(team1.id).not.toBe(team2.id);
    });
  });

  describe('findById()', () => {
    it('should find created team by id', async () => {
      const created = await repository.create({ name: 'Find Me' });
      const found = await repository.findById(created.id);

      expect(found).not.toBeNull();
      expect(found?.name).toBe('Find Me');
    });

    it('should return null for non-existent id', async () => {
      const found = await repository.findById('non-existent-id');

      expect(found).toBeNull();
    });
  });

  describe('findAll()', () => {
    it('should return all teams', async () => {
      await repository.create({ name: 'Team 1' });
      await repository.create({ name: 'Team 2' });
      await repository.create({ name: 'Team 3' });

      const teams = await repository.findAll();

      expect(teams).toHaveLength(3);
    });

    it('should return empty array when no teams', async () => {
      const teams = await repository.findAll();

      expect(teams).toEqual([]);
    });
  });

  describe('findByStatus()', () => {
    it('should filter teams by status', async () => {
      const team1 = await repository.create({ name: 'Team One' });
      const team2 = await repository.create({ name: 'Team Two' });

      await repository.archive(team1.id);

      const archivedTeams = await repository.findByStatus('archived');
      const activeTeams = await repository.findByStatus('active');

      expect(archivedTeams).toHaveLength(1);
      expect(archivedTeams[0].id).toBe(team1.id);
      expect(archivedTeams[0].name).toBe('Team One');
      
      expect(activeTeams).toHaveLength(1);
      expect(activeTeams[0].id).toBe(team2.id);
    });
  });

  describe('update()', () => {
    it('should update team name', async () => {
      const created = await repository.create({ name: 'Original Name' });
      const updated = await repository.update(created.id, { name: 'New Name' });

      expect(updated).not.toBeNull();
      expect(updated?.name).toBe('New Name');
      expect(updated?.id).toBe(created.id);
    });

    it('should update team description', async () => {
      const created = await repository.create({ name: 'Team' });
      const updated = await repository.update(created.id, { description: 'New description' });

      expect(updated?.description).toBe('New description');
    });

    it('should update team status', async () => {
      const created = await repository.create({ name: 'Team' });
      const updated = await repository.update(created.id, { status: 'inactive' });

      expect(updated?.status).toBe('inactive');
    });

    it('should update team config', async () => {
      const created = await repository.create({ name: 'Team' });
      const updated = await repository.update(created.id, { config: { key: 'value' } });

      expect(updated?.config).toEqual({ key: 'value' });
    });

    it('should update multiple fields at once', async () => {
      const created = await repository.create({ name: 'Team' });
      const updated = await repository.update(created.id, {
        name: 'New Name',
        description: 'New description',
      });

      expect(updated?.name).toBe('New Name');
      expect(updated?.description).toBe('New description');
    });

    it('should return null for non-existent id', async () => {
      const updated = await repository.update('non-existent-id', { name: 'New Name' });

      expect(updated).toBeNull();
    });

    it('should update updatedAt timestamp', async () => {
      const created = await repository.create({ name: 'Team' });
      const originalUpdatedAt = created.updatedAt;

      // Wait a bit to ensure timestamp difference
      await new Promise((resolve) => setTimeout(resolve, 10));

      const updated = await repository.update(created.id, { name: 'New Name' });

      expect(updated?.updatedAt).not.toBe(originalUpdatedAt);
    });
  });

  describe('delete()', () => {
    it('should delete team', async () => {
      const created = await repository.create({ name: 'To Delete' });
      const result = await repository.delete(created.id);

      expect(result).toBe(true);
      expect(await repository.findById(created.id)).toBeNull();
    });

    it('should return true even for non-existent id', async () => {
      const result = await repository.delete('non-existent-id');

      expect(result).toBe(true);
    });
  });

  describe('archive()', () => {
    it('should archive team', async () => {
      const created = await repository.create({ name: 'To Archive' });
      const archived = await repository.archive(created.id);

      expect(archived?.status).toBe('archived');
    });
  });

  describe('activate()', () => {
    it('should activate archived team', async () => {
      const created = await repository.create({ name: 'To Activate' });
      await repository.archive(created.id);

      const activated = await repository.activate(created.id);

      expect(activated?.status).toBe('active');
    });
  });
});
