import { create } from 'zustand'

export type ConnectionMode = 'local' | 'remote'

const MODE_KEY = 'soloqueue_connection_mode'
const REMOTE_URL_KEY = 'soloqueue_remote_url'
const USERNAME_KEY = 'soloqueue_remote_username'
const PASSWORD_KEY = 'soloqueue_remote_password'

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

function getStoredUsername(): string {
  try {
    return localStorage.getItem(USERNAME_KEY) || ''
  } catch { /* ignore */ }
  return ''
}

function getStoredPassword(): string {
  try {
    return localStorage.getItem(PASSWORD_KEY) || ''
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
  username: string
  password: string
  backendStatus: BackendStatus
  saving: boolean

  setMode: (mode: ConnectionMode) => void
  setRemoteUrl: (url: string) => void
  setUsername: (username: string) => void
  setPassword: (password: string) => void
  setBackendStatus: (status: BackendStatus) => void
  setSaving: (saving: boolean) => void

  /** Save connection config to persistent storage (localStorage + file via IPC). */
  saveConfig: () => Promise<void>

  /** Load connection config from persistent storage. */
  loadConfig: () => Promise<void>

  /** Returns the effective backend base URL for API calls. */
  getEffectiveBaseUrl: () => string

  /** Returns the effective WebSocket URL. */
  getEffectiveWsUrl: () => string

  /** Returns HTTP Basic Auth headers for remote connections, empty object for local. */
  getAuthHeaders: () => Record<string, string>
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  mode: getStoredMode(),
  remoteUrl: getStoredRemoteUrl(),
  username: getStoredUsername(),
  password: getStoredPassword(),
  backendStatus: { running: false, pid: null, uptime: null },
  saving: false,

  setMode: (mode) => {
    localStorage.setItem(MODE_KEY, mode)
    set({ mode })
  },

  setRemoteUrl: (url) => {
    localStorage.setItem(REMOTE_URL_KEY, url)
    set({ remoteUrl: url })
  },

  setUsername: (username) => {
    localStorage.setItem(USERNAME_KEY, username)
    set({ username })
  },

  setPassword: (password) => {
    localStorage.setItem(PASSWORD_KEY, password)
    set({ password })
  },

  setBackendStatus: (status) => set({ backendStatus: status }),
  setSaving: (saving) => set({ saving }),

  saveConfig: async () => {
    set({ saving: true })
    try {
      const { mode, remoteUrl, username, password } = get()
      // Persist to localStorage (always)
      localStorage.setItem(MODE_KEY, mode)
      localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
      localStorage.setItem(USERNAME_KEY, username)
      localStorage.setItem(PASSWORD_KEY, password)
      // Persist to work-dir JSON file via Electron IPC
      const ea = (window as any).electronAPI
      if (ea?.saveConnectionConfig) {
        await ea.saveConnectionConfig({ mode, remoteUrl, username, password })
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
          const username = config.username || ''
          const password = config.password || ''
          localStorage.setItem(MODE_KEY, mode)
          localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
          localStorage.setItem(USERNAME_KEY, username)
          localStorage.setItem(PASSWORD_KEY, password)
          set({ mode, remoteUrl, username, password })
          return
        }
      }
      // Fallback to localStorage
      set({
        mode: getStoredMode(),
        remoteUrl: getStoredRemoteUrl(),
        username: getStoredUsername(),
        password: getStoredPassword()
      })
    } catch {
      set({
        mode: getStoredMode(),
        remoteUrl: getStoredRemoteUrl(),
        username: getStoredUsername(),
        password: getStoredPassword()
      })
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

  getAuthHeaders: () => {
    const { mode, username, password } = get()
    if (mode === 'remote' && username && password) {
      return { Authorization: 'Basic ' + btoa(`${username}:${password}`) }
    }
    return {} as Record<string, string>
  },
}))
