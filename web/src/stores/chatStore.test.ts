import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore } from './chatStore'
import {
  deleteL2Session as apiDeleteL2Session,
  deleteSessionMessages as apiDeleteSessionMessages,
  listSessions,
  rewindSession as apiRewindSession,
} from '@/lib/api'
import { useInspectorStateStore } from './inspectorStateStore'

vi.mock('@/lib/api', () => ({
  listSessions: vi.fn(),
  createL2Session: vi.fn(),
  deleteL2Session: vi.fn(),
  rewindSession: vi.fn(),
  deleteSessionMessages: vi.fn(),
}))

beforeEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  useInspectorStateStore.setState({ sessions: {} })
  useChatStore.setState({
    sessions: [],
    activeSessionId: null,
    compactions: {},
    compactionClearWatermarks: {},
    activeRequests: {},
    streamingSessions: {},
    systemCommandSessions: {},
    delegatingSessions: {},
    routeSessions: {},
    titleGenerated: {},
    sessionsLoading: false,
    sessionsLoaded: false,
  })
  vi.clearAllMocks()
  vi.mocked(apiDeleteL2Session).mockResolvedValue(undefined)
})

describe('chatStore compaction notices', () => {
  it.each(['failed', 'interrupted'] as const)(
    'does not turn historical %s statuses into live notices',
    (status) => {
      const sid = 'l2:history'
      const row = { id: 'old', status, started_at: new Date().toISOString(), error: 'Old timeout' }
      useChatStore.getState().applyCompactionSnapshot(sid, [row])
      useChatStore.getState().setCompactionStatus(sid, row)
      expect(useChatStore.getState().compactions[sid]).toEqual([row])
    }
  )

  it('gives live failures one fixed thirty-second deadline and keeps manual ownership', () => {
    const sid = 'l2:manual'
    const now = Date.now()
    vi.spyOn(Date, 'now').mockReturnValue(now)
    const running = { id: 'one', status: 'running' as const, reason: 'user_command', started_at: new Date(now).toISOString() }
    useChatStore.getState().setCompactionStatus(sid, running, 'manual')
    const failed = { ...running, status: 'failed' as const, error: 'Timeout' }
    useChatStore.getState().setCompactionStatus(sid, failed)
    expect(useChatStore.getState().compactions[sid][0]).toMatchObject({ requestId: 'manual', noticeExpiresAt: now + 30_000 })
    vi.mocked(Date.now).mockReturnValue(now + 10_000)
    useChatStore.getState().setCompactionStatus(sid, failed)
    useChatStore.getState().applyCompactionSnapshot(sid, [failed])
    expect(useChatStore.getState().compactions[sid][0]).toMatchObject({ requestId: 'manual', noticeExpiresAt: now + 30_000 })
  })

  it('preserves a live terminal compaction over a stale running snapshot', () => {
    const sid = 'l1'
    const completed = { id: 'compact-1', status: 'completed' as const, started_at: '2026-10-16T10:00:00Z', finished_at: '2026-10-16T10:01:00Z' }
    useChatStore.setState({ compactions: { [sid]: [completed] } })
    useChatStore.getState().applyCompactionSnapshot(sid, [{ ...completed, status: 'running', finished_at: undefined }])
    expect(useChatStore.getState().compactions[sid]).toEqual([completed])
  })

  it('ignores a delayed compaction terminal event after clear but accepts a new compaction', () => {
    const sid = 'l1'
    const staleStartedAt = new Date(Date.now() - 60_000).toISOString()
    useChatStore.getState().clearCompactions(sid)
    const watermark = useChatStore.getState().compactionClearWatermarks[sid]
    useChatStore.getState().setCompactionStatus(sid, {
      id: 'compact-before-clear', status: 'completed', started_at: staleStartedAt,
      finished_at: new Date(Date.now() + 1_000).toISOString(),
    })
    expect(useChatStore.getState().compactions[sid]).toEqual([])
    useChatStore.getState().setCompactionStatus(sid, {
      id: 'compact-after-clear', status: 'running', started_at: new Date(Date.parse(watermark) + 1_000).toISOString(),
    })
    expect(useChatStore.getState().compactions[sid]).toMatchObject([{ id: 'compact-after-clear', status: 'running' }])
  })
})

describe('chatStore session activity', () => {
  it('replaces every activity flag of one session without touching others', () => {
    useChatStore.setState({
      activeRequests: {
        other: { requestId: 'other', sessionId: 'l2:b', status: 'streaming' },
        stale: { requestId: 'stale', sessionId: 'l2:a', status: 'streaming' },
      },
      streamingSessions: { 'l2:b': true },
    })
    const route = { requestId: 'r', sessionId: 'l2:a', taskLevel: 'engineering', modelId: 'm' }
    useChatStore.getState().setSessionActivity('l2:a', {
      streaming: true,
      delegating: true,
      systemCommand: false,
      route,
      requests: [{ requestId: 'r', sessionId: 'l2:a', status: 'streaming', route }],
    })
    let state = useChatStore.getState()
    expect(Object.keys(state.activeRequests).sort()).toEqual(['other', 'r'])
    expect(state.routeSessions['l2:a']).toEqual(route)
    expect(state.streamingSessions).toEqual({ 'l2:b': true, 'l2:a': true })
    expect(state.delegatingSessions['l2:a']).toBe(true)

    useChatStore.getState().setSessionActivity('l2:a', { streaming: false, delegating: false, systemCommand: false, requests: [] })
    state = useChatStore.getState()
    expect(Object.keys(state.activeRequests)).toEqual(['other'])
    expect(state.routeSessions['l2:a']).toBeUndefined()
    expect(state.streamingSessions['l2:a']).toBe(false)
  })

  it('applies runtime activity only when it changes', () => {
    useChatStore.getState().setRuntimeActivity('l2:a', true, false)
    const before = useChatStore.getState().streamingSessions
    useChatStore.getState().setRuntimeActivity('l2:a', true, false)
    expect(useChatStore.getState().streamingSessions).toBe(before)
    useChatStore.getState().setRuntimeActivity('l2:a', false, false)
    expect(useChatStore.getState().streamingSessions['l2:a']).toBe(false)
  })

  it('setDelegating scopes delegation status per session', () => {
    useChatStore.getState().setDelegating(true, 'l2:a')
    expect(useChatStore.getState().delegatingSessions).toEqual({ 'l2:a': true })
    useChatStore.setState({ activeSessionId: 'l2:b' })
    useChatStore.getState().setDelegating(true)
    expect(useChatStore.getState().delegatingSessions).toEqual({ 'l2:a': true, 'l2:b': true })
  })
})

describe('chatStore sessions', () => {
  it('deleteL2Session removes every per-session map entry', async () => {
    const sid = 'l2:gone'
    useChatStore.setState({
      sessions: [{ id: sid, type: 'l2', name: '', createdAt: '' }],
      activeSessionId: sid,
      activeRequests: { r: { requestId: 'r', sessionId: sid, status: 'streaming' } },
      streamingSessions: { [sid]: true },
      delegatingSessions: { [sid]: true },
      systemCommandSessions: { [sid]: true },
      routeSessions: { [sid]: { requestId: 'r', sessionId: sid, taskLevel: '', modelId: '' } },
      compactions: { [sid]: [] },
    })
    useInspectorStateStore.getState().updateSession(sid, { open: true })
    await useChatStore.getState().deleteL2Session(sid)
    const state = useChatStore.getState()
    expect(apiDeleteL2Session).toHaveBeenCalledWith('gone')
    expect(state.sessions).toEqual([])
    expect(state.activeSessionId).toBeNull()
    expect(state.activeRequests).toEqual({})
    expect(state.streamingSessions[sid]).toBeUndefined()
    expect(state.routeSessions[sid]).toBeUndefined()
    expect(useInspectorStateStore.getState().sessions[sid]).toBeUndefined()
  })

  it('rewind and delete only call the API; the transcript reset reloads the view', async () => {
    vi.mocked(apiRewindSession).mockResolvedValue(undefined)
    vi.mocked(apiDeleteSessionMessages).mockResolvedValue(undefined)
    await useChatStore.getState().rewindSession('l1', 't1')
    await useChatStore.getState().deleteSessionMessages('l1', ['t1', 't2'])
    expect(apiRewindSession).toHaveBeenCalledWith('l1', 't1')
    expect(apiDeleteSessionMessages).toHaveBeenCalledWith('l1', ['t1', 't2'])
  })

  it('sessionsLoading toggles true→false around loadSessions', async () => {
    let resolveList!: (v: { sessions: any[] }) => void
    vi.mocked(listSessions).mockImplementationOnce(() => new Promise((res) => (resolveList = res)) as any)
    const promise = useChatStore.getState().loadSessions()
    expect(useChatStore.getState().sessionsLoading).toBe(true)
    resolveList({ sessions: [] })
    await promise
    expect(useChatStore.getState().sessionsLoading).toBe(false)
  })

  it('coalesces concurrent loadSessions() calls', async () => {
    let resolveList!: (v: { sessions: any[] }) => void
    vi.mocked(listSessions).mockImplementation(() => new Promise((res) => (resolveList = res)) as any)
    const calls = [useChatStore.getState().loadSessions(), useChatStore.getState().loadSessions()]
    expect(vi.mocked(listSessions).mock.calls.length).toBe(1)
    resolveList({ sessions: [] })
    await Promise.all(calls)
    vi.mocked(listSessions).mockResolvedValueOnce({ sessions: [] } as any)
    await useChatStore.getState().loadSessions()
    expect(vi.mocked(listSessions).mock.calls.length).toBe(2)
  })

  it('loadSessions retries once and marks the list loaded', async () => {
    vi.mocked(listSessions)
      .mockRejectedValueOnce(new Error('ECONNREFUSED'))
      .mockResolvedValueOnce({ sessions: [{ id: 's1', name: 'after-retry' }] } as any)
    await useChatStore.getState().loadSessions()
    expect(vi.mocked(listSessions).mock.calls.length).toBe(2)
    expect(useChatStore.getState().sessions.map((s) => s.id)).toEqual(['s1'])
    expect(useChatStore.getState().sessionsLoaded).toBe(true)
  })

  it('loadSessions gives up cleanly when both attempts fail', async () => {
    vi.mocked(listSessions).mockRejectedValueOnce(new Error('first')).mockRejectedValueOnce(new Error('second'))
    await useChatStore.getState().loadSessions()
    expect(useChatStore.getState().sessions).toEqual([])
    expect(useChatStore.getState().sessionsLoading).toBe(false)
    expect(useChatStore.getState().sessionsLoaded).toBe(false)
  })
})
