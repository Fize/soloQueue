import { useConnectionStore, ensureBackendReady } from '@/stores/connectionStore'

export const API_BASE = '/api'

export type ApiRequestOptions = RequestInit & { waitForBackend?: boolean }

export class APIError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
    this.code = code
  }
}

function resolveUrl(path: string, root: boolean): string {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  const backendPath = root ? normalizedPath : `${API_BASE}${normalizedPath}`
  const base = useConnectionStore.getState().getEffectiveBaseUrl()
  return base ? `${base}${backendPath}` : backendPath
}

function buildHeaders(options: RequestInit | undefined): Headers {
  const headers = new Headers(options?.headers)
  const body = options?.body
  const isFormData = typeof FormData !== 'undefined' && body instanceof FormData
  if (body != null && !isFormData && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  return headers
}

async function throwAPIError(response: Response): Promise<never> {
  if (response.status === 401 || response.status === 403) {
    useConnectionStore.getState().setAuthFailure(response.status)
  }
  const body = await response.text()
  let message = response.statusText || `HTTP ${response.status}`
  let code: string | undefined
  try {
    const parsed = JSON.parse(body) as { error?: string; code?: string }
    if (parsed.error) message = parsed.error
    code = parsed.code
  } catch {
    // Do not surface arbitrary non-JSON response bodies in the UI. They may
    // contain proxy diagnostics, HTML, or other data unrelated to the API.
  }
  throw new APIError(message, response.status, code)
}

async function send(
  path: string,
  options: ApiRequestOptions | undefined,
  root: boolean
): Promise<Response> {
  if (options?.waitForBackend !== false) {
    await ensureBackendReady(10_000)
  }
  const {
    headers: _headers,
    mode: _mode,
    credentials: _credentials,
    cache: _cache,
    waitForBackend: _waitForBackend,
    ...rest
  } = options || {}
  const headers = buildHeaders(options)
  const authHeader = useConnectionStore.getState().getAuthHeader()
  if (authHeader && !headers.has('Authorization')) {
    headers.set('Authorization', authHeader)
  }
  try {
    return await fetch(resolveUrl(path, root), {
      ...rest,
      headers,
      mode: 'cors',
      credentials: 'omit',
      cache: 'no-store',
    })
  } catch (err) {
    if (err instanceof TypeError) {
      throw new APIError('Cannot reach backend', 0, 'network_error')
    }
    throw err
  }
}

export async function request<T>(path: string, options?: ApiRequestOptions): Promise<T> {
  return requestJson<T>(path, options)
}

export async function requestJson<T>(path: string, options?: ApiRequestOptions): Promise<T> {
  const response = await send(path, options, false)
  if (!response.ok) await throwAPIError(response)
  if (response.status === 204) return undefined as T

  const body = await response.text()
  if (!body.trim()) return undefined as T
  return JSON.parse(body) as T
}

export async function requestRootJson<T>(path: string, options?: ApiRequestOptions): Promise<T> {
  const response = await send(path, options, true)
  if (!response.ok) await throwAPIError(response)
  if (response.status === 204) return undefined as T

  const body = await response.text()
  if (!body.trim()) return undefined as T
  return JSON.parse(body) as T
}

export async function requestText(path: string, options?: ApiRequestOptions): Promise<string> {
  const response = await send(path, options, false)
  if (!response.ok) await throwAPIError(response)
  return response.text()
}

export async function requestBlob(path: string, options?: ApiRequestOptions): Promise<Blob> {
  const response = await send(path, options, false)
  if (!response.ok) await throwAPIError(response)
  return response.blob()
}

export async function requestForm<T>(
  path: string,
  formData: FormData,
  options?: ApiRequestOptions
): Promise<T> {
  return requestJson<T>(path, { ...options, body: formData })
}

export function getErrorMessage(err: unknown): string {
  return err instanceof Error && err.message ? err.message : 'Network error'
}

export function getFileUrl(path: string): string {
  return resolveUrl(`/files/content?path=${encodeURIComponent(path)}`, false)
}
