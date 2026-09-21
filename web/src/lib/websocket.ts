import type {
  WSMessage,
  RuntimeStatus,
  AgentListResponse,
  AgentStreamState,
  NotificationPayload,
  ClientMessage,
} from '@/types'
import { useRuntimeStore, runtimeSessionId } from '@/stores/runtimeStore'
import { useAgentStore } from '@/stores/agentStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useChatStore } from '@/stores/chatStore'

type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

export interface ChatHandler {
  onAccepted?: (data: { request_id: string; session_id: string }) => void
  onRoute?: (data: {
    request_id: string
    session_id: string
    task_type: string
    model_id: string
    provider_id?: string
    agent_instance_id?: string
  }) => void
  onChunk?: (delta: string) => void
  onReasoning?: (delta: string) => void
  onToolStart?: (data: { call_id: string; name: string; args: string; target_agent_id?: string }) => void
  onToolDone?: (data: {
    call_id: string
    name: string
    result: string
    error: string
    duration_ms: number
  }) => void
  onDone?: (data: { content: string; reasoning_content: string }) => void
  onError?: (error: string) => void
  onQueued?: (data: { error?: string }) => void
  onDelegationStart?: (data: { num_tasks: number }) => void
  onDelegationDone?: (data: {
    target_agent_id: string
    agent_name?: string
    duration_ms?: number
    result_content?: string
  }) => void
  onSessionName?: (name: string) => void
  onSessionPlans?: (plans: string[]) => void
  onClose?: (code?: number, final?: boolean) => void
  onRuntimeTerminal?: (data: { terminal_code?: string; error?: string }) => void
}

type MessageHandler = {
  runtime: Set<(data: RuntimeStatus) => void>
  agents: Set<(data: AgentListResponse) => void>
  status: Set<(status: ConnectionStatus) => void>
  notification: Set<(data: NotificationPayload) => void>
}

class WebSocketManager {
  private ws: WebSocket | null = null
  private cachedStreams: Record<string, AgentStreamState> = {}
  private streamTimestamps: Record<string, number> = {}
  private chatHandlers: Map<string, ChatHandler> = new Map()
  private externalChatOwners = new Map<string, { sessionId: string; origin: 'channel' }>()
  // A transient close can drop chat events while handlers remain registered
  // during the reconnect grace period. Mark those requests so the UI can
  // hydrate from the authoritative runtime snapshot until a fresh event is
  // observed for the request.
  private chatHandlersNeedRecovery = new Set<string>()
  private reconciledTerminals = new Set<string>()
  private pendingMessages: string[] = []
  private handlers: MessageHandler = {
    runtime: new Set(),
    agents: new Set(),
    status: new Set(),
    notification: new Set(),
  }
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000
  private maxReconnectDelay = 30000
  private sessionRevisions: Record<string, number> = {}
  private intentionalClose = false
  private pingTimer: ReturnType<typeof setInterval> | null = null

  async connect() {
    if (
      this.ws &&
      (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)
    ) {
      return
    }

    this.intentionalClose = false

    const connection = useConnectionStore.getState()
    this.ws = new WebSocket(connection.getEffectiveWsUrl())

    this.ws.onopen = () => {
      this.reconnectDelay = 1000
      this.sessionRevisions = {}
      this.setStatus('connected')
      this.startPingInterval()
      this.flushPendingMessages()
      // Cancel any pending handler-close timer from a previous transient close.
      const timer = (this as any)._handlerCloseTimer
      if (timer) {
        clearTimeout(timer)
        ;(this as any)._handlerCloseTimer = null
      }
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        this.dispatch(msg)
      } catch {
        // Ignore malformed messages
      }
    }

    this.ws.onclose = (event) => {
      const closeCode = event?.code
      this.stopPingInterval()
      if (!this.intentionalClose) {
        if (closeCode === 1009) {
          this.chatHandlers.forEach((h) => h.onClose?.(closeCode, false))
        }
        // Transient close (network hiccup): give handlers a grace period to
        // survive a quick reconnect. If the WS doesn't reconnect within 8s,
        // notify handlers of the permanent close.
        this.chatHandlers.forEach((_, requestId) => {
          this.chatHandlersNeedRecovery.add(requestId)
        })
        this.setStatus('reconnecting')
        this.scheduleReconnect()
        const handlerCloseTimer = setTimeout(() => {
          this.chatHandlers.forEach((h) => h.onClose?.(closeCode, true))
          this.chatHandlers.clear()
          this.externalChatOwners.clear()
          this.chatHandlersNeedRecovery.clear()
        }, 8000)
        // Store so connect() can clear it on successful reconnect.
        ;(this as any)._handlerCloseTimer = handlerCloseTimer
      } else {
        this.setStatus('disconnected')
        this.chatHandlers.forEach((h) => h.onClose?.(closeCode, true))
        this.chatHandlers.clear()
        this.externalChatOwners.clear()
        this.chatHandlersNeedRecovery.clear()
      }
    }

    this.ws.onerror = () => {
      // onclose will fire after onerror, handling reconnection there
    }
  }

  disconnect() {
    this.intentionalClose = true
    this.stopPingInterval()
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    const timer = (this as any)._handlerCloseTimer
    if (timer) {
      clearTimeout(timer)
      ;(this as any)._handlerCloseTimer = null
    }
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.setStatus('disconnected')
    this.chatHandlers.forEach((h) => h.onClose?.())
    this.chatHandlers.clear()
    this.externalChatOwners.clear()
    this.chatHandlersNeedRecovery.clear()
    this.reconciledTerminals.clear()
    this.pendingMessages = []
  }

  /** Register a chat handler for a specific request_id. */
  registerChat(requestId: string, handler: ChatHandler) {
    this.chatHandlers.set(requestId, handler)
    this.externalChatOwners.delete(requestId)
    this.chatHandlersNeedRecovery.delete(requestId)
    this.clearReconciledTerminals(requestId)
  }

  private clearReconciledTerminals(requestId: string) {
    this.reconciledTerminals.forEach((key) => {
      if (key.startsWith(`${requestId}:`)) this.reconciledTerminals.delete(key)
    })
  }

  private clearChatRegistration(requestId: string) {
    this.chatHandlers.delete(requestId)
    this.externalChatOwners.delete(requestId)
    this.chatHandlersNeedRecovery.delete(requestId)
  }

  /** Whether this renderer owns the live stream handler for a request. */
  hasChatHandler(requestId?: string) {
    return !!requestId && this.chatHandlers.has(requestId) && !this.chatHandlersNeedRecovery.has(requestId)
  }

  /** Unregister a chat handler. */
  unregisterChat(requestId: string) {
    this.clearChatRegistration(requestId)
  }

  private markChatHandlerProgress(requestId: string) {
    this.chatHandlersNeedRecovery.delete(requestId)
  }

  private reconcileRuntimeTerminal(
    sessionId: string,
    requestId: string,
    runtime: NonNullable<RuntimeStatus['sessions']>[string],
  ) {
    const terminalKey = `${requestId}:${sessionId}:${runtime.terminal_code || runtime.state}`
    if (this.reconciledTerminals.has(terminalKey)) return
    this.reconciledTerminals.add(terminalKey)

    const handler = this.chatHandlers.get(requestId)
    if (handler?.onRuntimeTerminal) {
      handler.onRuntimeTerminal({ terminal_code: runtime.terminal_code, error: runtime.error })
      return
    }

    const chat = useChatStore.getState()
    const externalOwner = this.externalChatOwners.get(requestId)
    chat.reconcileRuntimeTerminal(requestId, sessionId)
    if (externalOwner?.sessionId === sessionId) this.clearChatRegistration(requestId)
    void chat.loadHistory(sessionId)
  }

  /** Send a message to the server and report whether it was delivered or queued. */
  send(msg: ClientMessage): boolean {
    const data = JSON.stringify(msg)
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data)
      return true
    } else {
      // Don't queue chat_send to prevent duplicate messages when reconnecting.
      if (msg.type !== 'chat_send') {
        this.pendingMessages.push(data)
        return true
      }
      return false
    }
  }

  private flushPendingMessages() {
    while (this.pendingMessages.length > 0 && this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(this.pendingMessages.shift()!)
    }
  }

  private startPingInterval() {
    this.stopPingInterval()
    this.pingTimer = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send('ping')
      }
    }, 25000)
  }

  private stopPingInterval() {
    if (this.pingTimer !== null) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
  }

  subscribe<T extends keyof MessageHandler>(
    type: T,
    handler: Parameters<MessageHandler[T]['add']>[0]
  ): () => void {
    this.handlers[type].add(handler as never)
    return () => {
      this.handlers[type].delete(handler as never)
    }
  }

  private dispatch(msg: WSMessage) {
    const frame = msg as WSMessage & { request_id?: string; session_id?: string; origin?: string }
    const externalOwner = frame.request_id
      ? this.externalChatOwners.get(frame.request_id)
      : undefined
    if (externalOwner && (
      frame.origin !== externalOwner.origin
      || frame.session_id !== externalOwner.sessionId
    )) {
      return
    }

    // Chat streaming messages — route to request handler.
    switch (msg.type) {
      case 'chat_accepted': {
        const h = this.chatHandlers.get(msg.request_id)
        if (h) {
          h.onAccepted?.(msg)
        } else if (this.registerExternalChannelChat(msg)) {
          this.chatHandlers.get(msg.request_id)?.onAccepted?.(msg)
        }
        return
      }
      case 'chat_route': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        h?.onRoute?.(msg)
        return
      }
      case 'chat_chunk': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onChunk?.(msg.delta)
        return
      }
      case 'reasoning_chunk': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onReasoning?.(msg.delta)
        return
      }
      case 'tool_start': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onToolStart?.({ call_id: msg.call_id, name: msg.name, args: msg.args, target_agent_id: msg.target_agent_id })
        return
      }
      case 'tool_done': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onToolDone?.({
          call_id: msg.call_id,
          name: msg.name,
          result: msg.result,
          error: msg.error,
          duration_ms: msg.duration_ms,
        })
        return
      }
      case 'chat_done': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onDone?.({ content: msg.content, reasoning_content: msg.reasoning_content })
        return
      }
      case 'chat_error': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onError?.(msg.error)
        return
      }
      case 'chat_queued': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onQueued?.({ error: msg.error })
        return
      }
      case 'delegation_start': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onDelegationStart?.({ num_tasks: msg.num_tasks })
        return
      }
      case 'delegation_done': {
        let h = this.chatHandlers.get(msg.request_id)
        if (!h) {
          this.registerExternalChannelChat(msg)
          h = this.chatHandlers.get(msg.request_id)
        }
        if (h) this.markChatHandlerProgress(msg.request_id)
        h?.onDelegationDone?.({
          target_agent_id: msg.target_agent_id,
          agent_name: msg.agent_name,
          duration_ms: msg.duration_ms,
          result_content: msg.result_content,
        })
        return
      }
      case 'session_name': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onSessionName?.(msg.name)
        return
      }
      case 'session_plans': {
        const h = this.chatHandlers.get(msg.request_id || '')
        h?.onSessionPlans?.(msg.plans)
        return
      }
    }

    // Runtime state messages.
    if (msg.type === 'state') {
      if (msg.runtime) {
        if (msg.runtime.sessions) {
          const currentSessions = useRuntimeStore.getState().status?.sessions || {}
          const accepted: NonNullable<RuntimeStatus['sessions']> = {}
          for (const [sessionId, next] of Object.entries(msg.runtime.sessions)) {
            const previousRevision = this.sessionRevisions[sessionId]
            // Revisions track lifecycle topology. Context usage and watchdog
            // progress are live values and may change without a revision bump.
            // Accept equal revisions so channel requests do not freeze the
            // context ring at their initial snapshot.
            if (previousRevision === undefined || next.revision >= previousRevision) {
              accepted[sessionId] = next
              this.sessionRevisions[sessionId] = next.revision
            } else if (currentSessions[sessionId]) {
              accepted[sessionId] = currentSessions[sessionId]
            }
          }
          msg.runtime.sessions = accepted
          const chat = useChatStore.getState()
          const runtimeSessionIDs = new Map<string, Set<string>>()
          const activeRuntimeSessions = new Set<string>()
          const delegatingRuntimeSessions = new Set<string>()
          for (const [sessionKey, runtime] of Object.entries(accepted)) {
            const sessionId = runtimeSessionId(sessionKey, runtime)
            if (runtime.request_id) {
              const requestIDs = runtimeSessionIDs.get(sessionId) || new Set<string>()
              requestIDs.add(runtime.request_id)
              runtimeSessionIDs.set(sessionId, requestIDs)
            }
            if (runtime.state !== 'idle' && runtime.state !== 'error') {
              activeRuntimeSessions.add(sessionId)
            }
            if (runtime.delegating) {
              delegatingRuntimeSessions.add(sessionId)
            }
          }
          for (const [sessionKey, runtime] of Object.entries(accepted)) {
            // L1 may have several request-keyed entries at once. The map key
            // is an API identity, not the logical chat session ID.
            const sessionId = runtimeSessionId(sessionKey, runtime)
            const active = runtime.state !== 'idle' && runtime.state !== 'error'
            const wasActive = !!chat.streamingSessions[sessionId]
            const hasActiveRequests = Object.values(chat.activeRequests).some(
              (request) => request.sessionId === sessionId
            )
            const hasActiveRuntimeRequest = activeRuntimeSessions.has(sessionId)
            if (runtime.request_id && !active) {
              this.reconcileRuntimeTerminal(sessionId, runtime.request_id, runtime)
            }
            if (active) {
              chat.setStreaming(true, sessionId)
              chat.setDelegating(delegatingRuntimeSessions.has(sessionId), sessionId)
              if (runtime.request_id) {
                const existing = chat.routeSessions[sessionId]
                const runtimeRequestIDs = runtimeSessionIDs.get(sessionId)
                const currentRouteIsTracked = !!existing?.requestId && !!runtimeRequestIDs?.has(existing.requestId)
                const currentRouteIsActiveLocally = !!existing?.requestId && !!chat.activeRequests[existing.requestId]
                // Keep the route already selected by the local sender (or a
                // persisted route still present in runtime). A second
                // concurrent runtime entry must not replace it with an older
                // request merely because Go map iteration order changed.
                const shouldAdoptRoute = !existing ||
                  existing.requestId === runtime.request_id ||
                  (!currentRouteIsTracked && !currentRouteIsActiveLocally)
                if (!shouldAdoptRoute) continue
                const sameRequest = existing?.requestId === runtime.request_id
                const route = {
                  requestId: runtime.request_id,
                  sessionId,
                  taskLevel: runtime.task_type || (sameRequest ? existing?.taskLevel || '' : ''),
                  modelId: runtime.model_id || (sameRequest ? existing?.modelId || '' : ''),
                  providerId: runtime.provider_id || (sameRequest ? existing?.providerId : undefined),
                  agentInstanceId: runtime.agent_instance_id || (sameRequest ? existing?.agentInstanceId : undefined),
                }
                chat.updateRequestRoute(runtime.request_id, route)
                chat.setRoute(route)
              }
            } else if (!hasActiveRequests && !hasActiveRuntimeRequest && (wasActive || chat.routeSessions[sessionId])) {
              chat.setStreaming(false, sessionId)
              chat.setDelegating(false, sessionId)
              const requestId = chat.routeSessions[sessionId]?.requestId
              if (requestId) chat.clearRoute(sessionId, requestId)
              void chat.loadHistory(sessionId)
            }
          }

          // A legacy idle snapshot can omit request_id after a silent stream
          // close. Once no request for that logical session remains active in
          // the authoritative snapshot, clear stale local ownership exactly
          // once instead of leaving handlers and busy indicators stuck.
          for (const [sessionKey, runtime] of Object.entries(accepted)) {
            const sessionId = runtimeSessionId(sessionKey, runtime)
            if (runtime.request_id || runtime.state !== 'idle' || activeRuntimeSessions.has(sessionId)) continue
            for (const request of Object.values(chat.activeRequests)) {
              if (request.sessionId === sessionId) {
                this.reconcileRuntimeTerminal(sessionId, request.requestId, runtime)
              }
            }
          }
        }
        if (msg.runtime.agent_streams) {
          for (const [id, stream] of Object.entries(msg.runtime.agent_streams)) {
            this.cachedStreams[id] = stream
            this.streamTimestamps[id] = Date.now()
          }
          for (const [id, cachedStream] of Object.entries(this.cachedStreams)) {
            if (!msg.runtime.agent_streams[id]) {
              msg.runtime.agent_streams[id] = {
                ...cachedStream,
                processing: false,
              }
            }
          }
          this.pruneCachedStreams()
        }
        useRuntimeStore.getState().setStatus(msg.runtime)
        this.handlers.runtime.forEach((h) => h(msg.runtime))
      }
      if (msg.agents) {
        useAgentStore.getState().setAgents(msg.agents)
        this.handlers.agents.forEach((h) => h(msg.agents))
      }
    } else if (msg.type === 'notification' && msg.notification) {
      this.handlers.notification.forEach((h) => h(msg.notification))
    }
  }

  private setStatus(status: ConnectionStatus) {
    useRuntimeStore.getState().setConnectionStatus(status)
    this.handlers.status.forEach((h) => h(status))
  }

  private scheduleReconnect() {
    if (this.reconnectTimer !== null) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay)
      this.connect()
    }, this.reconnectDelay)
  }

  private pruneCachedStreams() {
    const MAX_CACHED = 200
    const keys = Object.keys(this.streamTimestamps)
    if (keys.length <= MAX_CACHED) return
    keys.sort((a, b) => (this.streamTimestamps[a] ?? 0) - (this.streamTimestamps[b] ?? 0))
    for (let i = 0; i < keys.length - MAX_CACHED; i++) {
      delete this.cachedStreams[keys[i]]
      delete this.streamTimestamps[keys[i]]
    }
  }

  /**
   * Channel requests do not have a browser-owned chat handler. Mirror them
   * into the same store primitives used by useChatStream so QQ/WeChat/Telegram
   * output is visible immediately instead of only after history hydration.
   */
  private registerExternalChannelChat(msg: WSMessage): boolean {
    const raw = msg as WSMessage & { origin?: string; prompt?: string; timestamp?: string }
    const requestId = (raw as { request_id?: string }).request_id
    const sessionId = (raw as { session_id?: string }).session_id
    if (raw.origin !== 'channel' || !requestId || !sessionId) return false
    if (this.chatHandlers.has(requestId)) return true

    const sid = sessionId
    const rawTimestamp = typeof raw.timestamp === 'string' ? raw.timestamp : ''
    const parsedTimestamp = Date.parse(rawTimestamp)
    const hasRequestTimestamp = Number.isFinite(parsedTimestamp)
    const requestStartedAt = hasRequestTimestamp ? parsedTimestamp : Date.now()
    const requestTimestamp = hasRequestTimestamp
      ? rawTimestamp
      : new Date(requestStartedAt).toISOString()
    const assistantId = `msg-${requestId}`
    const userId = `channel-user-${requestId}`
    const acceptedPrompt = msg.type === 'chat_accepted' && typeof raw.prompt === 'string'
      ? raw.prompt
      : ''
    useChatStore.setState((state) => {
      const current = [...(state.messages[sid] || [])]
      let userIndex = current.findIndex((message) => (
        message.role === 'user' && (message.id === userId || message.requestId === requestId)
      ))
      let assistantIndex = current.findIndex((message) => (
        message.role === 'assistant' && (message.id === assistantId || message.requestId === requestId)
      ))

      // A reload may hydrate the active turn with history-only IDs before the
      // first live channel frame arrives. Adopt that turn so every later frame
      // updates the same top-level assistant message.
      if (userIndex >= 0) current[userIndex] = { ...current[userIndex], id: userId }
      if (assistantIndex >= 0) current[assistantIndex] = { ...current[assistantIndex], id: assistantId }

      if (userIndex < 0 && acceptedPrompt.trim().length > 0) {
        const firstLaterUserIndex = current.findIndex((message) => {
          if (message.role !== 'user') return false
          const timestamp = Date.parse(message.timestamp)
          return Number.isFinite(timestamp) && timestamp > requestStartedAt
        })
        userIndex = firstLaterUserIndex < 0 ? current.length : firstLaterUserIndex
        current.splice(userIndex, 0, {
          id: userId,
          requestId,
          role: 'user',
          segments: [{ type: 'content', text: acceptedPrompt }],
          timestamp: requestTimestamp,
        })
        if (assistantIndex >= userIndex) assistantIndex++
      }

      if (assistantIndex < 0) {
        let insertionIndex = userIndex >= 0 ? userIndex + 1 : current.length
        if (userIndex < 0) {
          const firstLaterUserIndex = current.findIndex((message) => {
            if (message.role !== 'user') return false
            const timestamp = Date.parse(message.timestamp)
            return Number.isFinite(timestamp) && timestamp > requestStartedAt
          })
          if (firstLaterUserIndex >= 0) insertionIndex = firstLaterUserIndex
        }
        current.splice(insertionIndex, 0, {
          id: assistantId,
          requestId,
          role: 'assistant',
          segments: [],
          timestamp: requestTimestamp,
        })
      }

      return { messages: { ...state.messages, [sid]: current } }
    })
    const store = useChatStore.getState()
    const existingAssistant = store.messages[sid]?.find((message) => (
      message.id === assistantId && message.role === 'assistant'
    ))
    let streamedContent = existingAssistant?.segments
      .filter((segment) => segment.type === 'content')
      .map((segment) => segment.text)
      .join('') || ''
    const route = { requestId, sessionId: sid, taskLevel: '', modelId: '' }
    if (!store.activeRequests[requestId]) store.registerRequest(requestId, sid, route)
    store.setRoute(route)
    store.setStreaming(true, sid)

    const finish = () => {
      const current = useChatStore.getState()
      current.removeRequest(requestId)
      const remaining = Object.values(useChatStore.getState().activeRequests).some(
        (request) => request.sessionId === sid,
      )
      if (!remaining) {
        current.setStreaming(false, sid)
        current.setDelegating(false, sid)
        current.setSystemCommandRunning(false, sid)
        current.clearRoute(sid, requestId)
      }
      this.unregisterChat(requestId)
    }

    const handler: ChatHandler = {
      onAccepted: () => useChatStore.getState().updateRequestStatus(requestId, 'streaming'),
      onRoute: (data) => {
        const next = {
          requestId: data.request_id,
          sessionId: data.session_id,
          taskLevel: data.task_type,
          modelId: data.model_id,
          providerId: data.provider_id,
          agentInstanceId: data.agent_instance_id,
        }
        const current = useChatStore.getState()
        current.updateRequestRoute(requestId, next)
        current.setRoute(next)
      },
      onChunk: (delta) => {
        streamedContent += delta
        useChatStore.getState().appendAssistantContent(sid, assistantId, delta)
      },
      onReasoning: (delta) => useChatStore.getState().appendAssistantThinking(sid, assistantId, delta),
      onToolStart: (data) => useChatStore.getState().updateAssistantSegment(sid, assistantId, {
        type: 'tool_call',
        callId: data.call_id,
        name: data.name,
        args: data.args,
        done: false,
        agentInstanceId: data.target_agent_id,
      }),
      onToolDone: (data) => useChatStore.getState().updateToolCallResult(
        sid,
        data.call_id,
        data.result,
        data.error || undefined,
        data.duration_ms || undefined,
      ),
      onDone: ({ content }) => {
        // A browser can connect after the first deltas were emitted. Use the
        // terminal content as a gap filler without duplicating already mirrored
        // text when the complete delta stream was received.
        if (content && content !== streamedContent) {
          const suffix = content.startsWith(streamedContent)
            ? content.slice(streamedContent.length)
            : streamedContent
              ? ''
              : content
          if (suffix) useChatStore.getState().appendAssistantContent(sid, assistantId, suffix)
        }
        finish()
      },
      onError: (error) => {
        useChatStore.getState().failAssistantMessage(sid, assistantId, error)
        finish()
      },
      onDelegationStart: () => {
        useChatStore.getState().setDelegating(true, sid)
        useChatStore.getState().updateRequestStatus(requestId, 'streaming')
      },
      onDelegationDone: (data) => {
        const current = useChatStore.getState()
        current.setDelegating(false, sid)
        current.completeLastDelegation(sid, data.target_agent_id, data.duration_ms, data.result_content)
      },
      onClose: () => finish(),
    }
    this.clearReconciledTerminals(requestId)
    this.chatHandlers.set(requestId, handler)
    this.externalChatOwners.set(requestId, { sessionId: sid, origin: 'channel' })
    return true
  }
}

export const wsManager = new WebSocketManager()
