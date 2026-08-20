import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useMCPConfigStore } from './mcpConfigStore'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

const mockConfig = { mcpServers: {} }

beforeEach(() => {
  useMCPConfigStore.setState({ config: null, saving: false, error: null })
  vi.clearAllMocks()
})

describe('mcpConfigStore', () => {
  it('fetch loads config', async () => {
    vi.mocked(api.getMCPConfig).mockResolvedValue(mockConfig)
    await useMCPConfigStore.getState().fetch()
    expect(useMCPConfigStore.getState().config).toEqual(mockConfig)
    expect(useMCPConfigStore.getState().error).toBeNull()
  })

  it('fetch handles error', async () => {
    vi.mocked(api.getMCPConfig).mockRejectedValue(new Error('fail'))
    await useMCPConfigStore.getState().fetch()
    expect(useMCPConfigStore.getState().config).toBeNull()
  })

  it('save sets saving and updates config', async () => {
    vi.mocked(api.updateMCPConfig).mockResolvedValue(mockConfig)
    const promise = useMCPConfigStore.getState().save(mockConfig)
    expect(useMCPConfigStore.getState().saving).toBe(true)
    const result = await promise
    expect(result).toEqual(mockConfig)
    expect(useMCPConfigStore.getState().saving).toBe(false)
  })

  it('save handles error', async () => {
    vi.mocked(api.updateMCPConfig).mockRejectedValue(new Error('save failed'))
    await expect(useMCPConfigStore.getState().save(mockConfig)).rejects.toThrow()
    expect(useMCPConfigStore.getState().error).toBe('save failed')
    expect(useMCPConfigStore.getState().saving).toBe(false)
  })
})
