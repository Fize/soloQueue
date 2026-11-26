import { describe, expect, it } from 'vitest'
import type { TranscriptFrame, TranscriptSnapshot } from '@/types'
import { applyFrames, fromSnapshot, orderedItems, prependOlder, type TranscriptState } from './reducer'

const EPOCH = 'e1'

function frame(seq: number, kind: TranscriptFrame['kind'], fields: Partial<TranscriptFrame> = {}): TranscriptFrame {
  return { seq, ts: `2026-01-01T00:00:${String(seq).padStart(2, '0')}Z`, kind, ...fields }
}

function snapshot(fields: Partial<TranscriptSnapshot> = {}): TranscriptSnapshot {
  return {
    session_id: 'l2:s',
    epoch: EPOCH,
    seq: 0,
    items: [],
    pending: [],
    requests: [],
    has_more: false,
    compactions: [],
    ctxwin_used: 0,
    ctxwin_limit: 0,
    ...fields,
  }
}

function apply(state: TranscriptState, frames: TranscriptFrame[]) {
  const result = applyFrames(state, EPOCH, frames)
  expect(result.resync).toBeUndefined()
  return result.state
}

const ids = (state: TranscriptState) => orderedItems(state).map((item) => item.id)

describe('transcript reducer', () => {
  it('attributes sibling delegations to distinct dispatches', () => {
    const state = apply(fromSnapshot(snapshot()), [
      frame(1, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'go' }),
      frame(2, 'tool_started', { request_id: 'r', item_id: 'out:r:0', call_id: 'c1', name: 'delegate', args: '{}' }),
      frame(3, 'tool_started', { request_id: 'r', item_id: 'out:r:0', call_id: 'c2', name: 'delegate', args: '{}' }),
      frame(4, 'dispatch_started', { request_id: 'r', call_id: 'c1', dispatch_id: 'dlg-a', agent_instance_id: 'inst-a' }),
      frame(5, 'dispatch_started', { request_id: 'r', call_id: 'c2', dispatch_id: 'dlg-b', agent_instance_id: 'inst-b' }),
    ])
    const segments = state.items['out:r:0'].segments
    expect(segments.map((segment) => [segment.dispatch_id, segment.agent_instance_id])).toEqual([
      ['dlg-a', 'inst-a'],
      ['dlg-b', 'inst-b'],
    ])
  })

  it('keeps the dispatch identity when the delegation frame arrives first', () => {
    const state = apply(fromSnapshot(snapshot()), [
      frame(1, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'go' }),
      frame(2, 'dispatch_started', { request_id: 'r', call_id: 'c1', dispatch_id: 'dlg-a', agent_instance_id: 'inst-a' }),
      frame(3, 'tool_started', { request_id: 'r', item_id: 'out:r:0', call_id: 'c1', name: 'delegate', args: '{}' }),
    ])
    expect(state.items['out:r:0'].segments[0]).toMatchObject({
      dispatch_id: 'dlg-a',
      agent_instance_id: 'inst-a',
    })
  })

  it('materializes items exactly like the server view', () => {
    const state = apply(fromSnapshot(snapshot()), [
      frame(1, 'input_queued', { request_id: 'q', text: 'later' }),
      frame(2, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'hello', received_at: 't0' }),
      frame(3, 'item_delta', { request_id: 'r', item_id: 'out:r:0', channel: 'reasoning', text: 'think ' }),
      frame(4, 'item_delta', { request_id: 'r', item_id: 'out:r:0', channel: 'reasoning', text: 'more' }),
      frame(5, 'item_delta', { request_id: 'r', item_id: 'out:r:0', channel: 'content', text: 'answer' }),
      frame(6, 'tool_started', { request_id: 'r', item_id: 'out:r:0', call_id: 'c1', name: 'echo', args: '{}' }),
      frame(7, 'input_committed', { request_id: 'q', item_id: 'in:q', text: 'later' }),
      frame(8, 'tool_finished', { request_id: 'r', call_id: 'c1', result: 'ok' }),
      frame(9, 'item_delta', { request_id: 'r', item_id: 'out:r:1', channel: 'content', text: 'resumed' }),
      frame(10, 'request_end', { request_id: 'r', request_ids: ['r', 'q'], terminal_code: 'error', error: 'boom' }),
    ])
    expect(ids(state)).toEqual(['in:r', 'out:r:0', 'in:q', 'out:r:1', 'err:r'])
    const first = state.items['out:r:0']
    expect(first.segments).toEqual([
      { type: 'thinking', text: 'think more' },
      { type: 'content', text: 'answer' },
      { type: 'tool_call', call_id: 'c1', name: 'echo', args: '{}', done: true, result: 'ok', error: undefined, duration_ms: undefined },
    ])
    expect(first.done).toBe(true)
    expect(state.pending).toEqual({})
    expect(state.requests.r).toMatchObject({ ended: true, error: 'boom' })
    expect(state.headSeq).toBe(10)
  })

  it('splits an aggregated input into one item per queued request', () => {
    const state = apply(fromSnapshot(snapshot()), [
      frame(1, 'input_committed', {
        request_id: 'b',
        item_id: 'in:b',
        request_ids: ['a', 'b'],
        parts: [
          { request_id: 'a', item_id: 'in:a', text: 'one' },
          { request_id: 'b', item_id: 'in:b', text: 'two' },
        ],
      }),
    ])
    expect(ids(state)).toEqual(['in:a', 'in:b'])
    expect(state.items['in:b'].sub).toBe(1)
  })

  it('skips replayed frames and asks for a resync on a gap, epoch change or reset', () => {
    const base = apply(fromSnapshot(snapshot({ seq: 4 })), [frame(5, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'a' })])
    const replay = applyFrames(base, EPOCH, [frame(4, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'dup' }), frame(5, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'dup' })])
    expect(replay.resync).toBeUndefined()
    expect(replay.state.items['out:r:0'].segments[0].text).toBe('a')

    const gap = applyFrames(base, EPOCH, [frame(7, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'x' })])
    expect(gap.resync).toBe('gap')
    expect(gap.state.headSeq).toBe(5)

    expect(applyFrames(base, 'other', [frame(6, 'item_done', { item_id: 'out:r:0' })]).resync).toBe('epoch')
    expect(applyFrames(base, EPOCH, [frame(6, 'transcript_reset', { reason: 'clear' })]).resync).toBe('reset')
  })

  it('keeps untouched items by reference', () => {
    const base = apply(fromSnapshot(snapshot()), [
      frame(1, 'input_committed', { request_id: 'r', item_id: 'in:r', text: 'q' }),
      frame(2, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'a' }),
    ])
    const next = apply(base, [frame(3, 'item_delta', { request_id: 'r', item_id: 'out:r:0', text: 'b' })])
    expect(next.items['in:r']).toBe(base.items['in:r'])
    expect(next.items['out:r:0']).not.toBe(base.items['out:r:0'])
    expect(base.items['out:r:0'].segments[0].text).toBe('a')
    expect(next.items['out:r:0'].segments[0].text).toBe('ab')
  })

  it('completes persisted tool calls and dispatches from frames after a snapshot', () => {
    const state = fromSnapshot(
      snapshot({
        seq: 10,
        items: [
          {
            id: 'out:r:0',
            kind: 'assistant',
            request_id: 'r',
            seq: 3,
            ts: 't',
            segments: [{ type: 'tool_call', call_id: 'd1', name: 'delegate_x', done: false }],
          },
        ],
      })
    )
    const next = apply(state, [
      frame(11, 'dispatch_terminal', { request_id: 'r', call_id: 'd1', dispatch_id: 'x', error: 'failed' }),
    ])
    expect(next.items['out:r:0'].segments[0]).toMatchObject({ done: true, dispatch_id: 'x', error: 'failed' })
  })

  it('merges older pages without replacing live copies', () => {
    const state = apply(fromSnapshot(snapshot({ seq: 5, items: [{ id: 'in:new', kind: 'user', seq: 5, ts: 't', segments: [] }] })), [
      frame(6, 'item_delta', { request_id: 'new', item_id: 'out:new:0', text: 'live' }),
    ])
    const merged = prependOlder(state, [
      { id: 'in:old', kind: 'user', seq: 1, ts: 't', segments: [] },
      { id: 'in:new', kind: 'user', seq: 5, ts: 't', segments: [{ type: 'content', text: 'stale' }] },
    ])
    expect(ids(merged)).toEqual(['in:old', 'in:new', 'out:new:0'])
    expect(merged.items['in:new']).toBe(state.items['in:new'])
  })
})
