import { create } from 'zustand'
import { getMCPConfig, updateMCPConfig } from '@/lib/api'
import type { MCPConfig } from '@/types'

interface MCPConfigState {
  config: MCPConfig | null
  saving: boolean
  error: string | null
  fetch: () => Promise<void>
  save: (cfg: MCPConfig) => Promise<MCPConfig>
}

export const useMCPConfigStore = create<MCPConfigState>((set) => ({
  config: null,
  saving: false,
  error: null,

  fetch: async () => {
    try {
      const data = await getMCPConfig()
      set({ config: data, error: null })
    } catch {
      set({ config: null })
    }
  },

  save: async (cfg: MCPConfig) => {
    set({ saving: true, error: null })
    try {
      const updated = await updateMCPConfig(cfg)
      set({ config: updated, saving: false })
      return updated
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to save MCP config'
      set({ error: msg, saving: false })
      throw err
    }
  },
}))
