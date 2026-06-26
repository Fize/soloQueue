import { useCallback, useRef } from 'react'
import { useChatStore } from '@/stores/chatStore'
import { wsManager } from '@/lib/websocket'
import type { ChatHandler } from '@/lib/websocket'

function generateRequestId(): string {
  return `req-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

export function useChatStream() {
  const activeRequestIdRef = useRef<string | null>(null)

  const {
    activeSessionId,
    titleGenerated,
    addMessage,
    appendToLastAssistantContent,
    appendToLastAssistantThinking,
    updateLastAssistantSegment,
    updateToolCallResult,
    setStreaming,
    removeLastEmptyAssistantMessage,
    renameSession,
    markTitleGenerated,
    completeLastDelegation,
    setDelegating,
  } = useChatStore()

  const send = useCallback(
    async (prompt: string, files?: { name: string; path: string }[], sessionIdOverride?: string) => {
      const state = useChatStore.getState()
      const sid = sessionIdOverride || state.activeSessionId
      if (!sid || !prompt.trim()) return

      const requestId = generateRequestId()
      activeRequestIdRef.current = requestId

      const msgId = `msg-${Date.now()}`

      // Add user message.
      addMessage({
        id: msgId,
        role: 'user',
        segments: [{ type: 'content', text: prompt }],
        timestamp: new Date().toISOString(),
        files,
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

      const isL2 = sid.startsWith('l2:')
      const shouldGenTitle = isL2 && !state.titleGenerated[sid]
      let finalContent = ''

      let finished = false
      const finishRequest = () => {
        if (finished) return
        finished = true
        setStreaming(false)
        setDelegating(false)
        activeRequestIdRef.current = null
        wsManager.unregisterChat(requestId)
      }

      const handler: ChatHandler = {
        onChunk: (delta) => {
          appendToLastAssistantContent(delta)
          if (shouldGenTitle) finalContent += delta
        },
        onReasoning: (delta) => {
          appendToLastAssistantThinking(delta)
        },
        onToolStart: (data) => {
          updateLastAssistantSegment({
            type: 'tool_call',
            callId: data.call_id,
            name: data.name,
            args: data.args,
            done: false,
          })
        },
        onToolDone: (data) => {
          updateToolCallResult(
            data.call_id,
            data.result,
            data.error || undefined,
            data.duration_ms || undefined
          )
        },
        onToolConfirm: (data) => {
          updateLastAssistantSegment({
            type: 'tool_confirm',
            callId: data.call_id,
            name: data.name,
            prompt: data.prompt,
            allowInSession: data.allow_in_session ?? false,
            resolved: false,
          })
        },
        onDone: (_data) => {
          if (shouldGenTitle && prompt.trim()) {
            const title = generateTitle(prompt, finalContent)
            if (title) {
              renameSession(sid, title)
            }
            markTitleGenerated(sid)
          }
          finishRequest()
        },
        onError: (error) => {
          updateLastAssistantSegment({ type: 'error', text: error })
          finishRequest()
        },
        onDelegationStart: () => {
          setDelegating(true)
        },
        onDelegationDone: (data) => {
          setDelegating(false)
          completeLastDelegation(data.target_agent_id, data.duration_ms, data.result_content)
        },
        onClose: () => {
          finishRequest()
        },
      }

      wsManager.registerChat(requestId, handler)

      wsManager.send({
        type: 'chat_send',
        request_id: requestId,
        session_id: sid,
        prompt,
        files,
      })
    },
    [
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
      completeLastDelegation,
      setDelegating,
    ]
  )

  const cancel = useCallback(() => {
    const requestId = activeRequestIdRef.current
    if (!requestId) return

    wsManager.send({
      type: 'chat_cancel',
      request_id: requestId,
      session_id: activeSessionId!,
    })

    removeLastEmptyAssistantMessage()
    setStreaming(false)
    setDelegating(false)
    wsManager.unregisterChat(requestId)
    activeRequestIdRef.current = null
  }, [activeSessionId, removeLastEmptyAssistantMessage, setStreaming, setDelegating])

  return { send, cancel }
}

// generateTitle creates a concise title from the first exchange.
function generateTitle(prompt: string, _response: string): string {
  if (!prompt.trim()) return ''
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
