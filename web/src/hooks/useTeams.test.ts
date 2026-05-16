import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useTeams } from './useTeams'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

beforeEach(() => {
  vi.clearAllMocks()
})

describe('useTeams', () => {
  it('fetches teams on mount', async () => {
    const teamsData = { teams: [{ name: 'team1', description: '', agents: [] }] }
    vi.mocked(api.getTeams).mockResolvedValue(teamsData)
    const { result } = renderHook(() => useTeams())
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toEqual(teamsData)
  })

  it('handles fetch error', async () => {
    vi.mocked(api.getTeams).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => useTeams())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.data).toBeNull()
  })
})
