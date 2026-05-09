import type {
  Plan,
  PlanListResponse,
  PlanStatus,
  TodoItemWithDeps,
  AgentProfile,
  TeamListResponse,
  AppConfig,
} from '@/types';

const API_BASE = 'http://localhost:8765/api';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    console.error('API error:', err);
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json();
}

// ─── Plan APIs ────────────────────────────────────────────────────────────────

export async function listPlans(): Promise<Plan[]> {
  const data = await request<PlanListResponse>('/plans');
  return data.plans ?? [];
}

export async function getPlan(id: string): Promise<Plan> {
  return request<Plan>(`/plans/${id}`);
}

export async function updatePlanStatus(id: string, status: PlanStatus): Promise<Plan> {
  return request<Plan>(`/plans/${id}/status`, {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  });
}

// ─── Todo APIs ────────────────────────────────────────────────────────────────

export async function toggleTodo(planId: string, todoId: string): Promise<TodoItemWithDeps> {
  return request<TodoItemWithDeps>(`/plans/${planId}/todos/${todoId}/toggle`, {
    method: 'PATCH',
  });
}

export async function deleteTodo(planId: string, todoId: string): Promise<void> {
  await request(`/plans/${planId}/todos/${todoId}`, { method: 'DELETE' });
}

// ─── Agent APIs ───────────────────────────────────────────────────────────────

export async function getAgentProfile(id: string): Promise<AgentProfile> {
  return request<AgentProfile>(`/agents/${id}/profile`);
}

export async function getTeams(): Promise<TeamListResponse> {
  return request<TeamListResponse>('/teams');
}

// ─── Config APIs ──────────────────────────────────────────────────────────────

export async function getConfig(): Promise<AppConfig> {
  return request<AppConfig>('/config');
}

export async function updateConfig(patch: Partial<AppConfig>): Promise<AppConfig> {
  return request<AppConfig>('/config', { method: 'PATCH', body: JSON.stringify(patch) });
}
