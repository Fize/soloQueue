import { useCallback, useRef } from 'react'
import { useChatStore } from '@/stores/chatStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { wsManager } from '@/lib/websocket'
import type { ChatHandler } from '@/lib/websocket'

function generateRequestId(): string {
  return `req-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

const SYSTEM_SLASH_COMMANDS = new Set([
  '/clear',
  '/compact',
  '/cancel',
  '/help',
  '/version',
  '/init',
])

function isSystemSlashCommand(prompt: string): boolean {
  return SYSTEM_SLASH_COMMANDS.has(prompt.trim().toLowerCase())
}

export function useChatStream() {
  const activeRequestsRef = useRef<Record<string, string>>({})

  const {
    activeSessionId,
    titleGenerated,
    addMessage,
    appendAssistantContent,
    appendAssistantThinking,
    appendAssistantCompact,
    updateAssistantSegment,
    updateToolCallResult,
    setStreaming,
    setSystemCommandRunning,
    renameSession,
    updateSessionPlans,
    markTitleGenerated,
    completeLastDelegation,
    setDelegating,
    setRoute,
    clearRoute,
  } = useChatStore()

  const send = useCallback(
    async (
      prompt: string,
      files?: { name: string; path: string }[],
      sessionIdOverride?: string,
      selectedElement?: any,
      activeDesignFile?: string,
      hasDrawings?: boolean
    ) => {
      const state = useChatStore.getState()
      const sid = sessionIdOverride || state.activeSessionId
      if (!sid || !prompt.trim()) return
      if (state.streamingSessions[sid]) return

      const trimmedPrompt = prompt.trim().toLowerCase()
      const isClear = trimmedPrompt === '/clear'
      const isCompact = trimmedPrompt === '/compact'
      const isSystemCommand = isSystemSlashCommand(prompt)

      if (isClear) {
        useChatStore.setState((prev) => ({
          messages: {
            ...prev.messages,
            [sid]: [],
          },
        }))
      } else {
        // Add user message — always show in the UI.
        const msgId = `msg-${Date.now()}`
        addMessage(sid, {
          id: msgId,
          role: 'user',
          segments: [{ type: 'content', text: prompt }],
          timestamp: new Date().toISOString(),
          files,
        })
      }

      const isDesignMode = useRuntimeStore.getState().isDesignMode

      const requestId = generateRequestId()
      activeRequestsRef.current[sid] = requestId
      setRoute({
        requestId,
        sessionId: sid,
        taskLevel: '',
        modelId: '',
      })

      // Add empty assistant message placeholder.
      const asstId = `msg-${Date.now() + 1}`
      addMessage(sid, {
        id: asstId,
        role: 'assistant',
        segments: [],
        timestamp: new Date().toISOString(),
      })

      setStreaming(true, sid)
      if (isSystemCommand) {
        setSystemCommandRunning(true, sid)
      }

      const isL2 = sid.startsWith('l2:')
      const shouldGenTitle = isL2 && !state.titleGenerated[sid]
      let finalContent = ''

      let finished = false
      const finishRequest = () => {
        if (finished) return
        finished = true
        setStreaming(false, sid)
        setSystemCommandRunning(false, sid)
        setDelegating(false, sid)
        clearRoute(sid, requestId)
        delete activeRequestsRef.current[sid]
        wsManager.unregisterChat(requestId)
      }

      const handler: ChatHandler = {
        onRoute: (data) => {
          setRoute({
            requestId: data.request_id,
            sessionId: data.session_id,
            taskLevel: data.task_level,
            modelId: data.model_id,
            providerId: data.provider_id,
            agentInstanceId: data.agent_instance_id,
          })
        },
        onChunk: (delta) => {
          if (isCompact) {
            appendAssistantCompact(sid, asstId, delta)
          } else {
            appendAssistantContent(sid, asstId, delta)
          }
          if (shouldGenTitle) finalContent += delta
        },
        onReasoning: (delta) => {
          appendAssistantThinking(sid, asstId, delta)
        },
        onToolStart: (data) => {
          updateAssistantSegment(sid, asstId, {
            type: 'tool_call',
            callId: data.call_id,
            name: data.name,
            args: data.args,
            done: false,
            agentInstanceId: data.target_agent_id,
          })
        },
        onToolDone: (data) => {
          updateToolCallResult(
            sid,
            data.call_id,
            data.result,
            data.error || undefined,
            data.duration_ms || undefined
          )
        },
        onToolConfirm: (data) => {
          updateAssistantSegment(sid, asstId, {
            type: 'tool_confirm',
            callId: data.call_id,
            name: data.name,
            prompt: data.prompt,
            allowInSession: data.allow_in_session ?? false,
            resolved: false,
          })
        },
        onSessionName: (name) => {
          renameSession(sid, name)
        },
        onSessionPlans: (plans) => {
          updateSessionPlans(sid, plans)
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
          // Reload history to ensure local timestamps match the server-generated ones
          // This prevents issues where newly created local messages have client-side
          // timestamps that fail to match backend timestamps during operations like deletion.
          useChatStore.getState().loadHistory(sid)
        },
        onError: (error) => {
          updateAssistantSegment(sid, asstId, { type: 'error', text: error })
          finishRequest()
          useChatStore.getState().loadHistory(sid)
        },
        onDelegationStart: () => {
          setDelegating(true, sid)
        },
        onDelegationDone: (data) => {
          setDelegating(false, sid)
          completeLastDelegation(sid, data.target_agent_id, data.duration_ms, data.result_content)
        },
        onClose: () => {
          finishRequest()
          useChatStore.getState().loadHistory(sid)
        },
      }

      wsManager.registerChat(requestId, handler)

      wsManager.send({
        type: 'chat_send',
        request_id: requestId,
        session_id: sid,
        prompt,
        files,
        design_mode: isDesignMode,
        selected_element: selectedElement,
        active_design_file: activeDesignFile,
        has_drawings: hasDrawings,
      })
    },
    [
      activeSessionId,
      titleGenerated,
      addMessage,
      appendAssistantContent,
      appendAssistantThinking,
      appendAssistantCompact,
      updateAssistantSegment,
      updateToolCallResult,
      setStreaming,
      setSystemCommandRunning,
      renameSession,
      updateSessionPlans,
      markTitleGenerated,
      completeLastDelegation,
      setDelegating,
      setRoute,
      clearRoute,
    ]
  )

  const cancel = useCallback(() => {
    const sid = activeSessionId
    if (!sid) return

    const store = useChatStore.getState()
    const requestId = activeRequestsRef.current[sid] || store.routeSessions[sid]?.requestId
    if (!requestId) return

    // Immediately update local state so the stop button reverts to the send
    // button without waiting for the server chat_done round-trip.
    store.setStreaming(false, sid)
    store.setDelegating(false, sid)
    store.setSystemCommandRunning(false, sid)
    store.clearRoute(sid, requestId)
    store.cancelRunningDelegations(sid)
    delete activeRequestsRef.current[sid]
    // Unregister the chat handler to prevent buffered WS chunks from
    // appending to messages after the cancel has been initiated.
    wsManager.unregisterChat(requestId)

    wsManager.send({
      type: 'chat_cancel',
      request_id: requestId,
      session_id: sid,
    })
  }, [activeSessionId])

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
  if (title.length > 30) {
    title = title.slice(0, 27) + '...'
  }
  return title
}
