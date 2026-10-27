import { useCallback, useRef } from 'react'
import { useChatStore } from '@/stores/chatStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { runtimeSessionId } from '@/stores/runtimeStore'
import { wsManager } from '@/lib/websocket'
import { cancelSession } from '@/lib/api'
import { toast } from 'sonner'
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

const MAX_CHAT_PROMPT_BYTES = 4 << 20
const MAX_CHAT_ENVELOPE_BYTES = 8 << 20

function isSystemSlashCommand(prompt: string): boolean {
  return SYSTEM_SLASH_COMMANDS.has(prompt.trim().toLowerCase())
}

export function useChatStream() {
  const cancelRequested = useRef(new Set<string>())
  const {
    activeSessionId,
    addMessage,
    insertMessageAfterRequest,
    registerRequest,
    updateRequestStatus,
    updateRequestCancelId,
    updateRequestRoute,
    removeRequest,
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
    failAssistantMessage,
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
    ): Promise<boolean> => {
      const state = useChatStore.getState()
      const sid = sessionIdOverride || state.activeSessionId
      if (!sid || !prompt.trim()) return false

      if (new TextEncoder().encode(prompt).byteLength > MAX_CHAT_PROMPT_BYTES) {
        const now = new Date().toISOString()
        addMessage(sid, {
          id: `msg-${Date.now()}`,
          role: 'user',
          segments: [{ type: 'content', text: prompt }],
          timestamp: now,
          files,
        })
        addMessage(sid, {
          id: `msg-${Date.now() + 1}`,
          role: 'assistant',
          segments: [
            { type: 'error', text: 'Message is too large. Maximum prompt size is 4 MiB.' },
          ],
          timestamp: now,
        })
        return false
      }

      const requestId = generateRequestId()

      const activeForSession = Object.values(state.activeRequests).filter(
        (request) => request.sessionId === sid
      )
      // The backend permits L1 parallel requests but keeps L2/L3 single-flight.
      // A legacy streaming flag is retained as a compatibility fallback for
      // requests created before the request registry was introduced.
      const isBusy = sid !== 'l1' && (activeForSession.length > 0 || !!state.streamingSessions[sid])
      const ownsSessionState = activeForSession.length === 0 && !state.streamingSessions[sid]
      // Older restored sessions may only have the legacy streaming flag and
      // no request entry yet. Keep that state alive when this newer request
      // is acknowledged as queued.
      const inheritedSessionState = activeForSession.length === 0 && !!state.streamingSessions[sid]

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
        useChatStore.getState().clearCompactions(sid)
      } else {
        // Add user message — always show in the UI.
        const msgId = `msg-${Date.now()}`
        addMessage(sid, {
          id: msgId,
          requestId,
          role: 'user',
          segments: [{ type: 'content', text: prompt }],
          timestamp: new Date().toISOString(),
          files,
        })
      }

      const isDesignMode = useRuntimeStore.getState().isDesignMode

      const chatMessage = {
        type: 'chat_send' as const,
        request_id: requestId,
        session_id: sid,
        prompt,
        files,
        design_mode: isDesignMode,
        selected_element: selectedElement,
        active_design_file: activeDesignFile,
        has_drawings: hasDrawings,
      }
      if (
        new TextEncoder().encode(JSON.stringify(chatMessage)).byteLength > MAX_CHAT_ENVELOPE_BYTES
      ) {
        addMessage(sid, {
          id: `msg-${Date.now() + 1}`,
          role: 'assistant',
          segments: [
            {
              type: 'error',
              text: 'Message is too large. Maximum WebSocket message size is 8 MiB.',
            },
          ],
          timestamp: new Date().toISOString(),
        })
        return false
      }

      const initialRoute = {
        requestId,
        sessionId: sid,
        taskLevel: '',
        modelId: '',
      }
      const ownsRoute = !isBusy || sid === 'l1'
      registerRequest(requestId, sid, ownsRoute ? initialRoute : undefined)
      if (ownsRoute) {
        setRoute(initialRoute)
      }

      // Create the assistant message only after the session produces output.
      // A queued prompt has no assistant response yet, matching Bot behavior.
      let asstId: string | undefined
      let anchorAssistantToRequest = false
      const ensureAssistantMessage = () => {
        if (!asstId) {
          asstId = `msg-${requestId}`
          const assistantMessage = {
            id: asstId,
            requestId,
            role: 'assistant' as const,
            segments: [],
            timestamp: new Date().toISOString(),
          }
          if (anchorAssistantToRequest) {
            insertMessageAfterRequest(sid, requestId, assistantMessage)
          } else {
            addMessage(sid, assistantMessage)
          }
        }
        return asstId
      }

      if (ownsSessionState) {
        setStreaming(true, sid)
        if (isSystemCommand) {
          setSystemCommandRunning(true, sid)
        }
      }

      const isL2 = sid.startsWith('l2:')
      const shouldGenTitle = !isBusy && isL2 && !state.titleGenerated[sid]
      let finalContent = ''

      let finished = false
      let accepted = false
      let queueStarted = false
      let ackTimer: ReturnType<typeof setTimeout> | null = null
      const clearAckTimer = () => {
        if (ackTimer !== null) {
          clearTimeout(ackTimer)
          ackTimer = null
        }
      }
      const finishRequest = () => {
        if (finished) return
        finished = true
        cancelRequested.current.delete(requestId)
        clearAckTimer()
        removeRequest(requestId)
        const remaining = Object.values(useChatStore.getState().activeRequests).filter(
          (request) => request.sessionId === sid
        )
        if (remaining.length === 0 && !inheritedSessionState) {
          setStreaming(false, sid)
          setSystemCommandRunning(false, sid)
          setDelegating(false, sid)
          clearRoute(sid, requestId)
        } else if (useChatStore.getState().routeSessions[sid]?.requestId === requestId) {
          const fallbackRoute = [...remaining].reverse().find((request) => request.route)?.route
          if (fallbackRoute) setRoute(fallbackRoute)
          else if (!inheritedSessionState) clearRoute(sid, requestId)
        }
        wsManager.unregisterChat(requestId)
      }

      const handler: ChatHandler = {
        onAccepted: () => {
          accepted = true
          clearAckTimer()
          updateRequestStatus(requestId, 'streaming')
        },
        onRoute: (data) => {
          accepted = true
          clearAckTimer()
          updateRequestStatus(requestId, 'streaming')
          const route = {
            requestId: data.request_id,
            sessionId: data.session_id,
            taskLevel: data.task_type,
            modelId: data.model_id,
            providerId: data.provider_id,
            agentInstanceId: data.agent_instance_id,
          }
          updateRequestRoute(requestId, route)
          setRoute(route)
        },
        onChunk: (delta) => {
          if (cancelRequested.current.has(requestId)) return
          const assistantId = ensureAssistantMessage()
          if (isCompact) {
            appendAssistantCompact(sid, assistantId, delta)
          } else {
            appendAssistantContent(sid, assistantId, delta)
          }
          if (shouldGenTitle) finalContent += delta
        },
        onReasoning: (delta) => {
          if (cancelRequested.current.has(requestId)) return
          appendAssistantThinking(sid, ensureAssistantMessage(), delta)
        },
        onToolStart: (data) => {
          if (cancelRequested.current.has(requestId)) return
          updateAssistantSegment(sid, ensureAssistantMessage(), {
            type: 'tool_call',
            callId: data.call_id,
            name: data.name,
            args: data.args,
            done: false,
            agentInstanceId: data.target_agent_id,
            dispatchId: data.dispatch_id,
          })
        },
        onToolDone: (data) => {
          if (cancelRequested.current.has(requestId)) return
          updateToolCallResult(
            sid,
            data.call_id,
            data.result,
            data.error || undefined,
            data.duration_ms || undefined
          )
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
        onQueued: () => {
          // The session is serial: the server accepted the message into its
          // pending queue and will inject it before the agent's next LLM
          // call. Keep only the user message until output is produced.
          accepted = true
          clearAckTimer()
          if (!queueStarted) {
            updateRequestStatus(requestId, 'queued')
          }
        },
        onQueueStarted: ({ owns_stream, cancel_request_id }) => {
          if (!owns_stream) {
            finishRequest()
            return
          }
          queueStarted = true
          updateRequestStatus(requestId, 'streaming')
          updateRequestCancelId(requestId, cancel_request_id)
          anchorAssistantToRequest = true
          ensureAssistantMessage()
        },
        onError: (error) => {
          if (cancelRequested.current.has(requestId)) {
            finishRequest()
            void useChatStore.getState().loadHistory(sid)
            return
          }
          failAssistantMessage(sid, ensureAssistantMessage(), error)
          finishRequest()
        },
        onRuntimeTerminal: ({ terminal_code, error }) => {
          if (error && !cancelRequested.current.has(requestId)) {
            failAssistantMessage(sid, ensureAssistantMessage(), error)
          } else if (!error && !asstId && !cancelRequested.current.has(requestId)) {
            const terminalText =
              terminal_code === 'completed'
                ? 'Response completed; loading history…'
                : `Request ${terminal_code || 'ended'}.`
            appendAssistantContent(sid, ensureAssistantMessage(), terminalText)
          }
          finishRequest()
          void useChatStore.getState().loadHistory(sid)
        },
        onDelegationStart: () => {
          updateRequestStatus(requestId, 'streaming')
          setDelegating(true, sid)
        },
        onDelegationDone: (data) => {
          setDelegating(false, sid)
          completeLastDelegation(sid, data.target_agent_id, data.duration_ms, data.result_content)
        },
        onClose: (code, final) => {
          if (code === 1009 && accepted && !final) return
          finishRequest()
          useChatStore.getState().loadHistory(sid)
        },
      }

      wsManager.registerChat(requestId, handler)

      const sent = wsManager.send(chatMessage)
      if (!sent) {
        handler.onError?.('Message was not sent because the connection is unavailable.')
        return false
      }
      ackTimer = setTimeout(() => {
        if (!accepted) {
          handler.onError?.('Message was not acknowledged by the server.')
        }
      }, 10_000)
      return true
    },
    [
      addMessage,
      insertMessageAfterRequest,
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
      failAssistantMessage,
      setDelegating,
      setRoute,
      clearRoute,
      registerRequest,
      updateRequestStatus,
      updateRequestCancelId,
      updateRequestRoute,
      removeRequest,
    ]
  )

  const cancel = useCallback(async () => {
    const sid = activeSessionId
    if (!sid) return

    const store = useChatStore.getState()
    const requests = Object.values(store.activeRequests).filter((request) => request.sessionId === sid)
    const requestIds = new Set(requests.map((request) => request.requestId))
    const routeId = store.routeSessions[sid]?.requestId
    if (routeId) requestIds.add(routeId)
    for (const [key, runtime] of Object.entries(useRuntimeStore.getState().status?.sessions ?? {})) {
      if (runtimeSessionId(key, runtime) === sid && runtime.request_id) requestIds.add(runtime.request_id)
    }
    // Page Stop controls the whole session, including channel-owned roots.
    // The request-owned WebSocket cancel path deliberately cannot do this.
    try {
      await cancelSession(sid)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Failed to stop tasks')
      return
    }

    store.cancelRunningDelegations(sid)
    for (const requestId of requestIds) {
      store.removeRequest(requestId)
      wsManager.unregisterChat(requestId)
      cancelRequested.current.delete(requestId)
    }
    const remaining = Object.values(useChatStore.getState().activeRequests).filter(
      (request) => request.sessionId === sid
    )
    if (remaining.length === 0) {
      store.setStreaming(false, sid)
      store.setDelegating(false, sid)
      store.setSystemCommandRunning(false, sid)
      if (routeId) store.clearRoute(sid, routeId)
    } else if (routeId && requestIds.has(useChatStore.getState().routeSessions[sid]?.requestId ?? '')) {
      const fallbackRoute = [...remaining].reverse().find((request) => request.route)?.route
      if (fallbackRoute) store.setRoute(fallbackRoute)
      else store.clearRoute(sid, routeId)
    }
    await useChatStore.getState().loadHistory(sid)
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
