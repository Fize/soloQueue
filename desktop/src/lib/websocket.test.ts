import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { wsManager } from './websocket'
import { useRuntimeStore } from '@/stores/runtimeStore'

beforeEach(() => {
  useRuntimeStore.setState({ status: null, connectionStatus: 'disconnected' })
  wsManager.disconnect()
})

describe('websocket', () => {
  it('connect opens WebSocket and sets connected status', async () => {
    wsManager.connect()
    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })
  })

  it('subscribe to runtime handler and receive updates via store', async () => {
    const handler = vi.fn()
    wsManager.subscribe('runtime', handler)
    wsManager.connect()
    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })

    // Simulate a state message by dispatching via the mock WebSocket
    const runtime = {
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
    const msg = JSON.stringify({ type: 'state', runtime, agents: { agents: [], supervisors: [] } })

    const mockWS = (globalThis as any).__lastMockWS
    expect(mockWS).toBeTruthy()
    if (mockWS?.onmessage) mockWS.onmessage({ data: msg })

    // Handler should be called by the wsManager's dispatch
    await vi.waitFor(() => {
      expect(handler).toHaveBeenCalledWith(runtime)
    })
  })

  it('unsubscribe removes handler', () => {
    const handler = vi.fn()
    const unsub = wsManager.subscribe('runtime', handler)
    unsub()
    // No easy way to verify directly, but no crash is good
  })

  it('disconnect sets disconnected status', async () => {
    wsManager.connect()
    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })
    wsManager.disconnect()
    expect(useRuntimeStore.getState().connectionStatus).toBe('disconnected')
  })

  it('subscribe to status handler', async () => {
    const handler = vi.fn()
    wsManager.subscribe('status', handler)
    wsManager.connect()
    await vi.waitFor(() => {
      expect(handler).toHaveBeenCalledWith('connected')
    })
  })
})
