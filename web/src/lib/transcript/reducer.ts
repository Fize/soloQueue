// Pure transcript reducer. It mirrors internal/transcript View.Apply rule for
// rule: the server materializes in-flight items with the same logic, so a
// snapshot plus the frames after its watermark always equals a later snapshot.
//
// Updates are copy-on-write: an item that a frame does not touch keeps its
// object identity, which lets memoized message views skip re-rendering.

import type {
  TranscriptFrame,
  TranscriptItem,
  TranscriptPendingInput,
  TranscriptRequestState,
  TranscriptSegment,
  TranscriptSnapshot,
} from '@/types/transcript'

export interface TranscriptState {
  epoch: string | null
  /** Sequence of the last applied frame. */
  headSeq: number
  items: Record<string, TranscriptItem>
  /** Item IDs ordered by (seq, sub). */
  order: string[]
  pending: Record<string, TranscriptPendingInput>
  requests: Record<string, TranscriptRequestState>
  /** request ID + "\u0000" + call ID → item ID */
  calls: Record<string, string>
}

export type ResyncReason = 'epoch' | 'gap' | 'reset'

export interface ApplyResult {
  state: TranscriptState
  /** Set when the client must rebuild from a snapshot. */
  resync?: ResyncReason
}

export function emptyTranscript(): TranscriptState {
  return { epoch: null, headSeq: 0, items: {}, order: [], pending: {}, requests: {}, calls: {} }
}

const callKey = (requestId: string | undefined, callId: string | undefined) =>
  `${requestId ?? ''}\u0000${callId ?? ''}`

function compareItems(a: TranscriptItem, b: TranscriptItem): number {
  return a.seq - b.seq || (a.sub ?? 0) - (b.sub ?? 0)
}

function indexCalls(calls: Record<string, string>, item: TranscriptItem) {
  for (const segment of item.segments) {
    if (segment.type === 'tool_call' && segment.call_id) {
      calls[callKey(item.request_id, segment.call_id)] = item.id
    }
  }
}

export function fromSnapshot(snapshot: TranscriptSnapshot): TranscriptState {
  const state = emptyTranscript()
  state.epoch = snapshot.epoch || null
  state.headSeq = snapshot.seq
  for (const item of snapshot.items ?? []) {
    state.items[item.id] = item
    indexCalls(state.calls, item)
  }
  state.order = [...(snapshot.items ?? [])].sort(compareItems).map((item) => item.id)
  for (const pending of snapshot.pending ?? []) state.pending[pending.request_id] = pending
  for (const request of snapshot.requests ?? []) state.requests[request.request_id] = request
  return state
}

/** Merge an older history page; items already present keep their live copy. */
export function prependOlder(state: TranscriptState, older: TranscriptItem[]): TranscriptState {
  const fresh = older.filter((item) => !state.items[item.id])
  if (fresh.length === 0) return state
  const items = { ...state.items }
  const calls = { ...state.calls }
  for (const item of fresh) {
    items[item.id] = item
    indexCalls(calls, item)
  }
  const order = Object.values(items).sort(compareItems).map((item) => item.id)
  return { ...state, items, calls, order }
}

/** Mutable working copy for one batch; maps are copied on first write. */
class Draft {
  state: TranscriptState
  private copied = new Set<string>()

  constructor(state: TranscriptState) {
    this.state = { ...state }
  }

  private own<K extends 'items' | 'pending' | 'requests' | 'calls' | 'order'>(key: K): TranscriptState[K] {
    if (!this.copied.has(key)) {
      const value = this.state[key]
      this.state[key] = (Array.isArray(value) ? [...value] : { ...value }) as TranscriptState[K]
      this.copied.add(key)
    }
    return this.state[key]
  }

  get items() {
    return this.state.items
  }

  setItem(item: TranscriptItem) {
    const exists = !!this.state.items[item.id]
    this.own('items')[item.id] = item
    if (!exists) this.insertOrder(item)
  }

  removeItem(id: string) {
    if (!this.state.items[id]) return
    delete this.own('items')[id]
    const order = this.own('order')
    const index = order.indexOf(id)
    if (index >= 0) order.splice(index, 1)
  }

  private insertOrder(item: TranscriptItem) {
    const order = this.own('order')
    let lo = 0
    let hi = order.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      const other = this.state.items[order[mid]]
      if (other && compareItems(other, item) <= 0) lo = mid + 1
      else hi = mid
    }
    order.splice(lo, 0, item.id)
  }

  setPending(pending: TranscriptPendingInput) {
    this.own('pending')[pending.request_id] = pending
  }

  deletePending(id: string | undefined) {
    if (id && this.state.pending[id]) delete this.own('pending')[id]
  }

  request(id: string): TranscriptRequestState {
    return this.state.requests[id] ?? { request_id: id }
  }

  setRequest(request: TranscriptRequestState) {
    this.own('requests')[request.request_id] = request
  }

  setCall(requestId: string | undefined, callId: string | undefined, itemId: string) {
    const key = callKey(requestId, callId)
    if (this.state.calls[key] !== itemId) this.own('calls')[key] = itemId
  }

  callItem(frame: TranscriptFrame): TranscriptItem | undefined {
    if (frame.item_id && this.state.items[frame.item_id]) return this.state.items[frame.item_id]
    const id = this.state.calls[callKey(frame.request_id, frame.call_id)]
    return id ? this.state.items[id] : undefined
  }
}

function openItem(draft: Draft, frame: TranscriptFrame, kind: TranscriptItem['kind']): TranscriptItem {
  const existing = draft.items[frame.item_id!]
  if (existing) return existing
  return {
    id: frame.item_id!,
    kind,
    request_id: frame.request_id,
    seq: frame.seq,
    last_seq: frame.seq,
    ts: frame.ts,
    segments: [],
  }
}

function applyFrame(draft: Draft, frame: TranscriptFrame) {
  switch (frame.kind) {
    case 'input_queued': {
      if (!frame.request_id) return
      draft.setPending({
        request_id: frame.request_id,
        text: frame.text ?? '',
        files: frame.files,
        image_count: frame.image_count,
        received_at: frame.received_at,
        origin: frame.origin,
        seq: frame.seq,
      })
      return
    }
    case 'input_dropped': {
      draft.deletePending(frame.request_id)
      for (const id of frame.request_ids ?? []) draft.deletePending(id)
      return
    }
    case 'input_committed': {
      draft.deletePending(frame.request_id)
      for (const id of frame.request_ids ?? []) draft.deletePending(id)
      const parts = frame.parts ?? []
      if (parts.length > 0) {
        parts.forEach((part, index) => {
          draft.deletePending(part.request_id)
          const id = part.item_id || `in:${part.request_id}`
          if (draft.items[id]) return
          const last = index === parts.length - 1
          draft.setItem({
            id,
            kind: 'user',
            request_id: part.request_id,
            seq: frame.seq,
            sub: index,
            last_seq: frame.seq,
            ts: part.ts || frame.received_at || frame.ts,
            done: true,
            segments: [{ type: 'content', text: part.text }],
            files: last ? frame.files : undefined,
            image_count: last ? frame.image_count : undefined,
          })
        })
        return
      }
      const id = frame.item_id || `in:${frame.request_id}`
      if (draft.items[id]) return
      draft.setItem({
        id,
        kind: 'user',
        request_id: frame.request_id,
        seq: frame.seq,
        last_seq: frame.seq,
        ts: frame.received_at || frame.ts,
        done: true,
        segments: [{ type: 'content', text: frame.text ?? '' }],
        files: frame.files,
        image_count: frame.image_count,
      })
      return
    }
    case 'item_delta': {
      if (!frame.item_id || !frame.text) return
      const item = openItem(draft, frame, 'assistant')
      const type = frame.channel === 'reasoning' ? 'thinking' : 'content'
      const segments = [...item.segments]
      const last = segments[segments.length - 1]
      if (last && last.type === type) {
        segments[segments.length - 1] = { ...last, text: (last.text ?? '') + frame.text }
      } else {
        segments.push({ type, text: frame.text })
      }
      draft.setItem({ ...item, segments, last_seq: frame.seq })
      return
    }
    case 'item_snapshot': {
      if (!frame.item_id) return
      const item = openItem(draft, frame, 'assistant')
      const segments = (frame.segments ?? []).map((segment) => ({ ...segment }))
      const next = { ...item, segments, ephemeral: frame.ephemeral || undefined, last_seq: frame.seq }
      draft.setItem(next)
      for (const segment of segments) {
        if (segment.type === 'tool_call' && segment.call_id) draft.setCall(next.request_id, segment.call_id, next.id)
      }
      return
    }
    case 'tool_started': {
      if (!frame.item_id) return
      const item = openItem(draft, frame, 'assistant')
      draft.setCall(frame.request_id, frame.call_id, item.id)
      const index = item.segments.findIndex(
        (segment) => segment.type === 'tool_call' && segment.call_id === frame.call_id
      )
      const segments = [...item.segments]
      if (index >= 0) {
        segments[index] = { ...segments[index], name: frame.name, args: frame.args }
      } else {
        segments.push({
          type: 'tool_call',
          call_id: frame.call_id,
          name: frame.name,
          args: frame.args,
          done: false,
          dispatch_id: frame.dispatch_id || undefined,
          agent_instance_id: frame.target_agent_id || undefined,
        })
      }
      draft.setItem({ ...item, segments, last_seq: frame.seq })
      return
    }
    case 'tool_finished': {
      const item = draft.callItem(frame)
      if (!item) return
      const index = item.segments.findIndex(
        (segment) => segment.type === 'tool_call' && segment.call_id === frame.call_id
      )
      const finished: Partial<TranscriptSegment> = {
        result: frame.result,
        error: frame.error,
        duration_ms: frame.duration_ms,
        done: true,
      }
      const segments = [...item.segments]
      if (index >= 0) {
        segments[index] = { ...segments[index], ...finished }
      } else {
        segments.push({ type: 'tool_call', call_id: frame.call_id, name: frame.name, ...finished })
        draft.setCall(item.request_id, frame.call_id, item.id)
      }
      draft.setItem({ ...item, segments, last_seq: frame.seq })
      return
    }
    case 'dispatch_terminal': {
      const item = draft.callItem(frame)
      if (!item) return
      let changed = false
      const segments = item.segments.map((segment) => {
        if (segment.type !== 'tool_call') return segment
        if (segment.dispatch_id && segment.dispatch_id !== frame.dispatch_id) return segment
        if (!segment.dispatch_id && segment.call_id !== frame.call_id) return segment
        changed = true
        return {
          ...segment,
          dispatch_id: frame.dispatch_id,
          done: true,
          error: frame.error ? frame.error : segment.error,
        }
      })
      if (changed) draft.setItem({ ...item, segments, last_seq: frame.seq })
      return
    }
    case 'item_done': {
      const item = frame.item_id ? draft.items[frame.item_id] : undefined
      if (item && !item.done) draft.setItem({ ...item, done: true, last_seq: frame.seq })
      return
    }
    case 'delegation_started':
    case 'delegation_finished': {
      if (!frame.request_id) return
      const request = draft.request(frame.request_id)
      draft.setRequest({ ...request, delegating: frame.kind === 'delegation_started' })
      return
    }
    case 'request_started': {
      if (!frame.request_id) return
      const request = draft.request(frame.request_id)
      draft.setRequest({ ...request, origin: frame.origin, started_seq: frame.seq })
      return
    }
    case 'request_route': {
      if (!frame.request_id) return
      const request = draft.request(frame.request_id)
      draft.setRequest({
        ...request,
        task_type: frame.task_type,
        model_id: frame.model_id,
        provider_id: frame.provider_id,
        agent_instance_id: frame.agent_instance_id,
      })
      return
    }
    case 'request_end': {
      if (!frame.request_id) return
      const request = draft.request(frame.request_id)
      draft.setRequest({
        ...request,
        ended: true,
        delegating: false,
        terminal_code: frame.terminal_code,
        error: frame.error,
        ended_seq: frame.seq,
        request_ids: frame.request_ids,
      })
      const ended = new Set([frame.request_id, ...(frame.request_ids ?? [])])
      for (const id of frame.request_ids ?? []) draft.deletePending(id)
      for (const item of Object.values(draft.items)) {
        if (item.kind === 'assistant' && item.request_id && ended.has(item.request_id) && !item.done) {
          draft.setItem({ ...item, done: true })
        }
      }
      if (frame.error) {
        const id = `err:${frame.request_id}`
        if (!draft.items[id]) {
          draft.setItem({
            id,
            kind: 'error',
            request_id: frame.request_id,
            seq: frame.seq,
            last_seq: frame.seq,
            ts: frame.ts,
            done: true,
            segments: [{ type: 'error', text: frame.error }],
          })
        }
      }
      return
    }
    default:
      // compaction_status and session_meta carry session state, not items.
      return
  }
}

/**
 * Apply frames delivered for `epoch`. Frames at or below the head are replays
 * and skipped; a missing sequence, a different epoch or a reset stops the
 * batch and asks the caller to rebuild from a snapshot.
 */
export function applyFrames(state: TranscriptState, epoch: string, frames: TranscriptFrame[]): ApplyResult {
  if (state.epoch !== epoch) return { state, resync: 'epoch' }
  const draft = new Draft(state)
  for (const frame of frames) {
    const head = draft.state.headSeq
    if (frame.seq <= head) continue
    if (frame.seq > head + 1) return { state: draft.state, resync: 'gap' }
    draft.state.headSeq = frame.seq
    if (frame.kind === 'transcript_reset') return { state: draft.state, resync: 'reset' }
    applyFrame(draft, frame)
  }
  return { state: draft.state }
}

/** Items in transcript order. */
export function orderedItems(state: TranscriptState): TranscriptItem[] {
  return state.order.map((id) => state.items[id]).filter((item): item is TranscriptItem => !!item)
}

/** Requests that have not ended. */
export function activeRequests(state: TranscriptState): TranscriptRequestState[] {
  return Object.values(state.requests).filter((request) => !request.ended)
}

export function removeItem(state: TranscriptState, id: string): TranscriptState {
  const draft = new Draft(state)
  draft.removeItem(id)
  return draft.state
}
