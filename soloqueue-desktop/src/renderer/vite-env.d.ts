/// <reference types="vite/client" />

interface ElectronAPI {
  getProfile: () => Promise<unknown>
  saveProfile: (profile: unknown) => Promise<boolean>
  closeWindow: () => void
  minimizeWindow: () => void
  maximizeWindow: () => void
}

declare global {
  interface Window {
    electronAPI?: ElectronAPI
  }
}

export {}
