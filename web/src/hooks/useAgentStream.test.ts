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

  it('does not select an unscoped agent stream', () => {
    const stream = { agent_id: 'a1', processing: true, segments: [], iteration: 1 }
    mockUseRuntime.mockReturnValue(mockRuntime({ a1: stream }))
    const { result } = renderHook(() => useAgentStream('a1'))
    expect(result.current).toBeNull()
  })

  it('returns null for unknown agent', () => {
    mockUseRuntime.mockReturnValue(mockRuntime({}))
    const { result } = renderHook(() => useAgentStream('unknown'))
    expect(result.current).toBeNull()
  })

  it('separates two delegations served by the same agent instance', () => {
    const first = { agent_id: 'child-1', request_id: 'req-parent', dispatch_id: 'dlg-a', processing: false, segments: [], iteration: 1 }
    const second = { agent_id: 'child-1', request_id: 'req-parent', dispatch_id: 'dlg-b', processing: true, segments: [], iteration: 2 }
    mockUseRuntime.mockReturnValue(mockRuntime({
      'child-1\u0000req-parent\u0000dlg-a': first,
      'child-1\u0000req-parent\u0000dlg-b': second,
    }))
    const firstCard = renderHook(() => useAgentStream('child-1', 'req-parent', 'dlg-a'))
    const secondCard = renderHook(() => useAgentStream('child-1', 'req-parent', 'dlg-b'))
    expect(firstCard.result.current).toEqual(first)
    expect(secondCard.result.current).toEqual(second)
  })

  it('does not fall back to another delegation when the dispatch has no stream', () => {
    const other = { agent_id: 'child-1', request_id: 'req-parent', dispatch_id: 'dlg-b', processing: true, segments: [], iteration: 2 }
    mockUseRuntime.mockReturnValue(mockRuntime({ 'child-1\u0000req-parent\u0000dlg-b': other }))
    const { result } = renderHook(() => useAgentStream('child-1', 'req-parent', 'dlg-a'))
    expect(result.current).toBeNull()
  })

  it('selects the stream belonging to the requested turn', () => {
    const oldStream = { agent_id: 'a1', request_id: 'req-old', processing: true, segments: [], iteration: 1 }
    const newStream = { agent_id: 'a1', request_id: 'req-new', processing: true, segments: [], iteration: 2 }
    mockUseRuntime.mockReturnValue(mockRuntime({
      'a1\u0000req-old': oldStream,
      'a1\u0000req-new': newStream,
    }))
    const { result } = renderHook(() => useAgentStream('a1', 'req-new'))
    expect(result.current).toEqual(newStream)
  })
})
