import { create } from 'zustand'
import type { RuntimeStatus } from '@/types'

const DESIGN_MODE_KEY = 'soloqueue_design_mode'
const SIDEBAR_COLLAPSED_KEY = 'soloqueue_sidebar_collapsed'

function getStored(key: string, fallback: boolean): boolean {
  try {
    return localStorage.getItem(key) === 'true'
  } catch {
    return fallback
  }
}

function setStored(key: string, value: boolean) {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    // ignore storage errors
  }
}

interface RuntimeState {
  status: RuntimeStatus | null
  setStatus: (status: RuntimeStatus | null) => void
  connectionStatus: 'connected' | 'disconnected' | 'reconnecting'
  setConnectionStatus: (status: 'connected' | 'disconnected' | 'reconnecting') => void
  sidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  inspectorPanelWidth: number
  setInspectorPanelWidth: (w: number) => void
  isDesignMode: boolean
  setDesignMode: (active: boolean) => void
}

export const useRuntimeStore = create<RuntimeState>((set) => ({
  status: null,
  setStatus: (status) => set({ status }),
  connectionStatus: 'disconnected',
  setConnectionStatus: (connectionStatus) => set({ connectionStatus }),
  sidebarCollapsed: getStored(SIDEBAR_COLLAPSED_KEY, false),
  setSidebarCollapsed: (sidebarCollapsed) => {
    setStored(SIDEBAR_COLLAPSED_KEY, sidebarCollapsed)
    set({ sidebarCollapsed })
  },
  inspectorPanelWidth: 0,
  setInspectorPanelWidth: (inspectorPanelWidth) => set({ inspectorPanelWidth }),
  isDesignMode: getStored(DESIGN_MODE_KEY, false),
  setDesignMode: (isDesignMode) => {
    setStored(DESIGN_MODE_KEY, isDesignMode)
    set({ isDesignMode })
  },
}))
