import { useCallback, useEffect, useMemo } from 'react'
import { useShallow } from 'zustand/react/shallow'
import type { ChatMessage, CompactionStatus } from '@/types'
import { buildMessages, type MessageCache } from '@/lib/transcript/messages'
import { useTranscriptStore, type TranscriptStatus } from '@/stores/transcriptStore'
import { useChatStore } from '@/stores/chatStore'

const EMPTY_COMPACTIONS: CompactionStatus[] = []

// Bubble identities per session. Shared by every view of a session so a
// remounted page keeps the identities its memoized messages already have.
const messageCaches = new Map<string, MessageCache>()

export interface TranscriptView {
  messages: ChatMessage[]
  status: TranscriptStatus
  hasMore: boolean
  loadingOlder: boolean
  loadOlder: () => Promise<void>
  error?: string
}

/**
 * Open a session's live transcript and present it as chat bubbles in
 * production order. The returned bubbles keep their identity while their items
 * are unchanged, so memoized message views re-render only what changed.
 */
export function useTranscript(sessionId: string | null | undefined): TranscriptView {
  const entry = useTranscriptStore((s) => (sessionId ? s.sessions[sessionId] : undefined))
  const local = useTranscriptStore(
    useShallow((s) => Object.values(s.local).filter((input) => input.sessionId === sessionId))
  )
  const commands = useTranscriptStore(
    useShallow((s) => Object.values(s.commands).filter((command) => command.sessionId === sessionId))
  )
  const compactions = useChatStore((s) =>
    sessionId ? s.compactions?.[sessionId] ?? EMPTY_COMPACTIONS : EMPTY_COMPACTIONS
  )

  useEffect(() => {
    if (sessionId) void useTranscriptStore.getState().open(sessionId)
  }, [sessionId])

  const messages = useMemo(() => {
    if (!sessionId || !entry) return []
    const ordered = [...local].sort((a, b) => a.createdAt.localeCompare(b.createdAt))
    const result = buildMessages(entry.state, ordered, commands, messageCaches.get(sessionId) ?? new Map())
    messageCaches.set(sessionId, result.cache)
    // A failed /compact is reported by its compaction notice; do not repeat it
    // as an error bubble.
    const noticed = new Set(
      compactions
        .filter(
          (row) =>
            row.reason === 'user_command' &&
            row.requestId &&
            (row.status === 'failed' || row.status === 'interrupted')
        )
        .map((row) => row.requestId!)
    )
    if (noticed.size === 0) return result.messages
    return result.messages.filter(
      (message) =>
        !(
          message.role === 'assistant' &&
          message.requestId &&
          noticed.has(message.requestId) &&
          message.segments.every((segment) => segment.type === 'error')
        )
    )
  }, [sessionId, entry, local, commands, compactions])

  const loadOlder = useCallback(async () => {
    if (sessionId) await useTranscriptStore.getState().loadOlder(sessionId)
  }, [sessionId])

  return {
    messages,
    status: entry?.status ?? 'idle',
    hasMore: entry?.hasMore ?? false,
    loadingOlder: entry?.loadingOlder ?? false,
    loadOlder,
    error: entry?.error,
  }
}
