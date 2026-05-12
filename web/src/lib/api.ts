import type {
  Plan,
  PlanListResponse,
  PlanStatus,
  TodoItemWithDeps,
  AgentProfile,
  AgentConfig,
  UpdateAgentProfileRequest,
  UpdateAgentConfigRequest,
  TeamListResponse,
  AppConfig,
  ToolListResponse,
  SkillListResponse,
  MCPConfig,
  FileInfo,
  FileRoot,
} from '@/types'

const API_BASE = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
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

export async function updateConfig(patch: Partial<AppConfig>): Promise<AppConfig> {
  return request<AppConfig>('/config', { method: 'PATCH', body: JSON.stringify(patch) })
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
  const res = await fetch(`${API_BASE}/files/list?dir=${encodeURIComponent(dir)}`, {
    headers: { 'Content-Type': 'application/json' },
  })
  if (!res.ok) throw new Error(`Failed to list files: ${res.statusText}`)
  return res.json()
}

export async function getFileRoots(): Promise<FileRoot[]> {
  const res = await fetch(`${API_BASE}/files/roots`, {
    headers: { 'Content-Type': 'application/json' },
  })
  if (!res.ok) throw new Error(`Failed to fetch roots: ${res.statusText}`)
  return res.json()
}
