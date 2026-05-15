import { create } from 'zustand'
import { getConfig, updateConfig } from '@/lib/api'
import type { AppConfig } from '@/types'

interface ConfigState {
  config: AppConfig | null
  loading: boolean
  error: string | null
  saving: boolean
  fetch: () => Promise<void>
  patch: (partial: Partial<AppConfig>) => Promise<void>
}

export const useConfigStore = create<ConfigState>((set) => ({
  config: null,
  loading: false,
  error: null,
  saving: false,

  fetch: async () => {
    set({ loading: true, error: null })
    try {
      const config = await getConfig()
      set({ config, loading: false })
    } catch (err) {
      set({ error: (err as Error).message, loading: false })
    }
  },

  patch: async (partial: Partial<AppConfig>) => {
    set({ saving: true, error: null })
    try {
      const updated = await updateConfig(partial)
      set({ config: updated, saving: false })
    } catch (err) {
      set({ error: (err as Error).message, saving: false })
    }
  },
}))
