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

export interface ModelRoutesConfig {
	general: string;
	engineering: string;
	research: string;
	classifier: string;
	vision?: string;
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

export interface WeChatAccountView {
  id: string;
  name: string;
  enabled: boolean;
  connected: boolean;
  credentialConfigured: boolean;
  botIdMasked?: string;
  baseUrl?: string;
  botAgent?: string;
  bind_type: "l1" | "l2";
  bind_agent?: string;
  whitelist_enabled: boolean;
  whitelist: string[];
}

export type WeChatLoginStatus =
  | "creating_qr"
  | "awaiting_scan"
  | "scanned"
  | "awaiting_confirmation"
  | "awaiting_verification"
  | "connected"
  | "already_connected"
  | "expired"
  | "cancelled"
  | "failed";

export interface WeChatLoginSnapshot {
  sessionId: string;
  accountId: string;
  status: WeChatLoginStatus;
  qrPayload?: string;
  expiresAt: string;
  message?: string;
}

export interface StartWeChatLoginRequest {
  accountId: string;
  name: string;
  bindType: "l1" | "l2";
  bindAgent?: string;
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

export interface SpeechConfig {
  enabled: boolean;
  model: string;     // tiny | base | small | medium
  modelDir: string;  // "" = ~/.soloqueue/models
}

export interface SandboxConfig {
  runtime: 'host' | 'sandbox';
  backend?: string;
  network_enabled: boolean;
  enabled: boolean;
}

export interface SandboxRuntimeStatus {
  desired_runtime: 'host' | 'sandbox';
  state: 'disabled' | 'idle' | 'starting' | 'ready' | 'failed' | 'draining';
  backend?: string;
  workspace?: string;
  isolation_complete: boolean;
  host_exceptions: number;
  network_enabled: boolean;
  last_error?: string;
}

export interface SpeechStatus {
  enabled: boolean;
  model: string;
  modelDir: string;
  modelPath: string;
  modelExists: boolean;
  whisperBinary: string;
  whisperAvailable: boolean;
  silkDecoder: string;
  silkAvailable: boolean;
  ready: boolean;
}

export interface SpeechInstallResponse {
  success: boolean;
  binaryPath?: string;
  modelPath?: string;
  silkPath?: string;
  binaryMessage?: string;
  modelMessage?: string;
  silkMessage?: string;
  error?: string;
  detail?: string;  // step-by-step instructions on failure
}

export interface AppConfig {
  session: SessionConfig;
  log: LogConfig;
  tools: ToolsConfig;
  sandbox?: SandboxConfig;
  providers: LLMProvider[];
  models: LLMModel[];
  embedding: EmbeddingConfig;
	modelRoutes: ModelRoutesConfig;
  qqbots: QQBotConfig[];
  wechatBots?: WeChatAccountView[];
  agent: L1AgentSettings;
  lspmcp: LSPMCPConfig;
  simulation: SimulationConfig;
  speech: SpeechConfig;
}

// ─── Cron/Timer Task Types ──────────────────────────────────────────────────

export interface CronTask {
  id: string;
  title: string;
	task_type: "general" | "engineering" | "research";
  expression: string;
  instruction: string;
  target_agent: string;
  status: "active" | "paused" | "running" | "completed" | "failed";
  last_run_at?: string;
  next_run_at: string;
  created_at: string;
  updated_at: string;
}

export interface CreateCronTaskRequest {
  title: string;
	task_type: "general" | "engineering" | "research";
  expression: string;
  instruction: string;
  target_agent?: string;
}

export interface UpdateCronTaskRequest {
  title?: string;
	task_type?: "general" | "engineering" | "research";
  expression?: string;
  instruction?: string;
  target_agent?: string;
  status?: "active" | "paused";
}

// ─── Cron Execution History Types ───────────────────────────────────────────

export interface CronExecutionRecord {
  id: string;
  task_id: string;
  executed_at: string;
  completed_at: string;
  duration_ms: number;
  status: "success" | "failed" | "panic";
  result_summary: string;
  error_message: string;
	task_type: string;
  target_agent: string;
  model_id: string;
  provider_id: string;
  timeline_dir: string;
}

export interface CronHistoryDetail {
  execution: CronExecutionRecord;
  events: TimelineEvent[];
}

export interface TimelineEvent {
  ts: string;
  type: "message" | "control";
  msg?: TimelineMessage;
  ctrl?: TimelineControl;
}

export interface TimelineMessage {
  role: string;
  content: string;
  reasoning?: string;
  name?: string;
  tool_call_id?: string;
  tool_calls?: TimelineToolCall[];
  ephemeral?: boolean;
  agent_id?: string;
  ts?: string;
}

export interface TimelineToolCall {
  id: string;
  type: string;
  name: string;
  arguments: string;
}

export interface TimelineControl {
  action: string;
  reason?: string;
  content?: string;
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
