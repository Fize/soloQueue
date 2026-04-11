/**
 * ============================================
 * Actor Extensions 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { AgentFactory, RoutingStrategy, AgentCreateParams } from './extensions.js';
import type { ActorSystem } from './actor-system.js';
import type { ActorInstance, AgentDefinition } from './types.js';

describe('Extensions', () => {
  describe('AgentFactory 接口', () => {
    it('应该能创建符合接口的工厂', () => {
      const factory: AgentFactory = {
        kind: 'chat',
        create: vi.fn(),
        validate: vi.fn(() => true),
      };

      expect(factory.kind).toBe('chat');
      expect(typeof factory.create).toBe('function');
      expect(typeof factory.validate).toBe('function');
    });

    it('应该支持不带 validate 的工厂', () => {
      const factory: AgentFactory = {
        kind: 'code',
        create: vi.fn(),
        // validate 是可选的
      };

      expect(factory.kind).toBe('code');
      expect(factory.validate).toBeUndefined();
    });
  });

  describe('RoutingStrategy 接口', () => {
    it('应该能创建符合接口的策略', () => {
      const strategy: RoutingStrategy = {
        name: 'custom',
        select: vi.fn(() => null),
      };

      expect(strategy.name).toBe('custom');
      expect(typeof strategy.select).toBe('function');
    });

    it('策略应该接收 agents 和 message 参数', () => {
      const agents: ActorInstance[] = [];
      const message = { type: 'task', taskId: 't1', content: 'test', from: 'u' };

      const strategy: RoutingStrategy = {
        name: 'test',
        select: (agentsArg, messageArg) => {
          expect(agentsArg).toEqual(agents);
          expect(messageArg).toEqual(message);
          return null;
        },
      };

      strategy.select(agents, message);
    });
  });

  describe('AgentCreateParams 接口', () => {
    it('应该支持所有必需字段', () => {
      const params: AgentCreateParams = {
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'chat',
      };

      expect(params.name).toBe('Test Agent');
      expect(params.teamId).toBe('team-1');
      expect(params.kind).toBe('chat');
    });

    it('应该支持所有可选字段', () => {
      const params: AgentCreateParams = {
        id: 'custom-id',
        name: 'Test Agent',
        teamId: 'team-1',
        kind: 'code',
        modelId: 'deepseek-coder',
        providerId: 'deepseek',
        systemPrompt: 'You are a coding assistant.',
        capabilities: ['code', 'debug'],
        tools: ['bash', 'editor'],
        supervision: {
          strategy: 'one_for_one',
          maxRetries: 5,
        },
      };

      expect(params.id).toBe('custom-id');
      expect(params.modelId).toBe('deepseek-coder');
      expect(params.supervision?.maxRetries).toBe(5);
    });

    it('supervision 应该支持所有策略', () => {
      const strategies: AgentCreateParams['supervision'] = {
        strategy: 'one_for_one',
        maxRetries: 3,
        retryInterval: 1000,
        exponentialBackoff: true,
        maxBackoff: 30000,
      };

      expect(strategies.strategy).toBe('one_for_one');
    });
  });
});
