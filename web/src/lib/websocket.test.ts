import chineseSamples from '@/testdata/chinese_samples.json'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { wsManager } from './websocket'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useChatStore } from '@/stores/chatStore'
import { useTranscriptStore } from '@/stores/transcriptStore'
import { emptyTranscript, type TranscriptState } from '@/lib/transcript/reducer'
import type { TranscriptFrame } from '@/types'

interface WSInstance {
  url: string
  readyState: number
  sent: string[]
  onopen: ((ev?: any) => void) | null
  onmessage: ((ev: MessageEvent) => void) | null
  onclose: ((ev?: any) => void) | null
  onerror: ((ev?: any) => void) | null
  send: (data: string) => void
  close: ReturnType<typeof vi.fn>
}

let socket: WSInstance | null = null
const sockets: WSInstance[] = []

beforeEach(() => {
  localStorage.clear()
  useRuntimeStore.setState({ status: null, connectionStatus: 'disconnected' })
  useConnectionStore.setState({
    mode: 'local',
    remoteUrl: '',
    backendReady: true,
    backendStatus: { running: true, pid: null, uptime: 0 },
  })
  useChatStore.setState({
    sessions: [{ id: 'l2:live', type: 'l2', name: '', createdAt: '' }],
    activeRequests: {},
    routeSessions: {},
    streamingSessions: {},
    delegatingSessions: {},
    systemCommandSessions: {},
  })
  useTranscriptStore.setState({ sessions: {}, local: {}, commands: {} })
  wsManager.disconnect()
  socket = null
  sockets.length = 0
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}', { status: 200 }))
  const WebSocketStub = vi.fn(function (this: WSInstance, url: string) {
    this.url = url
    this.readyState = 0
    this.sent = []
    this.onopen = null
    this.onmessage = null
    this.onclose = null
    this.onerror = null
    this.send = (data: string) => {
      this.sent.push(data)
    }
    this.close = vi.fn(() => {
      this.readyState = 3
      this.onclose?.()
    })
    sockets.push(this)
  })
  vi.stubGlobal('WebSocket', Object.assign(WebSocketStub, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 }))
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function current(): WSInstance {
  socket = sockets[sockets.length - 1] ?? null
  return socket!
}

function open() {
  current().readyState = 1
  current().onopen?.({ type: 'open' })
}

function receive(data: unknown) {
  current().onmessage?.({ data: JSON.stringify(data) } as unknown as MessageEvent)
}

function sentMessages() {
  return current().sent.filter((data) => data !== 'ping').map((data) => JSON.parse(data))
}

function liveSession(sessionId: string, state: Partial<TranscriptState> = {}) {
  useTranscriptStore.setState((store) => ({
    sessions: {
      ...store.sessions,
      [sessionId]: {
        state: { ...emptyTranscript(), epoch: 'e1', ...state },
        status: 'live',
        hasMore: false,
        loadingOlder: false,
      },
    },
  }))
}

function frame(seq: number, kind: TranscriptFrame['kind'], fields: Partial<TranscriptFrame> = {}): TranscriptFrame {
  return { seq, ts: 't', kind, ...fields }
}

describe('websocket connection', () => {
  it('connect opens WebSocket and sets connected status', async () => {
    await wsManager.connect()
    open()
    expect(useRuntimeStore.getState().connectionStatus).toBe('connected')
    expect(vi.mocked(fetch)).not.toHaveBeenCalled()
  })

  it('opens a remote WebSocket without an application token', async () => {
    useConnectionStore.setState({ mode: 'remote', remoteUrl: 'https://remote.example' })
    await wsManager.connect()
    expect(current().url).toBe('wss://remote.example/ws')
  })

  it('notifies status and runtime subscribers', async () => {
    const onStatus = vi.fn()
    const onRuntime = vi.fn()
    const unsubscribe = wsManager.subscribe('status', onStatus)
    wsManager.subscribe('runtime', onRuntime)
    await wsManager.connect()
    open()
    expect(onStatus).toHaveBeenCalledWith('connected')
    const runtime = { phase: 'idle', agent_streams: {} }
    receive({ type: 'state', runtime, agents: { agents: [], supervisors: [] } })
    expect(onRuntime).toHaveBeenCalledWith(expect.objectContaining({ phase: 'idle' }))
    unsubscribe()
    wsManager.disconnect()
    expect(onStatus).not.toHaveBeenCalledWith('disconnected')
    expect(useRuntimeStore.getState().connectionStatus).toBe('disconnected')
  })

  it('never queues chat_send while disconnected but queues other messages', async () => {
    expect(wsManager.send({ type: 'chat_send', request_id: 'r', session_id: 'l1', prompt: 'hi' })).toBe(false)
    expect(wsManager.send({ type: 'chat_cancel', request_id: 'r', session_id: 'l1' })).toBe(true)
    await wsManager.connect()
    open()
    expect(sentMessages()).toEqual([{ type: 'chat_cancel', request_id: 'r', session_id: 'l1' }])
  })
})

describe('websocket transcript transport', () => {
  it('resubscribes every live transcript from its cursor after reconnecting', async () => {
    liveSession('l2:live', { headSeq: 42 })
    useTranscriptStore.setState((store) => ({
      sessions: { ...store.sessions, 'l2:closed': { ...store.sessions['l2:live'], status: 'idle' } },
    }))
    await wsManager.connect()
    open()
    expect(sentMessages()).toEqual([{ type: 'subscribe', session_id: 'l2:live', epoch: 'e1', after_seq: 42 }])
  })

  it('applies frames and reassembles split CJK deltas in order', async () => {
    liveSession('l2:live')
    await wsManager.connect()
    open()
    const text: string = chineseSamples.websocket_test_001
    const half = Math.floor(text.length / 2)
    receive({
      type: 'frames',
      session_id: 'l2:live',
      epoch: 'e1',
      frames: [
        frame(1, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'q' }),
        frame(2, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: text.slice(0, half) }),
      ],
    })
    receive({ type: 'frames', session_id: 'l2:live', epoch: 'e1', frames: [frame(3, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: text.slice(half) })] })
    const state = useTranscriptStore.getState().sessions['l2:live'].state
    expect(state.order).toEqual(['in:r', 'out:r:0'])
    expect(state.items['out:r:0'].segments[0].text).toBe(text)
  })

  it('routes resync, acknowledgements and command results to the transcript store', async () => {
    liveSession('l2:live')
    const resync = vi.spyOn(useTranscriptStore.getState(), 'resync').mockResolvedValue()
    const ack = vi.spyOn(useTranscriptStore.getState(), 'ackLocalInput')
    const command = vi.spyOn(useTranscriptStore.getState(), 'addCommandResult')
    await wsManager.connect()
    open()
    receive({ type: 'resync_required', session_id: 'l2:live', epoch: 'e2', head_seq: 9, reason: 'epoch' })
    receive({ type: 'resync_required', session_id: 'l2:unknown', epoch: 'e2', head_seq: 9, reason: 'epoch' })
    receive({ type: 'chat_ack', request_id: 'r', session_id: 'l2:live', status: 'queued' })
    receive({ type: 'command_result', request_id: 'h', session_id: 'l2:live', content: 'help text' })
    expect(resync).toHaveBeenCalledTimes(1)
    expect(resync).toHaveBeenCalledWith('l2:live')
    expect(ack).toHaveBeenCalledWith('r', 'queued', undefined)
    expect(command).toHaveBeenCalledWith('h', 'l2:live', 'help text')
  })

  it('lets runtime snapshots drive only sessions without a live transcript', async () => {
    liveSession('l2:live')
    await wsManager.connect()
    open()
    receive({
      type: 'state',
      runtime: {
        sessions: {
          'l2:live': { session_id: 'l2:live', state: 'processing', revision: 1 },
          'l2:other': { session_id: 'l2:other', state: 'processing', revision: 1, delegating: true },
        },
      },
    })
    let chat = useChatStore.getState()
    expect(chat.streamingSessions['l2:live']).toBeUndefined()
    expect(chat.streamingSessions['l2:other']).toBe(true)
    expect(chat.delegatingSessions['l2:other']).toBe(true)
    receive({ type: 'state', runtime: { sessions: { 'l2:other': { session_id: 'l2:other', state: 'idle', revision: 2 } } } })
    chat = useChatStore.getState()
    expect(chat.streamingSessions['l2:other']).toBe(false)
  })
})
