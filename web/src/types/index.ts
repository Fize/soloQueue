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
  provider_id: string
  group: string
  is_leader: boolean
  task_level: string
  thinking_enabled?: boolean
  reasoning_effort?: string
  level_locked?: boolean
  last_level?: string
  error_count: number
  last_error: string
  pending_delegations: number
  mailbox_high: number
  mailbox_normal: number
  is_qbot?: boolean
  iteration?: number
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

// ─── Simulation Types ────────────────────────────────────────────────────────

export interface SimulationPersona {
  id: string
  name: string
  role: string
  traits: Record<string, string>
  system_prompt: string
  goals?: string[]
  mbti?: string
  age?: number
  gender?: string
  country?: string
  profession?: string
  bio?: string
  persona?: string
  model_id?: string
  provider_id?: string
}

export interface SimulationConfig {
  id?: string
  topic: string
  personas: SimulationPersona[]
  max_wall_clock_ms?: number
  simulated_hours?: number
  tick_interval_ms?: number
  time_scale?: number
  enable_reflection?: boolean
}

export interface SimulationMessage {
  agent_id: string
  agent_name: string
  content: string
  reasoning?: string
  to: string
  type: string
  round: number
  seq_num: number
}

export interface SimulationRelationEdge {
  source: string
  target: string
  type: string
  weight: number
}

export interface SimulationRelationGraph {
  nodes: string[]
  edges: SimulationRelationEdge[]
}

export interface SimulationState {
  id: string
  status: 'pending' | 'idle' | 'running' | 'completed' | 'failed'
  config: SimulationConfig
  current_round: number
  messages: SimulationMessage[]
  report?: string
  graph?: SimulationRelationGraph
  started_at?: string
  completed_at?: string
  error?: string
}

export interface SimulationEvent {
  type: string
  simulation_id: string
  round: number
  data?: any
  error?: string
  timestamp: string
}

export interface AgentProgressState {
  persona_id: string
  name: string
  role: string
  message_count: number
  last_action_type: string
  last_action_time: string
  status: 'thinking' | 'spoke' | 'idle'
}

export interface GraphEdgeDTO {
  source: string
  target: string
  type: string
  weight: number
}

export interface SimulationProgress {
  simulation_id: string
  phase: 'initializing' | 'running' | 'generating_report' | 'completed' | 'failed'
  progress_percent: number
  current_actions: number
  elapsed_seconds: number
  estimated_remaining_seconds: number
  agent_states: Record<string, AgentProgressState>
  graph_edges: GraphEdgeDTO[]
  recent_logs: string[]
}

// ─── WebSocket Message Types ────────────────────────────────────────────────

export interface WSStateMessage {
  type: 'state'
  runtime: RuntimeStatus
  agents: AgentListResponse
}

export interface WSSimulationEventMessage {
  type: 'simulation_event'
  event: SimulationEvent
}

export interface WSSimulationProgressMessage {
  type: 'simulation_progress'
  progress: SimulationProgress
}

// Chat streaming messages (server → client)
export interface WSChatChunk {
  type: 'chat_chunk'
  request_id: string
  delta: string
}

export interface WSReasoningChunk {
  type: 'reasoning_chunk'
  request_id: string
  delta: string
}

export interface WSToolStart {
  type: 'tool_start'
  request_id: string
  call_id: string
  name: string
  args: string
}

export interface WSToolDone {
  type: 'tool_done'
  request_id: string
  call_id: string
  name: string
  result: string
  error: string
  duration_ms: number
}

export interface WSToolConfirm {
  type: 'tool_confirm'
  request_id: string
  call_id: string
  name: string
  prompt: string
  allow_in_session: boolean
}

export interface WSChatDone {
  type: 'chat_done'
  request_id: string
  content: string
  reasoning_content: string
}

export interface WSChatError {
  type: 'chat_error'
  request_id: string
  error: string
}

export interface WSDelegationStart {
  type: 'delegation_start'
  request_id: string
  num_tasks: number
}

export interface WSDelegationDone {
  type: 'delegation_done'
  request_id: string
  target_agent_id: string
  agent_name?: string
  duration_ms?: number
  result_content?: string
}

export interface WSSessionName {
  type: 'session_name'
  request_id: string
  name: string
}

export interface WSConnected {
  type: 'connected'
}

export interface WSPong {
  type: 'pong'
}

export type WSMessage =
  | WSStateMessage
  | WSSimulationEventMessage
  | WSSimulationProgressMessage
  | WSChatChunk
  | WSReasoningChunk
  | WSToolStart
  | WSToolDone
  | WSToolConfirm
  | WSChatDone
  | WSChatError
  | WSDelegationStart
  | WSDelegationDone
  | WSSessionName
  | WSConnected
  | WSPong

// Client → server messages
export interface ClientChatSend {
  type: 'chat_send'
  request_id: string
  session_id: string
  prompt: string
  files?: { name: string; path: string }[]
}

export interface ClientChatCancel {
  type: 'chat_cancel'
  request_id: string
  session_id: string
}

export interface ClientToolConfirm {
  type: 'tool_confirm'
  call_id: string
  choice: string
  session_id: string
}

export type ClientMessage = ClientChatSend | ClientChatCancel | ClientToolConfirm

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
  provider: string // "none", "openai"
  modelName: string // model name for API
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

export interface SimulationConfig {
  defaultModelId?: string
  defaultProviderId?: string
  dbPath?: string
  defaultMaxWallClockMs?: number
  enableReflection?: boolean
  simulatedHours?: number
  tickIntervalMs?: number
  timeScale?: number
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
  simulation: SimulationConfig
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
  required_env?: string[]
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

// ─── Chat Types ────────────────────────────────────────────────────────────

export interface ChatSession {
  id: string // "l1" or "l2:<uuid>"
  type: 'l1' | 'l2'
  name: string
  group?: string
  agent_name?: string
  agent_instance_id?: string
  project_path?: string
  createdAt: string
  ctxwin_used?: number
  ctxwin_limit?: number
}

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  segments: ChatSegment[]
  timestamp: string
  files?: { name: string; path: string }[]
}

export type ChatSegment =
  | { type: 'thinking'; text: string }
  | { type: 'content'; text: string }
  | {
      type: 'tool_call'
      callId: string
      name: string
      args: string
      result?: string
      error?: string
      durationMs?: number
      done: boolean
    }
  | {
      type: 'delegation'
      agentName: string
      task: string
      status: 'running' | 'completed' | 'failed'
      durationMs?: number
      resultContent?: string
    }
  | { type: 'error'; text: string }
  | {
      type: 'tool_confirm'
      callId: string
      name: string
      prompt: string
      allowInSession: boolean
      resolved: boolean
      choice?: string
    }

export interface SessionListResponse {
  sessions: ChatSession[]
}

export interface CreateL2SessionResponse {
  id: string
  name: string
  group: string
  agent_name: string
  created_at: string
}

export interface SessionHistorySegment {
  type: 'content' | 'thinking' | 'tool_call' | 'delegation' | 'error' | 'tool_confirm'
  text?: string
  call_id?: string
  name?: string
  args?: string
  result?: string
  error?: string
  duration_ms?: number
  done?: boolean
  status?: string
  agent_name?: string
  task?: string
  prompt?: string
  allow_in_session?: boolean
  resolved?: boolean
  choice?: string
}

export interface SessionHistoryMessage {
  id: string
  role: 'user' | 'assistant'
  segments: SessionHistorySegment[]
  timestamp: string
}

export interface SessionHistoryResponse {
  messages: SessionHistoryMessage[]
  has_more: boolean
  cursor?: string
}
