import type {
  AppConfig,
  LLMProvider,
  LLMModel,
  ModelRoutesConfig,
  SessionConfig,
  ToolsConfig,
  EmbeddingConfig,
  QQBotConfig,
  WeChatAccountView,
  WeChatLoginSnapshot,
  StartWeChatLoginRequest,
  LSPMCPConfig,
  SimulationConfig,
  SpeechConfig,
  SpeechStatus,
  SpeechInstallResponse,
  ToolListResponse,
  SkillListResponse,
  SkillInfo,
  MCPConfig,
  MCPAvailableResponse,
  MCPPolicy,
  MCPPolicyListResponse,
} from "@/types";
import { request, requestText } from "./core";

// ─── Config APIs ──────────────────────────────────────────────────────────────

export async function getConfig(): Promise<AppConfig> {
  return request<AppConfig>("/config");
}

export async function getConfigToml(): Promise<string> {
  return requestText("/config/toml");
}

// ─── DB-backed Config APIs ──────────────────────────────────────────────────

export async function listProviders(
  options?: RequestInit,
): Promise<LLMProvider[]> {
  return request<LLMProvider[]>("/config/providers", options);
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

export async function listProviderRemoteModels(id: string): Promise<string[]> {
  return request<string[]>(`/config/providers/${id}/remote-models`);
}

export async function listModels(options?: RequestInit): Promise<LLMModel[]> {
  return request<LLMModel[]>("/config/models", options);
}

export async function createModel(data: LLMModel): Promise<LLMModel> {
  return request<LLMModel>("/config/models", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function updateModel(
  providerId: string,
  id: string,
  data: LLMModel,
): Promise<LLMModel> {
  return request<LLMModel>(`/config/models/${providerId}/${id}`, {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteModel(
  providerId: string,
  id: string,
): Promise<void> {
  await request(`/config/models/${providerId}/${id}`, { method: "DELETE" });
}

export async function getModelRoutes(): Promise<ModelRoutesConfig> {
  return request<ModelRoutesConfig>("/config/model-routes");
}

export async function updateModelRoutes(
  data: ModelRoutesConfig,
): Promise<ModelRoutesConfig> {
  return request<ModelRoutesConfig>("/config/model-routes", {
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

export async function getWeChatBotsConfig(): Promise<WeChatAccountView[]> {
  return request<WeChatAccountView[]>("/config/wechat-bots");
}

export async function updateWeChatBotsConfig(
  data: WeChatAccountView[],
): Promise<WeChatAccountView[]> {
  return request<WeChatAccountView[]>("/config/wechat-bots", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function deleteWeChatBot(accountId: string): Promise<void> {
  return request<void>(`/config/wechat-bots/${encodeURIComponent(accountId)}`, {
    method: "DELETE",
    body: JSON.stringify({ confirmAccountId: accountId }),
  });
}

export async function startWeChatLogin(
  data: StartWeChatLoginRequest,
): Promise<WeChatLoginSnapshot> {
  return request<WeChatLoginSnapshot>("/channels/wechat/login", {
    method: "POST",
    body: JSON.stringify(data),
  });
}

export async function getWeChatLogin(
  sessionId: string,
): Promise<WeChatLoginSnapshot> {
  return request<WeChatLoginSnapshot>(
    `/channels/wechat/login/${encodeURIComponent(sessionId)}`,
  );
}

export async function submitWeChatVerification(
  sessionId: string,
  code: string,
): Promise<void> {
  return request<void>(
    `/channels/wechat/login/${encodeURIComponent(sessionId)}/verification`,
    {
      method: "POST",
      body: JSON.stringify({ code }),
    },
  );
}

export async function cancelWeChatLogin(sessionId: string): Promise<void> {
  return request<void>(
    `/channels/wechat/login/${encodeURIComponent(sessionId)}`,
    {
      method: "DELETE",
    },
  );
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

export async function getSpeechConfig(): Promise<SpeechConfig> {
  return request<SpeechConfig>("/config/speech");
}

export async function updateSpeechConfig(
  data: SpeechConfig,
): Promise<SpeechConfig> {
  return request<SpeechConfig>("/config/speech", {
    method: "PUT",
    body: JSON.stringify(data),
  });
}

export async function getSpeechStatus(): Promise<SpeechStatus> {
  return request<SpeechStatus>("/config/speech/status");
}

export async function installSpeech(
  model?: string,
): Promise<SpeechInstallResponse> {
  return request<SpeechInstallResponse>("/config/speech/install", {
    method: "POST",
    body: JSON.stringify({ model }),
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

export async function getAvailableMCPServers(): Promise<MCPAvailableResponse> {
  return request<MCPAvailableResponse>("/mcp/available");
}

export async function getMCPPolicies(): Promise<MCPPolicyListResponse> {
  return request<MCPPolicyListResponse>("/mcp/policies?scope=global");
}

export async function approveMCPPolicy(serverName: string): Promise<MCPPolicy> {
  return request<MCPPolicy>(`/mcp/policies/${encodeURIComponent(serverName)}`, {
    method: "PUT",
    body: JSON.stringify({
      scope: "global",
      confirm_host_access: true,
    }),
  });
}

export async function revokeMCPPolicy(serverName: string): Promise<void> {
  await request<void>(
    `/mcp/policies/${encodeURIComponent(serverName)}?scope=global`,
    {
      method: "DELETE",
    },
  );
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
