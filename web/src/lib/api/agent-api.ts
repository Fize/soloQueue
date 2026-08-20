import type {
  AgentProfile,
  AgentConfig,
  UpdateAgentProfileRequest,
  UpdateAgentConfigRequest,
  TeamListResponse,
  TeamResponse,
  AgentResponse,
  AgentListResponse,
  CreateTeamRequest,
  UpdateTeamRequest,
  CreateAgentRequest,
  UpdateAgentRequest,
  BuiltinTeamCatalogItem,
  InstallBuiltinTeamsResponse,
  Project,
  CronTask,
  CreateCronTaskRequest,
  UpdateCronTaskRequest,
  CronExecutionRecord,
  CronHistoryDetail,
  GlobalRuleFile,
  GlobalRuleContent,
} from "@/types";
import { request } from "./core";

// ─── Agent APIs ───────────────────────────────────────────────────────────────

export async function getAgentProfile(id: string): Promise<AgentProfile> {
  return request<AgentProfile>(`/agents/${id}/profile`);
}

export async function updateAgentProfile(
  id: string,
  data: UpdateAgentProfileRequest,
): Promise<AgentProfile> {
  return request<AgentProfile>(`/agents/${id}/profile`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getAgentConfig(id: string): Promise<AgentConfig> {
  return request<AgentConfig>(`/agents/${id}/config`);
}

export async function updateAgentConfig(
  id: string,
  data: UpdateAgentConfigRequest,
): Promise<AgentConfig> {
  return request<AgentConfig>(`/agents/${id}/config`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getTeams(): Promise<TeamListResponse> {
  return request<TeamListResponse>("/teams");
}

export async function getLiveAgents(): Promise<AgentListResponse> {
  return request<AgentListResponse>("/agents/live");
}

export async function listGlobalRules(): Promise<GlobalRuleFile[]> {
  return request<GlobalRuleFile[]>('/agents/l1/global-rules');
}

export async function getGlobalRule(filename: string): Promise<GlobalRuleContent> {
  return request<GlobalRuleContent>(`/agents/l1/global-rules/${encodeURIComponent(filename)}`);
}

export async function saveGlobalRule(filename: string, content: string): Promise<GlobalRuleFile> {
  return request<GlobalRuleFile>(`/agents/l1/global-rules/${encodeURIComponent(filename)}`, {
    method: 'PUT',
    body: JSON.stringify({ content }),
  });
}

export async function deleteGlobalRule(filename: string): Promise<void> {
  await request<void>(`/agents/l1/global-rules/${encodeURIComponent(filename)}`, {
    method: 'DELETE',
  });
}

// ─── Team CRUD APIs ─────────────────────────────────────────────────────────

export async function listTeams(): Promise<TeamResponse[]> {
  const data = await request<{ teams: TeamResponse[] }>("/teams");
  return data.teams ?? [];
}

export async function getTeam(name: string): Promise<TeamResponse> {
  return request<TeamResponse>(`/teams/${encodeURIComponent(name)}`);
}

export async function createTeam(
  data: CreateTeamRequest,
): Promise<TeamResponse> {
  return request<TeamResponse>("/teams", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateTeam(
  name: string,
  data: UpdateTeamRequest,
): Promise<TeamResponse> {
  return request<TeamResponse>(`/teams/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteTeam(name: string): Promise<void> {
  await request(`/teams/${encodeURIComponent(name)}`, { method: "DELETE" });
}

export async function listBuiltinTeams(): Promise<BuiltinTeamCatalogItem[]> {
  const data = await request<{ teams: BuiltinTeamCatalogItem[] }>("/builtin-teams");
  return data.teams ?? [];
}

export async function installBuiltinTeams(
  teamIds: string[],
): Promise<InstallBuiltinTeamsResponse> {
  return request<InstallBuiltinTeamsResponse>("/builtin-teams/install", {
    method: "POST",
    body: JSON.stringify({ team_ids: teamIds }),
  });
}

// ─── Agent CRUD APIs ────────────────────────────────────────────────────────

export async function listAgents(team?: string): Promise<AgentResponse[]> {
  const query = team ? `?team=${encodeURIComponent(team)}` : "";
  const data = await request<{ agents: AgentResponse[] }>(`/agents${query}`);
  return data.agents ?? [];
}

export async function getAgent(name: string): Promise<AgentResponse> {
  return request<AgentResponse>(`/agents/${encodeURIComponent(name)}`);
}

export async function createAgent(
  data: CreateAgentRequest,
): Promise<AgentResponse> {
  return request<AgentResponse>("/agents", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateAgent(
  name: string,
  data: UpdateAgentRequest,
): Promise<AgentResponse> {
  return request<AgentResponse>(`/agents/${encodeURIComponent(name)}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteAgent(name: string): Promise<void> {
  await request(`/agents/${encodeURIComponent(name)}`, { method: "DELETE" });
}

// ─── Project APIs ───────────────────────────────────────────────────────────

export async function listProjects(): Promise<Project[]> {
  const data = await request<{ projects: Project[] }>("/projects");
  return data.projects ?? [];
}

export async function getProject(id: string): Promise<Project> {
  return request<Project>(`/projects/${encodeURIComponent(id)}`);
}

export async function createProject(
  data: Omit<Project, "created_at" | "updated_at">,
): Promise<Project> {
  return request<Project>("/projects", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateProject(
  id: string,
  data: Partial<Omit<Project, "created_at" | "updated_at">>,
): Promise<Project> {
  return request<Project>(`/projects/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteProject(id: string): Promise<void> {
  await request(`/projects/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function getProjectBranches(projectId: string): Promise<string[]> {
  const data = await request<{ branches: string[] }>(
    `/projects/${encodeURIComponent(projectId)}/branches`,
  );
  return data.branches ?? [];
}

// ─── Cron Task APIs ──────────────────────────────────────────────────────────

export async function listCronTasks(): Promise<CronTask[]> {
  return request<CronTask[]>("/cron");
}

export async function createCronTask(
  data: CreateCronTaskRequest,
): Promise<CronTask> {
  return request<CronTask>("/cron", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateCronTask(
  id: string,
  data: UpdateCronTaskRequest,
): Promise<CronTask> {
  return request<CronTask>(`/cron/${id}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteCronTask(id: string): Promise<void> {
  await request(`/cron/${id}`, { method: "DELETE" });
}

// ─── Cron Execution History APIs ────────────────────────────────────────────

export async function listCronHistory(
  taskId: string,
  opts?: { limit?: number; offset?: number },
): Promise<CronExecutionRecord[]> {
  const params = new URLSearchParams();
  if (opts?.limit) params.set("limit", String(opts.limit));
  if (opts?.offset) params.set("offset", String(opts.offset));
  const qs = params.toString();
  return request<CronExecutionRecord[]>(`/cron/${taskId}/history${qs ? `?${qs}` : ""}`);
}

export async function getCronHistory(
  taskId: string,
  execId: string,
): Promise<CronHistoryDetail> {
  return request<CronHistoryDetail>(`/cron/${taskId}/history/${execId}`);
}
