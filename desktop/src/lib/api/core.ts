import { useConnectionStore } from "@/stores/connectionStore";

export const API_BASE = "/api";

export async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const base = useConnectionStore.getState().getEffectiveBaseUrl();
  const url = base ? `${base}${API_BASE}${path}` : `${API_BASE}${path}`;

  const authHeaders = useConnectionStore.getState().getAuthHeaders();
  const hasBody = options?.body != null;

  // Build headers: only include Content-Type for requests with a body.
  // GET requests with Content-Type: application/json trigger unnecessary
  // CORS preflights and can cause the Authorization header to be dropped
  // by some browsers.
  const headers: Record<string, string> = {
    ...(hasBody ? { "Content-Type": "application/json" } : {}),
    ...authHeaders,
  };

  // Merge user-supplied headers AFTER auth headers so callers can
  // override Content-Type but never accidentally strip Authorization.
  const userHeaders = options?.headers as Record<string, string> | undefined;
  if (userHeaders) {
    Object.assign(headers, userHeaders);
  }

  // Destructure to avoid overriding our explicit headers / mode / credentials.
  const { headers: _, body: __, mode: ___, credentials: ____, ...restOptions } = options || {};

  const res = await fetch(url, {
    cache: "no-store",
    ...restOptions,
    headers,
    ...(hasBody ? { body: options!.body } : {}),
    mode: "cors",
    credentials: "omit",
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
