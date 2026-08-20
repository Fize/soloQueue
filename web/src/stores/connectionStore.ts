import { create } from 'zustand'

export type ConnectionMode = 'local' | 'remote'
export type AuthState = 'checking' | 'not_required' | 'required' | 'authenticated' | 'error'

const MODE_KEY = 'soloqueue_connection_mode'
const REMOTE_URL_KEY = 'soloqueue_remote_url'
const USERNAME_KEY = 'soloqueue_remote_username'

let runtimeBackendUrl = ''

function getStoredMode(): ConnectionMode {
  try {
    return localStorage.getItem(MODE_KEY) === 'remote' ? 'remote' : 'local'
  } catch {
    return 'local'
  }
}

function getStored(key: string): string {
  try {
    return localStorage.getItem(key) || ''
  } catch {
    return ''
  }
}

function normalizeBase(value: string): string {
  const base = value.trim().replace(/\/+$/, '')
  if (!base) return ''
  if (/^https?:\/\//i.test(base)) return base
  return `https://${base}`
}

export interface BackendStatus {
  // Kept as a browser health snapshot for existing consumers; process fields
  // are intentionally unavailable in the Web Console.
  running: boolean
  pid: string | number | null
  uptime: number | null
}

interface ConnectionState {
  mode: ConnectionMode
  remoteUrl: string
  username: string
  password: string
  backendStatus: BackendStatus
  backendReady: boolean
  saving: boolean
  isChecking: boolean
  connectionError: string | null
  authState: AuthState
  authError: string | null
  setMode: (mode: ConnectionMode) => void
  setRemoteUrl: (url: string) => void
  setUsername: (username: string) => void
  setPassword: (password: string) => void
  setBackendStatus: (status: BackendStatus) => void
  setSaving: (saving: boolean) => void
  setIsChecking: (checking: boolean) => void
  setConnectionError: (error: string | null) => void
  setAuthFailure: (status: number) => void
  authenticate: (username: string, password: string) => Promise<void>
  saveConfig: () => Promise<void>
  loadConfig: () => Promise<void>
  getEffectiveBaseUrl: () => string
  getEffectiveWsUrl: () => string
  getAuthHeader: () => string | undefined
  startBackendHealthPolling: () => () => void
  ensureBackendReady: (timeoutMs: number) => Promise<void>
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  mode: getStoredMode(),
  remoteUrl: getStored(REMOTE_URL_KEY),
  username: getStored(USERNAME_KEY),
  password: '',
  backendStatus: { running: false, pid: null, uptime: null },
  backendReady: false,
  saving: false,
  isChecking: false,
  connectionError: null,
  authState: 'checking',
  authError: null,

  setMode: (mode) => set({ mode, ...(mode === 'local' ? { password: '' } : {}) }),
  setRemoteUrl: (remoteUrl) => set({ remoteUrl }),
  setUsername: (username) => set({ username }),
  setPassword: (password) => set({ password }),
  setBackendStatus: (status) => set({ backendStatus: status, backendReady: status.running }),
  setSaving: (saving) => set({ saving }),
  setIsChecking: (isChecking) => set({ isChecking }),
  setConnectionError: (connectionError) => set({ connectionError }),
  setAuthFailure: (status) =>
    set({
      authState: status === 403 ? 'error' : 'required',
      authError:
        status === 403
          ? 'Remote access is not configured on this backend.'
          : 'Your credentials are invalid or have expired.',
      ...(status === 401 ? { password: '' } : {}),
    }),
  authenticate: async (username, password) => {
    const trimmedUsername = username.trim()
    if (!trimmedUsername || !password) {
      set({ authState: 'required', authError: 'Username and password are required.' })
      return
    }

    set({ username: trimmedUsername, password, authError: null })
    const base = get().getEffectiveBaseUrl()
    const url = `${base || ''}/api/auth/check`
    const headers = new Headers({
      Authorization: `Basic ${btoa(`${trimmedUsername}:${password}`)}`,
    })
    try {
      const response = await fetch(url, {
        headers,
        mode: 'cors',
        credentials: 'omit',
        cache: 'no-store',
      })
      if (response.ok) {
        set({ authState: 'authenticated', authError: null })
        return
      }
      set({
        authState: response.status === 403 ? 'error' : 'required',
        authError:
          response.status === 403
            ? 'Remote access is not configured on this backend.'
            : 'Invalid username or password.',
        password: '',
      })
    } catch {
      set({ authState: 'error', authError: 'Cannot reach backend. Check the URL and try again.', password: '' })
    }
  },

  saveConfig: async () => {
    const { mode, remoteUrl, username } = get()
    set({ saving: true })
    try {
      localStorage.setItem(MODE_KEY, mode)
      localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
      localStorage.setItem(USERNAME_KEY, username)
      // Passwords remain memory-only in the browser and are never persisted.
    } finally {
      set({ saving: false })
    }
  },

  loadConfig: async () => {
    const mode = getStoredMode()
    const remoteUrl = getStored(REMOTE_URL_KEY)
    const username = getStored(USERNAME_KEY)
    set({ mode, remoteUrl, username, password: '', authState: 'checking', authError: null })

    // `web` exposes this tiny endpoint with its configured --backend URL;
    // `start` responds with an empty value, meaning same-origin.
    try {
      const configuredBase = mode === 'remote' ? normalizeBase(remoteUrl) : ''
      const response = await fetch(`${configuredBase}/api/runtime-config`, {
        cache: 'no-store',
        credentials: 'omit',
      })
      if (response.ok) {
        const config = (await response.json()) as { backend_url?: string }
        runtimeBackendUrl = normalizeBase(config.backend_url || '')
      }
    } catch {
      runtimeBackendUrl = ''
    }

    const base = get().getEffectiveBaseUrl()
    try {
      const response = await fetch(`${base || ''}/api/auth/status`, {
        cache: 'no-store',
        credentials: 'omit',
      })
      if (!response.ok) {
        set({ authState: 'error', authError: 'Cannot determine backend authentication status.' })
        return
      }
      const status = (await response.json()) as { required?: boolean }
      set({
        authState: status.required ? 'required' : 'not_required',
        authError: null,
      })
    } catch {
      set({ authState: 'error', authError: 'Cannot determine backend authentication status.' })
    }
  },

  getEffectiveBaseUrl: () => {
    const { mode, remoteUrl } = get()
    return mode === 'remote' ? normalizeBase(remoteUrl) : runtimeBackendUrl
  },

  startBackendHealthPolling: () => {
    let stopped = false
    let failures = 0
    const check = async () => {
      if (stopped) return
      const base = get().getEffectiveBaseUrl()
      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), 1500)
      try {
        const response = await fetch(`${base || ''}/healthz`, {
          signal: controller.signal,
          cache: 'no-store',
        })
        if (response.ok) {
          failures = 0
          set({ backendReady: true, backendStatus: { running: true, pid: null, uptime: null } })
        } else if (++failures >= 2) {
          set({ backendReady: false, backendStatus: { running: false, pid: null, uptime: null } })
        }
      } catch {
        if (++failures >= 2) {
          set({ backendReady: false, backendStatus: { running: false, pid: null, uptime: null } })
        }
      } finally {
        clearTimeout(timeout)
      }
    }
    void check()
    const timer = setInterval(() => void check(), 5000)
    return () => {
      stopped = true
      clearInterval(timer)
    }
  },

  ensureBackendReady: (timeoutMs) =>
    new Promise<void>((resolve) => {
      if (get().backendReady) {
        resolve()
        return
      }
      const unsubscribe = useConnectionStore.subscribe((state) => {
        if (state.backendReady) {
          unsubscribe()
          resolve()
        }
      })
      setTimeout(() => {
        unsubscribe()
        resolve()
      }, timeoutMs)
    }),

  getEffectiveWsUrl: () => {
    const base = get().getEffectiveBaseUrl()
    if (base) return `${base.replace(/^http/i, 'ws')}/ws`
    if (typeof window === 'undefined') return 'ws://127.0.0.1/ws'
    return `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`
  },

  getAuthHeader: () => {
    const { username, password } = get()
    if (!username || !password) return undefined
    return `Basic ${btoa(`${username}:${password}`)}`
  },
}))

export function isBackendReady(): boolean {
  return useConnectionStore.getState().backendReady
}

export function ensureBackendReady(timeoutMs: number): Promise<void> {
  return useConnectionStore.getState().ensureBackendReady(timeoutMs)
}
