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
    streamingSessions: {},
    delegatingSessions: {},
    routeSessions: {},
    titleGenerated: {},
  })
  vi.clearAllMocks()
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
    const reqB = sendCall2.request_id

    // 4. Switch back to session A and trigger cancel()
    act(() => {
      useChatStore.setState({ activeSessionId: 'l2:session-A' })
    })

    await act(async () => {
      result.current.cancel()
    })

    const cancelCall = vi.mocked(wsManager.send).mock.calls.find(
      (c) => (c[0] as any).type === 'chat_cancel'
    )
    expect(cancelCall).toBeDefined()
    const cancelMsg = cancelCall![0] as any

    // With session-keyed activeRequestsRef, cancelling session-A sends reqA (its own active request),
    // NOT reqB from session-B.
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

    // User message + assistant placeholder appended.
    const msgs = useChatStore.getState().messages['l2:session-A']
    expect(msgs.length).toBe(2)
    expect(msgs[0].role).toBe('user')
    expect(msgs[1].role).toBe('assistant')

    // Simulate the server replying chat_queued.
    const registerCall = vi.mocked(wsManager.registerChat).mock.calls[0]
    const handler = registerCall[1]
    act(() => {
      handler.onQueued?.({ error: 'session is busy; message queued' })
    })

    // The assistant placeholder shows the queued status.
    const after = useChatStore.getState().messages['l2:session-A']
    expect(after[1].segments).toEqual([
      { type: 'content', text: 'session is busy; message queued' },
    ])

    // The in-flight request's streaming state must NOT be cleared.
    expect(useChatStore.getState().streamingSessions['l2:session-A']).toBe(true)
  })

  it('routes stream chunks to the assistant placeholder created for that request', async () => {
    const { result } = renderHook(() => useChatStream())

    await act(async () => {
      await result.current.send('Prompt for A', undefined, 'l2:session-A')
    })

    const registerCall = vi.mocked(wsManager.registerChat).mock.calls[0]
    const handler = registerCall[1]
    const messagesBefore = useChatStore.getState().messages['l2:session-A']
    const target = messagesBefore.find((message) => message.role === 'assistant')!

    act(() => {
      useChatStore.getState().addMessage('l2:session-A', {
        id: 'newer-assistant',
        role: 'assistant',
        timestamp: '',
        segments: [],
      })
      handler.onChunk?.('owned response')
    })

    const messagesAfter = useChatStore.getState().messages['l2:session-A']
    expect(messagesAfter.find((message) => message.id === target.id)?.segments).toEqual([
      { type: 'content', text: 'owned response' },
    ])
    expect(messagesAfter.find((message) => message.id === 'newer-assistant')?.segments).toEqual([])
  })
})
