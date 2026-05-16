import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useToolsAndSkillsStore } from './toolsAndSkillsStore'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

const mockTools = { tools: [{ name: 'Bash', description: '', parameters: null }], total: 1 }
const mockSkills = { skills: [], total: 0 }

beforeEach(() => {
  useToolsAndSkillsStore.setState({
    tools: null, toolsLoading: false,
    skills: null, skillsLoading: false,
  })
  vi.clearAllMocks()
})

describe('toolsAndSkillsStore', () => {
  it('fetchTools loads tools', async () => {
    vi.mocked(api.getTools).mockResolvedValue(mockTools)
    const promise = useToolsAndSkillsStore.getState().fetchTools()
    expect(useToolsAndSkillsStore.getState().toolsLoading).toBe(true)
    await promise
    expect(useToolsAndSkillsStore.getState().tools).toEqual(mockTools)
    expect(useToolsAndSkillsStore.getState().toolsLoading).toBe(false)
  })

  it('fetchTools fails gracefully', async () => {
    vi.mocked(api.getTools).mockRejectedValue(new Error('fail'))
    await useToolsAndSkillsStore.getState().fetchTools()
    expect(useToolsAndSkillsStore.getState().tools).toBeNull()
  })

  it('fetchSkills loads skills', async () => {
    vi.mocked(api.getSkills).mockResolvedValue(mockSkills)
    await useToolsAndSkillsStore.getState().fetchSkills()
    expect(useToolsAndSkillsStore.getState().skills).toEqual(mockSkills)
  })

  it('fetchSkills fails gracefully', async () => {
    vi.mocked(api.getSkills).mockRejectedValue(new Error('fail'))
    await useToolsAndSkillsStore.getState().fetchSkills()
    expect(useToolsAndSkillsStore.getState().skills).toBeNull()
  })
})
