import { create } from 'zustand'

export type ConnectionMode = 'local' | 'remote'

const MODE_KEY = 'soloqueue_connection_mode'
const REMOTE_URL_KEY = 'soloqueue_remote_url'

function getStoredMode(): ConnectionMode {
  try {
    const v = localStorage.getItem(MODE_KEY)
    if (v === 'remote') return 'remote'
  } catch { /* ignore */ }
  return 'local'
}

function getStoredRemoteUrl(): string {
  try {
    return localStorage.getItem(REMOTE_URL_KEY) || ''
  } catch { /* ignore */ }
  return ''
}

export interface BackendStatus {
  running: boolean
  pid: string | number | null
  uptime: number | null
}

interface ConnectionState {
  mode: ConnectionMode
  remoteUrl: string
  backendStatus: BackendStatus
  backendLogs: string[]
  saving: boolean

  setMode: (mode: ConnectionMode) => void
  setRemoteUrl: (url: string) => void
  setBackendStatus: (status: BackendStatus) => void
  appendBackendLog: (line: string) => void
  clearBackendLogs: () => void
  setSaving: (saving: boolean) => void

  /** Save connection config to persistent storage (localStorage + file via IPC). */
  saveConfig: () => Promise<void>

  /** Load connection config from persistent storage. */
  loadConfig: () => Promise<void>

  /** Returns the effective backend base URL for API calls. */
  getEffectiveBaseUrl: () => string

  /** Returns the effective WebSocket URL. */
  getEffectiveWsUrl: () => string
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  mode: getStoredMode(),
  remoteUrl: getStoredRemoteUrl(),
  backendStatus: { running: false, pid: null, uptime: null },
  backendLogs: [],
  saving: false,

  setMode: (mode) => {
    localStorage.setItem(MODE_KEY, mode)
    set({ mode })
  },

  setRemoteUrl: (url) => {
    localStorage.setItem(REMOTE_URL_KEY, url)
    set({ remoteUrl: url })
  },

  setBackendStatus: (status) => set({ backendStatus: status }),
  appendBackendLog: (line) =>
    set((s) => ({ backendLogs: [...s.backendLogs.slice(-199), line] })),
  clearBackendLogs: () => set({ backendLogs: [] }),
  setSaving: (saving) => set({ saving }),

  saveConfig: async () => {
    set({ saving: true })
    try {
      const { mode, remoteUrl } = get()
      // Persist to localStorage (always)
      localStorage.setItem(MODE_KEY, mode)
      localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
      // Persist to work-dir JSON file via Electron IPC
      const ea = (window as any).electronAPI
      if (ea?.saveConnectionConfig) {
        await ea.saveConnectionConfig({ mode, remoteUrl })
      }
    } finally {
      set({ saving: false })
    }
  },

  loadConfig: async () => {
    try {
      const ea = (window as any).electronAPI
      if (ea?.getConnectionConfig) {
        const config = await ea.getConnectionConfig()
        if (config) {
          const mode: ConnectionMode = config.mode === 'remote' ? 'remote' : 'local'
          const remoteUrl = config.remoteUrl || ''
          localStorage.setItem(MODE_KEY, mode)
          localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
          set({ mode, remoteUrl })
          return
        }
      }
      // Fallback to localStorage
      set({ mode: getStoredMode(), remoteUrl: getStoredRemoteUrl() })
    } catch {
      set({ mode: getStoredMode(), remoteUrl: getStoredRemoteUrl() })
    }
  },

  getEffectiveBaseUrl: () => {
    const { mode, remoteUrl } = get()
    if (mode === 'remote' && remoteUrl) {
      // Strip trailing slash
      return remoteUrl.replace(/\/+$/, '')
    }
    // Local mode: use the existing logic (Vite proxy in dev, Electron port in prod)
    if (typeof window !== 'undefined' && window.location.protocol === 'file:') {
      const port = (window as any).electronAPI?.backendPort || 57647
      return `http://127.0.0.1:${port}`
    }
    return '' // empty = use relative paths (dev mode via Vite proxy)
  },

  getEffectiveWsUrl: () => {
    const { mode, remoteUrl } = get()
    if (mode === 'remote' && remoteUrl) {
      const base = remoteUrl.replace(/\/+$/, '')
      // Replace http:// with ws:// and https:// with wss://
      const wsBase = base.replace(/^http/, 'ws')
      return `${wsBase}/ws`
    }
    // Local mode: use existing logic
    if (typeof window !== 'undefined' && window.location.protocol === 'file:') {
      const port = (window as any).electronAPI?.backendPort || 57647
      return `ws://127.0.0.1:${port}/ws`
    }
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${window.location.host}/ws`
  },
}))
