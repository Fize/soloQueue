import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { TranscriptFrame, TranscriptSnapshot } from '@/types'
import { fetchTranscript } from '@/lib/api'
import { useChatStore } from './chatStore'
import {
  setTranscriptTransport,
  transcriptSubscriptions,
  useTranscriptStore,
  isTranscriptLive,
} from './transcriptStore'
import { useTranscript } from '@/hooks/useTranscript'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, fetchTranscript: vi.fn() }
})

const transport = { subscribe: vi.fn(), unsubscribe: vi.fn() }

function snapshot(fields: Partial<TranscriptSnapshot> = {}): TranscriptSnapshot {
  return {
    session_id: 'l2:s', epoch: 'e1', seq: 0, items: [], pending: [], requests: [],
    has_more: false, compactions: [], ctxwin_used: 0, ctxwin_limit: 0, ...fields,
  }
}

function frame(seq: number, kind: TranscriptFrame['kind'], fields: Partial<TranscriptFrame> = {}): TranscriptFrame {
  return { seq, ts: `2026-01-01T00:00:${String(seq).padStart(2, '0')}Z`, kind, ...fields }
}

beforeEach(() => {
  vi.mocked(fetchTranscript).mockReset()
  transport.subscribe.mockReset()
  transport.unsubscribe.mockReset()
  setTranscriptTransport(transport)
  for (const id of Object.keys(useTranscriptStore.getState().sessions)) useTranscriptStore.getState().forget(id)
  useTranscriptStore.setState({ sessions: {}, local: {}, commands: {} })
  useChatStore.setState({
    sessions: [{ id: 'l2:s', type: 'l2', name: '', createdAt: '' }],
    activeSessionId: 'l2:s',
    compactions: {},
    compactionClearWatermarks: {},
    activeRequests: {},
    streamingSessions: {},
    systemCommandSessions: {},
    delegatingSessions: {},
    routeSessions: {},
  })
})

describe('transcriptStore', () => {
  it('subscribes after the snapshot watermark and applies frames in order', async () => {
    vi.mocked(fetchTranscript).mockResolvedValue(snapshot({ seq: 7 }))
    await useTranscriptStore.getState().open('l2:s')
    expect(transport.subscribe).toHaveBeenCalledWith('l2:s', 'e1', 7)
    expect(isTranscriptLive('l2:s')).toBe(true)

    useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [
      frame(8, 'request_started', { request_id: 'r' }),
      frame(9, 'request_route', { request_id: 'r', model_id: 'm', task_type: 'engineering' }),
      frame(10, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'hi' }),
      frame(11, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'yo' }),
    ])
    const entry = useTranscriptStore.getState().sessions['l2:s']
    expect(entry.state.order).toEqual(['in:r', 'out:r:0'])
    const chat = useChatStore.getState()
    expect(chat.streamingSessions['l2:s']).toBe(true)
    expect(chat.routeSessions['l2:s']).toMatchObject({ requestId: 'r', modelId: 'm', taskLevel: 'engineering' })
    expect(chat.activeRequests.r).toMatchObject({ sessionId: 'l2:s', status: 'streaming' })

    useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [
      frame(12, 'request_end', { request_id: 'r', terminal_code: 'completed' }),
    ])
    expect(useChatStore.getState().streamingSessions['l2:s']).toBe(false)
    expect(useChatStore.getState().activeRequests.r).toBeUndefined()
  })

  it('reloads the snapshot when a frame is missing', async () => {
    vi.mocked(fetchTranscript).mockResolvedValueOnce(snapshot({ seq: 2 }))
    await useTranscriptStore.getState().open('l2:s')
    vi.mocked(fetchTranscript).mockResolvedValueOnce(
      snapshot({ seq: 9, items: [{ id: 'in:r', kind: 'user', request_id: 'r', seq: 4, ts: 't', segments: [] }] })
    )
    act(() => {
      useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [frame(5, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'x' })])
    })
    await waitFor(() => expect(transport.subscribe).toHaveBeenLastCalledWith('l2:s', 'e1', 9))
    expect(useTranscriptStore.getState().sessions['l2:s'].state.order).toEqual(['in:r'])
  })

  it('drops a snapshot response superseded by a newer load', async () => {
    let resolveOld!: (value: TranscriptSnapshot) => void
    vi.mocked(fetchTranscript)
      .mockImplementationOnce(() => new Promise((resolve) => (resolveOld = resolve)))
      .mockResolvedValueOnce(snapshot({ seq: 5, items: [{ id: 'in:new', kind: 'user', seq: 5, ts: 't', segments: [] }] }))
    const older = useTranscriptStore.getState().open('l2:s')
    await useTranscriptStore.getState().resync('l2:s')
    resolveOld(snapshot({ seq: 1 }))
    await older
    const entry = useTranscriptStore.getState().sessions['l2:s']
    expect(entry.state.headSeq).toBe(5)
    expect(entry.state.order).toEqual(['in:new'])
  })

  it('tracks a local send until the server commits it', async () => {
    vi.mocked(fetchTranscript).mockResolvedValue(snapshot({ seq: 1 }))
    await useTranscriptStore.getState().open('l2:s')
    useTranscriptStore.getState().addLocalInput({ requestId: 'r', sessionId: 'l2:s', text: 'hi', status: 'sending', createdAt: 't' })
    expect(useChatStore.getState().streamingSessions['l2:s']).toBe(true)
    useTranscriptStore.getState().ackLocalInput('r', 'accepted')
    expect(useTranscriptStore.getState().local.r.status).toBe('accepted')
    useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [frame(2, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'hi' })])
    expect(useTranscriptStore.getState().local.r).toBeUndefined()

    useTranscriptStore.getState().addLocalInput({ requestId: 'x', sessionId: 'l2:s', text: 'no', status: 'sending', createdAt: 't' })
    useTranscriptStore.getState().ackLocalInput('x', 'rejected', 'busy')
    expect(useTranscriptStore.getState().local.x).toMatchObject({ status: 'failed', error: 'busy' })
  })

  it('retires a command input whose end frame fell into the reset resync gap', async () => {
    vi.mocked(fetchTranscript).mockResolvedValueOnce(snapshot({ seq: 400 }))
    await useTranscriptStore.getState().open('l2:s')
    const store = useTranscriptStore.getState()
    store.addLocalInput({ requestId: 'clr', sessionId: 'l2:s', text: '/clear', status: 'sending', createdAt: 't', systemCommand: true })
    store.ackLocalInput('clr', 'accepted')
    expect(useChatStore.getState().streamingSessions['l2:s']).toBe(true)

    // /clear publishes request_started, transcript_reset and request_end within a
    // millisecond. The reset stops the batch and the snapshot is already past the end.
    vi.mocked(fetchTranscript).mockResolvedValueOnce(
      snapshot({
        seq: 403,
        requests: [{ request_id: 'clr', ended: true, terminal_code: 'completed', started_seq: 401, ended_seq: 403 }],
      })
    )
    act(() => {
      useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [
        frame(401, 'request_started', { request_id: 'clr' }),
        frame(402, 'transcript_reset', { reason: 'clear' }),
        frame(403, 'request_end', { request_id: 'clr', terminal_code: 'completed' }),
      ])
    })
    await waitFor(() => expect(useTranscriptStore.getState().local.clr).toBeUndefined())
    expect(useChatStore.getState().streamingSessions['l2:s']).toBe(false)
    expect(useChatStore.getState().systemCommandSessions['l2:s']).toBeFalsy()
  })

  it('settles local inputs a snapshot already commits, queues or ends, and keeps unseen ones', async () => {
    vi.mocked(fetchTranscript).mockResolvedValueOnce(snapshot({ seq: 1 }))
    await useTranscriptStore.getState().open('l2:s')
    const store = useTranscriptStore.getState()
    for (const requestId of ['committed', 'queued', 'ended', 'folded', 'unseen']) {
      store.addLocalInput({ requestId, sessionId: 'l2:s', text: requestId, status: 'sending', createdAt: 't' })
      store.ackLocalInput(requestId, 'accepted')
    }
    vi.mocked(fetchTranscript).mockResolvedValueOnce(
      snapshot({
        seq: 9,
        items: [{ id: 'in:committed', kind: 'user', request_id: 'committed', seq: 5, ts: 't', segments: [] }],
        pending: [{ request_id: 'queued', text: 'queued', seq: 6 }],
        requests: [{ request_id: 'root', request_ids: ['folded'], ended: true, started_seq: 2, ended_seq: 8 }, { request_id: 'ended', ended: true }],
      })
    )
    await useTranscriptStore.getState().resync('l2:s')
    expect(Object.keys(useTranscriptStore.getState().local)).toEqual(['unseen'])
    // The unseen request may still start; the tab stays busy until it does.
    expect(useChatStore.getState().streamingSessions['l2:s']).toBe(true)
  })

  it('drops command results of the conversation a reset cleared', async () => {
    vi.mocked(fetchTranscript).mockResolvedValue(snapshot({ seq: 3 }))
    await useTranscriptStore.getState().open('l2:s')
    useTranscriptStore.getState().addCommandResult('help', 'l2:s', 'Available commands')
    useTranscriptStore.getState().addCommandResult('other', 'l2:other', 'kept')
    expect(Object.keys(useTranscriptStore.getState().commands).sort()).toEqual(['help', 'other'])
    act(() => {
      useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [frame(4, 'transcript_reset', { reason: 'clear' })])
    })
    expect(Object.keys(useTranscriptStore.getState().commands)).toEqual(['other'])
  })

  it('resumes a closed session from its cursor without a snapshot', async () => {
    vi.mocked(fetchTranscript).mockResolvedValue(snapshot({ seq: 3 }))
    await useTranscriptStore.getState().open('l2:s')
    useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [frame(4, 'session_meta', { session_name: 'Titled' })])
    expect(useChatStore.getState().sessions[0].name).toBe('Titled')
    expect(transcriptSubscriptions()).toEqual([{ sessionId: 'l2:s', epoch: 'e1', afterSeq: 4 }])

    useTranscriptStore.getState().close('l2:s')
    expect(transport.unsubscribe).toHaveBeenCalledWith('l2:s')
    expect(transcriptSubscriptions()).toEqual([])
    await useTranscriptStore.getState().open('l2:s')
    expect(fetchTranscript).toHaveBeenCalledTimes(1)
    expect(transport.subscribe).toHaveBeenLastCalledWith('l2:s', 'e1', 4)
  })

  it('renders through useTranscript with real stores', async () => {
    vi.mocked(fetchTranscript).mockResolvedValue(
      snapshot({ seq: 2, items: [{ id: 'in:r', kind: 'user', request_id: 'r', seq: 1, ts: 't', segments: [{ type: 'content', text: 'hello' }] }] })
    )
    const { result } = renderHook(() => useTranscript('l2:s'))
    await waitFor(() => expect(result.current.status).toBe('live'))
    expect(result.current.messages.map((m) => m.id)).toEqual(['in:r'])
    const before = result.current.messages
    act(() => {
      useTranscriptStore.getState().receiveFrames('l2:s', 'e1', [frame(3, 'request_started', { request_id: 'r' })])
    })
    expect(result.current.messages[0]).toBe(before[0])
  })
})
