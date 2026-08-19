import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore } from './chatStore'
import { fetchSessionHistory, listSessions } from '@/lib/api'

vi.mock('@/lib/api', () => ({
  listSessions: vi.fn(),
  createL2Session: vi.fn(),
  deleteL2Session: vi.fn(),
  fetchSessionHistory: vi.fn(),
  rewindSession: vi.fn(),
  deleteSessionMessages: vi.fn(),
}))

beforeEach(() => {
  localStorage.clear()
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    messages: {},
    streamingSessions: {},
    delegatingSessions: {},
    routeSessions: {},
    titleGenerated: {},
    historyLoading: {},
    historyHasMore: {},
    historyCursor: {},
    historyPreservedMessageIds: {},
    sessionsLoading: false,
    sessionsLoaded: false,
  })
  vi.clearAllMocks()
})

describe('chatStore', () => {
  it('stores route metadata atomically and clears only the matching request', () => {
    useChatStore.setState({
      sessions: [{ id: 'l2:s1', type: 'l2', name: '', createdAt: '' }],
    })
    const route = {
      requestId: 'req-new',
      sessionId: 'l2:s1',
      taskLevel: 'L2-MediumMultiFile',
      modelId: 'routed-model',
      agentInstanceId: 'agent-instance',
    }

    useChatStore.getState().setRoute({
      requestId: 'req-old',
      sessionId: 'l2:s1',
      taskLevel: 'general',
      modelId: 'old-model',
    })
    useChatStore.getState().setRoute(route)
    expect(useChatStore.getState().routeSessions['l2:s1']).toEqual(route)
    expect(useChatStore.getState().sessions[0].agent_instance_id).toBe('agent-instance')
    expect(JSON.parse(localStorage.getItem('soloqueue_active_chat_routes') || '{}')).toEqual({
      'l2:s1': route,
    })

    useChatStore.getState().clearRoute('l2:s1', 'req-old')
    expect(useChatStore.getState().routeSessions['l2:s1']).toEqual(route)

    useChatStore.getState().clearRoute('l2:s1', 'req-new')
    expect(useChatStore.getState().routeSessions['l2:s1']).toBeUndefined()
    expect(localStorage.getItem('soloqueue_active_chat_routes')).toBeNull()
  })

  it('restores the active route after the store is reinitialized', async () => {
    const route = {
      requestId: 'req-refresh',
      sessionId: 'l1',
      taskLevel: 'engineering',
      modelId: 'deepseek-v4-flash-202605',
    }

    useChatStore.getState().setRoute(route)

    vi.resetModules()
    const { useChatStore: reloadedStore } = await import('./chatStore')

    expect(reloadedStore.getState().routeSessions.l1).toEqual(route)
  })

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

  it('marks running delegation cards cancelled when a request is stopped', () => {
    const sid = 'session-1'
    useChatStore.setState({
      messages: {
        [sid]: [
          {
            id: 'assistant-1',
            role: 'assistant',
            timestamp: '',
            segments: [
              {
                type: 'tool_call',
                callId: 'delegate-call',
                name: 'delegate_editor',
                args: '{}',
                done: false,
              },
              {
                type: 'tool_call',
                callId: 'unified-delegate-call',
                name: 'delegate',
                args: '{"target":"dev"}',
                done: false,
              },
              {
                type: 'tool_call',
                callId: 'regular-call',
                name: 'Read',
                args: '{}',
                done: false,
              },
              {
                type: 'delegation',
                agentName: 'editor',
                task: 'edit file',
                status: 'running',
              },
            ],
          },
        ],
      },
    })

    useChatStore.getState().cancelRunningDelegations(sid)

    const segments = useChatStore.getState().messages[sid][0].segments
    expect(segments[0]).toMatchObject({ done: true, error: 'Cancelled by user' })
    expect(segments[1]).toMatchObject({ done: true, error: 'Cancelled by user' })
    expect(segments[2]).toMatchObject({ done: false })
    expect(segments[3]).toMatchObject({ status: 'cancelled' })
  })

  it('fails unfinished segments only in the assistant message owned by the request', () => {
    const sid = 'l1'
    const timeoutError = 'Session request timed out after 20 minutes'
    const ownedMessage = {
      id: 'assistant-owned',
      role: 'assistant' as const,
      timestamp: '',
      segments: [
        { type: 'tool_call' as const, callId: 'write-1', name: 'Write', args: '{}', done: false },
        {
          type: 'delegation' as const,
          agentName: 'QuantAnalyst',
          task: 'write report',
          status: 'running' as const,
        },
        {
          type: 'tool_confirm' as const,
          callId: 'confirm-1',
          name: 'Write',
          prompt: 'Allow write?',
          allowInSession: false,
          resolved: false,
        },
      ],
    }
    const parallelMessage = {
      id: 'assistant-parallel',
      role: 'assistant' as const,
      timestamp: '',
      segments: [
        { type: 'tool_call' as const, callId: 'read-2', name: 'Read', args: '{}', done: false },
        {
          type: 'tool_confirm' as const,
          callId: 'confirm-2',
          name: 'Shell',
          prompt: 'Allow command?',
          allowInSession: false,
          resolved: false,
        },
      ],
    }
    useChatStore.setState({ messages: { [sid]: [ownedMessage, parallelMessage] } })

    useChatStore.getState().failAssistantMessage(sid, ownedMessage.id, timeoutError)

    const messages = useChatStore.getState().messages[sid]
    expect(messages[0].segments).toEqual([
      { ...ownedMessage.segments[0], done: true, error: timeoutError },
      { ...ownedMessage.segments[1], status: 'failed', resultContent: timeoutError },
      { type: 'error', text: timeoutError },
    ])
    expect(messages[1]).toEqual(parallelMessage)
  })

  it('sessionsLoading toggles true→false around loadSessions (drives the tree loading UI)', async () => {
    // Resolve after a tick so we can observe the in-flight state.
    let resolveList!: (v: { sessions: any[] }) => void
    vi.mocked(listSessions).mockImplementationOnce(
      () =>
        new Promise((res) => {
          resolveList = res
        }) as any
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
      () =>
        new Promise((res) => {
          resolveList = res
        }) as any
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

  it('sessionsLoaded is false initially and becomes true after a successful loadSessions', async () => {
    expect(useChatStore.getState().sessionsLoaded).toBe(false)

    vi.mocked(listSessions).mockResolvedValueOnce({
      sessions: [{ id: 'l2:abc', type: 'l2', name: 'x', createdAt: '' }],
    } as any)

    await useChatStore.getState().loadSessions()

    expect(useChatStore.getState().sessionsLoaded).toBe(true)
    expect(useChatStore.getState().sessionsLoading).toBe(false)
    expect(useChatStore.getState().sessions[0].id).toBe('l2:abc')
  })

  it('sessionsLoaded stays false when both listSessions attempts fail', async () => {
    vi.mocked(listSessions)
      .mockRejectedValueOnce(new Error('ECONNREFUSED'))
      .mockRejectedValueOnce(new Error('ECONNREFUSED'))

    await useChatStore.getState().loadSessions()

    expect(vi.mocked(listSessions).mock.calls.length).toBe(2)
    expect(useChatStore.getState().sessionsLoaded).toBe(false)
    expect(useChatStore.getState().sessionsLoading).toBe(false)
    expect(useChatStore.getState().sessions).toEqual([])
  })

  it('hydrates empty history while runtime reports the session is streaming', async () => {
    const sid = 'l1'
    vi.mocked(fetchSessionHistory).mockResolvedValueOnce({
      messages: [
        {
          id: 'history-user',
          role: 'user',
          timestamp: '',
          segments: [{ type: 'content', text: 'persisted prompt' }],
        },
      ],
      has_more: false,
    } as any)
    useChatStore.setState({
      activeSessionId: sid,
      messages: { [sid]: [] },
      streamingSessions: { [sid]: true },
    })

    await useChatStore.getState().loadHistory(sid)

    expect(fetchSessionHistory).toHaveBeenCalledWith(sid, undefined, 30)
    expect(useChatStore.getState().messages[sid][0].id).toBe('history-user')
  })

  it('does not overwrite handler-owned messages while streaming', async () => {
    const sid = 'l1'
    useChatStore.setState({
      activeSessionId: sid,
      messages: {
        [sid]: [
          {
            id: 'live-user',
            role: 'user',
            timestamp: '',
            segments: [{ type: 'content', text: 'live prompt' }],
          },
        ],
      },
      streamingSessions: { [sid]: true },
    })

    await useChatStore.getState().loadHistory(sid)

    expect(fetchSessionHistory).not.toHaveBeenCalled()
    expect(useChatStore.getState().messages[sid][0].id).toBe('live-user')
  })

  // ── Phase 0.3 Characterization Tests ──────────────────────────────────────────

  it('appendAssistantContent targets the assigned assistant message ID', () => {
    const sid = 'session-target-test'
    useChatStore.setState({
      activeSessionId: sid,
      messages: {
        [sid]: [
          {
            id: 'asst-1',
            role: 'assistant',
            timestamp: '',
            segments: [{ type: 'content', text: 'hello' }],
          },
          {
            id: 'user-2',
            role: 'user',
            timestamp: '',
            segments: [{ type: 'content', text: 'later prompt' }],
          },
          {
            id: 'asst-2',
            role: 'assistant',
            timestamp: '',
            segments: [{ type: 'content', text: 'newer response' }],
          },
        ],
      },
    })

    useChatStore.getState().appendAssistantContent(sid, 'asst-1', ' delta')

    const msgs = useChatStore.getState().messages[sid]
    expect(msgs[0].segments[0]).toEqual({ type: 'content', text: 'hello delta' })
    expect(msgs[2].segments[0]).toEqual({ type: 'content', text: 'newer response' })
  })

  it('deleteL2Session cleanup is complete for streaming and helper session maps', async () => {
    const sid = 'l2:delete-test-123'
    const route = {
      requestId: 'r1',
      sessionId: sid,
      taskLevel: 'engineering',
      modelId: 'routed-model',
    }
    localStorage.setItem('soloqueue_active_chat_routes', JSON.stringify({ [sid]: route }))
    useChatStore.setState({
      sessions: [{ id: sid, type: 'l2', name: 'Delete test', createdAt: '' }],
      activeSessionId: sid,
      messages: { [sid]: [] },
      streamingSessions: { [sid]: true },
      systemCommandSessions: { [sid]: true },
      delegatingSessions: { [sid]: true },
      routeSessions: { [sid]: route },
    })

    await useChatStore.getState().deleteL2Session(sid)

    const state = useChatStore.getState()
    expect(state.sessions.find((s) => s.id === sid)).toBeUndefined()
    expect(state.messages[sid]).toBeUndefined()
    expect(state.streamingSessions[sid]).toBeUndefined()
    expect(state.systemCommandSessions[sid]).toBeUndefined()
    expect(state.delegatingSessions[sid]).toBeUndefined()
    expect(localStorage.getItem('soloqueue_active_chat_routes')).toBeNull()
  })
})
