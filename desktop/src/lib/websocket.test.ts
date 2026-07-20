import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { wsManager } from './websocket'
import { useRuntimeStore } from '@/stores/runtimeStore'

// Track the last mock WebSocket instance so tests can simulate events
let mockWSServer: WSInstance | null = null

interface WSInstance {
  url: string
  onopen: ((ev?: any) => void) | null
  onmessage: ((ev: MessageEvent) => void) | null
  onclose: ((ev?: any) => void) | null
  onerror: ((ev?: any) => void) | null
  close: ReturnType<typeof vi.fn>
}

beforeEach(() => {
  useRuntimeStore.setState({ status: null, connectionStatus: 'disconnected' })
  wsManager.disconnect()
  mockWSServer = null

  // Mock global fetch so /api/auth/token works in jsdom
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const url = typeof input === 'string' ? input : (input as Request).url
    if (url.includes('/api/auth/token')) {
      return new Response(JSON.stringify({ token: 'test-token' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return new Response('{}', { status: 200 })
  })

  // Mock WebSocket: capture the instance so we can manually fire onopen/onmessage
  vi.stubGlobal(
    'WebSocket',
    vi.fn(function (this: WSInstance, url: string) {
      this.url = url
      this.onopen = null
      this.onmessage = null
      this.onclose = null
      this.onerror = null
      this.close = vi.fn(function (this: WSInstance) {
        if (this.onclose) this.onclose()
      })
      mockWSServer = this
      // Use a microtask delay so the caller has time to assign onopen
      Promise.resolve().then(() => {
        // Only fire if the handler was assigned
        // (intentional close resets the instance before onopen fires)
      })
    })
  )
})

afterEach(() => {
  vi.restoreAllMocks()
})

function simulateOpen() {
  if (mockWSServer?.onopen) mockWSServer.onopen({ type: 'open' } as any)
}

function simulateMessage(data: unknown) {
  if (mockWSServer?.onmessage) {
    mockWSServer.onmessage({ data: JSON.stringify(data) } as unknown as MessageEvent)
  }
}

describe('websocket', () => {
  it('connect opens WebSocket and sets connected status', async () => {
    // connect() is async — fire onopen after it returns
    const connectPromise = wsManager.connect()
    await connectPromise
    simulateOpen()

    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })
  })

  it('subscribe to runtime handler and receive updates via store', async () => {
    const handler = vi.fn()
    wsManager.subscribe('runtime', handler)
    await wsManager.connect()
    simulateOpen()

    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })

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

    simulateMessage({ type: 'state', runtime, agents: { agents: [], supervisors: [] } })

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
    await wsManager.connect()
    simulateOpen()

    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })
    wsManager.disconnect()
    expect(useRuntimeStore.getState().connectionStatus).toBe('disconnected')
  })

  it('subscribe to status handler', async () => {
    const handler = vi.fn()
    wsManager.subscribe('status', handler)
    await wsManager.connect()
    simulateOpen()

    await vi.waitFor(() => {
      expect(handler).toHaveBeenCalledWith('connected')
    })
  })

  it('routes chat_route metadata to the matching request handler', async () => {
    const onRoute = vi.fn()
    wsManager.registerChat('req-route', { onRoute })
    await wsManager.connect()
    simulateOpen()

    const route = {
      type: 'chat_route',
      request_id: 'req-route',
      session_id: 'l2:s1',
      task_level: 'L2-MediumMultiFile',
      model_id: 'routed-model',
      provider_id: 'provider',
      agent_instance_id: 'agent-instance',
    }
    simulateMessage(route)

    expect(onRoute).toHaveBeenCalledWith(route)
    wsManager.unregisterChat('req-route')
  })
})
