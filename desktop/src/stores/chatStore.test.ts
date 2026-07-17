import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore } from './chatStore'
import { listSessions } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  listSessions: vi.fn(),
  createL2Session: vi.fn(),
  deleteL2Session: vi.fn(),
  fetchSessionHistory: vi.fn(),
  rewindSession: vi.fn(),
  deleteSessionMessages: vi.fn(),
}))

beforeEach(() => {
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    messages: {},
    streamingSessions: {},
    delegatingSessions: {},
    titleGenerated: {},
    historyLoading: {},
    historyHasMore: {},
    historyCursor: {},
    sessionsLoading: false,
  })
  vi.clearAllMocks()
})

describe('chatStore', () => {
  it('updateToolCallResult updates tool call segment done state in any assistant message', () => {
    const sid = 'session-1'
    useChatStore.setState({
      activeSessionId: sid,
      messages: {
        [sid]: [
          {
            id: 'msg-1',
            role: 'assistant',
            timestamp: '',
            segments: [
              {
                type: 'tool_call',
                callId: 'call-1',
                name: 'test_tool',
                args: '{}',
                done: false,
              },
            ],
          },
          {
            id: 'msg-2',
            role: 'user',
            timestamp: '',
            segments: [{ type: 'content', text: 'user msg' }],
          },
          {
            id: 'msg-3',
            role: 'assistant',
            timestamp: '',
            segments: [
              {
                type: 'tool_call',
                callId: 'call-2',
                name: 'test_tool_2',
                args: '{}',
                done: false,
              },
            ],
          },
        ],
      },
    })

    // Update first tool call (which is not in the last message)
    useChatStore.getState().updateToolCallResult(sid, 'call-1', 'result-1', undefined, 100)

    const msgs = useChatStore.getState().messages[sid]
    expect(msgs).toBeDefined()
    expect(msgs[0].segments[0]).toEqual({
      type: 'tool_call',
      callId: 'call-1',
      name: 'test_tool',
      args: '{}',
      result: 'result-1',
      error: undefined,
      durationMs: 100,
      done: true,
    })

    // Update second tool call (which is in the last message)
    useChatStore.getState().updateToolCallResult(sid, 'call-2', 'result-2', 'error-2', 200)
    const updatedMsgs = useChatStore.getState().messages[sid]
    expect(updatedMsgs[2].segments[0]).toEqual({
      type: 'tool_call',
      callId: 'call-2',
      name: 'test_tool_2',
      args: '{}',
      result: 'result-2',
      error: 'error-2',
      durationMs: 200,
      done: true,
    })
  })

  it('setDelegating scopes delegation status per session', () => {
    useChatStore.setState({
      activeSessionId: 'session-1',
      delegatingSessions: {},
    })

    // Set delegation for session-1
    useChatStore.getState().setDelegating(true, 'session-1')
    expect(useChatStore.getState().delegatingSessions['session-1']).toBe(true)
    expect(useChatStore.getState().delegatingSessions['session-2']).toBeUndefined()

    // Set delegation for session-2
    useChatStore.getState().setDelegating(true, 'session-2')
    expect(useChatStore.getState().delegatingSessions['session-1']).toBe(true)
    expect(useChatStore.getState().delegatingSessions['session-2']).toBe(true)

    // Reset delegation for session-1
    useChatStore.getState().setDelegating(false, 'session-1')
    expect(useChatStore.getState().delegatingSessions['session-1']).toBe(false)
    expect(useChatStore.getState().delegatingSessions['session-2']).toBe(true)

    // Test default activeSessionId fallback when sessionId is omitted
    useChatStore.getState().setDelegating(true)
    expect(useChatStore.getState().delegatingSessions['session-1']).toBe(true)
  })

  it('sessionsLoading toggles true→false around loadSessions (drives the tree loading UI)', async () => {
    // Resolve after a tick so we can observe the in-flight state.
    let resolveList!: (v: { sessions: any[] }) => void
    vi.mocked(listSessions).mockImplementationOnce(
      () => new Promise((res) => { resolveList = res }) as any
    )

    const promise = useChatStore.getState().loadSessions()
    expect(useChatStore.getState().sessionsLoading).toBe(true)

    resolveList({ sessions: [] })
    await promise

    expect(useChatStore.getState().sessionsLoading).toBe(false)
  })

  it('coalesces concurrent loadSessions() calls (mount + auto-retry dedup)', async () => {
    let resolveList!: (v: { sessions: any[] }) => void
    // Use a single shared implementation that increments a counter. We
    // inspect `mock.calls.length` (vitest's own bookkeeping) to assert
    // dedup, since `mockResolvedValueOnce` does NOT trigger our side effect.
    vi.mocked(listSessions).mockImplementation(
      () => new Promise((res) => { resolveList = res }) as any
    )

    // Fire three calls back-to-back — only the first should hit the network.
    const p1 = useChatStore.getState().loadSessions()
    const p2 = useChatStore.getState().loadSessions()
    const p3 = useChatStore.getState().loadSessions()

    expect(vi.mocked(listSessions).mock.calls.length).toBe(1)

    resolveList({ sessions: [] })
    await Promise.all([p1, p2, p3])

    // After the in-flight resolves, a fresh call should be allowed again.
    vi.mocked(listSessions).mockResolvedValueOnce({ sessions: [] } as any)
    await useChatStore.getState().loadSessions()
    expect(vi.mocked(listSessions).mock.calls.length).toBe(2)
  })

  it('loadSessions retries once on failure (covers startup race with backend)', async () => {
    // First call rejects, second succeeds. The store should end up with
    // the data from the retry.
    vi.mocked(listSessions)
      .mockRejectedValueOnce(new Error('ECONNREFUSED'))
      .mockResolvedValueOnce({ sessions: [{ id: 's1', name: 'after-retry' }] } as any)

    await useChatStore.getState().loadSessions()

    expect(vi.mocked(listSessions).mock.calls.length).toBe(2)
    expect(useChatStore.getState().sessions.map((s) => s.id)).toEqual(['s1'])
  })

  it('loadSessions gives up cleanly when both attempts fail', async () => {
    vi.mocked(listSessions)
      .mockRejectedValueOnce(new Error('first'))
      .mockRejectedValueOnce(new Error('second'))

    await useChatStore.getState().loadSessions()

    expect(vi.mocked(listSessions).mock.calls.length).toBe(2)
    expect(useChatStore.getState().sessions).toEqual([])
    expect(useChatStore.getState().sessionsLoading).toBe(false)
  })
})
