import { describe, it, expect } from 'vitest'
import { renderHook } from '@testing-library/react'
import { useAgentStream } from './useAgentStream'

const mockRuntime = (streams: Record<string, any>) => ({
  phase: 'idle',
  prompt_tokens: 0,
  output_tokens: 0,
  cache_hit_tokens: 0,
  cache_miss_tokens: 0,
  context_pct: 0,
  current_iter: 0,
  content_deltas: 0,
  active_delegations: 0,
  total_agents: 0,
  running_agents: 0,
  idle_agents: 0,
  total_errors: 0,
  http_addr: ':8765',
  agent_streams: streams,
})

vi.mock('./useRuntime', () => ({
  useRuntime: vi.fn(),
}))

import { useRuntime } from './useRuntime'
const mockUseRuntime = vi.mocked(useRuntime)

describe('useAgentStream', () => {
  it('returns null when agentId is null', () => {
    mockUseRuntime.mockReturnValue(mockRuntime({}))
    const { result } = renderHook(() => useAgentStream(null))
    expect(result.current).toBeNull()
  })

  it('returns agent stream state', () => {
    const stream = { agent_id: 'a1', processing: true, segments: [], iteration: 1 }
    mockUseRuntime.mockReturnValue(mockRuntime({ a1: stream }))
    const { result } = renderHook(() => useAgentStream('a1'))
    expect(result.current).toEqual(stream)
  })

  it('returns null for unknown agent', () => {
    mockUseRuntime.mockReturnValue(mockRuntime({}))
    const { result } = renderHook(() => useAgentStream('unknown'))
    expect(result.current).toBeNull()
  })
})
