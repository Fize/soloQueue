import { useCallback } from 'react'
import { toast } from 'sonner'
import { useChatStore } from '@/stores/chatStore'
import { useTranscriptStore } from '@/stores/transcriptStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { wsManager } from '@/lib/websocket'
import { cancelSession } from '@/lib/api'

function generateRequestId(): string {
  return `req-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

const SYSTEM_SLASH_COMMANDS = new Set(['/clear', '/compact', '/cancel', '/help', '/version', '/init'])

const MAX_CHAT_PROMPT_BYTES = 4 << 20
const MAX_CHAT_ENVELOPE_BYTES = 8 << 20
const ACK_TIMEOUT_MS = 10_000

export function isSystemSlashCommand(prompt: string): boolean {
  return SYSTEM_SLASH_COMMANDS.has(prompt.trim().toLowerCase())
}

/**
 * Sending only records the input locally until the server acknowledges it.
 * Everything the conversation shows afterwards — the committed input, queue
 * state, output, errors — arrives as ordered transcript frames.
 */
export function useChatStream() {
  const send = useCallback(
    async (
      prompt: string,
      files?: { name: string; path: string }[],
      sessionIdOverride?: string,
      selectedElement?: any,
      activeDesignFile?: string,
      hasDrawings?: boolean
    ): Promise<boolean> => {
      const chat = useChatStore.getState()
      const sid = sessionIdOverride || chat.activeSessionId
      if (!sid || !prompt.trim()) return false

      const transcripts = useTranscriptStore.getState()
      void transcripts.open(sid)
      const requestId = generateRequestId()
      const createdAt = new Date().toISOString()
      const systemCommand = isSystemSlashCommand(prompt)
      const local = { requestId, sessionId: sid, text: prompt, files, createdAt, systemCommand }

      if (new TextEncoder().encode(prompt).byteLength > MAX_CHAT_PROMPT_BYTES) {
        transcripts.addLocalInput({
          ...local,
          status: 'failed',
          error: 'Message is too large. Maximum prompt size is 4 MiB.',
        })
        return false
      }

      const chatMessage = {
        type: 'chat_send' as const,
        request_id: requestId,
        session_id: sid,
        prompt,
        files,
        design_mode: useRuntimeStore.getState().isDesignMode,
        selected_element: selectedElement,
        active_design_file: activeDesignFile,
        has_drawings: hasDrawings,
      }
      if (new TextEncoder().encode(JSON.stringify(chatMessage)).byteLength > MAX_CHAT_ENVELOPE_BYTES) {
        transcripts.addLocalInput({
          ...local,
          status: 'failed',
          error: 'Message is too large. Maximum WebSocket message size is 8 MiB.',
        })
        return false
      }

      if (prompt.trim().toLowerCase() === '/clear') chat.clearCompactions(sid)
      transcripts.addLocalInput({ ...local, status: 'sending' })
      if (!wsManager.send(chatMessage)) {
        transcripts.failLocalInput(requestId, 'Message was not sent because the connection is unavailable.')
        return false
      }
      setTimeout(() => {
        const input = useTranscriptStore.getState().local[requestId]
        if (input?.status === 'sending') {
          useTranscriptStore.getState().failLocalInput(requestId, 'Message was not acknowledged by the server.')
        }
      }, ACK_TIMEOUT_MS)
      return true
    },
    []
  )

  const cancel = useCallback(async () => {
    const sid = useChatStore.getState().activeSessionId
    if (!sid) return
    // Page Stop controls the whole session, including channel-owned requests;
    // their ends arrive as transcript frames.
    try {
      await cancelSession(sid)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to stop tasks')
    }
  }, [])

  return { send, cancel }
}
