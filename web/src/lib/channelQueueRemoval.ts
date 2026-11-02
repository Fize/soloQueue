import type { ChatMessage } from '@/types'

const cancelled = new Map<string, Set<string>>()
const removed = new Set<string>()
const key = (sessionId: string, requestId: string) => `${sessionId}:${requestId}`

export function rememberCancelledInput(sessionId: string, requestId: string) {
  const ids = cancelled.get(sessionId) || new Set<string>()
  ids.add(requestId)
  while (ids.size > 1000) ids.delete(ids.values().next().value!)
  cancelled.set(sessionId, ids)
  while (cancelled.size > 1000) cancelled.delete(cancelled.keys().next().value!)
}

export function removeCancelledInputs(sessionId: string, messages: ChatMessage[]) {
  for (const message of messages) {
    if ((message.queueCancelled || message.queued) && message.requestId) removed.add(key(sessionId, message.requestId))
  }
  while (removed.size > 1000) removed.delete(removed.values().next().value!)
}

export function removeAllCancelledInputs(sessionId: string) {
  for (const id of cancelled.get(sessionId) || []) removed.add(key(sessionId, id))
  cancelled.delete(sessionId)
  while (removed.size > 1000) removed.delete(removed.values().next().value!)
}

export function isRemovedCancelledInput(sessionId: string, requestId: string) {
  return removed.has(key(sessionId, requestId))
}
