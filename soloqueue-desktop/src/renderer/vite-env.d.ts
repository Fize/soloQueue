/// <reference types="vite/client" />

interface AvailableModel {
  modelId: string
  modelName: string
  providerId: string
  apiModel: string
  contextWindow: number
}

interface ProviderInfo {
  providerId: string
  providerName: string
}

interface AvailableModelsResult {
  found: boolean
  providers: ProviderInfo[]
  models: AvailableModel[]
  error?: string
}

interface BackendStatus {
  running: boolean
  pid: number | null
  uptime: number | null
}

interface BackendStartResult {
  success: boolean
  error?: string
}

interface ElectronAPI {
  // Window controls
  closeWindow: () => void
  minimizeWindow: () => void
  maximizeWindow: () => void

  // Backend lifecycle
  startBackend: () => Promise<BackendStartResult>
  stopBackend: () => Promise<{ success: boolean }>
  restartBackend: () => Promise<BackendStartResult>
  getBackendStatus: () => Promise<BackendStatus>

  // Backend status push events
  onBackendStatusChanged: (callback: (status: BackendStatus) => void) => () => void
  onBackendLog: (callback: (line: string) => void) => () => void

  // Config
  getAvailableModels: () => Promise<AvailableModelsResult>
  saveL1Config: (modelRef: string) => Promise<{ success: boolean }>
}

declare global {
  interface Window {
    electronAPI?: ElectronAPI
  }
}

export {}
