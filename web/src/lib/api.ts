import type {
  Plan,
  PlanListResponse,
  PlanStatus,
  CreatePlanRequest,
  UpdatePlanRequest,
  TodoItemWithDeps,
  AgentProfile,
  AgentConfig,
  UpdateAgentProfileRequest,
  UpdateAgentConfigRequest,
  TeamListResponse,
  TeamResponse,
  AgentResponse,
  CreateTeamRequest,
  UpdateTeamRequest,
  CreateAgentRequest,
  UpdateAgentRequest,
  AppConfig,
  ToolListResponse,
  SkillInfo,
  SkillListResponse,
  MCPConfig,
  FileInfo,
  FileRoot,
  DependenciesResponse,
  SetDependenciesRequest,
  CronTask,
  CreateCronTaskRequest,
  UpdateCronTaskRequest,
  LLMProvider,
  LLMModel,
  DefaultModelsConfig,
} from '@/types'
import { useAuthStore } from '@/stores/authStore'

const API_BASE = '/api'

function getAuthHeaders(): Record<string, string> {
  return {}
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...getAuthHeaders(),
  }
  const res = await fetch(`${API_BASE}${path}`, {
    headers,
    ...options,
  })
  if (res.status === 401) {
    useAuthStore.getState().logout()
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    console.error('API error:', err)
    throw new Error(err.error || `HTTP ${res.status}`)
  }
  return res.json()
}

// ─── Plan APIs ────────────────────────────────────────────────────────────────

export async function listPlans(): Promise<Plan[]> {
  const data = await request<PlanListResponse>('/plans')
  return data.plans ?? []
}

export async function getPlan(id: string): Promise<Plan> {
  return request<Plan>(`/plans/${id}`)
}

export async function updatePlanStatus(id: string, status: PlanStatus): Promise<Plan> {
  return request<Plan>(`/plans/${id}/status`, {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}

export async function createPlan(data: CreatePlanRequest): Promise<Plan> {
  return request<Plan>('/plans', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updatePlan(id: string, data: UpdatePlanRequest): Promise<Plan> {
  return request<Plan>(`/plans/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deletePlan(id: string): Promise<void> {
  await request(`/plans/${id}`, { method: 'DELETE' })
}

// ─── Todo APIs ────────────────────────────────────────────────────────────────

export async function toggleTodo(planId: string, todoId: string): Promise<TodoItemWithDeps> {
  return request<TodoItemWithDeps>(`/plans/${planId}/todos/${todoId}/toggle`, {
    method: 'PATCH',
  })
}

export async function deleteTodo(planId: string, todoId: string): Promise<void> {
  await request(`/plans/${planId}/todos/${todoId}`, { method: 'DELETE' })
}

// ─── Agent APIs ───────────────────────────────────────────────────────────────

export async function getAgentProfile(id: string): Promise<AgentProfile> {
  return request<AgentProfile>(`/agents/${id}/profile`)
}

export async function updateAgentProfile(
  id: string,
  data: UpdateAgentProfileRequest
): Promise<AgentProfile> {
  return request<AgentProfile>(`/agents/${id}/profile`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function getAgentConfig(id: string): Promise<AgentConfig> {
  return request<AgentConfig>(`/agents/${id}/config`)
}

export async function updateAgentConfig(
  id: string,
  data: UpdateAgentConfigRequest
): Promise<AgentConfig> {
  return request<AgentConfig>(`/agents/${id}/config`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function getTeams(): Promise<TeamListResponse> {
  return request<TeamListResponse>('/teams')
}

// ─── Config APIs ──────────────────────────────────────────────────────────────

export async function getConfig(): Promise<AppConfig> {
  return request<AppConfig>('/config')
}

// ─── DB-backed Config APIs ──────────────────────────────────────────────────

export async function listProviders(): Promise<LLMProvider[]> {
  return request<LLMProvider[]>('/config/providers')
}

export async function createProvider(data: LLMProvider): Promise<LLMProvider> {
  return request<LLMProvider>('/config/providers', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateProvider(id: string, data: LLMProvider): Promise<LLMProvider> {
  return request<LLMProvider>(`/config/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteProvider(id: string): Promise<void> {
  await request(`/config/providers/${id}`, { method: 'DELETE' })
}

export async function listModels(): Promise<LLMModel[]> {
  return request<LLMModel[]>('/config/models')
}

export async function createModel(data: LLMModel): Promise<LLMModel> {
  return request<LLMModel>('/config/models', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateModel(id: string, data: LLMModel): Promise<LLMModel> {
  return request<LLMModel>(`/config/models/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteModel(id: string): Promise<void> {
  await request(`/config/models/${id}`, { method: 'DELETE' })
}

export async function getDefaultModels(): Promise<DefaultModelsConfig> {
  return request<DefaultModelsConfig>('/config/default-models')
}

export async function updateDefaultModels(data: DefaultModelsConfig): Promise<DefaultModelsConfig> {
  return request<DefaultModelsConfig>('/config/default-models', {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function getConfigToml(): Promise<string> {
  const res = await fetch(`${API_BASE}/config/toml`, {
    headers: getAuthHeaders(),
  })
  if (res.status === 401) {
    useAuthStore.getState().logout()
    throw new Error('Unauthorized')
  }
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || `HTTP ${res.status}`)
  }
  return res.text()
}

// ─── Tools & Skills APIs ────────────────────────────────────────────────────

export async function getTools(): Promise<ToolListResponse> {
  return request<ToolListResponse>('/tools')
}

export async function getSkills(): Promise<SkillListResponse> {
  return request<SkillListResponse>('/skills')
}

// ─── MCP APIs ──────────────────────────────────────────────────────────────────

export async function getMCPConfig(): Promise<MCPConfig> {
  return request<MCPConfig>('/mcp')
}

export async function updateMCPConfig(config: MCPConfig): Promise<MCPConfig> {
  return request<MCPConfig>('/mcp', { method: 'PATCH', body: JSON.stringify(config) })
}

// ─── File APIs ──────────────────────────────────────────────────────────────────

export function getFileUrl(path: string): string {
  return `${API_BASE}/files/content?path=${encodeURIComponent(path)}`
}

export async function listFiles(dir: string): Promise<FileInfo[]> {
  const headers = {
    'Content-Type': 'application/json',
    ...getAuthHeaders(),
  }
  const res = await fetch(`${API_BASE}/files/list?dir=${encodeURIComponent(dir)}`, { headers })
  if (res.status === 401) {
    useAuthStore.getState().logout()
    throw new Error('Unauthorized')
  }
  if (!res.ok) throw new Error(`Failed to list files: ${res.statusText}`)
  return res.json()
}

export async function getFileRoots(): Promise<FileRoot[]> {
  const headers = {
    'Content-Type': 'application/json',
    ...getAuthHeaders(),
  }
  const res = await fetch(`${API_BASE}/files/roots`, { headers })
  if (res.status === 401) {
    useAuthStore.getState().logout()
    throw new Error('Unauthorized')
  }
  if (!res.ok) throw new Error(`Failed to fetch roots: ${res.statusText}`)
  return res.json()
}

// ─── Dependency APIs ───────────────────────────────────────────────────────────

export async function getDependencies(todoId: string): Promise<DependenciesResponse> {
  return request<DependenciesResponse>(`/todos/${todoId}/dependencies`)
}

export async function setDependencies(todoId: string, data: SetDependenciesRequest): Promise<void> {
  await request(`/todos/${todoId}/dependencies`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// ─── Team CRUD APIs ─────────────────────────────────────────────────────────

export async function listTeams(): Promise<TeamResponse[]> {
  const data = await request<{ teams: TeamResponse[] }>('/teams')
  return data.teams ?? []
}

export async function getTeam(name: string): Promise<TeamResponse> {
  return request<TeamResponse>(`/teams/${encodeURIComponent(name)}`)
}

export async function createTeam(data: CreateTeamRequest): Promise<TeamResponse> {
  return request<TeamResponse>('/teams', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateTeam(name: string, data: UpdateTeamRequest): Promise<TeamResponse> {
  return request<TeamResponse>(`/teams/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteTeam(name: string): Promise<void> {
  await request(`/teams/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

// ─── Agent CRUD APIs ────────────────────────────────────────────────────────

export async function listAgents(team?: string): Promise<AgentResponse[]> {
  const query = team ? `?team=${encodeURIComponent(team)}` : ''
  const data = await request<{ agents: AgentResponse[] }>(`/agents${query}`)
  return data.agents ?? []
}

export async function getAgent(name: string): Promise<AgentResponse> {
  return request<AgentResponse>(`/agents/${encodeURIComponent(name)}`)
}

export async function createAgent(data: CreateAgentRequest): Promise<AgentResponse> {
  return request<AgentResponse>('/agents', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateAgent(name: string, data: UpdateAgentRequest): Promise<AgentResponse> {
  return request<AgentResponse>(`/agents/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteAgent(name: string): Promise<void> {
  await request(`/agents/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

// ─── Cron Task APIs ──────────────────────────────────────────────────────────

export async function listCronTasks(): Promise<CronTask[]> {
  return request<CronTask[]>('/cron')
}

export async function createCronTask(data: CreateCronTaskRequest): Promise<CronTask> {
  return request<CronTask>('/cron', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateCronTask(id: string, data: UpdateCronTaskRequest): Promise<CronTask> {
  return request<CronTask>(`/cron/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteCronTask(id: string): Promise<void> {
  await request(`/cron/${id}`, { method: 'DELETE' })
}

// ─── Skill Management & Store APIs ──────────────────────────────────────────

export interface InstallSkillRequest {
  source: 'store' | 'local' | 'github'
  id?: string
  path?: string
  url?: string
}

export interface SkillFileEntry {
  path: string
  kind: 'file' | 'directory'
  size?: number
}

export async function fetchStoreSkills(): Promise<SkillListResponse> {
  return request<SkillListResponse>('/skills/store')
}

export async function installSkill(data: InstallSkillRequest): Promise<void> {
  await request('/skills/install', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function fetchSkillDetail(id: string): Promise<SkillInfo> {
  return request<SkillInfo>(`/skills/${encodeURIComponent(id)}`)
}

export async function updateSkill(
  id: string,
  data: { description: string; body: string; triggers: string[] }
): Promise<void> {
  await request(`/skills/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteSkill(id: string): Promise<void> {
  await request(`/skills/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function fetchSkillFiles(id: string): Promise<{ files: SkillFileEntry[] }> {
  return request<{ files: SkillFileEntry[] }>(`/skills/${encodeURIComponent(id)}/files`)
}

export async function toggleSkill(id: string): Promise<{ id: string; enabled: boolean }> {
  return request<{ id: string; enabled: boolean }>(`/skills/${encodeURIComponent(id)}/toggle`, {
    method: 'POST',
  })
}

export async function importSkill(data: {
  name: string
  description: string
  body: string
  triggers: string[]
}): Promise<void> {
  await request('/skills', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}
