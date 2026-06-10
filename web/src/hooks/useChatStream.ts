import { useCallback, useRef } from 'react'
import { useChatStore } from '@/stores/chatStore'
import { streamAsk } from '@/lib/sse'

export function useChatStream() {
  const abortRef = useRef<AbortController | null>(null)
  const {
    activeSessionId,
    titleGenerated,
    addMessage,
    appendToLastAssistantContent,
    appendToLastAssistantThinking,
    updateLastAssistantSegment,
    updateToolCallResult,
    setStreaming,
    renameSession,
    markTitleGenerated,
  } = useChatStore()

  const send = useCallback(async (prompt: string) => {
    const sid = activeSessionId
    if (!sid || !prompt.trim()) return

    const msgId = `msg-${Date.now()}`

    // Add user message.
    addMessage({
      id: msgId,
      role: 'user',
      segments: [{ type: 'content', text: prompt }],
      timestamp: new Date().toISOString(),
    })

    // Add empty assistant message placeholder.
    const asstId = `msg-${Date.now() + 1}`
    addMessage({
      id: asstId,
      role: 'assistant',
      segments: [],
      timestamp: new Date().toISOString(),
    })

    setStreaming(true)

    const abort = new AbortController()
    abortRef.current = abort

    // Track content for title generation (L2 only, first exchange).
    const isL2 = sid.startsWith('l2:')
    const shouldGenTitle = isL2 && !titleGenerated[sid]
    let finalContent = ''

    try {
      const gen = await streamAsk(prompt, sid, abort.signal)
      for await (const ev of gen) {
        switch (ev.type) {
          case 'content_delta':
            appendToLastAssistantContent(ev.delta)
            if (shouldGenTitle) finalContent += ev.delta
            break
          case 'session_name':
            if (ev.name) {
              renameSession(sid, ev.name)
            }
            break
          case 'reasoning_delta':
            appendToLastAssistantThinking(ev.delta)
            break
          case 'tool_start':
            updateLastAssistantSegment({
              type: 'tool_call',
              callId: ev.call_id,
              name: ev.name,
              args: ev.args,
              done: false,
            })
            break
          case 'tool_done':
            updateToolCallResult(ev.call_id, ev.result, ev.error || undefined, ev.duration_ms || undefined)
            break
          case 'error':
            updateLastAssistantSegment({ type: 'error', text: ev.error })
            break
          case 'done':
            // Auto-generate title for L2 sessions on first exchange.
            if (shouldGenTitle && prompt.trim()) {
              const title = generateTitle(prompt, finalContent || ev.content)
              if (title) {
                renameSession(sid, title)
              }
              markTitleGenerated(sid)
            }
            break
          case 'tool_confirm':
            // Auto-approved on server side, ignore.
            break
          case 'delegation_start':
          case 'delegation_done':
            // Could show delegation status in future.
            break
        }
      }
    } catch (err: unknown) {
      if (err instanceof Error && err.name === 'AbortError') return
      updateLastAssistantSegment({
        type: 'error',
        text: err instanceof Error ? err.message : 'Stream failed',
      })
    } finally {
      setStreaming(false)
      abortRef.current = null
    }
  }, [activeSessionId, titleGenerated, addMessage, appendToLastAssistantContent, appendToLastAssistantThinking, updateLastAssistantSegment, updateToolCallResult, setStreaming, renameSession, markTitleGenerated])

  const cancel = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  return { send, cancel }
}

// generateTitle creates a concise title from the first exchange.
function generateTitle(prompt: string, _response: string): string {
  if (!prompt.trim()) return ''
  // Use the first line or first 60 chars of the prompt as title.
  let title: string
  const newlineIdx = prompt.indexOf('\n')
  if (newlineIdx >= 0) {
    title = prompt.slice(0, newlineIdx).trim()
  } else {
    title = prompt.trim()
  }
  if (title.length > 60) {
    title = title.slice(0, 57) + '...'
  }
  return title
}
