// ─── Agent Types ─────────────────────────────────────────────────────────────

export type AgentState = "idle" | "processing" | "stopping" | "stopped";

export interface AgentInfo {
  id: string;
  instance_id: string;
  name: string;
  state: AgentState;
  model_id: string;
  provider_id: string;
  group: string;
  is_leader: boolean;
  task_level: string;
  thinking_enabled?: boolean;
  reasoning_effort?: string;
  level_locked?: boolean;
  last_level?: string;
  error_count: number;
  last_error: string;
  pending_delegations: number;
  mailbox_high: number;
  mailbox_normal: number;
  is_qbot?: boolean;
  iteration?: number;
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
  channels?: Record<string, string>;
  notify_channel?: string;
}

export interface AgentConfig {
  raw_config: string;
  system_prompt: string;
  name: string;
  description: string;
  model: string;
  group: string;
  is_leader: boolean;
  mcp_servers: string[];
}

export interface UpdateAgentProfileRequest {
  soul?: string;
  rules?: string;
  channels?: Record<string, string> | null;
  notify_channel?: string | null;
}

export interface UpdateAgentConfigRequest {
  raw_config?: string;
  system_prompt?: string;
  channels?: Record<string, string> | null;
  notify_channel?: string | null;
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

// ─── Auth Types ───────────────────────────────────────────────────────────────

export interface LoginRequest {
  user: string;
  password: string;
}

export interface LoginResponse {
  token: string;
  user: string;
}

export interface AuthCheckResponse {
  authenticated: boolean;
  user?: string;
}

// ─── Runtime Types ───────────────────────────────────────────────────────────

export type Segment =
  | { type: "thinking"; text: string }
  | { type: "content"; text: string }
  | {
      type: "tool_call";
      call_id: string;
      name: string;
      args: string;
      result: string;
      error: string;
      done: boolean;
      duration_ms: number;
    };

export interface AgentStreamState {
  agent_id: string;
  processing: boolean;
  segments: Segment[];
  iteration: number;
  error?: string;
}

export interface RuntimeStatus {
  phase: string;
  prompt_tokens: number;
  output_tokens: number;
  cache_hit_tokens: number;
  cache_miss_tokens: number;
  context_pct: number;
  current_tokens: number;
  max_tokens: number;
  current_iter: number;
  content_deltas: number;
  active_delegations: number;
  total_agents: number;
  running_agents: number;
  idle_agents: number;
  total_errors: number;
  http_addr: string;
  agent_streams: Record<string, AgentStreamState>;
}

// ─── Dependency Types ─────────────────────────────────────────────────────────

export interface DependenciesResponse {
  todo_id: string;
  depends_on: string[];
  blockers: string[];
}

export interface SetDependenciesRequest {
  depends_on: string[];
}

// ─── Team & Agent CRUD Types (DB-backed) ────────────────────────────────────

export interface TeamWorkspace {
  name: string;
  path: string;
  autoWork?: {
    enabled: boolean;
    initialCooldownMinutes: number;
    postTaskCooldownMinutes: number;
    maxIntervalsPerDay: number;
  };
}

export interface TeamResponse {
  id: string;
  name: string;
  description: string;
  workspaces: TeamWorkspace[];
  projects?: string[];
  agents?: AgentResponse[];
  created_at: string;
  updated_at: string;
}

export interface AgentResponse {
  id: string;
  name: string;
  description: string;
  team_name: string;
  is_leader: boolean;
  model: string;
  system_prompt: string;
  permission: boolean;
  mcp_servers: string[];
  skill_ids: string[];
  channels?: Record<string, string>;
  notify_channel?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateTeamRequest {
  name: string;
  description?: string;
  workspaces?: TeamWorkspace[];
  projects?: string[];
}

export interface UpdateTeamRequest {
  description?: string;
  workspaces?: TeamWorkspace[];
  projects?: string[];
}

export interface Project {
  id: string;
  name: string;
  path: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface CreateAgentRequest {
  name: string;
  description?: string;
  team_name: string;
  is_leader?: boolean;
  model?: string;
  system_prompt?: string;
  permission?: boolean;
  mcp_servers?: string[];
  skill_ids?: string[];
  channels?: Record<string, string>;
  notify_channel?: string;
}

export interface UpdateAgentRequest {
  description?: string;
  team_name?: string;
  is_leader?: boolean;
  model?: string;
  system_prompt?: string;
  permission?: boolean;
  mcp_servers?: string[];
  skill_ids?: string[];
  channels?: Record<string, string> | null;
  notify_channel?: string | null;
}
