import type { ChatMessage } from '@/types'
import { groupSegments } from './groupSegments'

export function workedStateKey(message: ChatMessage, groupId: string): string {
  return message.workedStateKeys?.[groupId] ?? JSON.stringify([message.id, groupId])
}

function workedGroups(messages: ChatMessage[]) {
  return messages.flatMap((message) => groupSegments(message.segments).flatMap((group) => {
    if (message.role !== 'assistant' || group.type !== 'worked') return []
    const segments = group.segments.map(({ segment }) => segment)
    const callIds = segments.flatMap((segment) => segment.type === 'tool_call' && segment.callId ? [segment.callId] : [])
    const textOnly = segments.every((segment) => (segment.type === 'thinking' || segment.type === 'compact') && segment.text.trim())
    return [{ message, group, callIds, text: textOnly ? JSON.stringify(segments) : undefined }]
  }))
}

/** Keep only presentation state when the server replaces live IDs with history IDs. */
export function preserveWorkedStateKeys(current: ChatMessage[], incoming: ChatMessage[]): ChatMessage[] {
  const previous = workedGroups(current)
  const next = workedGroups(incoming)
  const keys = new Map<ChatMessage, Record<string, string>>()
  for (const item of next) {
    const candidates = previous.filter((candidate) => item.callIds.length
      ? candidate.callIds.some((id) => item.callIds.includes(id))
      : item.text && candidate.text === item.text)
    if (candidates.length !== 1) continue
    // Repeated thinking text has no stable identity; never transfer a preference by guessing.
    if (!item.callIds.length && next.filter((candidate) => candidate.text === item.text).length !== 1) continue
    const previousKey = workedStateKey(candidates[0].message, candidates[0].group.id)
    keys.set(item.message, { ...keys.get(item.message), [item.group.id]: previousKey })
  }
  return incoming.map((message) => keys.has(message) ? { ...message, workedStateKeys: keys.get(message) } : message)
}
