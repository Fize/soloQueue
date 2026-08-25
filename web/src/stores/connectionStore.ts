import { create } from 'zustand'

export type ConnectionMode = 'local' | 'remote'

const MODE_KEY = 'soloqueue_connection_mode'
const REMOTE_URL_KEY = 'soloqueue_remote_url'

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
  backendStatus: BackendStatus
  backendReady: boolean
  saving: boolean
  isChecking: boolean
  connectionError: string | null
  setMode: (mode: ConnectionMode) => void
  setRemoteUrl: (url: string) => void
  setBackendStatus: (status: BackendStatus) => void
  setSaving: (saving: boolean) => void
  setIsChecking: (checking: boolean) => void
  setConnectionError: (error: string | null) => void
  saveConfig: () => Promise<void>
  loadConfig: () => Promise<void>
  getEffectiveBaseUrl: () => string
  getEffectiveWsUrl: () => string
  startBackendHealthPolling: () => () => void
  ensureBackendReady: (timeoutMs: number) => Promise<void>
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  mode: getStoredMode(),
  remoteUrl: getStored(REMOTE_URL_KEY),
  backendStatus: { running: false, pid: null, uptime: null },
  backendReady: false,
  saving: false,
  isChecking: false,
  connectionError: null,

  setMode: (mode) => set({ mode }),
  setRemoteUrl: (remoteUrl) => set({ remoteUrl }),
  setBackendStatus: (status) => set({ backendStatus: status, backendReady: status.running }),
  setSaving: (saving) => set({ saving }),
  setIsChecking: (isChecking) => set({ isChecking }),
  setConnectionError: (connectionError) => set({ connectionError }),

  saveConfig: async () => {
    const { mode, remoteUrl } = get()
    set({ saving: true })
    try {
      localStorage.setItem(MODE_KEY, mode)
      localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
    } finally {
      set({ saving: false })
    }
  },

  loadConfig: async () => {
    const mode = getStoredMode()
    const remoteUrl = getStored(REMOTE_URL_KEY)
    set({ mode, remoteUrl })
    runtimeBackendUrl = ''

    // `web` exposes this tiny endpoint with its configured --backend URL;
    // `start` responds with an empty value, meaning same-origin.
    try {
      const configuredBase = mode === 'remote' ? normalizeBase(remoteUrl) : ''
      const response = await fetch(`${configuredBase}/api/runtime-config`, {
        cache: 'no-store',
        credentials: 'include',
      })
      if (response.ok) {
        const config = (await response.json()) as { backend_url?: string }
        runtimeBackendUrl = normalizeBase(config.backend_url || '')
      }
    } catch {
      runtimeBackendUrl = ''
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
          credentials: 'include',
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
}))

export function isBackendReady(): boolean {
  return useConnectionStore.getState().backendReady
}

export function ensureBackendReady(timeoutMs: number): Promise<void> {
  return useConnectionStore.getState().ensureBackendReady(timeoutMs)
}
