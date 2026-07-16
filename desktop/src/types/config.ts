// ─── Config Types ────────────────────────────────────────────────────────────

export interface SessionConfig {
  timelineMaxFileMB: number;
}

export interface LogConfig {
  level: string;
  console: boolean;
  file: boolean;
}

export interface RetryConfig {
  maxRetries: number;
  initialDelayMs: number;
  maxDelayMs: number;
  backoffMultiplier: number;
}

export interface LLMProvider {
  id: string;
  name: string;
  baseUrl: string;
  apiKey?: string;
  apiKeyEnv: string;
  enabled: boolean;
  isDefault: boolean;
  timeoutMs: number;
  retry: RetryConfig;
  headers?: Record<string, string>;
}

export interface GenerationParams {
  temperature: number;
  maxTokens: number;
}

export interface ThinkingConfig {
  enabled: boolean;
  reasoningEffort: string;
  thinkingType?: string;
}

export interface LLMModel {
  id: string;
  providerId: string;
  name: string;
  apiModel?: string;
  contextWindow: number;
  enabled: boolean;
  generation: GenerationParams;
  thinking: ThinkingConfig;
  vision?: boolean;
}

export interface ImageModelConfig {
  id: string;
  name: string;
  provider: string;
  secretId: string;
  secretIdEnv: string;
  secretKey: string;
  secretKeyEnv: string;
  apiKey: string;
  apiKeyEnv: string;
  apiBaseHost: string;
  region: string;
  isDefault: boolean;
  enabled: boolean;
}

export interface ToolsConfig {
  maxFileSize: number;
  maxMatches: number;
  maxLineLen: number;
  maxGlobItems: number;
  maxWriteSize: number;
  maxMultiWriteBytes: number;
  maxMultiWriteFiles: number;
  maxReplaceEdits: number;
  httpAllowedHosts?: string[];
  httpMaxBody: number;
  httpTimeoutMs: number;
  httpBlockPrivate: boolean;
  shellBlockRegexes?: string[];
  shellConfirmRegexes?: string[];
  shellMaxOutput: number;
  webSearchTimeoutMs: number;
  imageModels?: ImageModelConfig[];
}

export interface EmbeddingProvider {
  id: string;
  name: string;
  baseUrl: string;
  apiKey?: string;
  apiKeyEnv: string;
  enabled: boolean;
}

export interface EmbeddingModel {
  id: string;
  providerId: string;
  name: string;
  dimension: number;
  batchSize: number;
  normalize: boolean;
  enabled: boolean;
  isDefault: boolean;
}

export interface EmbeddingConfig {
  enabled: boolean;
  minSimilarity: number;
  provider: string;
  modelName: string;
  providers: EmbeddingProvider[];
  models: EmbeddingModel[];
}

export interface DefaultModelsConfig {
  basic: string;
  universal: string;
  superior: string;
  expert: string;
  apex: string;
  fast: string;
  fallback: string;
}

export interface QQBotConfig {
  id?: string;
  name?: string;
  enabled: boolean;
  appId: string;
  appSecret: string;
  intents: number;
  sandbox: boolean;
  bind_type?: string;
  bind_agent?: string;
  whitelist_enabled?: boolean;
  whitelist?: string[];
}

export interface L1AgentSettings {
  builtinMcpServers?: string[];
  externalMcpServers?: string[];
}

export interface SimulationConfig {
  defaultModelId?: string;
  defaultProviderId?: string;
  dbPath?: string;
  defaultMaxWallClockMs?: number;
  enableReflection?: boolean;
  simulatedHours?: number;
  tickIntervalMs?: number;
  timeScale?: number;
  language?: string;
}

export interface AppConfig {
  session: SessionConfig;
  log: LogConfig;
  tools: ToolsConfig;
  providers: LLMProvider[];
  models: LLMModel[];
  embedding: EmbeddingConfig;
  defaultModels: DefaultModelsConfig;
  qqbots: QQBotConfig[];
  agent: L1AgentSettings;
  lspmcp: LSPMCPConfig;
  simulation: SimulationConfig;
}

// ─── Cron/Timer Task Types ──────────────────────────────────────────────────

export interface CronTask {
  id: string;
  expression: string;
  instruction: string;
  target_agent: string;
  status: "active" | "paused" | "completed";
  last_run_at?: string;
  next_run_at: string;
  created_at: string;
  updated_at: string;
}

export interface CreateCronTaskRequest {
  expression: string;
  instruction: string;
  target_agent?: string;
}

export interface UpdateCronTaskRequest {
  expression?: string;
  instruction?: string;
  target_agent?: string;
  status?: "active" | "paused";
}

// ─── LSP MCP ──────────────────────────────────────────────────────────────────

export interface LSPMCPEntry {
  id: string;
  command: string;
  args: string[];
  languages: string[];
  extensions: string[];
  disabled: boolean;
}

export interface LSPMCPConfig {
  servers: LSPMCPEntry[];
}
