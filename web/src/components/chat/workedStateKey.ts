import type { ChatMessage } from '@/types'

/** Presentation key of a worked group; message IDs are stable transcript identities. */
export function workedStateKey(message: ChatMessage, groupId: string): string {
  return message.workedStateKeys?.[groupId] ?? JSON.stringify([message.id, groupId])
}
