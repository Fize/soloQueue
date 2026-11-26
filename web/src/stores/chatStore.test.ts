import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useChatStore, compareSessions, sortSessions, isDraftSessionId } from './chatStore'
import {
  createL2Session as apiCreateL2Session,
  deleteL2Session as apiDeleteL2Session,
  deleteSessionMessages as apiDeleteSessionMessages,
  listSessions,
  rewindSession as apiRewindSession,
  setSessionModel as apiSetSessionModel,
} from '@/lib/api'
import { useInspectorStateStore } from './inspectorStateStore'

vi.mock('@/lib/api', () => ({
  listSessions: vi.fn(),
  createL2Session: vi.fn(),
  deleteL2Session: vi.fn(),
  rewindSession: vi.fn(),
  deleteSessionMessages: vi.fn(),
  setSessionModel: vi.fn(),
}))

beforeEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
  useInspectorStateStore.setState({ sessions: {} })
  useChatStore.setState({
    sessions: [],
    drafts: [],
    pendingModel: null,
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

describe('session ordering and draft placeholders', () => {
  const session = (id: string, lastActive: string, createdAt = lastActive) => ({
    id,
    type: 'l2' as const,
    name: id,
    createdAt,
    last_active_at: lastActive,
  })

  it('orders by last activity rather than creation time', () => {
    const oldButBusy = session('busy', '2026-11-26T12:00:00Z', '2026-01-01T00:00:00Z')
    const newButIdle = session('idle', '2026-11-20T12:00:00Z', '2026-11-25T00:00:00Z')

    expect(sortSessions([newButIdle, oldButBusy]).map((s) => s.id)).toEqual(['busy', 'idle'])
  })

  it('compares timestamps numerically across timezone offsets', () => {
    // 10:00+08:00 is 02:00Z, which is earlier than 05:00Z. A lexicographic or
    // localeCompare-based comparison inverts this pair.
    const shanghai = session('shanghai', '2026-11-26T10:00:00+08:00')
    const utc = session('utc', '2026-11-26T05:00:00Z')

    expect(sortSessions([shanghai, utc]).map((s) => s.id)).toEqual(['utc', 'shanghai'])
  })

  it('breaks ties by id so the order is deterministic', () => {
    const a = session('a-session', '2026-11-26T10:00:00Z')
    const b = session('b-session', '2026-11-26T10:00:00Z')

    expect(compareSessions(a, b)).toBeLessThan(0)
    expect(sortSessions([b, a]).map((s) => s.id)).toEqual(['a-session', 'b-session'])
  })

  it('loadSessions maps last_active_at and keeps one order', async () => {
    vi.mocked(listSessions).mockResolvedValueOnce({
      sessions: [
        { id: 'l2:quiet', name: 'quiet', created_at: '2026-11-26T09:00:00Z', last_active_at: '2026-11-26T09:00:00Z' },
        { id: 'l2:loud', name: 'loud', created_at: '2026-11-01T09:00:00Z', last_active_at: '2026-11-26T11:00:00Z' },
      ],
    } as any)

    await useChatStore.getState().loadSessions()

    expect(useChatStore.getState().sessions.map((s) => s.id)).toEqual(['l2:loud', 'l2:quiet'])
    expect(useChatStore.getState().sessions[0].last_active_at).toBe('2026-11-26T11:00:00Z')
  })

  it('touchSession moves a session to the top without a refetch', () => {
    const hourAgo = new Date(Date.now() - 3_600_000).toISOString()
    const twoHoursAgo = new Date(Date.now() - 7_200_000).toISOString()
    useChatStore.setState({
      sessions: [session('l2:a', hourAgo), session('l2:b', twoHoursAgo)],
    })
    const callsBefore = vi.mocked(listSessions).mock.calls.length

    useChatStore.getState().touchSession('l2:b')

    expect(useChatStore.getState().sessions.map((s) => s.id)).toEqual(['l2:b', 'l2:a'])
    expect(vi.mocked(listSessions).mock.calls.length).toBe(callsBefore)
  })

  it('beginDraft adds a client-only placeholder and abandons it when switching away', () => {
    useChatStore.setState({ sessions: [session('l2:real', '2026-11-26T10:00:00Z')] })

    const draftId = useChatStore.getState().beginDraft('engineering', '/repo')

    expect(isDraftSessionId(draftId)).toBe(true)
    expect(useChatStore.getState().activeSessionId).toBe(draftId)
    expect(useChatStore.getState().drafts).toHaveLength(1)
    expect(useChatStore.getState().drafts[0]).toMatchObject({
      group: 'engineering',
      project_path: '/repo',
      draft: true,
    })
    // Starting a new chat never touches the backend.
    expect(vi.mocked(apiCreateL2Session)).not.toHaveBeenCalled()

    useChatStore.getState().setActiveSession('l2:real')

    expect(useChatStore.getState().drafts).toEqual([])
    expect(useChatStore.getState().sessions.map((s) => s.id)).toEqual(['l2:real'])
  })

  it('keeps the placeholder while the router settles to "no session"', () => {
    // beginDraft() activates the placeholder and navigate() lands one render
    // later, so ChatPage reports an empty session id first. That must not drop
    // the row — otherwise the pending chat loses its group, project and model.
    const draftId = useChatStore.getState().beginDraft('engineering', '/repo')
    useChatStore.getState().setDraftModel(draftId, { provider_id: 'tencent', model_id: 'kimi-k3' })

    useChatStore.getState().setActiveSession('')

    expect(useChatStore.getState().activeSessionId).toBe('')
    expect(useChatStore.getState().drafts.map((d) => d.id)).toEqual([draftId])
    expect(useChatStore.getState().drafts[0].model?.model_id).toBe('kimi-k3')

    // The route catches up with the placeholder it just created.
    useChatStore.getState().setActiveSession(draftId)
    expect(useChatStore.getState().drafts).toHaveLength(1)

    // Switching to a real session still abandons the placeholder.
    useChatStore.getState().setActiveSession('l2:real')
    expect(useChatStore.getState().drafts).toEqual([])
  })

  it('keeps a welcome-screen choice until the chat it starts exists', async () => {
    useChatStore.getState().setPendingModel({ provider_id: 'tencent', model_id: 'kimi-k3' })
    expect(useChatStore.getState().pendingModel?.model_id).toBe('kimi-k3')

    // Opening an existing chat abandons the choice: it belonged to a chat that
    // was never started.
    useChatStore.setState({ sessions: [session('l2:real', '2026-11-26T10:00:00Z')] })
    useChatStore.getState().setActiveSession('l2:real')
    expect(useChatStore.getState().pendingModel).toBeNull()
  })

  it('clears the welcome-screen choice once the session it created exists', async () => {
    vi.mocked(apiCreateL2Session).mockResolvedValueOnce({
      id: 'uuid-4',
      group: 'engineering',
      project_path: '',
      created_at: '2026-11-26T12:00:00Z',
    } as any)
    vi.mocked(apiSetSessionModel).mockResolvedValueOnce({
      selection: null,
      effective: null,
      source: 'default',
    })
    const model = { provider_id: 'tencent', model_id: 'kimi-k3' }
    useChatStore.getState().setPendingModel(model)

    const created = await useChatStore.getState().createL2Session('engineering', '', model)

    expect(created).toBe('l2:uuid-4')
    expect(vi.mocked(apiSetSessionModel)).toHaveBeenCalledWith('uuid-4', model)
    expect(useChatStore.getState().pendingModel).toBeNull()
  })

  it('drops the welcome-screen choice when a placeholder new chat starts', () => {
    useChatStore.getState().setPendingModel({ provider_id: 'tencent', model_id: 'kimi-k3' })

    const draftId = useChatStore.getState().beginDraft('engineering')

    // The placeholder shows its own (empty) choice, so the created session must
    // not silently inherit the abandoned one.
    expect(useChatStore.getState().pendingModel).toBeNull()
    expect(useChatStore.getState().drafts[0]).toMatchObject({ id: draftId })
    expect(useChatStore.getState().drafts[0].model).toBeUndefined()
  })

  it('beginDraft replaces a previous unstarted placeholder', () => {
    const first = useChatStore.getState().beginDraft('engineering')
    const second = useChatStore.getState().beginDraft('engineering')

    expect(second).not.toBe(first)
    expect(useChatStore.getState().drafts.map((d) => d.id)).toEqual([second])
  })

  it('deleteL2Session drops a draft without calling the API', async () => {
    const draftId = useChatStore.getState().beginDraft('engineering')

    await useChatStore.getState().deleteL2Session(draftId)

    expect(vi.mocked(apiDeleteL2Session)).not.toHaveBeenCalled()
    expect(useChatStore.getState().drafts).toEqual([])
    expect(useChatStore.getState().activeSessionId).toBeNull()
  })

  it('createL2Session replaces the placeholder and clears it', async () => {
    vi.mocked(apiCreateL2Session).mockResolvedValueOnce({
      id: 'uuid-1',
      group: 'engineering',
      project_path: '/repo',
      created_at: '2026-11-26T12:00:00Z',
    } as any)
    const draftId = useChatStore.getState().beginDraft('engineering', '/repo')

    const created = await useChatStore.getState().createL2Session('engineering', '/repo')

    expect(created).toBe('l2:uuid-1')
    expect(useChatStore.getState().drafts).toEqual([])
    expect(useChatStore.getState().activeSessionId).toBe('l2:uuid-1')
    expect(draftId).not.toBe(created)
    expect(useChatStore.getState().sessions.map((s) => s.id)).toEqual(['l2:uuid-1'])
  })

  it('setDraftModel keeps the choice on the placeholder only', () => {
    const draftId = useChatStore.getState().beginDraft('engineering')
    const selection = { provider_id: 'tencent', model_id: 'kimi-k3' }

    useChatStore.getState().setDraftModel(draftId, selection)

    expect(useChatStore.getState().drafts[0].model).toEqual(selection)
    expect(vi.mocked(apiSetSessionModel)).not.toHaveBeenCalled()

    useChatStore.getState().setDraftModel(draftId, null)

    expect(useChatStore.getState().drafts[0].model).toBeUndefined()
  })

  it('createL2Session applies the placeholder model before activating the session', async () => {
    vi.mocked(apiCreateL2Session).mockResolvedValueOnce({
      id: 'uuid-2',
      group: 'engineering',
      project_path: '',
      created_at: '2026-11-26T12:00:00Z',
    } as any)
    // The composer reads the model from the server as soon as the session is
    // active, so the write must land before the store switches to it.
    const draftId = useChatStore.getState().beginDraft('engineering')
    const writes: Array<string | null> = []
    vi.mocked(apiSetSessionModel).mockImplementationOnce(async () => {
      writes.push(useChatStore.getState().activeSessionId)
      return { selection: null, effective: null, source: 'default' }
    })
    const model = { provider_id: 'tencent', model_id: 'kimi-k3', reasoning_effort: 'high' }

    const created = await useChatStore.getState().createL2Session('engineering', '', model)

    expect(vi.mocked(apiSetSessionModel)).toHaveBeenCalledWith('uuid-2', model)
    expect(writes).toEqual([draftId])
    expect(created).toBe('l2:uuid-2')
    expect(useChatStore.getState().activeSessionId).toBe('l2:uuid-2')
  })

  it('createL2Session keeps the default model when applying the choice fails', async () => {
    vi.mocked(apiCreateL2Session).mockResolvedValueOnce({
      id: 'uuid-3',
      group: 'engineering',
      project_path: '',
      created_at: '2026-11-26T12:00:00Z',
    } as any)
    vi.mocked(apiSetSessionModel).mockRejectedValueOnce(new Error('rejected'))

    const created = await useChatStore
      .getState()
      .createL2Session('engineering', '', { provider_id: 'tencent', model_id: 'gone' })

    expect(created).toBe('l2:uuid-3')
    expect(useChatStore.getState().activeSessionId).toBe('l2:uuid-3')
  })
})
