import { create } from 'zustand'
import type { RuntimeStatus } from '@/types'

interface RuntimeState {
  status: RuntimeStatus | null
  setStatus: (status: RuntimeStatus | null) => void
  connectionStatus: 'connected' | 'disconnected' | 'reconnecting'
  setConnectionStatus: (status: 'connected' | 'disconnected' | 'reconnecting') => void
}

export const useRuntimeStore = create<RuntimeState>((set) => ({
  status: null,
  setStatus: (status) => set({ status }),
  connectionStatus: 'disconnected',
  setConnectionStatus: (connectionStatus) => set({ connectionStatus }),
}))
