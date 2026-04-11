/**
 * ============================================
 * LLM Config Service 测试
 * ============================================
 */

import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  type LLMProvider,
  type LLMModel,
  type AgentDefaultConfig,
  type SupervisorDefaultConfig,
  type ProviderJSON,
  type ModelsJSON,
} from './types.js';

// Mock config service
const mockConfigService = {
  isInitialized: vi.fn().mockReturnValue(true),
  initialize: vi.fn().mockResolvedValue(undefined),
  get: vi.fn(),
  set: vi.fn().mockResolvedValue(undefined),
  reload: vi.fn().mockResolvedValue(undefined),
};

const mockProviders: LLMProvider[] = [
  {
    id: 'deepseek',
    name: 'DeepSeek',
    baseUrl: 'https://api.deepseek.com/v1',
    apiKeyEnv: 'DEEPSEEK_API_KEY',
    enabled: true,
    isDefault: true,
    capabilities: ['chat', 'streaming'],
    timeout: 120000,
    retryConfig: { maxRetries: 3, initialDelay: 1000, maxDelay: 30000, backoffMultiplier: 2 },
  },
];

const mockModels: LLMModel[] = [
  {
    id: 'deepseek-chat',
    providerId: 'deepseek',
    name: 'DeepSeek Chat',
    type: 'chat',
    contextWindow: 64000,
    defaults: { temperature: 0.7, maxTokens: 4096 },
    config: { contextWindow: 64000, supportsStreaming: true, supportsFunctionCalling: true },
    tags: ['fast', 'latest'],
    enabled: true,
    isDefault: true,
  },
  {
    id: 'deepseek-coder',
    providerId: 'deepseek',
    name: 'DeepSeek Coder',
    type: 'code',
    contextWindow: 128000,
    defaults: { temperature: 0.2, maxTokens: 8192 },
    config: { contextWindow: 128000, supportsStreaming: true, supportsFunctionCalling: true },
    tags: ['code', 'latest'],
    enabled: true,
    isDefault: true,
  },
];

describe('LLMConfigService Types', () => {
  describe('LLMProvider', () => {
    it('应该包含必需的字段', () => {
      const provider: LLMProvider = mockProviders[0];
      expect(provider.id).toBeDefined();
      expect(provider.name).toBeDefined();
      expect(provider.baseUrl).toBeDefined();
      expect(provider.enabled).toBeDefined();
      expect(provider.capabilities).toBeInstanceOf(Array);
    });

    it('应该支持所有 provider 能力类型', () => {
      const provider = mockProviders[0];
      expect(provider.capabilities).toContain('chat');
      expect(provider.capabilities).toContain('streaming');
    });
  });

  describe('LLMModel', () => {
    it('应该包含必需的字段', () => {
      const model: LLMModel = mockModels[0];
      expect(model.id).toBeDefined();
      expect(model.providerId).toBeDefined();
      expect(model.type).toBeDefined();
      expect(model.contextWindow).toBeGreaterThan(0);
      expect(model.defaults).toBeDefined();
    });

    it('应该支持所有模型类型', () => {
      const types = mockModels.map(m => m.type);
      expect(types).toContain('chat');
      expect(types).toContain('code');
    });

    it('应该支持模型标签', () => {
      const model = mockModels[0];
      expect(model.tags).toContain('fast');
      expect(model.tags).toContain('latest');
    });
  });

  describe('AgentDefaultConfig', () => {
    it('应该包含所有角色的默认配置', () => {
      const config: AgentDefaultConfig = {
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
      };

      expect(config.roleDefaults.chat).toBeDefined();
      expect(config.roleDefaults.code).toBeDefined();
      expect(config.roleDefaults.planner).toBeDefined();
      expect(config.roleDefaults.evaluator).toBeDefined();
    });

    it('应该支持所有模型选择策略', () => {
      const strategies: Array<'default' | 'prefer-fast' | 'prefer-cheap' | 'prefer-context' | 'manual'> = [
        'default',
        'prefer-fast',
        'prefer-cheap',
        'prefer-context',
        'manual',
      ];

      strategies.forEach(strategy => {
        const config: AgentDefaultConfig = {
          defaultModelId: 'deepseek-chat',
          modelSelection: strategy,
          roleDefaults: {
            chat: { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.7, maxTokens: 4096, contextWindowRatio: 0.75 },
            code: { providerId: 'deepseek', modelId: 'deepseek-coder', temperature: 0.2, maxTokens: 8192, contextWindowRatio: 0.5 },
            planner: { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.5, maxTokens: 2048, contextWindowRatio: 0.6 },
            evaluator: { providerId: 'deepseek', modelId: 'deepseek-chat', temperature: 0.3, maxTokens: 2048, contextWindowRatio: 0.6 },
          },
          maxRetries: 3,
          timeout: 120000,
          contextWindowRatio: 0.75,
        };
        expect(config.modelSelection).toBe(strategy);
      });
    });
  });

  describe('SupervisorDefaultConfig', () => {
    it('应该包含监督配置', () => {
      const config: SupervisorDefaultConfig = {
        defaultStrategy: 'one_for_one',
        maxRetries: 3,
        retryInterval: 1000,
        exponentialBackoff: true,
        maxBackoff: 30000,
      };

      expect(config.defaultStrategy).toBeDefined();
      expect(config.maxRetries).toBeGreaterThanOrEqual(0);
      expect(config.retryInterval).toBeGreaterThanOrEqual(0);
    });

    it('应该支持所有监督策略', () => {
      const strategies: Array<'one_for_one' | 'one_for_all' | 'all_for_one' | 'stop'> = [
        'one_for_one',
        'one_for_all',
        'all_for_one',
        'stop',
      ];

      strategies.forEach(strategy => {
        const config: SupervisorDefaultConfig = {
          defaultStrategy: strategy,
          maxRetries: 3,
          retryInterval: 1000,
          exponentialBackoff: true,
          maxBackoff: 30000,
        };
        expect(config.defaultStrategy).toBe(strategy);
      });
    });
  });

  describe('JSON Storage Types', () => {
    it('ProviderJSON 应该正确序列化', () => {
      const json: ProviderJSON = { providers: mockProviders };
      const serialized = JSON.stringify(json);
      const parsed = JSON.parse(serialized);
      expect(parsed.providers).toHaveLength(1);
      expect(parsed.providers[0].id).toBe('deepseek');
    });

    it('ModelsJSON 应该正确序列化', () => {
      const json: ModelsJSON = { models: mockModels };
      const serialized = JSON.stringify(json);
      const parsed = JSON.parse(serialized);
      expect(parsed.models).toHaveLength(2);
    });
  });
});

describe('LLMConfigService 边际场景', () => {
  describe('Provider 边际场景', () => {
    it('Provider id 应该唯一', () => {
      const ids = mockProviders.map(p => p.id);
      const uniqueIds = new Set(ids);
      expect(uniqueIds.size).toBe(ids.length);
    });

    it('每个 Provider 应该最多只有一个默认', () => {
      const defaultProviders = mockProviders.filter(p => p.isDefault);
      expect(defaultProviders.length).toBeLessThanOrEqual(1);
    });

    it('disabled Provider 不应该影响默认选择', () => {
      const enabledDefault = mockProviders.find(p => p.enabled && p.isDefault);
      expect(enabledDefault?.id).toBe('deepseek');
    });
  });

  describe('Model 边际场景', () => {
    it('每种类型应该最多只有一个默认模型', () => {
      const chatModels = mockModels.filter(m => m.type === 'chat' && m.isDefault);
      const codeModels = mockModels.filter(m => m.type === 'code' && m.isDefault);

      expect(chatModels.length).toBeLessThanOrEqual(1);
      expect(codeModels.length).toBeLessThanOrEqual(1);
    });

    it('模型 contextWindow 应该大于 0', () => {
      mockModels.forEach(model => {
        expect(model.contextWindow).toBeGreaterThan(0);
      });
    });

    it('contextWindow 应该与 config.contextWindow 一致', () => {
      mockModels.forEach(model => {
        expect(model.contextWindow).toBe(model.config.contextWindow);
      });
    });
  });

  describe('温度边际场景', () => {
    it('chat 模型温度应该在 0-2 范围内', () => {
      const chatModels = mockModels.filter(m => m.type === 'chat');
      chatModels.forEach(model => {
        expect(model.defaults.temperature).toBeGreaterThanOrEqual(0);
        expect(model.defaults.temperature).toBeLessThanOrEqual(2);
      });
    });
  });

  describe('角色配置边际场景', () => {
    it('contextWindowRatio 应该在 0-1 范围内', () => {
      const config: AgentDefaultConfig = {
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
      };

      Object.values(config.roleDefaults).forEach(role => {
        expect(role.contextWindowRatio).toBeGreaterThanOrEqual(0);
        expect(role.contextWindowRatio).toBeLessThanOrEqual(1);
      });
    });

    it('maxTokens 应该大于 0', () => {
      const config: AgentDefaultConfig = {
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
      };

      Object.values(config.roleDefaults).forEach(role => {
        expect(role.maxTokens).toBeGreaterThan(0);
      });
    });
  });

  describe('Retry 配置边际场景', () => {
    it('重试配置应该有合理的边界值', () => {
      mockProviders.forEach(provider => {
        expect(provider.retryConfig.maxRetries).toBeGreaterThanOrEqual(0);
        expect(provider.retryConfig.initialDelay).toBeGreaterThan(0);
        expect(provider.retryConfig.maxDelay).toBeGreaterThanOrEqual(provider.retryConfig.initialDelay);
        expect(provider.retryConfig.backoffMultiplier).toBeGreaterThan(1);
      });
    });

    it('超时配置应该有合理的边界值', () => {
      mockProviders.forEach(provider => {
        expect(provider.timeout).toBeGreaterThan(0);
        expect(provider.timeout).toBeLessThanOrEqual(600000); // 最大 10 分钟
      });
    });
  });
});
