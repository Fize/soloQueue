import type {
  AppConfig,
  LLMProvider,
  LLMModel,
  DefaultModelsConfig,
  SessionConfig,
  ToolsConfig,
  EmbeddingConfig,
  QQBotConfig,
  LSPMCPConfig,
  SimulationConfig,
  ToolListResponse,
  SkillListResponse,
  SkillInfo,
  MCPConfig,
} from "@/types";
import { request, API_BASE } from "./core";
import { useConnectionStore } from "@/stores/connectionStore";

// ─── Config APIs ──────────────────────────────────────────────────────────────

export async function getConfig(): Promise<AppConfig> {
  return request<AppConfig>("/config");
}

export async function getConfigToml(): Promise<string> {
  const res = await fetch(`${API_BASE}/config/toml`, {
    headers: useConnectionStore.getState().getAuthHeaders(),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.text();
}

// ─── DB-backed Config APIs ──────────────────────────────────────────────────

export async function listProviders(): Promise<LLMProvider[]> {
  return request<LLMProvider[]>("/config/providers");
}

export async function createProvider(data: LLMProvider): Promise<LLMProvider> {
  return request<LLMProvider>("/config/providers", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateProvider(
  id: string,
  data: LLMProvider,
): Promise<LLMProvider> {
  return request<LLMProvider>(`/config/providers/${id}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteProvider(id: string): Promise<void> {
  await request(`/config/providers/${id}`, { method: "DELETE" });
}

export async function listModels(): Promise<LLMModel[]> {
  return request<LLMModel[]>("/config/models");
}

export async function createModel(data: LLMModel): Promise<LLMModel> {
  return request<LLMModel>("/config/models", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateModel(
  id: string,
  data: LLMModel,
): Promise<LLMModel> {
  return request<LLMModel>(`/config/models/${id}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteModel(id: string): Promise<void> {
  await request(`/config/models/${id}`, { method: "DELETE" });
}

export async function getDefaultModels(): Promise<DefaultModelsConfig> {
  return request<DefaultModelsConfig>("/config/default-models");
}

export async function updateDefaultModels(
  data: DefaultModelsConfig,
): Promise<DefaultModelsConfig> {
  return request<DefaultModelsConfig>("/config/default-models", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

// ─── System Settings DB-backed APIs ─────────────────────────────────────────

export async function getToolsConfig(): Promise<ToolsConfig> {
  return request<ToolsConfig>("/config/tools");
}

export async function updateToolsConfig(
  data: ToolsConfig,
): Promise<ToolsConfig> {
  return request<ToolsConfig>("/config/tools", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getQQBotsConfig(): Promise<QQBotConfig[]> {
  return request<QQBotConfig[]>("/config/qqbots");
}

export async function updateQQBotsConfig(
  data: QQBotConfig[],
): Promise<QQBotConfig[]> {
  return request<QQBotConfig[]>("/config/qqbots", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getLSPMCPConfig(): Promise<LSPMCPConfig> {
  return request<LSPMCPConfig>("/config/lspmcp");
}

export async function updateLSPMCPConfig(
  data: LSPMCPConfig,
): Promise<LSPMCPConfig> {
  return request<LSPMCPConfig>("/config/lspmcp", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getEmbeddingConfig(): Promise<EmbeddingConfig> {
  return request<EmbeddingConfig>("/config/embedding");
}

export async function updateEmbeddingConfig(
  data: EmbeddingConfig,
): Promise<EmbeddingConfig> {
  return request<EmbeddingConfig>("/config/embedding", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getSessionConfig(): Promise<SessionConfig> {
  return request<SessionConfig>("/config/session");
}

export async function updateSessionConfig(
  data: SessionConfig,
): Promise<SessionConfig> {
  return request<SessionConfig>("/config/session", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getSimulationConfig(): Promise<SimulationConfig> {
  return request<SimulationConfig>("/config/simulation");
}

export async function updateSimulationConfig(
  data: SimulationConfig,
): Promise<SimulationConfig> {
  return request<SimulationConfig>("/config/simulation", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

// ─── Tools & Skills APIs ────────────────────────────────────────────────────

export async function getTools(): Promise<ToolListResponse> {
  return request<ToolListResponse>("/tools");
}

export async function getSkills(): Promise<SkillListResponse> {
  return request<SkillListResponse>("/skills");
}

// ─── MCP APIs ──────────────────────────────────────────────────────────────────

export async function getMCPConfig(): Promise<MCPConfig> {
  return request<MCPConfig>("/mcp");
}

export async function updateMCPConfig(config: MCPConfig): Promise<MCPConfig> {
  return request<MCPConfig>("/mcp", {
    method: "PATCH",
    body: JSON.stringify(config),
  });
}

// ─── Skill Management & Store APIs ──────────────────────────────────────────

export interface InstallSkillRequest {
  source: "store" | "local" | "github";
  id?: string;
  path?: string;
  url?: string;
}

export interface SkillFileEntry {
  path: string;
  kind: "file" | "directory";
  size?: number;
}

export async function fetchStoreSkills(): Promise<SkillListResponse> {
  return request<SkillListResponse>("/skills/store");
}

export async function installSkill(data: InstallSkillRequest): Promise<void> {
  await request("/skills/install", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function fetchSkillDetail(id: string): Promise<SkillInfo> {
  return request<SkillInfo>(`/skills/${encodeURIComponent(id)}`);
}

export async function updateSkill(
  id: string,
  data: { description: string; body: string; triggers: string[] },
): Promise<void> {
  await request(`/skills/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteSkill(id: string): Promise<void> {
  await request(`/skills/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export async function fetchSkillFiles(
  id: string,
): Promise<{ files: SkillFileEntry[] }> {
  return request<{ files: SkillFileEntry[] }>(
    `/skills/${encodeURIComponent(id)}/files`,
  );
}

export async function toggleSkill(
  id: string,
): Promise<{ id: string; enabled: boolean }> {
  return request<{ id: string; enabled: boolean }>(
    `/skills/${encodeURIComponent(id)}/toggle`,
    {
      method: "POST",
    },
  );
}

export async function toggleSkillAutoUpdate(
  id: string,
  enabled: boolean,
): Promise<{ id: string; auto_update: boolean }> {
  return request<{ id: string; auto_update: boolean }>(
    `/skills/${encodeURIComponent(id)}/auto-update`,
    {
      method: "POST",
      body: JSON.stringify({ enabled }),
    },
  );
}

export async function importSkill(data: {
  name: string;
  description: string;
  body: string;
  triggers: string[];
}): Promise<void> {
  await request("/skills", {
    method: "POST",
    body: JSON.stringify(data),
  });
}
