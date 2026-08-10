import type {
  WSMessage,
  RuntimeStatus,
  AgentListResponse,
  AgentStreamState,
  SimulationEvent,
  SimulationProgress,
  NotificationPayload,
  ClientMessage,
} from '@/types'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useAgentStore } from '@/stores/agentStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useChatStore } from '@/stores/chatStore'
import { request } from '@/lib/api/core'

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
  onToolConfirm?: (data: {
    call_id: string
    name: string
    prompt: string
    allow_in_session: boolean
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
}

type MessageHandler = {
  runtime: Set<(data: RuntimeStatus) => void>
  agents: Set<(data: AgentListResponse) => void>
  status: Set<(status: ConnectionStatus) => void>
  simulation_event: Set<(data: SimulationEvent) => void>
  simulation_progress: Set<(data: SimulationProgress) => void>
  notification: Set<(data: NotificationPayload) => void>
}

class WebSocketManager {
  private ws: WebSocket | null = null
  private cachedStreams: Record<string, AgentStreamState> = {}
  private streamTimestamps: Record<string, number> = {}
  private chatHandlers: Map<string, ChatHandler> = new Map()
  private pendingMessages: string[] = []
  private handlers: MessageHandler = {
    runtime: new Set(),
    agents: new Set(),
    status: new Set(),
    simulation_event: new Set(),
    simulation_progress: new Set(),
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
    let token = ''
    if (connection.mode === 'remote') {
      try {
        const data = await request<{ token: string }>('/auth/token')
        token = data.token
      } catch (err) {
        console.warn('Failed to fetch WS auth token, attempting direct connection:', err)
      }
    }

    let url = connection.getEffectiveWsUrl()
    if (token) {
      url += `?token=${encodeURIComponent(token)}`
    }

    this.ws = new WebSocket(url)

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
        this.setStatus('reconnecting')
        this.scheduleReconnect()
        const handlerCloseTimer = setTimeout(() => {
          this.chatHandlers.forEach((h) => h.onClose?.(closeCode, true))
          this.chatHandlers.clear()
        }, 8000)
        // Store so connect() can clear it on successful reconnect.
        ;(this as any)._handlerCloseTimer = handlerCloseTimer
      } else {
        this.setStatus('disconnected')
        this.chatHandlers.forEach((h) => h.onClose?.(closeCode, true))
        this.chatHandlers.clear()
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
    this.pendingMessages = []
  }

  /** Register a chat handler for a specific request_id. */
  registerChat(requestId: string, handler: ChatHandler) {
    this.chatHandlers.set(requestId, handler)
  }

  /** Whether this renderer owns the live stream handler for a request. */
  hasChatHandler(requestId?: string) {
    return !!requestId && this.chatHandlers.has(requestId)
  }

  /** Unregister a chat handler. */
  unregisterChat(requestId: string) {
    this.chatHandlers.delete(requestId)
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
    // Chat streaming messages — route to request handler.
    switch (msg.type) {
      case 'chat_accepted': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onAccepted?.(msg)
        return
      }
      case 'chat_route': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onRoute?.(msg)
        return
      }
      case 'chat_chunk': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onChunk?.(msg.delta)
        return
      }
      case 'reasoning_chunk': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onReasoning?.(msg.delta)
        return
      }
      case 'tool_start': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onToolStart?.({ call_id: msg.call_id, name: msg.name, args: msg.args, target_agent_id: msg.target_agent_id })
        return
      }
      case 'tool_done': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onToolDone?.({
          call_id: msg.call_id,
          name: msg.name,
          result: msg.result,
          error: msg.error,
          duration_ms: msg.duration_ms,
        })
        return
      }
      case 'tool_confirm': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onToolConfirm?.({
          call_id: msg.call_id,
          name: msg.name,
          prompt: msg.prompt,
          allow_in_session: msg.allow_in_session,
        })
        return
      }
      case 'chat_done': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onDone?.({ content: msg.content, reasoning_content: msg.reasoning_content })
        return
      }
      case 'chat_error': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onError?.(msg.error)
        return
      }
      case 'chat_queued': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onQueued?.({ error: msg.error })
        return
      }
      case 'delegation_start': {
        const h = this.chatHandlers.get(msg.request_id)
        h?.onDelegationStart?.({ num_tasks: msg.num_tasks })
        return
      }
      case 'delegation_done': {
        const h = this.chatHandlers.get(msg.request_id)
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

    // State / simulation messages.
    if (msg.type === 'state') {
      if (msg.runtime) {
        if (msg.runtime.sessions) {
          const currentSessions = useRuntimeStore.getState().status?.sessions || {}
          const accepted: NonNullable<RuntimeStatus['sessions']> = {}
          for (const [sessionId, next] of Object.entries(msg.runtime.sessions)) {
            const previousRevision = this.sessionRevisions[sessionId]
            if (previousRevision === undefined || next.revision > previousRevision) {
              accepted[sessionId] = next
              this.sessionRevisions[sessionId] = next.revision
            } else if (currentSessions[sessionId]) {
              accepted[sessionId] = currentSessions[sessionId]
            }
          }
          msg.runtime.sessions = accepted
          const chat = useChatStore.getState()
          for (const [sessionId, runtime] of Object.entries(accepted)) {
            const active = runtime.state !== 'idle' && runtime.state !== 'error'
            const wasActive = !!chat.streamingSessions[sessionId]
            const hasActiveRequests = Object.values(chat.activeRequests).some(
              (request) => request.sessionId === sessionId
            )
            if (active) {
              chat.setStreaming(true, sessionId)
              chat.setDelegating(runtime.delegating, sessionId)
              if (runtime.request_id) {
                const existing = chat.routeSessions[sessionId]
                const sameRequest = existing?.requestId === runtime.request_id
                const route = {
                  requestId: runtime.request_id,
                  sessionId,
                  taskLevel: sameRequest ? existing?.taskLevel || '' : '',
                  modelId: sameRequest ? existing?.modelId || '' : '',
                  providerId: sameRequest ? existing?.providerId : undefined,
                  agentInstanceId: sameRequest ? existing?.agentInstanceId : undefined,
                }
                chat.updateRequestRoute(runtime.request_id, route)
                chat.setRoute(route)
              }
            } else if (!hasActiveRequests && (wasActive || chat.routeSessions[sessionId])) {
              chat.setStreaming(false, sessionId)
              chat.setDelegating(false, sessionId)
              const requestId = chat.routeSessions[sessionId]?.requestId
              if (requestId) chat.clearRoute(sessionId, requestId)
              void chat.loadHistory(sessionId)
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
    } else if (msg.type === 'simulation_event') {
      this.handlers.simulation_event.forEach((h) => h(msg.event))
    } else if (msg.type === 'simulation_progress' && msg.progress) {
      this.handlers.simulation_progress.forEach((h) => h(msg.progress))
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
}

export const wsManager = new WebSocketManager()
