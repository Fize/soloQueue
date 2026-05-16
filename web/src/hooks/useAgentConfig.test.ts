import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAgentConfig } from './useAgentConfig'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useAgentConfig', () => {
  it('fetches config for given agent id', async () => {
    const config = { raw_config: '', system_prompt: '', name: '', description: '', model: '', group: '', is_leader: false, mcp_servers: [] }
    vi.mocked(api.getAgentConfig).mockResolvedValue(config)
    const { result } = renderHook(() => useAgentConfig('main'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.config).toEqual(config)
  })

  it('returns null when agentId is null', () => {
    const { result } = renderHook(() => useAgentConfig(null))
    expect(result.current.config).toBeNull()
    expect(result.current.loading).toBe(false)
  })

  it('handles fetch error', async () => {
    vi.mocked(api.getAgentConfig).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => useAgentConfig('main'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.config).toBeNull()
  })

  it('refetch works', async () => {
    const config = { raw_config: '', system_prompt: '', name: '', description: '', model: '', group: '', is_leader: false, mcp_servers: [] }
    vi.mocked(api.getAgentConfig).mockResolvedValue(config)
    const { result } = renderHook(() => useAgentConfig('main'))
    await waitFor(() => expect(result.current.config).toEqual(config))
    expect(result.current.refetch).toBeDefined()
  })
})
