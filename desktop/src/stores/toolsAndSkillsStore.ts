import { create } from 'zustand'
import { getTools, getSkills, fetchStoreSkills as getStoreSkills } from '@/lib/api'
import type { ToolListResponse, SkillListResponse } from '@/types'

interface ToolsAndSkillsState {
  tools: ToolListResponse | null
  toolsLoading: boolean
  fetchTools: () => Promise<void>

  skills: SkillListResponse | null
  skillsLoading: boolean
  fetchSkills: () => Promise<void>

  storeSkills: SkillListResponse | null
  storeSkillsLoading: boolean
  fetchStoreSkills: () => Promise<void>
}

export const useToolsAndSkillsStore = create<ToolsAndSkillsState>((set) => ({
  tools: null,
  toolsLoading: false,
  fetchTools: async () => {
    set({ toolsLoading: true })
    try {
      const data = await getTools()
      set({ tools: data, toolsLoading: false })
    } catch {
      set({ tools: null, toolsLoading: false })
    }
  },

  skills: null,
  skillsLoading: false,
  fetchSkills: async () => {
    set({ skillsLoading: true })
    try {
      const data = await getSkills()
      set({ skills: data, skillsLoading: false })
    } catch {
      set({ skills: null, skillsLoading: false })
    }
  },

  storeSkills: null,
  storeSkillsLoading: false,
  fetchStoreSkills: async () => {
    set({ storeSkillsLoading: true })
    try {
      const data = await getStoreSkills()
      set({ storeSkills: data, storeSkillsLoading: false })
    } catch {
      set({ storeSkills: null, storeSkillsLoading: false })
    }
  },
}))
