/// <reference types="vite/client" />

interface ElectronAPI {
  backendPort: number
  closeWindow: () => Promise<void>
  minimizeWindow: () => Promise<void>
  maximizeWindow: () => Promise<void>
  startBackend: () => Promise<{ success: boolean; error?: string }>
  stopBackend: () => Promise<{ success: boolean }>
  restartBackend: () => Promise<{ success: boolean; error?: string }>
  getBackendStatus: () => Promise<{
    running: boolean
    pid: string | number | null
    uptime: number | null
  }>
  onBackendStatusChanged: (
    callback: (status: { running: boolean; pid: string | number | null; uptime: number | null }) => void
  ) => () => void
  onBackendLog: (callback: (line: string) => void) => () => void
  getAvailableModels: () => Promise<unknown>
  saveL1Config: (modelRef: string) => Promise<{ success: boolean }>
  selectDirectory: () => Promise<string | null>
}

declare global {
  interface Window {
    electronAPI?: ElectronAPI
  }
}

export {}
