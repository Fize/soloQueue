import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAgentProfile } from './useAgentProfile'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useAgentProfile', () => {
  it('fetches profile for given agent id', async () => {
    const profile = { soul: 'soul', rules: 'rules' }
    vi.mocked(api.getAgentProfile).mockResolvedValue(profile)
    const { result } = renderHook(() => useAgentProfile('main'))
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.profile).toEqual(profile)
  })

  it('returns null when agentId is null', async () => {
    const { result } = renderHook(() => useAgentProfile(null))
    expect(result.current.profile).toBeNull()
    expect(result.current.loading).toBe(false)
  })

  it('handles fetch error', async () => {
    vi.mocked(api.getAgentProfile).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => useAgentProfile('main'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.profile).toBeNull()
  })
})
