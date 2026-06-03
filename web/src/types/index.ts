export type PlanStatus = 'backlog' | 'todo' | 'running' | 'done'

export interface Comment {
  id: string
  issue_id: string
  author: string
  content: string
  created_at: string
}

export interface Plan {
  id: string
  title: string
  description: string
  plan: string
  content?: string
  status: PlanStatus
  tags: string
  author: string
  created_at: string
  updated_at: string
  todo_items?: TodoItemWithDeps[]
  comments?: Comment[]
}

export interface TodoItemWithDeps {
  id: string
  plan_id: string
  content: string
  completed: boolean
  sort_order: number
  created_at: string
  depends_on: string[]
  blockers: string[]
}

export interface PlanListResponse {
  plans: Plan[]
  total: number
}

export interface CreatePlanRequest {
  title: string
  description?: string
  plan?: string
  content?: string
  status?: string
  tags?: string
  author?: string
}

export interface UpdatePlanRequest {
  title?: string
  description?: string
  plan?: string
  content?: string
  status?: string
  tags?: string
}

// ─── Agent Types ─────────────────────────────────────────────────────────────

export type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'

export interface AgentInfo {
  id: string
  instance_id: string
  name: string
  state: AgentState
  model_id: string
  group: string
  is_leader: boolean
  task_level: string
  error_count: number
  last_error: string
  pending_delegations: number
  mailbox_high: number
  mailbox_normal: number
}

export interface SupervisorInfo {
  group: string
  leader_id: string
  children_ids: string[]
}

export interface AgentListResponse {
  agents: AgentInfo[]
  supervisors: SupervisorInfo[]
}

export interface AgentProfile {
  soul: string
  rules: string
}

export interface AgentConfig {
  raw_config: string
  system_prompt: string
  name: string
  description: string
  model: string
  group: string
  is_leader: boolean
  mcp_servers: string[]
}

export interface UpdateAgentProfileRequest {
  soul?: string
  rules?: string
}

export interface UpdateAgentConfigRequest {
  raw_config?: string
  system_prompt?: string
}

export interface AgentTemplate {
  id: string
  name: string
  description: string
  is_leader: boolean
  group: string
  model_id: string
}

export interface TeamInfo {
  name: string
  description: string
  agents: AgentTemplate[]
}

export interface TeamListResponse {
  teams: TeamInfo[]
}

// ─── Runtime Types ───────────────────────────────────────────────────────────

export type Segment =
  | { type: 'thinking'; text: string }
  | { type: 'content'; text: string }
  | {
      type: 'tool_call'
      call_id: string
      name: string
      args: string
      result: string
      error: string
      done: boolean
      duration_ms: number
    }

export interface AgentStreamState {
  agent_id: string
  processing: boolean
  segments: Segment[]
  iteration: number
  error?: string
}

export interface RuntimeStatus {
  phase: string
  prompt_tokens: number
  output_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
  context_pct: number
  current_iter: number
  content_deltas: number
  active_delegations: number
  total_agents: number
  running_agents: number
  idle_agents: number
  total_errors: number
  http_addr: string
  agent_streams: Record<string, AgentStreamState>
}

// ─── Auth Types ───────────────────────────────────────────────────────────────

export interface LoginRequest {
  user: string
  password: string
}

export interface LoginResponse {
  token: string
  user: string
}

export interface AuthCheckResponse {
  authenticated: boolean
  user?: string
}

// ─── Dependency Types ─────────────────────────────────────────────────────────

export interface DependenciesResponse {
  todo_id: string
  depends_on: string[]
  blockers: string[]
}

export interface SetDependenciesRequest {
  depends_on: string[]
}

// ─── WebSocket Message Types ────────────────────────────────────────────────

export interface WSStateMessage {
  type: 'state'
  runtime: RuntimeStatus
  agents: AgentListResponse
}

export type WSMessage = WSStateMessage

// ─── Config Types ────────────────────────────────────────────────────────────

export interface SessionConfig {
  timelineMaxFileMB: number
}

export interface LogConfig {
  level: string
  console: boolean
  file: boolean
}

export interface RetryConfig {
  maxRetries: number
  initialDelayMs: number
  maxDelayMs: number
  backoffMultiplier: number
}

export interface LLMProvider {
  id: string
  name: string
  baseUrl: string
  apiKey?: string
  apiKeyEnv: string
  enabled: boolean
  isDefault: boolean
  timeoutMs: number
  retry: RetryConfig
  headers?: Record<string, string>
}

export interface GenerationParams {
  temperature: number
  maxTokens: number
}

export interface ThinkingConfig {
  enabled: boolean
  reasoningEffort: string
}

export interface LLMModel {
  id: string
  providerId: string
  name: string
  apiModel?: string
  contextWindow: number
  enabled: boolean
  generation: GenerationParams
  thinking: ThinkingConfig
}

export interface ImageModelConfig {
  id: string
  name: string
  provider: string
  secretId: string
  secretIdEnv: string
  secretKey: string
  secretKeyEnv: string
  apiKey: string
  apiKeyEnv: string
  apiBaseHost: string
  region: string
  isDefault: boolean
  enabled: boolean
}

export interface ToolsConfig {
  maxFileSize: number
  maxMatches: number
  maxLineLen: number
  maxGlobItems: number
  maxWriteSize: number
  maxMultiWriteBytes: number
  maxMultiWriteFiles: number
  maxReplaceEdits: number
  httpAllowedHosts?: string[]
  httpMaxBody: number
  httpTimeoutMs: number
  httpBlockPrivate: boolean
  shellBlockRegexes?: string[]
  shellConfirmRegexes?: string[]
  shellMaxOutput: number
  webSearchTimeoutMs: number
  imageModels?: ImageModelConfig[]
}

export interface EmbeddingProvider {
  id: string
  name: string
  baseUrl: string
  apiKey?: string
  apiKeyEnv: string
  enabled: boolean
}

export interface EmbeddingModel {
  id: string
  providerId: string
  name: string
  dimension: number
  batchSize: number
  normalize: boolean
  enabled: boolean
  isDefault: boolean
}

export interface EmbeddingConfig {
  enabled: boolean
  minSimilarity: number
  providers: EmbeddingProvider[]
  models: EmbeddingModel[]
}

export interface DefaultModelsConfig {
  expert: string
  superior: string
  universal: string
  fast: string
  fallback: string
}

export interface QQBotConfig {
  enabled: boolean
  appId: string
  appSecret: string
  intents: number
  sandbox: boolean
}

export interface L1AgentSettings {
  builtinMcpServers?: string[]
  externalMcpServers?: string[]
}

export interface AppConfig {
  session: SessionConfig
  log: LogConfig
  tools: ToolsConfig
  providers: LLMProvider[]
  models: LLMModel[]
  embedding: EmbeddingConfig
  defaultModels: DefaultModelsConfig
  qqbot: QQBotConfig
  agent: L1AgentSettings
  lspmcp: LSPMCPConfig
}

// ─── Tool & Skill Types ────────────────────────────────────────────────────

export interface ToolInfo {
  name: string
  description: string
  parameters: Record<string, unknown> | null
}

export interface ToolListResponse {
  tools: ToolInfo[]
  total: number
}

export type SkillCategory = 'builtin' | 'user'

export interface SkillInfo {
  id: string
  name: string
  description: string
  when_to_use: string
  category: SkillCategory
  user_invocable: boolean
  disable_model_invocation: boolean
  context: string
  agent: string
  file_path: string
  allowed_tools: string[]
  triggers?: string[]
  enabled?: boolean
  body?: string
}

export interface SkillListResponse {
  skills: SkillInfo[]
  total: number
}

// ─── MCP Types ─────────────────────────────────────────────────────────────────

export interface MCPServerConfig {
  name: string
  command: string
  args: string[]
  env?: Record<string, string>
  transport: string
  enabled: boolean
}

export interface MCPServerWire {
  command: string
  args: string[]
  env?: Record<string, string>
  transport?: string
  enabled?: boolean
}

export interface MCPConfig {
  mcpServers: Record<string, MCPServerWire>
}

// ─── File Types ─────────────────────────────────────────────────────────────────

export interface FileInfo {
  name: string
  path: string
  size: number
  isDir: boolean
  ext: string
  modTime: string
}

export interface FileRoot {
  label: string
  path: string
  group: string
}

// ─── Team & Agent CRUD Types (DB-backed) ────────────────────────────────────

export interface TeamWorkspace {
  name: string
  path: string
  autoWork?: {
    enabled: boolean
    initialCooldownMinutes: number
    postTaskCooldownMinutes: number
    maxIntervalsPerDay: number
  }
}

export interface TeamResponse {
  id: string
  name: string
  description: string
  workspaces: TeamWorkspace[]
  projects?: string[]
  agents?: AgentResponse[]
  created_at: string
  updated_at: string
}

export interface AgentResponse {
  id: string
  name: string
  description: string
  team_name: string
  is_leader: boolean
  model: string
  system_prompt: string
  permission: boolean
  mcp_servers: string[]
  skill_ids: string[]
  created_at: string
  updated_at: string
}

export interface CreateTeamRequest {
  name: string
  description?: string
  workspaces?: TeamWorkspace[]
  projects?: string[]
}

export interface UpdateTeamRequest {
  description?: string
  workspaces?: TeamWorkspace[]
  projects?: string[]
}

export interface Project {
  id: string
  name: string
  path: string
  description: string
  created_at: string
  updated_at: string
}

export interface CreateAgentRequest {
  name: string
  description?: string
  team_name: string
  is_leader?: boolean
  model?: string
  system_prompt?: string
  permission?: boolean
  mcp_servers?: string[]
  skill_ids?: string[]
}

export interface UpdateAgentRequest {
  description?: string
  team_name?: string
  is_leader?: boolean
  model?: string
  system_prompt?: string
  permission?: boolean
  mcp_servers?: string[]
  skill_ids?: string[]
}

// ─── Cron/Timer Task Types ──────────────────────────────────────────────────

export interface CronTask {
  id: string
  expression: string
  instruction: string
  target_agent: string
  status: 'active' | 'paused' | 'completed'
  last_run_at?: string
  next_run_at: string
  created_at: string
  updated_at: string
}

export interface CreateCronTaskRequest {
  expression: string
  instruction: string
  target_agent?: string
}

export interface UpdateCronTaskRequest {
  expression?: string
  instruction?: string
  target_agent?: string
  status?: 'active' | 'paused'
}

export interface LSPMCPEntry {
  id: string
  command: string
  args: string[]
  languages: string[]
  extensions: string[]
  disabled: boolean
}

export interface LSPMCPConfig {
  servers: LSPMCPEntry[]
}
