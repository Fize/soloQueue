import { create } from 'zustand'

export type ConnectionMode = 'local' | 'remote'

const MODE_KEY = 'soloqueue_connection_mode'
const REMOTE_URL_KEY = 'soloqueue_remote_url'
const USERNAME_KEY = 'soloqueue_remote_username'

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
  isChecking: boolean
  connectionError: string | null

  setMode: (mode: ConnectionMode) => void
  setRemoteUrl: (url: string) => void
  setUsername: (username: string) => void
  setPassword: (password: string) => void
  setBackendStatus: (status: BackendStatus) => void
  setSaving: (saving: boolean) => void
  setIsChecking: (checking: boolean) => void
  setConnectionError: (error: string | null) => void

  saveConfig: () => Promise<void>
  loadConfig: () => Promise<void>

  getEffectiveBaseUrl: () => string
  getEffectiveWsUrl: () => string
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  mode: getStoredMode(),
  remoteUrl: getStoredRemoteUrl(),
  username: getStoredUsername(),
  // Password is intentionally session-only in the Renderer. Electron Main
  // stores the persisted remote credential and injects auth for remote calls.
  password: '',
  backendStatus: { running: false, pid: null, uptime: null },
  saving: false,
  isChecking: false,
  connectionError: null,

  setMode: (mode) => {
    set({ mode, ...(mode === 'local' ? { password: '' } : {}) })
  },

  setRemoteUrl: (url) => {
    set({ remoteUrl: url })
  },

  setUsername: (username) => {
    set({ username })
  },

  setPassword: (password) => {
    set({ password })
  },

  setBackendStatus: (status) => set({ backendStatus: status }),
  setSaving: (saving) => set({ saving }),
  setIsChecking: (checking) => set({ isChecking: checking }),
  setConnectionError: (error) => set({ connectionError: error }),

  saveConfig: async () => {
    const { mode, remoteUrl, username, password } = get()
    set({ saving: true })
    try {
      const electronAPI = typeof window !== 'undefined' ? window.electronAPI : undefined
      if (electronAPI?.saveRemoteConfig) {
        const result = await electronAPI.saveRemoteConfig({ mode, remoteUrl, username, password })
        if (!result.success) throw new Error(result.error || 'failed to save connection config')
      }
      localStorage.setItem(MODE_KEY, mode)
      localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
      localStorage.setItem(USERNAME_KEY, username)
      localStorage.removeItem('soloqueue_remote_password')
    } finally {
      set({ saving: false })
    }
  },

  loadConfig: async () => {
    const electronAPI = typeof window !== 'undefined' ? window.electronAPI : undefined
    if (electronAPI?.getRemoteConfig) {
      try {
        const config = await electronAPI.getRemoteConfig()
        set({ ...config, password: '' })
        localStorage.setItem(MODE_KEY, config.mode)
        localStorage.setItem(REMOTE_URL_KEY, config.remoteUrl)
        localStorage.setItem(USERNAME_KEY, config.username)
        return
      } catch {
        // Fall back to legacy public fields for development or an older
        // preload script. No password is read on this path.
      }
    }
    set({ mode: getStoredMode(), remoteUrl: getStoredRemoteUrl(), username: getStoredUsername(), password: '' })
  },

  getEffectiveBaseUrl: () => {
    const { mode, remoteUrl } = get()
    if (mode === 'remote' && remoteUrl) {
      const base = remoteUrl.replace(/\/+$/, '')
      if (/^https?:\/\//.test(base)) {
        return base
      }
      return `https://${base}`
    }
    if (typeof window !== 'undefined' && window.location.protocol === 'file:') {
      const port = (window as any).electronAPI?.backendPort || 57647
      return `http://127.0.0.1:${port}`
    }
    return ''
  },

  getEffectiveWsUrl: () => {
    const { mode, remoteUrl } = get()
    if (mode === 'remote' && remoteUrl) {
      const base = remoteUrl.replace(/\/+$/, '')
      let wsBase: string
      if (/^https?:\/\//.test(base)) {
        wsBase = base.replace(/^http/, 'ws')
      } else {
        wsBase = `wss://${base}`
      }
      return `${wsBase}/ws`
    }
    if (typeof window !== 'undefined' && window.location.protocol === 'file:') {
      const port = (window as any).electronAPI?.backendPort || 57647
      return `ws://127.0.0.1:${port}/ws`
    }
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    return `${proto}//${window.location.host}/ws`
  },

}))
