/**
 * ============================================
 * Actor System Types 单元测试
 * ============================================
 */

import { describe, it, expect } from 'vitest';
import {
  AgentRole,
  AgentKind,
  SYSTEM_AGENTS,
  ActorSystemError,
  ActorErrorCode,
  SupervisionStrategy,
  SupervisionConfig,
} from './types.js';
import { DEFAULT_SUPERVISOR_DEFAULTS } from '../llm/defaults.js';

describe('Actor Types', () => {
  describe('AgentRole', () => {
    it('应该支持 system 角色', () => {
      const role: AgentRole = 'system';
      expect(role).toBe('system');
    });

    it('应该支持 user 角色', () => {
      const role: AgentRole = 'user';
      expect(role).toBe('user');
    });
  });

  describe('AgentKind', () => {
    it('应该支持所有预定义的 Agent 类型', () => {
      const kinds: AgentKind[] = ['chat', 'tool', 'code', 'planner', 'evaluator', 'custom'];
      expect(kinds).toHaveLength(6);
    });
  });

  describe('SupervisionStrategy', () => {
    it('应该支持所有监督策略', () => {
      const strategies: SupervisionStrategy[] = ['one_for_one', 'one_for_all', 'all_for_one', 'stop'];
      expect(strategies).toHaveLength(4);
    });
  });

  describe('SupervisorDefaults (from llm defaults)', () => {
    it('应该有正确的默认值', () => {
      expect(DEFAULT_SUPERVISOR_DEFAULTS.defaultStrategy).toBe('one_for_one');
      expect(DEFAULT_SUPERVISOR_DEFAULTS.maxRetries).toBe(3);
      expect(DEFAULT_SUPERVISOR_DEFAULTS.retryInterval).toBe(1000);
      expect(DEFAULT_SUPERVISOR_DEFAULTS.exponentialBackoff).toBe(true);
      expect(DEFAULT_SUPERVISOR_DEFAULTS.maxBackoff).toBe(30000);
    });
  });

  describe('SYSTEM_AGENTS', () => {
    it('应该有 3 个系统 Agent', () => {
      expect(SYSTEM_AGENTS).toHaveLength(3);
    });

    it('每个系统 Agent 应该有正确的角色', () => {
      for (const agent of SYSTEM_AGENTS) {
        expect(agent.role).toBe('system');
      }
    });

    it('应该有 system-router', () => {
      const router = SYSTEM_AGENTS.find(a => a.id === 'system-router');
      expect(router).toBeDefined();
      expect(router?.name).toBe('System Router');
      expect(router?.capabilities).toContain('routing');
    });

    it('应该有 system-logger', () => {
      const logger = SYSTEM_AGENTS.find(a => a.id === 'system-logger');
      expect(logger).toBeDefined();
      expect(logger?.name).toBe('System Logger');
      expect(logger?.capabilities).toContain('logging');
    });

    it('应该有 system-persister', () => {
      const persister = SYSTEM_AGENTS.find(a => a.id === 'system-persister');
      expect(persister).toBeDefined();
      expect(persister?.name).toBe('System Persister');
      expect(persister?.capabilities).toContain('persistence');
    });

    it('系统 Agent 的监督策略应该是 stop', () => {
      for (const agent of SYSTEM_AGENTS) {
        expect(agent.supervision.strategy).toBe('stop');
        expect(agent.supervision.maxRetries).toBe(0);
      }
    });

    it('系统 Agent 的 teamId 应该是 system', () => {
      for (const agent of SYSTEM_AGENTS) {
        expect(agent.teamId).toBe('system');
      }
    });
  });

  describe('ActorSystemError', () => {
    it('应该正确创建错误', () => {
      const error = new ActorSystemError(
        'Test error',
        'AGENT_NOT_FOUND',
        'agent-1',
        new Error('Original error')
      );

      expect(error.message).toBe('Test error');
      expect(error.code).toBe('AGENT_NOT_FOUND');
      expect(error.agentId).toBe('agent-1');
      expect(error.cause).toBeInstanceOf(Error);
      expect(error.name).toBe('ActorSystemError');
    });

    it('应该支持所有错误码', () => {
      const errorCodes: ActorErrorCode[] = [
        'AGENT_NOT_FOUND',
        'AGENT_ALREADY_EXISTS',
        'AGENT_RUNNING',
        'AGENT_STOPPED',
        'FACTORY_NOT_FOUND',
        'FACTORY_ALREADY_EXISTS',
        'SYSTEM_NOT_RUNNING',
        'INVALID_CONFIG',
        'CANNOT_DELETE_SYSTEM_AGENT',
      ];

      expect(errorCodes).toHaveLength(9);
    });
  });
});
