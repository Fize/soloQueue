import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { usePlans } from './usePlans'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

const mockPlan = {
  id: 'p1',
  title: 'Test',
  content: '',
  status: 'plan' as const,
  tags: '',
  creator: 'user',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('usePlans', () => {
  it('fetches plans on mount', async () => {
    vi.mocked(api.listPlans).mockResolvedValue([mockPlan])
    const { result } = renderHook(() => usePlans())
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.plans).toEqual([mockPlan])
  })

  it('handles fetch error', async () => {
    vi.mocked(api.listPlans).mockRejectedValue(new Error('fail'))
    const { result } = renderHook(() => usePlans())
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.error).toBe('fail')
  })

  it('groups plans by status', async () => {
    const plans = [
      { ...mockPlan, id: 'p1', status: 'plan' as const },
      { ...mockPlan, id: 'p2', status: 'running' as const },
      { ...mockPlan, id: 'p3', status: 'done' as const },
    ]
    vi.mocked(api.listPlans).mockResolvedValue(plans)
    const { result } = renderHook(() => usePlans())
    await waitFor(() => expect(result.current.plans).toHaveLength(3))
    expect(result.current.plansByStatus.plan).toHaveLength(1)
    expect(result.current.plansByStatus.running).toHaveLength(1)
    expect(result.current.plansByStatus.done).toHaveLength(1)
  })
})
