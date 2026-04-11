/**
 * ============================================
 * Agent Factories 单元测试
 * ============================================
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createAgentActor } from '../machines/agent-machine.js';
import { chatAgentFactory } from './chat-agent.factory.js';
import { codeAgentFactory } from './code-agent.factory.js';
import { customAgentFactory } from './custom-agent.factory.js';
import { DEFAULT_FACTORIES, registerDefaultFactories } from './index.js';
import type { AgentDefinition, ActorSystem } from '../types.js';

// Mock llmConfigService
vi.mock('../../llm/index.js', () => ({
  llmConfigService: {
    getAgentDefaults: vi.fn(() => ({
      defaultModelId: 'deepseek-chat',
      modelSelection: 'default',
      roleDefaults: {
        chat: { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.7, maxTokens: 4096, contextWindowRatio: 0.75 },
        code: { providerId: 'deepseek', modelId: 'deepseek-coder', temperature: 0.2, maxTokens: 8192, contextWindowRatio: 0.5 },
        planner: { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.5, maxTokens: 2048, contextWindowRatio: 0.6 },
        evaluator: { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.3, maxTokens: 2048, contextWindowRatio: 0.6 },
      },
      maxRetries: 3,
      timeout: 120000,
      contextWindowRatio: 0.75,
    })),
    getRoleDefaultConfig: vi.fn((role: string) => {
      if (role === 'chat') {
        return { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.7, maxTokens: 4096, contextWindowRatio: 0.75 };
      }
      if (role === 'code') {
        return { providerId: 'deepseek', modelId: 'deepseek-coder', temperature: 0.2, maxTokens: 8192, contextWindowRatio: 0.5 };
      }
      if (role === 'planner') {
        return { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.5, maxTokens: 2048, contextWindowRatio: 0.6 };
      }
      if (role === 'evaluator') {
        return { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.3, maxTokens: 2048, contextWindowRatio: 0.6 };
      }
      throw new Error(`Unknown role: ${role}`);
    }),
    getSupervisorDefaults: vi.fn(() => ({
      defaultStrategy: 'one_for_one' as const,
      maxRetries: 3,
      retryInterval: 1000,
      exponentialBackoff: true,
      maxBackoff: 30000,
    })),
  },
}));

// Mock createAgentActor
vi.mock('../machines/agent-machine.js', () => ({
  createAgentActor: vi.fn(() => ({
    start: vi.fn(),
    stop: vi.fn(),
    send: vi.fn(),
    subscribe: vi.fn(() => () => {}),
    getSnapshot: vi.fn(() => ({ value: 'idle', context: {} })),
  })),
}));

describe('Agent Factories', () => {
  const mockSystem = {} as ActorSystem;

  const createTestDefinition = (overrides: Partial<AgentDefinition> = {}): AgentDefinition => ({
    id: 'test-agent',
    name: 'Test Agent',
    teamId: 'team-1',
    role: 'user',
    kind: 'chat',
    modelId: 'deepseek-chat',
    providerId: 'deepseek',
    systemPrompt: 'You are a helpful assistant.',
    capabilities: [],
    supervision: {
      strategy: 'one_for_one',
      maxRetries: 3,
      retryInterval: 1000,
      exponentialBackoff: true,
      maxBackoff: 30000,
    },
    enabled: true,
    createdAt: Date.now(),
    updatedAt: Date.now(),
    ...overrides,
  });

  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('chatAgentFactory', () => {
    it('kind 应该是 chat', () => {
      expect(chatAgentFactory.kind).toBe('chat');
    });

    it('应该创建 chat 类型实例', () => {
      const definition = createTestDefinition();
      const instance = chatAgentFactory.create(definition, mockSystem);

      expect(instance.kind).toBe('chat');
      expect(instance.id).toBe('test-agent');
      expect(instance.role).toBe('user');
    });

    it('应该启动 actor', () => {
      const definition = createTestDefinition();
      chatAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalled();
    });

    it('应该从配置系统获取默认参数', () => {
      const definition = createTestDefinition();
      chatAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          agentId: 'test-agent',
          teamId: 'team-1',
          temperature: 0.7,      // 从配置系统
          maxTokens: 4096,       // 从配置系统 (不是 2000)
          model: 'deepseek-chat', // 使用 definition 的 model
        })
      );
    });

    it('应该支持自定义配置', () => {
      const definition = createTestDefinition({
        systemPrompt: 'Custom prompt',
        modelId: 'custom-model',
      });
      chatAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          systemPrompt: 'Custom prompt',
          model: 'custom-model',
        })
      );
    });

    it('当 definition 没有 modelId 时使用配置系统的默认值', () => {
      // 空字符串被视为 falsy，会使用配置系统默认值
      const definition = createTestDefinition({ modelId: '' });
      chatAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'deepseek-chat', // 从配置系统
        })
      );
    });

    it('validate 应该检查 teamId', () => {
      expect(chatAgentFactory.validate!({ teamId: 'team-1' })).toBe(true);
      expect(chatAgentFactory.validate!({})).toBe(false);
    });
  });

  describe('codeAgentFactory', () => {
    it('kind 应该是 code', () => {
      expect(codeAgentFactory.kind).toBe('code');
    });

    it('应该创建 code 类型实例', () => {
      const definition = createTestDefinition({ kind: 'code' });
      const instance = codeAgentFactory.create(definition, mockSystem);

      expect(instance.kind).toBe('code');
    });

    it('应该从配置系统获取代码优化的默认参数', () => {
      const definition = createTestDefinition({ kind: 'code' });
      codeAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          temperature: 0.2,     // 从配置系统
          maxTokens: 8192,      // 从配置系统 (不是 4000)
        })
      );
    });

    it('当 definition 没有 modelId 时使用配置系统的默认值', () => {
      // 空字符串被视为 falsy，会使用配置系统默认值
      const definition = createTestDefinition({ kind: 'code', modelId: '' });
      codeAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'deepseek-coder', // 从配置系统
        })
      );
    });

    it('validate 应该只检查 teamId', () => {
      expect(codeAgentFactory.validate!({ teamId: 'team-1' })).toBe(true);
      expect(codeAgentFactory.validate!({})).toBe(false);
    });
  });

  describe('customAgentFactory', () => {
    it('kind 应该是 custom', () => {
      expect(customAgentFactory.kind).toBe('custom');
    });

    it('应该创建 custom 类型实例', () => {
      const definition = createTestDefinition({ kind: 'custom' });
      const instance = customAgentFactory.create(definition, mockSystem);

      expect(instance.kind).toBe('custom');
    });

    it('应该使用 definition 的 modelId 和 systemPrompt', () => {
      const definition = createTestDefinition({
        kind: 'custom',
        modelId: 'custom-model',
        systemPrompt: 'Custom prompt',
      });
      customAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          model: 'custom-model',
          systemPrompt: 'Custom prompt',
        })
      );
    });

    it('应该使用配置服务中的 chat 默认值', () => {
      const definition = createTestDefinition({
        kind: 'custom',
      });
      customAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          temperature: 0.7,
          maxTokens: 4096,  // 来自 llmConfigService.getAgentDefaults().roleDefaults.chat.maxTokens
        })
      );
    });

    it('应该使用配置服务中的 chat 默认值', () => {
      const definition = createTestDefinition({ kind: 'custom' });
      customAgentFactory.create(definition, mockSystem);

      expect(createAgentActor).toHaveBeenCalledWith(
        expect.objectContaining({
          temperature: 0.7,
          maxTokens: 4096,  // 来自 llmConfigService.getAgentDefaults().roleDefaults.chat.maxTokens
        })
      );
    });

    it('validate 应该检查 id 和 teamId', () => {
      expect(customAgentFactory.validate!({ id: 'test', teamId: 'team-1' })).toBe(true);
      expect(customAgentFactory.validate!({ id: 'test' })).toBe(false);
      expect(customAgentFactory.validate!({ teamId: 'team-1' })).toBe(false);
    });
  });

  describe('DEFAULT_FACTORIES', () => {
    it('应该包含所有默认工厂', () => {
      expect(DEFAULT_FACTORIES).toHaveLength(3);
    });

    it('应该包含 chatAgentFactory', () => {
      expect(DEFAULT_FACTORIES.find(f => f.kind === 'chat')).toBeDefined();
    });

    it('应该包含 codeAgentFactory', () => {
      expect(DEFAULT_FACTORIES.find(f => f.kind === 'code')).toBeDefined();
    });

    it('应该包含 customAgentFactory', () => {
      expect(DEFAULT_FACTORIES.find(f => f.kind === 'custom')).toBeDefined();
    });
  });

  describe('registerDefaultFactories', () => {
    it('应该注册所有默认工厂到系统', () => {
      const mockSystem = {
        registerFactory: vi.fn(),
      } as unknown as ActorSystem;

      registerDefaultFactories(mockSystem);

      expect(mockSystem.registerFactory).toHaveBeenCalledTimes(3);
    });
  });
});
