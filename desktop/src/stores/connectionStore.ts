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

  saveConfig: () => void
  loadConfig: () => void

  getEffectiveBaseUrl: () => string
  getEffectiveWsUrl: () => string
  getAuthHeaders: () => Record<string, string>
}

export const useConnectionStore = create<ConnectionState>((set, get) => ({
  mode: getStoredMode(),
  remoteUrl: getStoredRemoteUrl(),
  username: getStoredUsername(),
  password: getStoredPassword(),
  backendStatus: { running: false, pid: null, uptime: null },
  saving: false,
  isChecking: false,
  connectionError: null,

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
  setIsChecking: (checking) => set({ isChecking: checking }),
  setConnectionError: (error) => set({ connectionError: error }),

  saveConfig: () => {
    const { mode, remoteUrl, username, password } = get()
    set({ saving: true })
    localStorage.setItem(MODE_KEY, mode)
    localStorage.setItem(REMOTE_URL_KEY, remoteUrl)
    localStorage.setItem(USERNAME_KEY, username)
    localStorage.setItem(PASSWORD_KEY, password)
    set({ saving: false })
  },

  loadConfig: () => {
    set({
      mode: getStoredMode(),
      remoteUrl: getStoredRemoteUrl(),
      username: getStoredUsername(),
      password: getStoredPassword(),
    })
  },

  getEffectiveBaseUrl: () => {
    const { mode, remoteUrl } = get()
    if (mode === 'remote' && remoteUrl) {
      return remoteUrl.replace(/\/+$/, '')
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
      const wsBase = base.replace(/^http/, 'ws')
      return `${wsBase}/ws`
    }
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
