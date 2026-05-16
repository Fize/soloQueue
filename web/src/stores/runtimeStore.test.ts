import { describe, it, expect, beforeEach } from 'vitest'
import { useRuntimeStore } from './runtimeStore'

beforeEach(() => {
  useRuntimeStore.setState({ status: null, connectionStatus: 'disconnected' })
})

describe('runtimeStore', () => {
  it('defaults to disconnected', () => {
    expect(useRuntimeStore.getState().connectionStatus).toBe('disconnected')
    expect(useRuntimeStore.getState().status).toBeNull()
  })

  it('setStatus updates runtime status', () => {
    const status = {
      phase: 'processing',
      prompt_tokens: 100,
      output_tokens: 50,
      cache_hit_tokens: 0,
      cache_miss_tokens: 0,
      context_pct: 0,
      current_iter: 1,
      content_deltas: 0,
      active_delegations: 0,
      total_agents: 2,
      running_agents: 1,
      idle_agents: 1,
      total_errors: 0,
      http_addr: ':8765',
      agent_streams: {},
    }
    useRuntimeStore.getState().setStatus(status)
    expect(useRuntimeStore.getState().status).toEqual(status)
  })

  it('setConnectionStatus updates connection status', () => {
    useRuntimeStore.getState().setConnectionStatus('connected')
    expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    useRuntimeStore.getState().setConnectionStatus('reconnecting')
    expect(useRuntimeStore.getState().connectionStatus).toBe('reconnecting')
  })
})
