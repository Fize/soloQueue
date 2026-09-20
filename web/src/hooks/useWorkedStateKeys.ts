import { useLayoutEffect, useMemo } from 'react'
import type { ChatMessage } from '@/types'
import { preserveWorkedStateKeys, workedStateKey } from '@/components/chat/preserveWorkedStateKeys'
import { groupSegments, type GroupedWorked } from '@/components/chat/groupSegments'

// Recovery messages are rendered without entering chatStore. Remember their
// presentation identities too, including across page navigation, until reload.
const identities = new Map<string, Map<string, ChatMessage>>()
const aliases = new Map<string, Map<string, Map<string, string>>>()

function aliasIdentity(message: ChatMessage, group: GroupedWorked): string {
  const segments = group.segments.map(({ segment }) => segment)
  const calls = segments.flatMap((segment) => segment.type === 'tool_call' ? [segment.callId] : [])
  // History row IDs are positional. Timestamp and exact group evidence prevent
  // a different row or group from inheriting an alias after history changes.
  const evidence = calls.length ? calls : segments
  return JSON.stringify([message.id, message.timestamp ?? '', group.id, evidence])
}

export function useWorkedStateKeys(sessionId: string | null, messages: ChatMessage[]): ChatMessage[] {
  const result = useMemo(() => sessionId
    ? preserveWorkedStateKeys([...identities.get(sessionId)?.values() ?? []], messages).map((message, index) =>
      // A new live request already has its own stable ID; repeated reasoning
      // in another request must never inherit that request's manual choice.
      messages[index].id.startsWith('msg-') ? messages[index] : {
        ...message,
        workedStateKeys: {
          ...message.workedStateKeys,
          ...Object.fromEntries(groupSegments(message.segments).flatMap((group) => {
            if (group.type !== 'worked') return []
            const row = JSON.stringify([message.id, group.id])
            const identity = aliasIdentity(message, group)
            const known = aliases.get(sessionId)?.get(row)
            const established = known?.get(identity)
            // A reused positional row is not a new hydration candidate. Give
            // it a distinct default key too, so native history choices cannot leak.
            const key = established ?? (known ? identity : message.workedStateKeys?.[group.id] ?? identity)
            return [[group.id, key]]
          })),
        },
      })
    : messages, [sessionId, messages])
  useLayoutEffect(() => {
    if (!sessionId) return
    const sessionIdentities = identities.get(sessionId) ?? new Map<string, ChatMessage>()
    const sessionAliases = aliases.get(sessionId) ?? new Map<string, Map<string, string>>()
    // Keep identities across the gap between stream completion and async
    // history hydration. Cache no file paths, tool arguments/results or answers.
    for (const message of result) {
      if (message.role !== 'assistant') continue
      for (const group of groupSegments(message.segments)) {
        if (group.type !== 'worked') continue
        const key = workedStateKey(message, group.id)
        const row = JSON.stringify([message.id, group.id])
        const rowAliases = sessionAliases.get(row) ?? new Map<string, string>()
        rowAliases.set(aliasIdentity(message, group), key)
        sessionAliases.set(row, rowAliases)
        sessionIdentities.set(key, {
          id: key,
          role: 'assistant',
          timestamp: '',
          workedStateKeys: { 'worked-0': key },
          segments: group.segments.map(({ segment }) => segment.type === 'tool_call'
            ? { type: 'tool_call', callId: segment.callId, name: segment.name, args: '', done: true }
            : segment),
        })
      }
    }
    identities.set(sessionId, sessionIdentities)
    aliases.set(sessionId, sessionAliases)
  }, [sessionId, result])
  return result
}
