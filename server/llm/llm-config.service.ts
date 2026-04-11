/**
 * ============================================
 * LLM Config Service (LLM 配置服务)
 * ============================================
 *
 * 【职责】
 * - 管理 LLM Provider 配置
 * - 管理 LLM Model 配置
 * - 管理 Agent 默认配置
 * - 管理 Supervisor 默认配置
 * - 提供模型选择和回退机制
 *
 * 【架构】
 *
 *   ┌─────────────────────────────────────────────────────┐
 *   │                  LLMConfigService                    │
 *   │  ┌─────────────────────────────────────────────────┐ │
 *   │  │           In-Memory Cache                        │ │
 *   │  │  - providers: Map<string, LLMProvider>          │ │
 *   │  │  - models: Map<string, LLMModel>                │ │
 *   │  │  - agentDefaults: AgentDefaultConfig            │ │
 *   │  │  - supervisorDefaults: SupervisorDefaultConfig  │ │
 *   │  └─────────────────────────────────────────────────┘ │
 *   └─────────────────────────────────────────────────────┘
 *                            │
 *                            ▼
 *   ┌─────────────────────────────────────────────────────┐
 *   │               ConfigService (存储层)                  │
 *   │  llm.providers    →  providers JSON                  │
 *   │  llm.models       →  models JSON                     │
 *   │  agent.defaults   →  agent defaults JSON             │
 *   │  supervisor.defaults → supervisor defaults JSON       │
 *   └─────────────────────────────────────────────────────┘
 *
 * 【使用方式】
 *
 *   // 获取默认 chat 模型
 *   const chatModel = llmConfigService.getDefaultModel('chat');
 *
 *   // 获取模型实例
 *   const model = llmConfigService.getModel('deepseek-chat');
 *
 *   // 获取 Provider
 *   const provider = llmConfigService.getProvider('deepseek');
 *
 * ============================================
 */

import { Logger } from '../logger/index.js';
import { configService } from '../storage/config.service.js';
import {
  type LLMProvider,
  type LLMModel,
  type AgentDefaultConfig,
  type SupervisorDefaultConfig,
  type ModelSelectionStrategy,
  type ModelType,
  type ProviderJSON,
  type ModelsJSON,
  type AgentDefaultsJSON,
  type SupervisorDefaultsJSON,
} from './types.js';
import {
  CONFIG_KEYS,
  DEFAULT_PROVIDERS,
  DEFAULT_MODELS,
  DEFAULT_AGENT_DEFAULTS,
  DEFAULT_SUPERVISOR_DEFAULTS,
} from './defaults.js';

// ============== Service ==============

class LLMConfigService {
  private logger: Logger;
  private providers: Map<string, LLMProvider> = new Map();
  private models: Map<string, LLMModel> = new Map();
  private modelsByType: Map<ModelType, LLMModel[]> = new Map();
  private modelsByProvider: Map<string, LLMModel[]> = new Map();
  private agentDefaults: AgentDefaultConfig = DEFAULT_AGENT_DEFAULTS;
  private supervisorDefaults: SupervisorDefaultConfig = DEFAULT_SUPERVISOR_DEFAULTS;
  private initialized = false;

  constructor() {
    this.logger = Logger.system();
  }

  /**
   * 初始化 - 从 ConfigService 加载配置
   */
  async initialize(): Promise<void> {
    if (this.initialized) return;

    this.logger.info({ category: 'llm', message: 'Initializing LLM config service' });

    // 等待 ConfigService 初始化
    if (!configService.isInitialized()) {
      await configService.initialize();
    }

    // 加载 Providers
    this.loadProviders();

    // 加载 Models
    this.loadModels();

    // 加载 Agent 默认配置
    this.loadAgentDefaults();

    // 加载 Supervisor 默认配置
    this.loadSupervisorDefaults();

    this.initialized = true;
    this.logger.info({
      category: 'llm',
      message: 'LLM config service initialized',
      context: {
        providers: this.providers.size,
        models: this.models.size,
      },
    });
  }

  // ============== Provider Methods ==============

  private loadProviders(): void {
    const config = configService.get<ProviderJSON>(CONFIG_KEYS.PROVIDERS);
    const providers = config?.providers || DEFAULT_PROVIDERS;
    this.providers.clear();
    for (const p of providers) {
      this.providers.set(p.id, p);
    }
  }

  getProvider(id: string): LLMProvider | undefined {
    return this.providers.get(id);
  }

  getAllProviders(): LLMProvider[] {
    return Array.from(this.providers.values());
  }

  getEnabledProviders(): LLMProvider[] {
    return Array.from(this.providers.values()).filter(p => p.enabled);
  }

  getDefaultProvider(): LLMProvider | undefined {
    return Array.from(this.providers.values()).find(p => p.isDefault && p.enabled);
  }

  async setProvider(provider: LLMProvider): Promise<void> {
    this.providers.set(provider.id, provider);
    await this.saveProviders();
  }

  async deleteProvider(id: string): Promise<void> {
    this.providers.delete(id);
    await this.saveProviders();
  }

  private async saveProviders(): Promise<void> {
    const json: ProviderJSON = { providers: Array.from(this.providers.values()) };
    await configService.set(CONFIG_KEYS.PROVIDERS, json);
  }

  // ============== Model Methods ==============

  private loadModels(): void {
    const config = configService.get<ModelsJSON>(CONFIG_KEYS.MODELS);
    const models = config?.models || DEFAULT_MODELS;
    this.models.clear();
    this.modelsByType.clear();
    this.modelsByProvider.clear();

    for (const m of models) {
      this.models.set(m.id, m);

      // 按类型索引
      if (!this.modelsByType.has(m.type)) {
        this.modelsByType.set(m.type, []);
      }
      this.modelsByType.get(m.type)!.push(m);

      // 按 Provider 索引
      if (!this.modelsByProvider.has(m.providerId)) {
        this.modelsByProvider.set(m.providerId, []);
      }
      this.modelsByProvider.get(m.providerId)!.push(m);
    }
  }

  getModel(id: string): LLMModel | undefined {
    return this.models.get(id);
  }

  getAllModels(): LLMModel[] {
    return Array.from(this.models.values());
  }

  getEnabledModels(): LLMModel[] {
    return Array.from(this.models.values()).filter(m => m.enabled);
  }

  getModelsByType(type: ModelType): LLMModel[] {
    return this.modelsByType.get(type) || [];
  }

  getModelsByProvider(providerId: string): LLMModel[] {
    return this.modelsByProvider.get(providerId) || [];
  }

  getDefaultModel(type: ModelType): LLMModel | undefined {
    return Array.from(this.models.values()).find(m => m.type === type && m.isDefault && m.enabled);
  }

  async setModel(model: LLMModel): Promise<void> {
    this.models.set(model.id, model);
    await this.saveModels();
  }

  async deleteModel(id: string): Promise<void> {
    this.models.delete(id);
    await this.saveModels();
  }

  private async saveModels(): Promise<void> {
    const json: ModelsJSON = { models: Array.from(this.models.values()) };
    await configService.set(CONFIG_KEYS.MODELS, json);
  }

  // ============== Model Selection ==============

  /**
   * 根据策略选择模型
   * 注意：不支持回退机制，模型调用失败将直接抛出错误
   */
  selectModel(
    type: ModelType,
    strategy: ModelSelectionStrategy = 'default',
    preferredProvider?: string
  ): LLMModel | undefined {
    const models = this.getModelsByType(type).filter(m => m.enabled);

    if (models.length === 0) return undefined;

    // 如果指定了 Provider，优先使用该 Provider 的模型
    if (preferredProvider) {
      const providerModel = models.find(m => m.providerId === preferredProvider);
      if (providerModel) return providerModel;
    }

    switch (strategy) {
      case 'default':
        return models.find(m => m.isDefault) || models[0];

      case 'prefer-fast':
        // 优先选择有 'fast' 标签的模型
        return models.find(m => m.tags.includes('fast')) || models[0];

      case 'prefer-cheap':
        // 优先选择有 'cheap' 标签的模型
        return models.find(m => m.tags.includes('cheap')) || models[0];

      case 'prefer-context':
        // 优先选择上下文窗口最大的模型
        return models.reduce((a, b) => (a.contextWindow > b.contextWindow ? a : b));

      case 'manual':
        return undefined;

      default:
        return models.find(m => m.isDefault) || models[0];
    }
  }

  // ============== Agent Defaults Methods ==============

  private loadAgentDefaults(): void {
    const config = configService.get<AgentDefaultsJSON>(CONFIG_KEYS.AGENT_DEFAULTS);
    this.agentDefaults = config?.agent || DEFAULT_AGENT_DEFAULTS;
  }

  getAgentDefaults(): AgentDefaultConfig {
    return JSON.parse(JSON.stringify(this.agentDefaults)); // 深拷贝
  }

  async setAgentDefaults(defaults: AgentDefaultConfig): Promise<void> {
    this.agentDefaults = { ...defaults };
    const json: AgentDefaultsJSON = { agent: this.agentDefaults };
    await configService.set(CONFIG_KEYS.AGENT_DEFAULTS, json);
  }

  getRoleDefaultConfig(role: 'chat' | 'code' | 'planner' | 'evaluator') {
    return this.agentDefaults.roleDefaults[role];
  }

  // ============== Supervisor Defaults Methods ==============

  private loadSupervisorDefaults(): void {
    const config = configService.get<SupervisorDefaultsJSON>(CONFIG_KEYS.SUPERVISOR_DEFAULTS);
    this.supervisorDefaults = config?.supervisor || DEFAULT_SUPERVISOR_DEFAULTS;
  }

  getSupervisorDefaults(): SupervisorDefaultConfig {
    return { ...this.supervisorDefaults };
  }

  async setSupervisorDefaults(defaults: SupervisorDefaultConfig): Promise<void> {
    this.supervisorDefaults = { ...defaults };
    const json: SupervisorDefaultsJSON = { supervisor: this.supervisorDefaults };
    await configService.set(CONFIG_KEYS.SUPERVISOR_DEFAULTS, json);
  }

  // ============== Utility Methods ==============

  /**
   * 获取模型的实际上下文窗口大小
   */
  getEffectiveContextWindow(modelId: string, ratio?: number): number {
    const model = this.models.get(modelId);
    if (!model) return 0;
    const r = ratio ?? this.agentDefaults.contextWindowRatio;
    return Math.floor(model.contextWindow * r);
  }

  /**
   * 检查 Provider API Key 是否配置
   */
  isProviderConfigured(providerId: string): boolean {
    const provider = this.providers.get(providerId);
    if (!provider) return false;
    if (!provider.apiKeyEnv) return true; // 不需要 API Key
    return !!process.env[provider.apiKeyEnv];
  }

  /**
   * 重新加载配置
   */
  async reload(): Promise<void> {
    this.initialized = false;
    await this.initialize();
  }

  /**
   * 重置为默认配置
   */
  async resetToDefaults(): Promise<void> {
    this.providers.clear();
    this.models.clear();
    for (const p of DEFAULT_PROVIDERS) this.providers.set(p.id, p);
    for (const m of DEFAULT_MODELS) this.models.set(m.id, m);
    this.agentDefaults = JSON.parse(JSON.stringify(DEFAULT_AGENT_DEFAULTS));
    this.supervisorDefaults = { ...DEFAULT_SUPERVISOR_DEFAULTS };

    await Promise.all([
      this.saveProviders(),
      this.saveModels(),
      configService.set(CONFIG_KEYS.AGENT_DEFAULTS, { agent: this.agentDefaults }),
      configService.set(CONFIG_KEYS.SUPERVISOR_DEFAULTS, { supervisor: this.supervisorDefaults }),
    ]);

    this.logger.info({ category: 'llm', message: 'LLM config reset to defaults' });
  }
}

export const llmConfigService = new LLMConfigService();
