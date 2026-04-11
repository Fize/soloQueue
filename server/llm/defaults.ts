/**
 * ============================================
 * LLM 默认配置常量
 * ============================================
 *
 * 【职责】
 * - 存放 LLM 配置的默认值
 * - 独立于服务，避免循环依赖
 * - 被 seeds.ts 和 llm-config.service.ts 共同使用
 *
 * ============================================
 */

import type {
  LLMProvider,
  LLMModel,
  AgentDefaultConfig,
  SupervisorDefaultConfig,
} from './types.js';

// ============== Provider Defaults ==============

export const DEFAULT_PROVIDERS: LLMProvider[] = [
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

// ============== Model Defaults ==============

export const DEFAULT_MODELS: LLMModel[] = [
  // DeepSeek Chat
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
  // DeepSeek Coder
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
  // DeepSeek Reasoner
  {
    id: 'deepseek-reasoner',
    providerId: 'deepseek',
    name: 'DeepSeek Reasoner',
    type: 'chat',
    contextWindow: 64000,
    defaults: { temperature: 0.6, maxTokens: 8192 },
    config: { contextWindow: 64000, supportsStreaming: true },
    tags: ['reasoning', 'latest'],
    enabled: true,
    isDefault: false,
  },
];

// ============== Agent Defaults ==============

export const DEFAULT_AGENT_DEFAULTS: AgentDefaultConfig = {
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

// ============== Supervisor Defaults ==============

export const DEFAULT_SUPERVISOR_DEFAULTS: SupervisorDefaultConfig = {
  defaultStrategy: 'one_for_one',
  maxRetries: 3,
  retryInterval: 1000,
  exponentialBackoff: true,
  maxBackoff: 30000,
};

// ============== Config Keys ==============

export const CONFIG_KEYS = {
  PROVIDERS: 'llm.providers',
  MODELS: 'llm.models',
  AGENT_DEFAULTS: 'agent.defaults',
  SUPERVISOR_DEFAULTS: 'supervisor.defaults',
} as const;
