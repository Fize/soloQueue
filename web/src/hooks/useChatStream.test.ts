import { describe, it, expect, beforeEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useChatStream } from './useChatStream'
import { useChatStore } from '@/stores/chatStore'
import { wsManager } from '@/lib/websocket'
import { fetchSessionHistory } from '@/lib/api'

vi.mock('@/lib/websocket', () => ({
  wsManager: {
    send: vi.fn(),
    registerChat: vi.fn(),
    unregisterChat: vi.fn(),
  },
}))

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchSessionHistory: vi.fn() }
})

beforeEach(() => {
  useChatStore.setState({
    sessions: [
      { id: 'l2:session-A', type: 'l2', name: 'Session A', createdAt: '' },
      { id: 'l2:session-B', type: 'l2', name: 'Session B', createdAt: '' },
    ],
    activeSessionId: 'l2:session-A',
    messages: {},
    activeRequests: {},
    streamingSessions: {},
    delegatingSessions: {},
    routeSessions: {},
    historyPreservedMessageIds: {},
    titleGenerated: {},
  })
  vi.clearAllMocks()
  vi.mocked(wsManager.send).mockReturnValue(true)
  vi.mocked(fetchSessionHistory).mockResolvedValue({
    messages: [],
    has_more: false,
  })
})

describe('useChatStream', () => {
  it('cancelling a request in session A only affects session A active request', async () => {
    const { result } = renderHook(() => useChatStream())

    // 1. Send message in session A
    await act(async () => {
      await result.current.send('Prompt for A', undefined, 'l2:session-A')
    })

    const sendCall1 = vi.mocked(wsManager.send).mock.calls[0][0] as any
    expect(sendCall1.type).toBe('chat_send')
    expect(sendCall1.session_id).toBe('l2:session-A')
    const reqA = sendCall1.request_id

    // 2. Switch active session to B
    act(() => {
      useChatStore.setState({ activeSessionId: 'l2:session-B' })
    })

    // 3. Send message in session B using the same hook instance
    await act(async () => {
      await result.current.send('Prompt for B', undefined, 'l2:session-B')
    })

    const sendCall2 = vi.mocked(wsManager.send).mock.calls[1][0] as any
    expect(sendCall2.type).toBe('chat_send')
    expect(sendCall2.session_id).toBe('l2:session-B')
    // 4. Switch back to session A and trigger cancel()
    act(() => {
      useChatStore.setState({ activeSessionId: 'l2:session-A' })
    })

    await act(async () => {
      result.current.cancel()
    })

    const cancelCall = vi
      .mocked(wsManager.send)
      .mock.calls.find((c) => (c[0] as any).type === 'chat_cancel')
    expect(cancelCall).toBeDefined()
    const cancelMsg = cancelCall![0] as any

    // Request ownership is keyed by request id and session id, so cancelling
    // session A sends reqA, not reqB from session B.
    expect(cancelMsg.session_id).toBe('l2:session-A')
    expect(cancelMsg.request_id).toBe(reqA)
  })

  it('queues a second prompt while the session is busy', async () => {
    useChatStore.setState({
      activeSessionId: 'l2:session-A',
      streamingSessions: { 'l2:session-A': true },
      messages: { 'l2:session-A': [] },
    })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('keep this draft', undefined, 'l2:session-A')
    })

    // The message is still sent so the server can queue it (not dropped).
    expect(wsManager.send).toHaveBeenCalled()
    const sendMsg = vi.mocked(wsManager.send).mock.calls[0][0] as any
    expect(sendMsg.type).toBe('chat_send')
    expect(sendMsg.prompt).toBe('keep this draft')

    // Only the user message is appended. The bot does not create an assistant
    // reply for a queued prompt, so Desktop should not invent one either.
    const msgs = useChatStore.getState().messages['l2:session-A']
    expect(msgs.length).toBe(1)
    expect(msgs[0].role).toBe('user')

    // Simulate the server replying chat_queued.
    const registerCall = vi.mocked(wsManager.registerChat).mock.calls[0]
    const handler = registerCall[1]
    act(() => {
      handler.onQueued?.({ error: 'session is busy; message queued' })
    })

    // The queued acknowledgement is transport state only; it does not become
    // an assistant message in the conversation.
    const after = useChatStore.getState().messages['l2:session-A']
    expect(after).toHaveLength(1)
    expect(after[0].role).toBe('user')

    // The in-flight request's streaming state must NOT be cleared.
    expect(useChatStore.getState().streamingSessions['l2:session-A']).toBe(true)
  })

  it('cancels the running request instead of a queued follow-up', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('running request', undefined, 'l2:session-A')
      await result.current.send('queued follow-up', undefined, 'l2:session-A')
    })

    const sent = vi.mocked(wsManager.send).mock.calls.map((call) => call[0] as any)
    const runningRequestId = sent[0].request_id
    const queuedRequestId = sent[1].request_id
    const handlers = vi.mocked(wsManager.registerChat).mock.calls.map((call) => call[1])

    act(() => {
      handlers[0].onAccepted?.()
      handlers[1].onAccepted?.()
      handlers[1].onQueued?.({ error: 'session is busy; message queued' })
      expect(useChatStore.getState().activeRequests[queuedRequestId]?.status).toBe('queued')
      result.current.cancel()
    })

    const cancelMessage = vi
      .mocked(wsManager.send)
      .mock.calls.find((call) => (call[0] as any).type === 'chat_cancel')?.[0] as any
    expect(cancelMessage.request_id).toBe(runningRequestId)
    expect(cancelMessage.request_id).not.toBe(queuedRequestId)
  })

  it('cancels the root transport request after queued input owns the stream', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('running request', undefined, 'l2:session-A')
      await result.current.send('queued follow-up', undefined, 'l2:session-A')
    })

    const sent = vi.mocked(wsManager.send).mock.calls.map((call) => call[0] as any)
    const rootRequestId = sent[0].request_id
    const queuedRequestId = sent[1].request_id
    const [rootHandler, queuedHandler] = vi
      .mocked(wsManager.registerChat)
      .mock.calls.map((call) => call[1])

    act(() => {
      queuedHandler.onQueued?.({ error: 'queued' })
      const boundary = {
        request_ids: [queuedRequestId],
        active_request_id: queuedRequestId,
        cancel_request_id: rootRequestId,
        owns_stream: false,
      }
      rootHandler.onQueueStarted?.(boundary)
      queuedHandler.onQueueStarted?.({ ...boundary, owns_stream: true })
      queuedHandler.onChunk?.('queued response')
    })

    expect(useChatStore.getState().messages['l2:session-A'].at(-1)?.segments).toEqual([
      { type: 'content', text: 'queued response' },
    ])

    act(() => result.current.cancel())

    const cancelMessage = vi
      .mocked(wsManager.send)
      .mock.calls.findLast((call) => (call[0] as any).type === 'chat_cancel')?.[0] as any
    expect(cancelMessage.request_id).toBe(rootRequestId)
    expect(cancelMessage.request_id).not.toBe(queuedRequestId)
    expect(useChatStore.getState().activeRequests[queuedRequestId]).toBeUndefined()
    expect(wsManager.unregisterChat).toHaveBeenCalledWith(queuedRequestId)
  })

  it('does not downgrade a started queued request when its queued ack arrives late', async () => {
    useChatStore.setState({ streamingSessions: { 'l2:session-A': true } })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('queued follow-up', undefined, 'l2:session-A')
    })
    const handler = vi.mocked(wsManager.registerChat).mock.calls[0][1]
    const requestId = (vi.mocked(wsManager.send).mock.calls[0][0] as any).request_id

    act(() => {
      handler.onQueueStarted?.({
        request_ids: [requestId],
        active_request_id: requestId,
        cancel_request_id: 'req-root',
        owns_stream: true,
      })
      handler.onQueued?.({ error: 'late queued acknowledgement' })
    })

    expect(useChatStore.getState().activeRequests[requestId]?.status).toBe('streaming')
    expect(useChatStore.getState().messages['l2:session-A']).toHaveLength(2)

    act(() => result.current.cancel())
    const cancelMessage = vi
      .mocked(wsManager.send)
      .mock.calls.findLast((call) => (call[0] as any).type === 'chat_cancel')?.[0] as any
    expect(cancelMessage.request_id).toBe('req-root')
  })

  it('cancels the original root after two consecutive queue ownership handoffs', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('root', undefined, 'l2:session-A')
      await result.current.send('first queued', undefined, 'l2:session-A')
      await result.current.send('second queued', undefined, 'l2:session-A')
    })

    const sent = vi.mocked(wsManager.send).mock.calls.map((call) => call[0] as any)
    const [rootRequestId, firstQueuedId, secondQueuedId] = sent.map((message) => message.request_id)
    const [rootHandler, firstQueuedHandler, secondQueuedHandler] = vi
      .mocked(wsManager.registerChat)
      .mock.calls.map((call) => call[1])

    act(() => {
      firstQueuedHandler.onQueued?.({ error: 'queued' })
      secondQueuedHandler.onQueued?.({ error: 'queued' })
      rootHandler.onQueueStarted?.({
        request_ids: [firstQueuedId],
        active_request_id: firstQueuedId,
        cancel_request_id: rootRequestId,
        owns_stream: false,
      })
      firstQueuedHandler.onQueueStarted?.({
        request_ids: [firstQueuedId],
        active_request_id: firstQueuedId,
        cancel_request_id: rootRequestId,
        owns_stream: true,
      })
      firstQueuedHandler.onQueueStarted?.({
        request_ids: [secondQueuedId],
        active_request_id: secondQueuedId,
        cancel_request_id: rootRequestId,
        owns_stream: false,
      })
      secondQueuedHandler.onQueueStarted?.({
        request_ids: [secondQueuedId],
        active_request_id: secondQueuedId,
        cancel_request_id: rootRequestId,
        owns_stream: true,
      })
    })

    act(() => result.current.cancel())
    const cancelMessage = vi
      .mocked(wsManager.send)
      .mock.calls.findLast((call) => (call[0] as any).type === 'chat_cancel')?.[0] as any
    expect(cancelMessage.request_id).toBe(rootRequestId)
  })

  it('opens one new assistant bubble after an aggregate of queued messages starts', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('first', undefined, 'l2:session-A')
    })
    const firstHandler = vi.mocked(wsManager.registerChat).mock.calls[0][1]
    act(() => firstHandler.onChunk?.('old response'))

    await act(async () => {
      await result.current.send('second', undefined, 'l2:session-A')
      await result.current.send('third', undefined, 'l2:session-A')
    })
    const secondHandler = vi.mocked(wsManager.registerChat).mock.calls[1][1]
    const thirdHandler = vi.mocked(wsManager.registerChat).mock.calls[2][1]
    const sent = vi.mocked(wsManager.send).mock.calls.map((call) => call[0] as any)
    const queuedRequestIds = [sent[1].request_id, sent[2].request_id]
    act(() => {
      secondHandler.onQueued?.({ error: 'queued' })
      thirdHandler.onQueued?.({ error: 'queued' })
    })

    expect(Object.keys(useChatStore.getState().activeRequests)).toHaveLength(3)

    act(() => {
      const data = {
        request_ids: queuedRequestIds,
        active_request_id: queuedRequestIds[1],
        owns_stream: false,
      }
      firstHandler.onQueueStarted?.(data)
      secondHandler.onQueueStarted?.(data)
      thirdHandler.onQueueStarted?.({ ...data, owns_stream: true })
    })

    let messages = useChatStore.getState().messages['l2:session-A']
    expect(messages.map((message) => message.role)).toEqual([
      'user',
      'assistant',
      'user',
      'user',
      'assistant',
    ])
    const assistantsBefore = messages.filter((message) => message.role === 'assistant')
    expect(assistantsBefore).toHaveLength(2)
    expect(assistantsBefore[0].segments).toEqual([{ type: 'content', text: 'old response' }])
    expect(Object.keys(useChatStore.getState().activeRequests)).toEqual([queuedRequestIds[1]])

    act(() => {
      thirdHandler.onReasoning?.('new reasoning')
      thirdHandler.onChunk?.('new response')
      thirdHandler.onToolStart?.({ call_id: 'call-new', name: 'Read', args: '{}' })
    })

    messages = useChatStore.getState().messages['l2:session-A']
    const assistantsAfter = messages.filter((message) => message.role === 'assistant')
    expect(assistantsAfter[0].segments).toEqual([{ type: 'content', text: 'old response' }])
    expect(assistantsAfter[1].segments).toEqual([
      { type: 'thinking', text: 'new reasoning' },
      { type: 'content', text: 'new response' },
      expect.objectContaining({ type: 'tool_call', callId: 'call-new' }),
    ])

    act(() => thirdHandler.onDone?.({ content: 'new response', reasoning_content: '' }))
    expect(Object.keys(useChatStore.getState().activeRequests)).toHaveLength(0)
    expect(
      useChatStore
        .getState()
        .messages['l2:session-A'].filter((message) => message.role === 'assistant')
    ).toHaveLength(2)
  })

  it('inserts the aggregate assistant before a later queued user message', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('first', undefined, 'l2:session-A')
    })
    const firstHandler = vi.mocked(wsManager.registerChat).mock.calls[0][1]
    act(() => firstHandler.onChunk?.('old response'))

    await act(async () => {
      await result.current.send('consumed second', undefined, 'l2:session-A')
      await result.current.send('still queued third', undefined, 'l2:session-A')
    })
    const secondHandler = vi.mocked(wsManager.registerChat).mock.calls[1][1]
    const sent = vi.mocked(wsManager.send).mock.calls.map((call) => call[0] as any)
    const secondRequestId = sent[1].request_id
    const thirdRequestId = sent[2].request_id

    act(() => {
      firstHandler.onQueueStarted?.({
        request_ids: [secondRequestId],
        active_request_id: secondRequestId,
        owns_stream: false,
      })
      secondHandler.onQueueStarted?.({
        request_ids: [secondRequestId],
        active_request_id: secondRequestId,
        owns_stream: true,
      })
    })

    const messages = useChatStore.getState().messages['l2:session-A']
    expect(messages.map((message) => [message.role, message.requestId])).toEqual([
      ['user', sent[0].request_id],
      ['assistant', undefined],
      ['user', secondRequestId],
      ['assistant', undefined],
      ['user', thirdRequestId],
    ])
  })

  it('delivers a queued processing failure to its anchored assistant bubble', async () => {
    useChatStore.setState({ streamingSessions: { 'l2:session-A': true } })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('queued failure', undefined, 'l2:session-A')
    })
    const handler = vi.mocked(wsManager.registerChat).mock.calls[0][1]
    const requestId = (vi.mocked(wsManager.send).mock.calls[0][0] as any).request_id
    act(() => {
      handler.onQueued?.({ error: 'queued' })
      handler.onQueueStarted?.({
        request_ids: [requestId],
        active_request_id: requestId,
        owns_stream: true,
      })
      handler.onError?.('follow-up failed')
    })

    const messages = useChatStore.getState().messages['l2:session-A']
    expect(messages.map((message) => [message.role, message.requestId])).toEqual([
      ['user', requestId],
      ['assistant', undefined],
    ])
    expect(messages[1].segments).toEqual([{ type: 'error', text: 'follow-up failed' }])
  })

  it('creates the assistant message when that request first streams output', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('Prompt for A', undefined, 'l2:session-A')
    })

    const registerCall = vi.mocked(wsManager.registerChat).mock.calls[0]
    const handler = registerCall[1]
    const messagesBefore = useChatStore.getState().messages['l2:session-A']
    expect(messagesBefore.filter((message) => message.role === 'assistant')).toHaveLength(0)

    act(() => {
      handler.onChunk?.('owned response')
    })

    const messagesAfter = useChatStore.getState().messages['l2:session-A']
    const assistant = messagesAfter.find((message) => message.role === 'assistant')
    expect(assistant?.segments).toEqual([{ type: 'content', text: 'owned response' }])
  })

  it('keeps a completed silent terminal visible while history reloads', async () => {
    const originalLoadHistory = useChatStore.getState().loadHistory
    const loadHistory = vi.fn().mockResolvedValue(undefined)
    useChatStore.setState({ activeSessionId: 'l2:session-A', messages: {}, loadHistory })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('silent completion', undefined, 'l2:session-A')
    })
    const handler = vi.mocked(wsManager.registerChat).mock.calls[0][1]

    act(() => {
      handler.onRuntimeTerminal?.({ terminal_code: 'completed' })
    })

    const assistant = useChatStore
      .getState()
      .messages['l2:session-A'].find((message) => message.role === 'assistant')
    expect(assistant?.segments).toEqual([
      { type: 'content', text: 'Response completed; loading history…' },
    ])
    expect(loadHistory).toHaveBeenCalledWith('l2:session-A')
    useChatStore.setState({ loadHistory: originalLoadHistory })
  })

  it('keeps L1 requests independent when one completes or is cancelled', async () => {
    useChatStore.setState({ activeSessionId: 'l1', messages: {}, streamingSessions: {} })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('first L1 request', undefined, 'l1')
      await result.current.send('second L1 request', undefined, 'l1')
    })

    const handlers = vi.mocked(wsManager.registerChat).mock.calls.map((call) => call[1])
    expect(handlers).toHaveLength(2)
    expect(Object.keys(useChatStore.getState().activeRequests)).toHaveLength(2)
    expect(useChatStore.getState().streamingSessions.l1).toBe(true)

    act(() => {
      handlers[0].onDone?.({ content: '', reasoning_content: '' })
    })

    expect(Object.keys(useChatStore.getState().activeRequests)).toHaveLength(1)
    expect(useChatStore.getState().streamingSessions.l1).toBe(true)

    act(() => {
      result.current.cancel()
    })

    expect(useChatStore.getState().activeRequests).toEqual({})
    expect(useChatStore.getState().streamingSessions.l1).toBe(false)
  })

  it('reconciles only the failing L1 request message when the server reports a timeout', async () => {
    const nowSpy = vi.spyOn(Date, 'now').mockReturnValue(1_000)
    const randomSpy = vi.spyOn(Math, 'random').mockReturnValueOnce(0.1).mockReturnValueOnce(0.2)
    const originalLoadHistory = useChatStore.getState().loadHistory
    const loadHistory = vi.fn()
    useChatStore.setState({
      activeSessionId: 'l1',
      messages: {},
      streamingSessions: {},
      loadHistory,
    })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('timing out', undefined, 'l1')
      await result.current.send('still running', undefined, 'l1')
    })

    const handlers = vi.mocked(wsManager.registerChat).mock.calls.map((call) => call[1])
    act(() => {
      handlers[0].onToolStart?.({ call_id: 'write-1', name: 'Write', args: '{}' })
      handlers[1].onToolStart?.({ call_id: 'read-2', name: 'Read', args: '{}' })
    })

    const timeoutError = 'Session request timed out after 20 minutes'
    act(() => {
      handlers[0].onError?.(timeoutError)
    })

    const assistants = useChatStore
      .getState()
      .messages.l1.filter((message) => message.role === 'assistant')
    expect(assistants[0].id).not.toBe(assistants[1].id)
    expect(assistants[0].segments).toEqual([
      expect.objectContaining({
        type: 'tool_call',
        callId: 'write-1',
        done: true,
        error: timeoutError,
      }),
      { type: 'error', text: timeoutError },
    ])
    expect(assistants[1].segments).toEqual([
      expect.objectContaining({ type: 'tool_call', callId: 'read-2', done: false }),
    ])
    expect(Object.values(useChatStore.getState().activeRequests)).toHaveLength(1)
    expect(useChatStore.getState().streamingSessions.l1).toBe(true)
    expect(loadHistory).not.toHaveBeenCalled()
    useChatStore.setState({ loadHistory: originalLoadHistory })
    randomSpy.mockRestore()
    nowSpy.mockRestore()
  })

  it('preserves the timed-out request through runtime history hydration without changing a parallel L1 request', async () => {
    let now = 2_000
    const nowSpy = vi.spyOn(Date, 'now').mockImplementation(() => now++)
    useChatStore.setState({ activeSessionId: 'l1', messages: {}, streamingSessions: {} })
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('request that times out', undefined, 'l1')
      await result.current.send('parallel request', undefined, 'l1')
    })

    const handlers = vi.mocked(wsManager.registerChat).mock.calls.map((call) => call[1])
    act(() => {
      handlers[0].onToolStart?.({ call_id: 'write-timeout', name: 'Write', args: '{}' })
      handlers[1].onToolStart?.({ call_id: 'read-parallel', name: 'Read', args: '{}' })
    })

    const timeoutError = 'Session request timed out after 20 minutes'
    act(() => handlers[0].onError?.(timeoutError))

    const beforeHydration = useChatStore.getState().messages.l1
    const parallelBefore = beforeHydration.find((message) =>
      message.segments.some(
        (segment) => segment.type === 'tool_call' && segment.callId === 'read-parallel'
      )
    )

    act(() => {
      useChatStore.setState({
        activeRequests: {},
        streamingSessions: { l1: false },
      })
    })
    await act(async () => {
      await useChatStore.getState().loadHistory('l1')
    })

    const afterHydration = useChatStore.getState().messages.l1
    const timedOut = afterHydration.find((message) =>
      message.segments.some(
        (segment) => segment.type === 'tool_call' && segment.callId === 'write-timeout'
      )
    )
    const parallelAfter = afterHydration.find((message) =>
      message.segments.some(
        (segment) => segment.type === 'tool_call' && segment.callId === 'read-parallel'
      )
    )
    expect(timedOut?.segments).toContainEqual({ type: 'error', text: timeoutError })
    expect(parallelAfter).toEqual(parallelBefore)

    vi.mocked(fetchSessionHistory).mockResolvedValueOnce({
      messages: [
        {
          id: 'server-history',
          role: 'assistant',
          segments: [{ type: 'content', text: 'fresh server history' }],
          timestamp: '2026-08-19T15:00:00Z',
        },
      ],
      has_more: false,
    })
    await act(async () => {
      await useChatStore.getState().loadHistory('l1')
    })
    expect(useChatStore.getState().messages.l1).toEqual([
      {
        id: 'server-history',
        role: 'assistant',
        segments: [{ type: 'content', text: 'fresh server history' }],
        timestamp: '2026-08-19T15:00:00Z',
        files: undefined,
      },
    ])
    nowSpy.mockRestore()
  })

  it('cleans up the optimistic request when the chat message is not sent', async () => {
    vi.mocked(wsManager.send).mockReturnValue(false)
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('message while disconnected', undefined, 'l2:session-A')
    })

    expect(useChatStore.getState().activeRequests).toEqual({})
    expect(useChatStore.getState().streamingSessions['l2:session-A']).toBe(false)
    const messages = useChatStore.getState().messages['l2:session-A']
    expect(messages.at(-1)?.segments).toEqual([
      { type: 'error', text: 'Message was not sent because the connection is unavailable.' },
    ])
  })

  it('keeps an acknowledged request alive across a message-too-large reconnect', async () => {
    vi.mocked(wsManager.send).mockReturnValue(true)
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('request already accepted', undefined, 'l2:session-A')
    })

    const handler = vi.mocked(wsManager.registerChat).mock.calls[0][1]
    act(() => {
      handler.onAccepted?.({ request_id: 'req-accepted', session_id: 'l2:session-A' })
      handler.onClose?.(1009)
    })

    expect(Object.keys(useChatStore.getState().activeRequests)).toHaveLength(1)
    expect(useChatStore.getState().streamingSessions['l2:session-A']).toBe(true)
  })

  it('rejects an oversized prompt before sending it to the socket', async () => {
    vi.mocked(wsManager.send).mockReturnValue(true)
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('a'.repeat(4 * 1024 * 1024 + 1), undefined, 'l2:session-A')
    })

    expect(wsManager.send).not.toHaveBeenCalled()
    const messages = useChatStore.getState().messages['l2:session-A']
    expect(messages.at(-1)?.segments).toEqual([
      { type: 'error', text: 'Message is too large. Maximum prompt size is 4 MiB.' },
    ])
  })

  it('rejects an oversized chat envelope before sending it to the socket', async () => {
    vi.mocked(wsManager.send).mockReturnValue(true)
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('short prompt', undefined, 'l2:session-A', {
        text: 'b'.repeat(8 * 1024 * 1024),
      })
    })

    expect(wsManager.send).not.toHaveBeenCalled()
    const messages = useChatStore.getState().messages['l2:session-A']
    expect(messages.at(-1)?.segments).toEqual([
      { type: 'error', text: 'Message is too large. Maximum WebSocket message size is 8 MiB.' },
    ])
  })
})
