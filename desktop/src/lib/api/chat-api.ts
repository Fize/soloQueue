import type {
  FileInfo,
  DependenciesResponse,
  SetDependenciesRequest,
  SessionListResponse,
  CreateL2SessionResponse,
  SessionHistoryResponse,
  ChangesResponse,
} from "@/types";
import { request, API_BASE } from "./core";
import { useConnectionStore } from "@/stores/connectionStore";

// ─── File APIs ──────────────────────────────────────────────────────────────────

export async function listFiles(dir: string): Promise<FileInfo[]> {
  return request<FileInfo[]>(`/files/list?dir=${encodeURIComponent(dir)}`);
}

export async function toggleFileCheckbox(
  path: string,
  index: number,
): Promise<{ status: string }> {
  return request<{ status: string }>("/files/toggle-checkbox", {
    method: "POST",
    body: JSON.stringify({ path, index }),
  });
}

export async function saveFile(path: string, content: string): Promise<void> {
  await request<void>("/files/save", {
    method: "POST",
    body: JSON.stringify({ path, content }),
  });
}

// ─── Dependency APIs ───────────────────────────────────────────────────────────

export async function getDependencies(
  todoId: string,
): Promise<DependenciesResponse> {
  return request<DependenciesResponse>(`/todos/${todoId}/dependencies`);
}

export async function setDependencies(
  todoId: string,
  data: SetDependenciesRequest,
): Promise<void> {
  await request(`/todos/${todoId}/dependencies`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

// ─── Session / Chat APIs ────────────────────────────────────────────────────

export async function listSessions(): Promise<SessionListResponse> {
  return request<SessionListResponse>("/session/list");
}

export async function createL2Session(
  group: string,
  workDir?: string,
): Promise<CreateL2SessionResponse> {
  return request<CreateL2SessionResponse>("/session/l2", {
    method: "POST",
    body: JSON.stringify({ group, work_dir: workDir || "" }),
  });
}

export async function deleteL2Session(id: string): Promise<void> {
  await request(`/session/l2/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function listL2Groups(): Promise<string[]> {
  const data = await request<{ groups: string[] }>("/session/groups");
  return data.groups ?? [];
}

export async function fetchSessionHistory(
  sessionId: string,
  before?: string,
  limit?: number,
): Promise<SessionHistoryResponse> {
  const cleanId = sessionId.replace(/^l2:/, "");
  const params = new URLSearchParams({ session_id: cleanId });
  if (before) params.set("before", before);
  if (limit) params.set("limit", String(limit));
  return request<SessionHistoryResponse>(
    `/session/history?${params.toString()}`,
  );
}

export async function confirmSessionTool(
  sessionId: string,
  callId: string,
  choice: string,
): Promise<void> {
  await request("/session/confirm", {
    method: "POST",
    body: JSON.stringify({ session_id: sessionId, call_id: callId, choice }),
  });
}

export async function uploadFile(
  file: File,
  sessionId?: string,
): Promise<{ name: string; path: string; size: number }> {
  const formData = new FormData();
  formData.append("file", file);
  if (sessionId) {
    formData.append("session_id", sessionId);
  }

  const headers = {
    ...useConnectionStore.getState().getAuthHeaders(),
  };

  const res = await fetch(`${API_BASE}/session/upload`, {
    method: "POST",
    headers,
    body: formData,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `Upload failed: ${res.statusText}`);
  }

  return res.json();
}

export async function getSessionChanges(
  sessionId: string,
): Promise<ChangesResponse> {
  const id = sessionId.startsWith("l2:") ? sessionId.slice(3) : sessionId;
  return request<ChangesResponse>(
    `/session/l2/${encodeURIComponent(id)}/changes`,
  );
}

// ─── Misc ────────────────────────────────────────────────────────────────────

export async function getHealthInfo(): Promise<{ status: string; work_dir?: string }> {
  const res = await fetch("/healthz");
  if (!res.ok) throw new Error("Failed to fetch health info");
  return res.json();
}

// ─── Stats APIs ───────────────────────────────────────────────────────────────

export interface TokenStat {
  period: string
  usage_type: string
  team_id: string
  model_name: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
}

export interface RouterStat {
  period: string
  classification_source: string
  classification_level: string
  count: number
}

export async function getTokenStats(timeframe: string, teamId?: string): Promise<TokenStat[]> {
  const query = new URLSearchParams()
  query.append('timeframe', timeframe)
  if (teamId) query.append('team_id', teamId)
  return request<TokenStat[]>(`/stats/tokens?${query.toString()}`)
}

export async function getRouterStats(timeframe: string, teamId?: string): Promise<RouterStat[]> {
  const query = new URLSearchParams()
  query.append('timeframe', timeframe)
  if (teamId) query.append('team_id', teamId)
  return request<RouterStat[]>(`/stats/router?${query.toString()}`)
}

export async function getStatTeams(): Promise<string[]> {
  return request<string[]>("/stats/teams")
}
