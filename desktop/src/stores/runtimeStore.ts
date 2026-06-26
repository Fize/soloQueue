import { create } from 'zustand'
import type { RuntimeStatus } from '@/types'

interface RuntimeState {
  status: RuntimeStatus | null
  setStatus: (status: RuntimeStatus | null) => void
  connectionStatus: 'connected' | 'disconnected' | 'reconnecting'
  setConnectionStatus: (status: 'connected' | 'disconnected' | 'reconnecting') => void
  sidebarCollapsed: boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  inspectorPanelWidth: number
  setInspectorPanelWidth: (w: number) => void
}

export const useRuntimeStore = create<RuntimeState>((set) => ({
  status: null,
  setStatus: (status) => set({ status }),
  connectionStatus: 'disconnected',
  setConnectionStatus: (connectionStatus) => set({ connectionStatus }),
  sidebarCollapsed: false,
  setSidebarCollapsed: (sidebarCollapsed) => set({ sidebarCollapsed }),
  inspectorPanelWidth: 0,
  setInspectorPanelWidth: (inspectorPanelWidth) => set({ inspectorPanelWidth }),
}))
