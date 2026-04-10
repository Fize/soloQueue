/**
 * 存储层类型测试
 */

import { describe, it, expect } from 'vitest';
import { TeamStatus, type Team, type CreateTeamInput, type UpdateTeamInput } from './types.js';

describe('Storage Types', () => {
  describe('TeamStatus', () => {
    it('should have correct enum values', () => {
      expect(TeamStatus.ACTIVE).toBe('active');
      expect(TeamStatus.INACTIVE).toBe('inactive');
      expect(TeamStatus.ARCHIVED).toBe('archived');
    });
  });

  describe('Team interface', () => {
    it('should accept valid Team object', () => {
      const team: Team = {
        id: 'team-123',
        name: 'Test Team',
        description: 'A test team',
        status: TeamStatus.ACTIVE,
        config: { key: 'value' },
        createdAt: '2026-04-11T00:00:00.000Z',
        updatedAt: '2026-04-11T00:00:00.000Z',
      };

      expect(team.id).toBe('team-123');
      expect(team.name).toBe('Test Team');
    });

    it('should accept team with null description', () => {
      const team: Team = {
        id: 'team-123',
        name: 'Test Team',
        description: null,
        status: TeamStatus.ACTIVE,
        config: {},
        createdAt: '2026-04-11T00:00:00.000Z',
        updatedAt: '2026-04-11T00:00:00.000Z',
      };

      expect(team.description).toBeNull();
    });
  });

  describe('CreateTeamInput', () => {
    it('should accept minimal input', () => {
      const input: CreateTeamInput = {
        name: 'Team Name',
      };

      expect(input.name).toBe('Team Name');
    });

    it('should accept full input', () => {
      const input: CreateTeamInput = {
        name: 'Team Name',
        description: 'Description',
        config: { theme: 'dark' },
      };

      expect(input.name).toBe('Team Name');
      expect(input.description).toBe('Description');
      expect(input.config).toEqual({ theme: 'dark' });
    });
  });

  describe('UpdateTeamInput', () => {
    it('should accept partial updates', () => {
      const input: UpdateTeamInput = {
        name: 'New Name',
      };

      expect(input.name).toBe('New Name');
      expect(input.description).toBeUndefined();
    });

    it('should accept multiple updates', () => {
      const input: UpdateTeamInput = {
        name: 'New Name',
        status: TeamStatus.ARCHIVED,
        config: { new: 'config' },
      };

      expect(input.name).toBe('New Name');
      expect(input.status).toBe(TeamStatus.ARCHIVED);
      expect(input.config).toEqual({ new: 'config' });
    });
  });
});
