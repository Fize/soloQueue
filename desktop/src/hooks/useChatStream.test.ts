import { describe, it, expect, beforeEach, vi } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useChatStream } from './useChatStream'
import { useChatStore } from '@/stores/chatStore'
import { wsManager } from '@/lib/websocket'

vi.mock('@/lib/websocket', () => ({
  wsManager: {
    send: vi.fn(),
    registerChat: vi.fn(),
    unregisterChat: vi.fn(),
  },
}))

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
    titleGenerated: {},
  })
  vi.clearAllMocks()
  vi.mocked(wsManager.send).mockReturnValue(true)
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
    expect(assistant?.segments).toEqual([
      { type: 'content', text: 'owned response' },
    ])
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
