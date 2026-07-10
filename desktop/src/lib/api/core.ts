import { useConnectionStore } from "@/stores/connectionStore";

export const API_BASE = "/api";

export async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...useConnectionStore.getState().getAuthHeaders(),
  };
  const res = await fetch(`${API_BASE}${path}`, {
    headers,
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    console.error("API error:", err);
    throw new Error(err.error || `HTTP ${res.status}`);
  }
  return res.json();
}

export function getFileUrl(path: string): string {
  const base = useConnectionStore.getState().getEffectiveBaseUrl();
  if (base) {
    return `${base}${API_BASE}/files/content?path=${encodeURIComponent(path)}`;
  }
  return `${API_BASE}/files/content?path=${encodeURIComponent(path)}`;
}
