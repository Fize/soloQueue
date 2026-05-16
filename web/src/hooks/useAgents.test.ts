import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useAgents } from './useAgents'

const { mockSubscribe } = vi.hoisted(() => ({
  mockSubscribe: vi.fn().mockReturnValue(() => {}),
}))

vi.mock('@/lib/websocket', () => ({
  wsManager: { subscribe: mockSubscribe },
}))

beforeEach(() => {
  mockSubscribe.mockClear()
})

describe('useAgents', () => {
  it('subscribes to agents on mount', () => {
    renderHook(() => useAgents())
    expect(mockSubscribe).toHaveBeenCalledWith('agents', expect.any(Function))
  })

  it('returns agents data via subscription', async () => {
    let handler: (data: any) => void = () => {}
    mockSubscribe.mockImplementation((_type: string, h: any) => {
      handler = h
      return () => {}
    })

    const { result } = renderHook(() => useAgents())
    const agentsData = { agents: [{ id: 'a1', instance_id: 'i1', name: 'Agent1', state: 'idle' as const, model_id: '', group: '', is_leader: false, task_level: '', error_count: 0, last_error: '', pending_delegations: 0, mailbox_high: 0, mailbox_normal: 0 }], supervisors: [] }

    handler(agentsData)
    await waitFor(() => expect(result.current).toEqual(agentsData))
  })

  it('unsubscribes on unmount', () => {
    const unsub = vi.fn()
    mockSubscribe.mockReturnValue(unsub)
    const { unmount } = renderHook(() => useAgents())
    unmount()
    expect(unsub).toHaveBeenCalled()
  })
})
