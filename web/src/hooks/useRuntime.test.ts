import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useRuntime } from './useRuntime'

const { mockSubscribe } = vi.hoisted(() => ({
  mockSubscribe: vi.fn().mockReturnValue(() => {}),
}))

vi.mock('@/lib/websocket', () => ({
  wsManager: { subscribe: mockSubscribe },
}))

beforeEach(() => {
  mockSubscribe.mockClear()
})

describe('useRuntime', () => {
  it('subscribes to runtime on mount', () => {
    renderHook(() => useRuntime())
    expect(mockSubscribe).toHaveBeenCalledWith('runtime', expect.any(Function))
  })

  it('returns runtime data via subscription', async () => {
    let handler: (data: any) => void = () => {}
    mockSubscribe.mockImplementation((_type: string, h: any) => {
      handler = h
      return () => {}
    })

    const { result } = renderHook(() => useRuntime())
    const runtimeData = {
      phase: 'processing', prompt_tokens: 100, output_tokens: 50,
      cache_hit_tokens: 0, cache_miss_tokens: 0, context_pct: 0,
      current_iter: 1, content_deltas: 0, active_delegations: 0,
      total_agents: 2, running_agents: 1, idle_agents: 1, total_errors: 0,
      http_addr: ':8765', agent_streams: {},
    }
    handler(runtimeData)
    await waitFor(() => expect(result.current).toEqual(runtimeData))
  })

  it('unsubscribes on unmount', () => {
    const unsub = vi.fn()
    mockSubscribe.mockReturnValue(unsub)
    const { unmount } = renderHook(() => useRuntime())
    unmount()
    expect(unsub).toHaveBeenCalled()
  })
})
