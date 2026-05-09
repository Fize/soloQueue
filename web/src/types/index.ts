export type PlanStatus = 'plan' | 'running' | 'done';

export interface Plan {
  id: string;
  title: string;
  content: string;
  status: PlanStatus;
  tags: string;
  creator: string;
  created_at: string;
  updated_at: string;
  todo_items?: TodoItemWithDeps[];
}

export interface TodoItemWithDeps {
  id: string;
  plan_id: string;
  content: string;
  completed: boolean;
  sort_order: number;
  created_at: string;
  depends_on: string[];
  blockers: string[];
}

export interface PlanListResponse {
  plans: Plan[];
  total: number;
}

// ─── Agent Types ─────────────────────────────────────────────────────────────

export type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped';

export interface AgentInfo {
  id: string;
  instance_id: string;
  name: string;
  state: AgentState;
  model_id: string;
  group: string;
  is_leader: boolean;
  task_level: string;
  error_count: number;
  last_error: string;
  pending_delegations: number;
  mailbox_high: number;
  mailbox_normal: number;
}

export interface SupervisorInfo {
  group: string;
  leader_id: string;
  children_ids: string[];
}

export interface AgentListResponse {
  agents: AgentInfo[];
  supervisors: SupervisorInfo[];
}

export interface AgentProfile {
  soul: string;
  rules: string;
}

export interface UpdateAgentProfileRequest {
  soul?: string;
  rules?: string;
}

export interface AgentTemplate {
  id: string;
  name: string;
  description: string;
  is_leader: boolean;
  group: string;
  model_id: string;
}

export interface TeamInfo {
  name: string;
  description: string;
  agents: AgentTemplate[];
}

export interface TeamListResponse {
  teams: TeamInfo[];
}

// ─── Runtime Types ───────────────────────────────────────────────────────────

export interface RuntimeStatus {
  phase: string;
  prompt_tokens: number;
  output_tokens: number;
  cache_hit_tokens: number;
  cache_miss_tokens: number;
  context_pct: number;
  current_iter: number;
  content_deltas: number;
  active_delegations: number;
  total_agents: number;
  running_agents: number;
  idle_agents: number;
  total_errors: number;
  http_addr: string;
}

// ─── WebSocket Message Types ────────────────────────────────────────────────

export interface WSStateMessage {
  type: 'state';
  runtime: RuntimeStatus;
  agents: AgentListResponse;
}

export type WSMessage = WSStateMessage;

// ─── Config Types ────────────────────────────────────────────────────────────

export interface SessionConfig {
  timelineMaxFileMB: number;
  timelineMaxFiles: number;
  contextIdleThresholdMin: number;
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
}

export interface EmbeddingProvider {
  id: string;
  name: string;
  baseUrl: string;
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
  providers: EmbeddingProvider[];
  models: EmbeddingModel[];
}

export interface DefaultModelsConfig {
  expert: string;
  superior: string;
  universal: string;
  fast: string;
  fallback: string;
}

export interface QQBotConfig {
  enabled: boolean;
  appId: string;
  appSecret: string;
  intents: number;
  sandbox: boolean;
}

export interface AppConfig {
  session: SessionConfig;
  log: LogConfig;
  tools: ToolsConfig;
  providers: LLMProvider[];
  models: LLMModel[];
  embedding: EmbeddingConfig;
  defaultModels: DefaultModelsConfig;
  qqbot: QQBotConfig;
}

// ─── Tool & Skill Types ────────────────────────────────────────────────────

export interface ToolInfo {
  name: string;
  description: string;
  parameters: Record<string, unknown> | null;
}

export interface ToolListResponse {
  tools: ToolInfo[];
  total: number;
}

export type SkillCategory = 'builtin' | 'user';

export interface SkillInfo {
  id: string;
  description: string;
  category: SkillCategory;
  user_invocable: boolean;
  disable_model_invocation: boolean;
  context: string;
  agent: string;
  file_path: string;
  allowed_tools: string[];
}

export interface SkillListResponse {
  skills: SkillInfo[];
  total: number;
}
