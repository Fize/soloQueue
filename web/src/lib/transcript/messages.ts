// Presentation of a transcript as chat bubbles.
//
// Items are already in production order. Consecutive assistant/error items of
// one request form a single bubble; when a request resumes after another item
// (for example a delegated reply arriving after the next question) its output
// forms a new bubble marked as a continuation, so nothing is ever rendered
// above content that was produced earlier.

import type { ChatMessage, ChatSegment } from '@/types'
import type { TranscriptItem, TranscriptSegment } from '@/types/transcript'
import type { TranscriptState } from './reducer'

export interface LocalInput {
  requestId: string
  sessionId: string
  text: string
  files?: { name: string; path: string }[]
  status: 'sending' | 'accepted' | 'queued' | 'failed'
  error?: string
  createdAt: string
  systemCommand?: boolean
}

export interface CommandResult {
  requestId: string
  sessionId: string
  prompt: string
  text: string
  /** Transcript head when the result arrived; it renders after those items. */
  afterSeq: number
  ts: string
}

export function toChatSegment(segment: TranscriptSegment): ChatSegment {
  switch (segment.type) {
    case 'thinking':
      return { type: 'thinking', text: segment.text ?? '' }
    case 'compact':
      return { type: 'compact', text: segment.text ?? '' }
    case 'error':
      return { type: 'error', text: segment.text ?? '' }
    case 'tool_call':
      return {
        type: 'tool_call',
        callId: segment.call_id ?? '',
        name: segment.name ?? '',
        args: segment.args ?? '',
        result: segment.result || undefined,
        error: segment.error || undefined,
        durationMs: segment.duration_ms || undefined,
        done: segment.done ?? true,
        agentInstanceId: segment.agent_instance_id || undefined,
        dispatchId: segment.dispatch_id || undefined,
      }
    default:
      return { type: 'content', text: segment.text ?? '' }
  }
}

interface CachedMessage {
  items: TranscriptItem[]
  running: boolean
  continuation: boolean
  message: ChatMessage
}

/** Per-session cache so unchanged bubbles keep their object identity. */
export type MessageCache = Map<string, CachedMessage>

function sameItems(a: TranscriptItem[], b: TranscriptItem[]) {
  return a.length === b.length && a.every((item, index) => item === b[index])
}

function userMessage(item: TranscriptItem): ChatMessage {
  return {
    id: item.id,
    requestId: item.request_id,
    role: 'user',
    segments: item.segments.map(toChatSegment),
    timestamp: item.ts,
    files: item.files,
    queuedImageCount: item.image_count || undefined,
  }
}

function assistantMessage(items: TranscriptItem[], running: boolean, continuation: boolean): ChatMessage {
  return {
    id: items[0].id,
    requestId: items[0].request_id,
    role: 'assistant',
    // An empty content delta (for example a compact that now only publishes a
    // segment) must not become an empty bubble.
    segments: items
      .flatMap((item) => item.segments.map(toChatSegment))
      .filter((segment) => segment.type !== 'content' || segment.text.trim().length > 0),
    timestamp: items[0].ts,
    running,
    continuation: continuation || undefined,
  }
}

/**
 * Request ids whose `/compact` event row is already committed to the flow.
 * The compaction status row and its event row share the request id, so the
 * status strip can step aside instead of reporting the same compaction twice.
 */
export function compactionEventRequests(messages: ChatMessage[]): Set<string> {
  const ids = new Set<string>()
  for (const message of messages) {
    if (!message.requestId) continue
    if (message.segments.some((segment) => segment.type === 'compact')) ids.add(message.requestId)
  }
  return ids
}

function cached(
  cache: MessageCache,
  next: MessageCache,
  id: string,
  items: TranscriptItem[],
  running: boolean,
  continuation: boolean,
  build: () => ChatMessage
): ChatMessage {
  const previous = cache.get(id)
  const entry =
    previous &&
    previous.running === running &&
    previous.continuation === continuation &&
    sameItems(previous.items, items)
      ? previous
      : { items, running, continuation, message: build() }
  next.set(id, entry)
  return entry.message
}

/**
 * Build the bubbles of a session: transcript items, then inputs that are not
 * committed yet (server-side queue and this browser's unacknowledged sends).
 */
export function buildMessages(
  state: TranscriptState,
  local: LocalInput[],
  commands: CommandResult[],
  cache: MessageCache
): { messages: ChatMessage[]; cache: MessageCache } {
  const next: MessageCache = new Map()
  const messages: ChatMessage[] = []
  const seenRequests = new Set<string>()
  const pendingCommands = [...commands].sort((a, b) => a.afterSeq - b.afterSeq)

  const flushCommands = (uptoSeq: number) => {
    while (pendingCommands.length > 0 && pendingCommands[0].afterSeq < uptoSeq) {
      const command = pendingCommands.shift()!
      messages.push(...commandMessages(command))
    }
  }

  let group: TranscriptItem[] = []
  const flushGroup = () => {
    if (group.length === 0) return
    const items = group
    group = []
    const requestId = items[0].request_id ?? ''
    const request = requestId ? state.requests[requestId] : undefined
    const running = request ? !request.ended : items.some((item) => !item.done)
    const continuation = !!requestId && seenRequests.has(requestId)
    if (requestId) seenRequests.add(requestId)
    const message = cached(cache, next, items[0].id, items, running, continuation, () =>
      assistantMessage(items, running, continuation)
    )
    // Nothing left to show and nothing still producing: no empty bubble.
    if (message.segments.length === 0 && !message.running) return
    messages.push(message)
  }

  for (const id of state.order) {
    const item = state.items[id]
    if (!item) continue
    flushCommands(item.seq)
    if (item.kind === 'user') {
      flushGroup()
      messages.push(cached(cache, next, item.id, [item], false, false, () => userMessage(item)))
      continue
    }
    if (group.length > 0 && group[0].request_id !== item.request_id) flushGroup()
    group.push(item)
  }
  flushGroup()
  flushCommands(Number.POSITIVE_INFINITY)

  // Inputs the session has accepted but not yet committed to the transcript.
  const committed = (requestId: string) => !!state.items[`in:${requestId}`]
  const pending = Object.values(state.pending).sort((a, b) => a.seq - b.seq)
  const shown = new Set<string>()
  for (const input of pending) {
    if (committed(input.request_id)) continue
    shown.add(input.request_id)
    messages.push({
      id: `pending:${input.request_id}`,
      requestId: input.request_id,
      role: 'user',
      segments: [{ type: 'content', text: input.text }],
      timestamp: input.received_at ?? '',
      files: input.files,
      queued: true,
      queuedImageCount: input.image_count || undefined,
    })
  }
  for (const input of local) {
    if (shown.has(input.requestId) || committed(input.requestId)) continue
    messages.push({
      id: `local:${input.requestId}`,
      requestId: input.requestId,
      role: 'user',
      segments: [{ type: 'content', text: input.text }],
      timestamp: input.createdAt,
      files: input.files,
      queued: input.status === 'queued' || undefined,
      queueError: input.status === 'failed' || undefined,
    })
    if (input.status === 'failed' && input.error) {
      messages.push({
        id: `local-error:${input.requestId}`,
        requestId: input.requestId,
        role: 'assistant',
        segments: [{ type: 'error', text: input.error }],
        timestamp: input.createdAt,
      })
    }
  }
  return { messages, cache: next }
}

function commandMessages(command: CommandResult): ChatMessage[] {
  const messages: ChatMessage[] = [
    {
      id: `command:${command.requestId}`,
      requestId: command.requestId,
      role: 'user',
      segments: [{ type: 'content', text: command.prompt }],
      timestamp: command.ts,
    },
  ]
  // Commands that report through a transcript segment (for example /compact)
  // reply with no text; an empty assistant bubble would read as a blank answer.
  if (!command.text.trim()) return messages
  messages.push({
    id: `command-result:${command.requestId}`,
    requestId: command.requestId,
    role: 'assistant',
    segments: [{ type: 'content', text: command.text }],
    timestamp: command.ts,
  })
  return messages
}
