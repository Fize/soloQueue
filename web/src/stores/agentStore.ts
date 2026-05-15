import { create } from 'zustand'
import { getAgentProfile, updateAgentProfile, getAgentConfig, updateAgentConfig, getTeams } from '@/lib/api'
import type { AgentProfile, AgentConfig, UpdateAgentProfileRequest, UpdateAgentConfigRequest, AgentListResponse, TeamListResponse } from '@/types'

interface AgentState {
  // Agent list (from WebSocket)
  agents: AgentListResponse | null
  setAgents: (data: AgentListResponse) => void

  // Agent profile
  profile: AgentProfile | null
  profileLoading: boolean
  fetchProfile: (id: string) => Promise<void>

  // Agent config
  config: AgentConfig | null
  configLoading: boolean
  fetchConfig: (id: string) => Promise<void>
  updateProfile: (id: string, data: UpdateAgentProfileRequest) => Promise<void>
  updateConfig: (id: string, data: UpdateAgentConfigRequest) => Promise<void>

  // Teams
  teams: TeamListResponse | null
  teamsLoading: boolean
  fetchTeams: () => Promise<void>
}

export const useAgentStore = create<AgentState>((set) => ({
  // Agent list
  agents: null,
  setAgents: (data) => set({ agents: data }),

  // Agent profile
  profile: null,
  profileLoading: false,
  fetchProfile: async (id: string) => {
    set({ profileLoading: true })
    try {
      const data = await getAgentProfile(id)
      set({ profile: data, profileLoading: false })
    } catch {
      set({ profile: null, profileLoading: false })
    }
  },

  // Agent config
  config: null,
  configLoading: false,
  fetchConfig: async (id: string) => {
    set({ configLoading: true })
    try {
      const data = await getAgentConfig(id)
      set({ config: data, configLoading: false })
    } catch {
      set({ config: null, configLoading: false })
    }
  },

  updateProfile: async (id: string, data: UpdateAgentProfileRequest) => {
    const updated = await updateAgentProfile(id, data)
    set({ profile: updated })
  },

  updateConfig: async (id: string, data: UpdateAgentConfigRequest) => {
    const updated = await updateAgentConfig(id, data)
    set({ config: updated })
  },

  // Teams
  teams: null,
  teamsLoading: false,
  fetchTeams: async () => {
    set({ teamsLoading: true })
    try {
      const data = await getTeams()
      set({ teams: data as TeamListResponse, teamsLoading: false })
    } catch {
      set({ teams: null, teamsLoading: false })
    }
  },
}))
