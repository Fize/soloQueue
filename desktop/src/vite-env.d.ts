/// <reference types="vite/client" />

// Vite ?raw imports
declare module '*?raw' {
  const content: string
  export default content
}

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
  selectDirectory: () => Promise<string | null>

  // Connection config
  getConnectionConfig: () => Promise<{ mode: string; remoteUrl: string; username?: string; password?: string } | null>
  saveConnectionConfig: (config: { mode: string; remoteUrl: string; username?: string; password?: string }) => Promise<{ success: boolean }>
}

declare global {
  interface Window {
    electronAPI?: ElectronAPI
  }
}

export {}
