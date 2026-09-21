import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { wsManager } from './websocket'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useChatStore } from '@/stores/chatStore'

// Track the last mock WebSocket instance so tests can simulate events
let mockWSServer: WSInstance | null = null

function rememberWebSocket(instance: WSInstance) {
  mockWSServer = instance
}

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
      this.close = vi.fn(() => {
        if (this.onclose) this.onclose()
      })
      rememberWebSocket(this)
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
  it('normalizes legacy request-keyed runtime entries to their logical session', async () => {
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
          'l1:req-legacy': {
            session_id: '',
            request_id: 'req-legacy',
            state: 'streaming',
            revision: 1,
            ctxwin_used: 0,
            ctxwin_limit: 0,
            delegating: false,
          },
        },
      },
    })

    expect(useChatStore.getState().streamingSessions.l1).toBe(true)
  })

  it('reconciles a silent terminal runtime snapshot once', async () => {
    const onRuntimeTerminal = vi.fn(() => {
      useChatStore.getState().reconcileRuntimeTerminal('req-silent', 'l1')
    })
    useChatStore.setState({
      activeRequests: {
        'req-silent': {
          requestId: 'req-silent',
          sessionId: 'l1',
          status: 'streaming',
        },
      },
      streamingSessions: { l1: true },
      routeSessions: {
        l1: { requestId: 'req-silent', sessionId: 'l1', taskLevel: 'research', modelId: 'model' },
      },
    })
    wsManager.registerChat('req-silent', { onRuntimeTerminal })
    await wsManager.connect()
    simulateOpen()

    const runtime = {
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
        'l1:req-silent': {
          session_id: 'l1',
          request_id: 'req-silent',
          state: 'idle',
          terminal_code: 'completed',
          revision: 1,
          ctxwin_used: 0,
          ctxwin_limit: 0,
          delegating: false,
        },
      },
    }
    simulateMessage({ type: 'state', runtime })
    simulateMessage({ type: 'state', runtime })

    expect(onRuntimeTerminal).toHaveBeenCalledTimes(1)
    expect(useChatStore.getState().activeRequests['req-silent']).toBeUndefined()
    expect(useChatStore.getState().streamingSessions.l1).toBe(false)
    expect(useChatStore.getState().routeSessions.l1).toBeUndefined()
  })

  it('cleans up a terminal external handler before the same request id starts again', async () => {
    useChatStore.setState({ messages: {} })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_accepted',
      origin: 'channel',
      request_id: 'req-reused-external',
      session_id: 'l1',
      prompt: '第一次渠道消息',
      timestamp: '2026-09-21T10:24:00.000Z',
    })
    expect(wsManager.hasChatHandler('req-reused-external')).toBe(true)

    const terminalRuntime = (revision: number, sessionId = 'l1') => ({
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
        [`${sessionId}:req-reused-external`]: {
          session_id: sessionId,
          request_id: 'req-reused-external',
          state: 'idle',
          terminal_code: 'completed',
          revision,
          ctxwin_used: 0,
          ctxwin_limit: 0,
          delegating: false,
        },
      },
    })

    simulateMessage({ type: 'state', runtime: terminalRuntime(1, 'l2:wrong') })
    expect(wsManager.hasChatHandler('req-reused-external')).toBe(true)

    simulateMessage({ type: 'state', runtime: terminalRuntime(1) })
    expect(wsManager.hasChatHandler('req-reused-external')).toBe(false)

    useChatStore.setState({ messages: { l1: [] } })
    simulateMessage({
      type: 'chat_accepted',
      origin: 'channel',
      request_id: 'req-reused-external',
      session_id: 'l1',
      prompt: '第二次渠道消息',
      timestamp: '2026-09-21T10:25:00.000Z',
    })
    expect(wsManager.hasChatHandler('req-reused-external')).toBe(true)
    expect(useChatStore.getState().messages.l1?.map((message) => message.role)).toEqual([
      'user',
      'assistant',
    ])
    expect(useChatStore.getState().messages.l1?.[0].segments[0]).toMatchObject({
      type: 'content',
      text: '第二次渠道消息',
    })

    simulateMessage({ type: 'state', runtime: terminalRuntime(2) })
    expect(wsManager.hasChatHandler('req-reused-external')).toBe(false)
  })

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

  it('keeps the local L1 route when runtime reports concurrent requests', async () => {
    const currentRoute = {
      requestId: 'req-current',
      sessionId: 'l1',
      taskLevel: 'research',
      modelId: 'current-model',
    }
    useChatStore.setState({
      routeSessions: { l1: currentRoute },
      activeRequests: {
        'req-current': {
          requestId: 'req-current',
          sessionId: 'l1',
          status: 'streaming',
          route: currentRoute,
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
        active_delegations: 1,
        total_agents: 1,
        running_agents: 1,
        idle_agents: 0,
        total_errors: 0,
        http_addr: ':8765',
        agent_streams: {},
        sessions: {
          'l1:req-old': {
            session_id: 'l1',
            request_id: 'req-old',
            state: 'streaming',
            revision: 2,
            ctxwin_used: 0,
            ctxwin_limit: 0,
            delegating: false,
          },
          'l1:req-current': {
            session_id: 'l1',
            request_id: 'req-current',
            state: 'delegating',
            revision: 2,
            ctxwin_used: 0,
            ctxwin_limit: 0,
            delegating: true,
          },
        },
      },
    })

    expect(useChatStore.getState().routeSessions.l1).toEqual(currentRoute)
    expect(useChatStore.getState().delegatingSessions.l1).toBe(true)
    expect(useChatStore.getState().streamingSessions.l1).toBe(true)
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
      task_type: 'L2-MediumMultiFile',
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

  it('hydrates an unowned channel stream into the active chat store', async () => {
    useChatStore.setState({ messages: {} })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_accepted',
      origin: 'channel',
      request_id: 'req-channel',
      session_id: 'l1',
      prompt: '来自 QQ 的消息',
    })
    simulateMessage({
      type: 'chat_route',
      origin: 'channel',
      request_id: 'req-channel',
      session_id: 'l1',
      task_type: 'general',
      model_id: 'channel-model',
    })
    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'req-channel',
      session_id: 'l1',
      delta: '实时回复',
    })

    const beforeDone = useChatStore.getState().messages.l1 || []
    expect(beforeDone.map((message) => message.role)).toEqual(['user', 'assistant'])
    expect(beforeDone[0].segments[0]).toMatchObject({ type: 'content', text: '来自 QQ 的消息' })
    expect(beforeDone[1].segments[0]).toMatchObject({ type: 'content', text: '实时回复' })
    expect(useChatStore.getState().routeSessions.l1).toMatchObject({
      requestId: 'req-channel',
      modelId: 'channel-model',
    })

    simulateMessage({
      type: 'chat_done',
      origin: 'channel',
      request_id: 'req-channel',
      session_id: 'l1',
    })
    expect(useChatStore.getState().activeRequests['req-channel']).toBeUndefined()
    expect(useChatStore.getState().streamingSessions.l1).toBe(false)

    simulateMessage({
      type: 'chat_accepted',
      origin: 'channel',
      request_id: 'req-channel-final',
      session_id: 'l1',
      prompt: '第二条消息',
    })
    simulateMessage({
      type: 'chat_done',
      origin: 'channel',
      request_id: 'req-channel-final',
      session_id: 'l1',
      content: '未错过的终结内容',
    })
    const finalMessages = useChatStore.getState().messages.l1 || []
    expect(finalMessages.at(-1)?.segments[0]).toMatchObject({
      type: 'content',
      text: '未错过的终结内容',
    })
  })

  it('does not invent a user message when a channel chunk arrives without an accepted prompt', async () => {
    useChatStore.setState({ messages: {} })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'req-channel-recovery',
      session_id: 'l1',
      delta: '恢复中的实时回复',
    })

    const messages = useChatStore.getState().messages.l1 || []
    expect(messages.map((message) => message.role)).toEqual(['assistant'])
    expect(messages[0].segments[0]).toMatchObject({
      type: 'content',
      text: '恢复中的实时回复',
    })
  })

  it('continues a hydrated channel turn in its existing assistant message', async () => {
    useChatStore.setState({
      messages: {
        l1: [
          {
            id: 'hist-user',
            requestId: 'req-hydrated-channel',
            role: 'user',
            segments: [{ type: 'content', text: '分析万华化学持仓' }],
            timestamp: '2026-09-21T04:00:39.487Z',
          },
          {
            id: 'hist-assistant',
            requestId: 'req-hydrated-channel',
            role: 'assistant',
            segments: [{ type: 'content', text: '先说事实。' }],
            timestamp: '2026-09-21T04:00:46.719Z',
          },
        ],
      },
    })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'tool_start',
      origin: 'channel',
      request_id: 'req-hydrated-channel',
      session_id: 'l1',
      timestamp: '2026-09-21T04:00:39.500Z',
      call_id: 'delegate-call',
      name: 'delegate',
      args: '{"target":"ray dalio"}',
    })

    const messages = useChatStore.getState().messages.l1 || []
    expect(messages).toHaveLength(2)
    expect(messages[0]).toMatchObject({ id: 'channel-user-req-hydrated-channel', role: 'user' })
    expect(messages[1]).toMatchObject({
      id: 'msg-req-hydrated-channel',
      role: 'assistant',
      segments: [
        { type: 'content', text: '先说事实。' },
        { type: 'tool_call', callId: 'delegate-call', name: 'delegate', done: false },
      ],
    })
  })

  it('resumes a delayed channel turn at its original position without duplicating messages', async () => {
    useChatStore.setState({
      messages: {
        l1: [
          {
            id: 'hist-old-user',
            role: 'user',
            segments: [{ type: 'content', text: '旧渠道请求' }],
            timestamp: '2026-09-21T10:24:00.000Z',
          },
          {
            id: 'msg-req-delayed-channel',
            role: 'assistant',
            segments: [{ type: 'content', text: '旧回复片段' }],
            timestamp: '2026-09-21T10:24:00.000Z',
          },
          {
            id: 'hist-new-user',
            role: 'user',
            segments: [{ type: 'content', text: '新请求' }],
            timestamp: '2026-09-21T10:32:00.000Z',
          },
          {
            id: 'hist-new-assistant',
            role: 'assistant',
            segments: [{ type: 'content', text: '新回复' }],
            timestamp: '2026-09-21T10:32:00.000Z',
          },
        ],
      },
    })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'req-delayed-channel',
      session_id: 'l1',
      timestamp: '2026-09-21T10:24:00.000Z',
      delta: '，继续输出',
    })
    simulateMessage({
      type: 'chat_done',
      origin: 'channel',
      request_id: 'req-delayed-channel',
      session_id: 'l1',
      timestamp: '2026-09-21T10:24:00.000Z',
      content: '旧回复片段，继续输出',
    })

    const messages = useChatStore.getState().messages.l1 || []
    expect(messages.map((message) => message.id)).toEqual([
      'hist-old-user',
      'msg-req-delayed-channel',
      'hist-new-user',
      'hist-new-assistant',
    ])
    expect(messages[1].segments).toEqual([
      { type: 'content', text: '旧回复片段，继续输出' },
    ])
    expect(messages[0].timestamp).toBe('2026-09-21T10:24:00.000Z')
    expect(messages[1].timestamp).toBe('2026-09-21T10:24:00.000Z')
  })

  it('does not merge a delayed channel request into a nearby unrelated turn', async () => {
    useChatStore.setState({
      messages: {
        l1: [
          {
            id: 'new-user',
            role: 'user',
            segments: [{ type: 'content', text: '半秒后的新请求' }],
            timestamp: '2026-09-21T10:24:00.500Z',
          },
          {
            id: 'new-assistant',
            role: 'assistant',
            segments: [{ type: 'content', text: '新请求的回复' }],
            timestamp: '2026-09-21T10:24:00.500Z',
          },
        ],
      },
    })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'old',
      session_id: 'l1',
      timestamp: '2026-09-21T10:24:00.000Z',
      delta: '旧请求迟到的回复',
    })

    const messages = useChatStore.getState().messages.l1 || []
    expect(messages.map((message) => message.id)).toEqual([
      'msg-old',
      'new-user',
      'new-assistant',
    ])
    expect(messages[0].segments).toEqual([{ type: 'content', text: '旧请求迟到的回复' }])
    expect(messages[2].segments).toEqual([{ type: 'content', text: '新请求的回复' }])
  })

  it('inserts a delayed channel assistant before later user turns', async () => {
    useChatStore.setState({
      messages: {
        l1: [
          {
            id: 'hist-new-user',
            role: 'user',
            segments: [{ type: 'content', text: '新请求' }],
            timestamp: '2026-09-21T10:32:00.000Z',
          },
          {
            id: 'hist-new-assistant',
            role: 'assistant',
            segments: [{ type: 'content', text: '新回复' }],
            timestamp: '2026-09-21T10:32:00.000Z',
          },
        ],
      },
    })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'req-delayed-insert',
      session_id: 'l1',
      timestamp: '2026-09-21T10:24:00.000Z',
      delta: '迟到的旧回复',
    })

    const messages = useChatStore.getState().messages.l1 || []
    expect(messages.map((message) => message.id)).toEqual([
      'msg-req-delayed-insert',
      'hist-new-user',
      'hist-new-assistant',
    ])
    expect(messages[0]).toMatchObject({
      role: 'assistant',
      timestamp: '2026-09-21T10:24:00.000Z',
      segments: [{ type: 'content', text: '迟到的旧回复' }],
    })
  })

  it('rejects frames that do not match an external channel handler owner', async () => {
    useChatStore.setState({ messages: {} })
    await wsManager.connect()
    simulateOpen()

    simulateMessage({
      type: 'chat_accepted',
      origin: 'channel',
      request_id: 'req-owned-channel',
      session_id: 'l1',
      prompt: '真实渠道消息',
      timestamp: '2026-09-21T10:24:00.000Z',
    })
    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'req-owned-channel',
      session_id: 'l2:wrong',
      delta: '错误会话',
    })
    simulateMessage({
      type: 'chat_chunk',
      request_id: 'req-owned-channel',
      session_id: 'l1',
      delta: '缺少来源',
    })
    simulateMessage({
      type: 'chat_chunk',
      origin: 'browser',
      request_id: 'req-owned-channel',
      session_id: 'l1',
      delta: '错误来源',
    })
    simulateMessage({
      type: 'chat_chunk',
      origin: 'channel',
      request_id: 'req-owned-channel',
      session_id: 'l1',
      delta: '正确回复',
    })

    const messages = useChatStore.getState().messages.l1 || []
    expect(messages).toHaveLength(2)
    expect(messages[1].segments).toEqual([{ type: 'content', text: '正确回复' }])
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

  it('marks transiently disconnected chat handlers for runtime recovery', async () => {
    const onChunk = vi.fn()
    wsManager.registerChat('req-recover', { onChunk })
    await wsManager.connect()
    simulateOpen()
    expect(wsManager.hasChatHandler('req-recover')).toBe(true)

    mockWSServer?.onclose?.({ code: 1006, reason: 'network reset' })
    expect(wsManager.hasChatHandler('req-recover')).toBe(false)

    // A fresh event proves the request stream has resumed; normal handler
    // ownership can then suppress redundant runtime snapshot hydration.
    await wsManager.connect()
    simulateOpen()
    simulateMessage({ type: 'chat_chunk', request_id: 'req-recover', delta: 'resumed' })
    expect(onChunk).toHaveBeenCalledWith('resumed')
    expect(wsManager.hasChatHandler('req-recover')).toBe(true)
    wsManager.disconnect()
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
