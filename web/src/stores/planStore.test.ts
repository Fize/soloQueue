import { describe, it, expect, beforeEach, vi } from 'vitest'
import { usePlanStore } from './planStore'
import * as api from '@/lib/api'

vi.mock('@/lib/api')

const mockPlan = {
  id: 'p1',
  title: 'Test Plan',
  content: '# Test',
  status: 'plan' as const,
  tags: 'test',
  creator: 'user',
  created_at: '2024-01-01T00:00:00Z',
  updated_at: '2024-01-01T00:00:00Z',
}

beforeEach(() => {
  usePlanStore.setState({ plans: [], loading: false, error: null })
  vi.clearAllMocks()
})

describe('planStore', () => {
  it('fetchPlans sets loading then plans on success', async () => {
    vi.mocked(api.listPlans).mockResolvedValue([mockPlan])
    const promise = usePlanStore.getState().fetchPlans()
    expect(usePlanStore.getState().loading).toBe(true)
    await promise
    expect(usePlanStore.getState().plans).toEqual([mockPlan])
    expect(usePlanStore.getState().loading).toBe(false)
    expect(usePlanStore.getState().error).toBeNull()
  })

  it('fetchPlans sets error on failure', async () => {
    vi.mocked(api.listPlans).mockRejectedValue(new Error('network error'))
    await usePlanStore.getState().fetchPlans()
    expect(usePlanStore.getState().loading).toBe(false)
    expect(usePlanStore.getState().error).toBe('network error')
    expect(usePlanStore.getState().plans).toEqual([])
  })

  it('movePlan does optimistic update and rolls back on failure', async () => {
    usePlanStore.setState({ plans: [{ ...mockPlan, id: 'p1', status: 'plan' }] })
    vi.mocked(api.updatePlanStatus).mockRejectedValue(new Error('fail'))

    await usePlanStore.getState().movePlan('p1', 'running')

    // After rollback, status should be back to 'plan'
    expect(usePlanStore.getState().plans[0].status).toBe('plan')
  })

  it('movePlan succeeds', async () => {
    usePlanStore.setState({ plans: [{ ...mockPlan, id: 'p1', status: 'plan' }] })
    vi.mocked(api.updatePlanStatus).mockResolvedValue({ ...mockPlan, status: 'running' })

    await usePlanStore.getState().movePlan('p1', 'running')

    expect(usePlanStore.getState().plans[0].status).toBe('running')
  })

  it('createPlan prepends to list', async () => {
    const newPlan = { ...mockPlan, id: 'p2' }
    vi.mocked(api.createPlan).mockResolvedValue(newPlan)
    usePlanStore.setState({ plans: [mockPlan] })

    const result = await usePlanStore.getState().createPlan({ title: 'New' })
    expect(result).toEqual(newPlan)
    expect(usePlanStore.getState().plans).toHaveLength(2)
    expect(usePlanStore.getState().plans[0].id).toBe('p2')
  })

  it('updatePlan does optimistic update and replaces with server response', async () => {
    usePlanStore.setState({ plans: [mockPlan] })
    const updated = { ...mockPlan, title: 'Updated' }
    vi.mocked(api.updatePlan).mockResolvedValue(updated)

    const result = await usePlanStore.getState().updatePlan('p1', { title: 'Updated' })
    expect(result).toEqual(updated)
    expect(usePlanStore.getState().plans[0].title).toBe('Updated')
  })

  it('updatePlan rolls back on failure', async () => {
    usePlanStore.setState({ plans: [mockPlan] })
    vi.mocked(api.updatePlan).mockRejectedValue(new Error('fail'))

    await expect(usePlanStore.getState().updatePlan('p1', { title: 'Updated' })).rejects.toThrow('Failed to update plan')
    expect(usePlanStore.getState().plans[0].title).toBe('Test Plan')
  })

  it('deletePlan removes optimistically and rolls back', async () => {
    usePlanStore.setState({ plans: [mockPlan, { ...mockPlan, id: 'p2' }] })
    vi.mocked(api.deletePlan).mockRejectedValue(new Error('fail'))

    await expect(usePlanStore.getState().deletePlan('p1')).rejects.toThrow('Failed to delete plan')
    expect(usePlanStore.getState().plans).toHaveLength(2)
  })

  it('deletePlan succeeds', async () => {
    usePlanStore.setState({ plans: [mockPlan, { ...mockPlan, id: 'p2' }] })
    vi.mocked(api.deletePlan).mockResolvedValue(undefined)

    await usePlanStore.getState().deletePlan('p1')
    expect(usePlanStore.getState().plans).toHaveLength(1)
    expect(usePlanStore.getState().plans[0].id).toBe('p2')
  })
})
