import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { toast } from 'sonner'
import { useChatStream } from './useChatStream'
import { useChatStore } from '@/stores/chatStore'
import { useTranscriptStore } from '@/stores/transcriptStore'
import { wsManager } from '@/lib/websocket'
import { cancelSession, fetchTranscript } from '@/lib/api'

vi.mock('sonner', () => ({ toast: { error: vi.fn() } }))

vi.mock('@/lib/websocket', () => ({
  wsManager: { send: vi.fn() },
}))

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchTranscript: vi.fn(), cancelSession: vi.fn() }
})

const SID = 'l2:session-A'

beforeEach(() => {
  useChatStore.setState({
    sessions: [
      { id: SID, type: 'l2', name: 'Session A', createdAt: '' },
      { id: 'l2:session-B', type: 'l2', name: 'Session B', createdAt: '' },
    ],
    activeSessionId: SID,
    compactions: {},
    compactionClearWatermarks: {},
    activeRequests: {},
    streamingSessions: {},
    systemCommandSessions: {},
    delegatingSessions: {},
    routeSessions: {},
    titleGenerated: {},
  })
  useTranscriptStore.setState({ sessions: {}, local: {}, commands: {} })
  vi.clearAllMocks()
  vi.mocked(wsManager.send).mockReturnValue(true)
  vi.mocked(cancelSession).mockResolvedValue(undefined)
  vi.mocked(fetchTranscript).mockResolvedValue({
    session_id: SID, epoch: 'e1', seq: 0, items: [], pending: [], requests: [],
    has_more: false, compactions: [], ctxwin_used: 0, ctxwin_limit: 0,
  })
})

afterEach(() => {
  vi.useRealTimers()
})

function onlyLocalInput() {
  const inputs = Object.values(useTranscriptStore.getState().local)
  expect(inputs).toHaveLength(1)
  return inputs[0]
}

describe('useChatStream send', () => {
  it('records the input locally and sends it with a client request ID', async () => {
    const { result } = renderHook(() => useChatStream())
    let accepted = false
    await act(async () => {
      accepted = await result.current.send('hello', [{ name: 'a.txt', path: '/tmp/a.txt' }])
    })
    expect(accepted).toBe(true)
    const input = onlyLocalInput()
    expect(input).toMatchObject({ sessionId: SID, text: 'hello', status: 'sending', systemCommand: false })
    expect(wsManager.send).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'chat_send',
        request_id: input.requestId,
        session_id: SID,
        prompt: 'hello',
        files: [{ name: 'a.txt', path: '/tmp/a.txt' }],
      })
    )
    expect(useChatStore.getState().streamingSessions[SID]).toBe(true)
  })

  it('marks the input failed when the socket is unavailable', async () => {
    vi.mocked(wsManager.send).mockReturnValue(false)
    const { result } = renderHook(() => useChatStream())
    let accepted = true
    await act(async () => {
      accepted = await result.current.send('hello')
    })
    expect(accepted).toBe(false)
    expect(onlyLocalInput()).toMatchObject({
      status: 'failed',
      error: 'Message was not sent because the connection is unavailable.',
    })
    expect(useChatStore.getState().streamingSessions[SID]).toBe(false)
  })

  it('fails an input the server never acknowledges', async () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.send('hello')
    })
    act(() => {
      vi.advanceTimersByTime(10_000)
    })
    expect(onlyLocalInput()).toMatchObject({ status: 'failed', error: 'Message was not acknowledged by the server.' })
  })

  it('keeps an acknowledged input until its transcript commit', async () => {
    vi.useFakeTimers()
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.send('hello')
    })
    const { requestId } = onlyLocalInput()
    act(() => {
      useTranscriptStore.getState().ackLocalInput(requestId, 'queued')
      vi.advanceTimersByTime(10_000)
    })
    expect(onlyLocalInput()).toMatchObject({ status: 'queued' })
  })

  it('rejects an oversized prompt before sending it to the socket', async () => {
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.send('a'.repeat(4 * 1024 * 1024 + 1), undefined, SID)
    })
    expect(wsManager.send).not.toHaveBeenCalled()
    expect(onlyLocalInput()).toMatchObject({
      status: 'failed',
      error: 'Message is too large. Maximum prompt size is 4 MiB.',
    })
  })

  it('rejects an oversized chat envelope before sending it to the socket', async () => {
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.send('short prompt', undefined, SID, { text: 'b'.repeat(8 * 1024 * 1024) })
    })
    expect(wsManager.send).not.toHaveBeenCalled()
    expect(onlyLocalInput()).toMatchObject({
      status: 'failed',
      error: 'Message is too large. Maximum WebSocket message size is 8 MiB.',
    })
  })

  it('flags built-in slash commands and clears compactions on /clear', async () => {
    useChatStore.setState({ compactions: { [SID]: [{ id: 'c', status: 'completed', started_at: '' }] } })
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.send('/clear')
    })
    expect(onlyLocalInput().systemCommand).toBe(true)
    expect(useChatStore.getState().systemCommandSessions[SID]).toBe(true)
    expect(useChatStore.getState().compactions[SID]).toEqual([])
  })

  it('sends to an explicit session instead of the active one', async () => {
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.send('to B', undefined, 'l2:session-B')
    })
    expect(onlyLocalInput().sessionId).toBe('l2:session-B')
    expect(wsManager.send).toHaveBeenCalledWith(expect.objectContaining({ session_id: 'l2:session-B' }))
  })
})

describe('useChatStream cancel', () => {
  it('stops the whole active session through the session endpoint', async () => {
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.cancel()
    })
    expect(cancelSession).toHaveBeenCalledWith(SID)
    expect(toast.error).not.toHaveBeenCalled()
  })

  it('reports a failed Stop and leaves the request state to the server', async () => {
    useChatStore.getState().setSessionActivity(SID, {
      streaming: true,
      delegating: false,
      systemCommand: false,
      requests: [{ requestId: 'qq-running', sessionId: SID, status: 'streaming' }],
    })
    vi.mocked(cancelSession).mockRejectedValueOnce(new Error('Stop connection failed'))
    const { result } = renderHook(() => useChatStream())
    await act(async () => {
      await result.current.cancel()
    })
    expect(toast.error).toHaveBeenCalledWith('Stop connection failed')
    expect(useChatStore.getState().activeRequests['qq-running']?.status).toBe('streaming')
    expect(useChatStore.getState().streamingSessions[SID]).toBe(true)
  })
})
