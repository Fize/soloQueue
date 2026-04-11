/**
 * ============================================
 * LLM 配置类型定义
 * ============================================
 *
 * 【职责】
 * - 定义 LLM Provider、Model、Embedding 的类型
 * - 支持多 Provider 和多模型
 * - 提供配置默认值
 *
 * 【类型层级】
 *
 *   LLMProvider (LLM 提供商)
 *   └── baseUrl, apiKeyEnv, capabilities, timeout, retry
 *
 *   LLMModel (模型配置)
 *   ├── providerId → 关联到 LLMProvider
 *   ├── type → chat | embedding | code | vision
 *   └── contextWindow, defaults, config
 *
 *   AgentDefaultConfig (Agent 默认配置)
 *   └── roleDefaults → chat/code/planner/evaluator
 *
 * ============================================
 */

// ============== Provider Types ==============

/**
 * Provider 支持的能力
 */
export type ProviderCapability = 'chat' | 'embedding' | 'streaming' | 'vision' | 'function-calling';

/**
 * 重试配置
 */
export interface RetryConfig {
  maxRetries: number;
  initialDelay: number;   // 初始延迟 (ms)
  maxDelay: number;       // 最大延迟 (ms)
  backoffMultiplier: number;  // 退避乘数
}

/**
 * LLM Provider 配置
 */
export interface LLMProvider {
  id: string;
  name: string;
  baseUrl: string;
  apiKeyEnv: string;       // 环境变量名，如 DEEPSEEK_API_KEY
  enabled: boolean;
  isDefault: boolean;
  capabilities: ProviderCapability[];
  timeout: number;         // 超时时间 (ms)
  retryConfig: RetryConfig;
  headers?: Record<string, string>;  // 额外的请求头
}

/**
 * 创建 Provider 的输入
 */
export interface CreateProviderInput {
  id: string;
  name: string;
  baseUrl: string;
  apiKeyEnv?: string;
  enabled?: boolean;
  isDefault?: boolean;
  capabilities?: ProviderCapability[];
  timeout?: number;
  retryConfig?: Partial<RetryConfig>;
  headers?: Record<string, string>;
}

// ============== Model Types ==============

/**
 * 模型类型
 */
export type ModelType = 'chat' | 'embedding' | 'code' | 'vision';

/**
 * 模型默认值
 */
export interface ModelDefaults {
  temperature: number;
  maxTokens: number;
  topP?: number;
  frequencyPenalty?: number;
  presencePenalty?: number;
}

/**
 * 模型配置
 */
export interface ModelConfig {
  contextWindow: number;   // 上下文窗口大小 (tokens)
  supportsStreaming: boolean;
  supportsVision?: boolean;
  supportsFunctionCalling?: boolean;
  supportsJSONMode?: boolean;
  maxOutputTokens?: number;
}

/**
 * LLM Model 配置
 */
export interface LLMModel {
  id: string;              // 如 'deepseek-chat', 'gpt-4o'
  providerId: string;
  name: string;
  type: ModelType;
  contextWindow: number;
  defaults: ModelDefaults;
  config: ModelConfig;
  tags: string[];         // 如 ['fast', 'latest', 'cheap']
  enabled: boolean;
  isDefault: boolean;    // 是否为该类型的默认模型
}

/**
 * 创建 Model 的输入
 */
export interface CreateModelInput {
  id: string;
  providerId: string;
  name?: string;
  type: ModelType;
  contextWindow: number;
  defaults?: Partial<ModelDefaults>;
  config?: Partial<ModelConfig>;
  tags?: string[];
  enabled?: boolean;
  isDefault?: boolean;
}

// ============== Embedding Types ==============

/**
 * Embedding 模型配置
 * 注意：DeepSeek 当前不支持 embedding，如需使用需配置其他 Provider
 */
export interface EmbeddingConfig {
  providerId: string;
  dimension: number;
  batchSize: number;      // 批处理大小
  normalize: boolean;     // 是否归一化
  cacheEnabled: boolean;  // 是否启用缓存
}

/**
 * 创建 Embedding 配置的输入
 */
export interface CreateEmbeddingInput {
  providerId: string;
  dimension: number;
  batchSize?: number;
  normalize?: boolean;
  cacheEnabled?: boolean;
}

// ============== Agent Default Config Types ==============

/**
 * 模型选择策略
 */
export type ModelSelectionStrategy =
  | 'default'        // 使用默认模型
  | 'prefer-fast'    // 优先选择快速模型
  | 'prefer-cheap'   // 优先选择便宜模型
  | 'prefer-context' // 优先选择大上下文模型
  | 'manual';        // 手动指定

/**
 * 角色模型配置
 */
export interface RoleModelConfig {
  providerId: string;
  modelId: string;
  temperature: number;
  maxTokens: number;
  contextWindowRatio: number;  // 使用上下文窗口的比例 (0-1)
}

/**
 * Agent 默认配置
 */
export interface AgentDefaultConfig {
  defaultModelId: string;
  modelSelection: ModelSelectionStrategy;
  roleDefaults: {
    chat: RoleModelConfig;
    code: RoleModelConfig;
    planner: RoleModelConfig;
    evaluator: RoleModelConfig;
  };
  maxRetries: number;
  timeout: number;
  contextWindowRatio: number;  // 默认上下文窗口使用比例
}

// ============== Supervisor Default Config ==============

/**
 * Supervisor 默认监督配置
 */
export interface SupervisorDefaultConfig {
  defaultStrategy: 'one_for_one' | 'one_for_all' | 'all_for_one' | 'stop';
  maxRetries: number;
  retryInterval: number;
  exponentialBackoff: boolean;
  maxBackoff: number;
}

// ============== JSON Storage Types ==============

/**
 * 配置存储格式 - Provider JSON
 */
export interface ProviderJSON {
  providers: LLMProvider[];
}

/**
 * 配置存储格式 - Models JSON
 */
export interface ModelsJSON {
  models: LLMModel[];
}

/**
 * 配置存储格式 - Embedding JSON
 */
export interface EmbeddingJSON {
  embedding: EmbeddingConfig;
}

/**
 * 配置存储格式 - Agent Defaults JSON
 */
export interface AgentDefaultsJSON {
  agent: AgentDefaultConfig;
}

/**
 * 配置存储格式 - Supervisor Defaults JSON
 */
export interface SupervisorDefaultsJSON {
  supervisor: SupervisorDefaultConfig;
}
