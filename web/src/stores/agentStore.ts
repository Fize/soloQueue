import { create } from 'zustand'
import {
  getAgentProfile,
  updateAgentProfile,
  getAgentConfig,
  updateAgentConfig,
  getTeams,
  getLiveAgents,
} from '@/lib/api'
import type {
  AgentProfile,
  AgentConfig,
  UpdateAgentProfileRequest,
  UpdateAgentConfigRequest,
  AgentListResponse,
  TeamListResponse,
} from '@/types'

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
  fetchLiveAgents: () => Promise<void>
}

// Coalesce concurrent fetches across the three callers (ChatPage mount,
// SessionTree mount, App.tsx auto-retry on backend up).
let inflightTeamsLoad: Promise<void> | null = null
let inflightLiveAgentsLoad: Promise<void> | null = null

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
    if (inflightTeamsLoad) return inflightTeamsLoad
    set({ teamsLoading: true })
    const tryFetch = async () => {
      const data = await getTeams()
      set({ teams: data as TeamListResponse })
    }
    inflightTeamsLoad = (async () => {
      try {
        await tryFetch()
      } catch {
        // Retry once — covers the startup race where the IPC reports
        // the backend up but the HTTP route isn't accepting traffic yet.
        try {
          await tryFetch()
        } catch {
          set({ teams: null })
        }
      } finally {
        set({ teamsLoading: false })
        inflightTeamsLoad = null
      }
    })()
    return inflightTeamsLoad
  },
  fetchLiveAgents: async () => {
    if (inflightLiveAgentsLoad) return inflightLiveAgentsLoad
    const tryFetch = async () => {
      const data = await getLiveAgents()
      set({ agents: data })
    }
    inflightLiveAgentsLoad = (async () => {
      try {
        await tryFetch()
      } catch {
        // Same retry-once pattern as loadSessions/fetchTeams.
        try {
          await tryFetch()
        } catch (err2) {
          console.error('Failed to fetch live agents:', err2)
        }
      } finally {
        inflightLiveAgentsLoad = null
      }
    })()
    return inflightLiveAgentsLoad
  },
}))
