import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { wsManager } from './websocket'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useChatStore } from '@/stores/chatStore'

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
  localStorage.clear()
  useRuntimeStore.setState({ status: null, connectionStatus: 'disconnected' })
  useConnectionStore.setState({
    mode: 'local',
    remoteUrl: '',
    backendReady: true,
    backendStatus: { running: true, pid: null, uptime: 0 },
  })
  useChatStore.setState({ activeRequests: {}, routeSessions: {}, streamingSessions: {} })
  wsManager.disconnect()
  mockWSServer = null

  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 200 }))

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
  it('does not reuse route metadata from a different runtime request', async () => {
    useChatStore.setState({
      routeSessions: {
        l1: {
          requestId: 'req-old',
          sessionId: 'l1',
          taskLevel: 'research',
          modelId: 'old-model',
        },
      },
    })

    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'state',
      runtime: {
        phase: 'processing',
        prompt_tokens: 0,
        output_tokens: 0,
        cache_hit_tokens: 0,
        cache_miss_tokens: 0,
        context_pct: 0,
        current_tokens: 0,
        max_tokens: 0,
        current_iter: 0,
        content_deltas: 0,
        active_delegations: 0,
        total_agents: 1,
        running_agents: 1,
        idle_agents: 0,
        total_errors: 0,
        http_addr: ':8765',
        agent_streams: {},
        sessions: {
          l1: {
            session_id: 'l1',
            request_id: 'req-new',
            state: 'streaming',
            revision: 1,
            ctxwin_used: 0,
            ctxwin_limit: 0,
            delegating: false,
          },
        },
      },
    })

    expect(useChatStore.getState().routeSessions.l1).toMatchObject({
      requestId: 'req-new',
      sessionId: 'l1',
      taskLevel: '',
      modelId: '',
    })
  })

  it('clears a persisted route when a refreshed session is already idle', async () => {
    const route = {
      requestId: 'req-completed',
      sessionId: 'l1',
      taskLevel: 'engineering',
      modelId: 'completed-model',
    }
    localStorage.setItem('soloqueue_active_chat_routes', JSON.stringify({ l1: route }))
    useChatStore.setState({ routeSessions: { l1: route }, streamingSessions: {} })

    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'state',
      runtime: {
        phase: 'idle',
        prompt_tokens: 0,
        output_tokens: 0,
        cache_hit_tokens: 0,
        cache_miss_tokens: 0,
        context_pct: 0,
        current_tokens: 0,
        max_tokens: 0,
        current_iter: 0,
        content_deltas: 0,
        active_delegations: 0,
        total_agents: 1,
        running_agents: 0,
        idle_agents: 1,
        total_errors: 0,
        http_addr: ':8765',
        agent_streams: {},
        sessions: {
          l1: {
            session_id: 'l1',
            state: 'idle',
            revision: 1,
            ctxwin_used: 0,
            ctxwin_limit: 0,
            delegating: false,
          },
        },
      },
    })

    expect(useChatStore.getState().routeSessions.l1).toBeUndefined()
    expect(localStorage.getItem('soloqueue_active_chat_routes')).toBeNull()
  })

  it('connect opens WebSocket and sets connected status', async () => {
    // connect() is async — fire onopen after it returns
    const connectPromise = wsManager.connect()
    await connectPromise
    simulateOpen()

    await vi.waitFor(() => {
      expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    })
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()
  })

  it('opens a remote WebSocket without an application token', async () => {
    useConnectionStore.setState({ mode: 'remote', remoteUrl: 'https://remote.example' })

    await wsManager.connect()

    expect(mockWSServer?.url).toBe('wss://remote.example/ws')
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()
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
    expect(wsManager.hasChatHandler('req-route')).toBe(true)
    simulateMessage(route)

    expect(onRoute).toHaveBeenCalledWith(route)
    wsManager.unregisterChat('req-route')
    expect(wsManager.hasChatHandler('req-route')).toBe(false)
  })

  it('routes chat_accepted to the matching request handler', async () => {
    const onAccepted = vi.fn()
    wsManager.registerChat('req-accepted', { onAccepted })
    await wsManager.connect()
    simulateOpen()

    const accepted = {
      type: 'chat_accepted',
      request_id: 'req-accepted',
      session_id: 'l2:s1',
    }
    simulateMessage(accepted)

    expect(onAccepted).toHaveBeenCalledWith(accepted)
  })

  it('reports chat_send as unsent when the socket is not open', () => {
    const sent = wsManager.send({
      type: 'chat_send',
      request_id: 'req-disconnected',
      session_id: 'l2:s1',
      prompt: 'hello',
    })

    expect(sent).toBe(false)
  })

  it('reports a message-too-large close immediately to chat handlers', async () => {
    const onClose = vi.fn()
    wsManager.registerChat('req-too-large', { onClose })
    await wsManager.connect()
    simulateOpen()

    mockWSServer?.onclose?.({ code: 1009, reason: 'Message Too Large' })

    expect(onClose).toHaveBeenCalledWith(1009, false)
  })
})
