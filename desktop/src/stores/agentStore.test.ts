import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useAgentStore } from './agentStore'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

const mockProfile = { soul: 'soul', rules: 'rules' }
const mockConfig = {
  raw_config: '',
  system_prompt: '',
  name: '',
  description: '',
  model: '',
  group: '',
  is_leader: false,
  mcp_servers: [],
}

beforeEach(() => {
  useAgentStore.setState({
    agents: null,
    profile: null,
    profileLoading: false,
    config: null,
    configLoading: false,
    teams: null,
    teamsLoading: false,
  })
  vi.clearAllMocks()
})

describe('agentStore', () => {
  it('setAgents updates agents', () => {
    const data = { agents: [], supervisors: [] }
    useAgentStore.getState().setAgents(data)
    expect(useAgentStore.getState().agents).toEqual(data)
  })

  it('fetchProfile succeeds', async () => {
    vi.mocked(api.getAgentProfile).mockResolvedValue(mockProfile)
    const promise = useAgentStore.getState().fetchProfile('main')
    expect(useAgentStore.getState().profileLoading).toBe(true)
    await promise
    expect(useAgentStore.getState().profile).toEqual(mockProfile)
    expect(useAgentStore.getState().profileLoading).toBe(false)
  })

  it('fetchProfile fails gracefully', async () => {
    vi.mocked(api.getAgentProfile).mockRejectedValue(new Error('fail'))
    await useAgentStore.getState().fetchProfile('main')
    expect(useAgentStore.getState().profile).toBeNull()
    expect(useAgentStore.getState().profileLoading).toBe(false)
  })

  it('fetchConfig succeeds', async () => {
    vi.mocked(api.getAgentConfig).mockResolvedValue(mockConfig)
    const promise = useAgentStore.getState().fetchConfig('main')
    expect(useAgentStore.getState().configLoading).toBe(true)
    await promise
    expect(useAgentStore.getState().config).toEqual(mockConfig)
  })

  it('updateProfile updates profile', async () => {
    vi.mocked(api.updateAgentProfile).mockResolvedValue(mockProfile)
    await useAgentStore.getState().updateProfile('main', { soul: 'new' })
    expect(useAgentStore.getState().profile).toEqual(mockProfile)
  })

  it('fetchTeams succeeds', async () => {
    const teamsData = { teams: [{ name: 'team1', description: '', agents: [] }] }
    vi.mocked(api.getTeams).mockResolvedValue(teamsData)
    await useAgentStore.getState().fetchTeams()
    expect(useAgentStore.getState().teams).toEqual(teamsData)
    expect(useAgentStore.getState().teamsLoading).toBe(false)
  })

  it('fetchTeams fails gracefully', async () => {
    vi.mocked(api.getTeams).mockRejectedValue(new Error('fail'))
    await useAgentStore.getState().fetchTeams()
    expect(useAgentStore.getState().teams).toBeNull()
  })
})
