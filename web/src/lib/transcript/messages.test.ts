import { describe, expect, it } from 'vitest'
import type { TranscriptFrame, TranscriptSnapshot } from '@/types'
import { applyFrames, fromSnapshot, type TranscriptState } from './reducer'
import { buildMessages, type LocalInput, type MessageCache } from './messages'

const EPOCH = 'e1'
let seq = 0

function frame(kind: TranscriptFrame['kind'], fields: Partial<TranscriptFrame> = {}): TranscriptFrame {
  seq++
  return { seq, ts: `2026-01-01T00:00:${String(seq).padStart(2, '0')}Z`, kind, ...fields }
}

function start(): TranscriptState {
  seq = 0
  const snapshot: TranscriptSnapshot = {
    session_id: 'l2:s', epoch: EPOCH, seq: 0, items: [], pending: [], requests: [],
    has_more: false, compactions: [], ctxwin_used: 0, ctxwin_limit: 0,
  }
  return fromSnapshot(snapshot)
}

function run(state: TranscriptState, frames: TranscriptFrame[]) {
  const result = applyFrames(state, EPOCH, frames)
  expect(result.resync).toBeUndefined()
  return result.state
}

function render(state: TranscriptState, local: LocalInput[] = [], cache: MessageCache = new Map()) {
  return buildMessages(state, local, [], cache)
}

const layout = (state: TranscriptState, local: LocalInput[] = []) =>
  render(state, local).messages.map(
    (m) => `${m.role}:${m.segments.map((s) => ('text' in s ? s.text : s.type)).join('|')}${m.continuation ? ' (cont)' : ''}${m.queued ? ' (queued)' : ''}`
  )

describe('transcript messages', () => {
  it('keeps a reply under its question when a follow-up is sent before the first token', () => {
    // Q1 commits, Q2 is queued while Q1 routes, Q1's reply streams, Q2 is drained.
    let state = start()
    state = run(state, [
      frame('request_started', { request_id: 'r1' }),
      frame('input_committed', { request_id: 'r1', item_id: 'in:r1', text: 'Q1' }),
      frame('input_queued', { request_id: 'r2', text: 'Q2' }),
    ])
    expect(layout(state)).toEqual(['user:Q1', 'user:Q2 (queued)'])
    state = run(state, [frame('item_delta', { request_id: 'r1', item_id: 'out:r1:0', text: 'A1' })])
    expect(layout(state)).toEqual(['user:Q1', 'assistant:A1', 'user:Q2 (queued)'])
    state = run(state, [
      frame('input_committed', { request_id: 'r2', item_id: 'in:r2', text: 'Q2' }),
      frame('item_delta', { request_id: 'r2', item_id: 'out:r2:1', text: 'A2' }),
    ])
    expect(layout(state)).toEqual(['user:Q1', 'assistant:A1', 'user:Q2', 'assistant:A2'])
  })

  it('renders resumed output after later items as a continuation', () => {
    let state = start()
    state = run(state, [
      frame('request_started', { request_id: 'r1' }),
      frame('input_committed', { request_id: 'r1', item_id: 'in:r1', text: 'Q1' }),
      frame('item_delta', { request_id: 'r1', item_id: 'out:r1:0', text: 'delegating' }),
      frame('tool_started', { request_id: 'r1', item_id: 'out:r1:0', call_id: 'd', name: 'delegate_x' }),
      frame('input_committed', { request_id: 'r2', item_id: 'in:r2', text: 'Q2' }),
      frame('item_delta', { request_id: 'r1', item_id: 'out:r1:1', text: 'delegation result' }),
      frame('item_delta', { request_id: 'r2', item_id: 'out:r2:0', text: 'A2' }),
    ])
    expect(layout(state)).toEqual([
      'user:Q1',
      'assistant:delegating|tool_call',
      'user:Q2',
      'assistant:delegation result (cont)',
      'assistant:A2',
    ])
  })

  it('groups consecutive iterations of one request and reports whether it runs', () => {
    let state = start()
    state = run(state, [
      frame('request_started', { request_id: 'r' }),
      frame('input_committed', { request_id: 'r', item_id: 'in:r', text: 'Q' }),
      frame('item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'step' }),
      frame('item_delta', { request_id: 'r', item_id: 'out:r:1', text: 'done' }),
    ])
    let messages = render(state).messages
    expect(messages).toHaveLength(2)
    expect(messages[1]).toMatchObject({ id: 'out:r:0', requestId: 'r', running: true })
    state = run(state, [frame('request_end', { request_id: 'r', terminal_code: 'completed' })])
    messages = render(state).messages
    expect(messages[1].running).toBe(false)
  })

  it('keeps unchanged bubbles by reference', () => {
    let state = start()
    state = run(state, [
      frame('input_committed', { request_id: 'r', item_id: 'in:r', text: 'Q' }),
      frame('request_started', { request_id: 'r' }),
      frame('item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'a' }),
    ])
    const first = render(state)
    state = run(state, [frame('item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'b' })])
    const second = render(state, [], first.cache)
    expect(second.messages[0]).toBe(first.messages[0])
    expect(second.messages[1]).not.toBe(first.messages[1])
  })

  it('shows unacknowledged and failed local sends after the transcript', () => {
    let state = start()
    state = run(state, [frame('input_committed', { request_id: 'r', item_id: 'in:r', text: 'Q' })])
    const local: LocalInput[] = [
      { requestId: 'x', sessionId: 'l2:s', text: 'sending', status: 'sending', createdAt: 't1' },
      { requestId: 'r', sessionId: 'l2:s', text: 'Q', status: 'accepted', createdAt: 't0' },
      { requestId: 'y', sessionId: 'l2:s', text: 'too big', status: 'failed', error: 'too large', createdAt: 't2' },
    ]
    expect(layout(state, local)).toEqual(['user:Q', 'user:sending', 'user:too big', 'assistant:too large'])
  })

  it('places command results after the items they followed', () => {
    let state = start()
    state = run(state, [frame('input_committed', { request_id: 'r', item_id: 'in:r', text: 'Q' })])
    const { messages } = buildMessages(state, [], [{ requestId: 'h', sessionId: 'l2:s', prompt: '/help', text: 'commands', afterSeq: 1, ts: 't' }], new Map())
    state = run(state, [frame('item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'A' })])
    const later = buildMessages(state, [], [{ requestId: 'h', sessionId: 'l2:s', prompt: '/help', text: 'commands', afterSeq: 1, ts: 't' }], new Map())
    expect(messages.map((m) => m.id)).toEqual(['in:r', 'command:h', 'command-result:h'])
    expect(later.messages.map((m) => m.id)).toEqual(['in:r', 'command:h', 'command-result:h', 'out:r:0'])
  })

  it('drops a zero-length assistant item instead of rendering an empty bubble', () => {
    // /compact reports through a transcript segment, so no content delta arrives.
    let state = start()
    state = run(state, [
      frame('request_started', { request_id: 'r', item_id: 'in:r' }),
      frame('input_committed', { request_id: 'r', item_id: 'in:r', text: '/compact' }),
    ])
    state = run(state, [
      frame('item_snapshot', {
        request_id: 'r',
        item_id: 'out:r:0',
        segments: [{ type: 'compact', text: 'summary' }],
      }),
      frame('request_ended', { request_id: 'r' }),
    ])
    expect(layout(state)).toEqual(['user:/compact', 'assistant:summary'])
  })

  it('keeps the command prompt without an empty assistant answer', () => {
    let state = start()
    state = run(state, [frame('input_committed', { request_id: 'r', item_id: 'in:r', text: 'Q' })])
    const { messages } = buildMessages(
      state,
      [],
      [{ requestId: 'c', sessionId: 'l2:s', prompt: '/compact', text: '', afterSeq: 1, ts: 't' }],
      new Map()
    )
    expect(messages.map((m) => m.id)).toEqual(['in:r', 'command:c'])
  })
})
