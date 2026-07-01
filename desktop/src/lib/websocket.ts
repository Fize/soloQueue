import type {
  WSMessage,
  RuntimeStatus,
  AgentListResponse,
  AgentStreamState,
  SimulationEvent,
  SimulationProgress,
  ClientMessage,
} from '@/types'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useAgentStore } from '@/stores/agentStore'

type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

export interface ChatHandler {
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
  onDelegationStart?: (data: { num_tasks: number }) => void
  onDelegationDone?: (data: {
    target_agent_id: string
    agent_name?: string
    duration_ms?: number
    result_content?: string
  }) => void
  onClose?: () => void
}

type MessageHandler = {
  runtime: Set<(data: RuntimeStatus) => void>
  agents: Set<(data: AgentListResponse) => void>
  status: Set<(status: ConnectionStatus) => void>
  simulation_event: Set<(data: SimulationEvent) => void>
  simulation_progress: Set<(data: SimulationProgress) => void>
}

function wsBase(): string {
  if (window.location.protocol === 'file:') {
    const port = (window as any).electronAPI?.backendPort || 57647
    return `ws://127.0.0.1:${port}/ws`
  }
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws`
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
  }
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private reconnectDelay = 1000
  private maxReconnectDelay = 30000
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

    let token = ''
    try {
      const res = await fetch('/api/auth/token')
      if (res.ok) {
        const data = await res.json()
        token = data.token
      }
    } catch (err) {
      console.warn('Failed to fetch WS auth token, attempting direct connection:', err)
    }

    let url = wsBase()
    if (token) {
      url += `?token=${encodeURIComponent(token)}`
    }

    this.ws = new WebSocket(url)

    this.ws.onopen = () => {
      this.reconnectDelay = 1000
      this.setStatus('connected')
      this.startPingInterval()
      this.flushPendingMessages()
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: WSMessage = JSON.parse(event.data)
        this.dispatch(msg)
      } catch {
        // Ignore malformed messages
      }
    }

    this.ws.onclose = () => {
      this.stopPingInterval()
      // Notify all active chat handlers of close.
      this.chatHandlers.forEach((h) => h.onClose?.())
      this.chatHandlers.clear()
      if (!this.intentionalClose) {
        this.setStatus('reconnecting')
        this.scheduleReconnect()
      } else {
        this.setStatus('disconnected')
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
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.setStatus('disconnected')
    this.chatHandlers.clear()
    this.pendingMessages = []
  }

  /** Register a chat handler for a specific request_id. */
  registerChat(requestId: string, handler: ChatHandler) {
    this.chatHandlers.set(requestId, handler)
  }

  /** Unregister a chat handler. */
  unregisterChat(requestId: string) {
    this.chatHandlers.delete(requestId)
  }

  /** Send a message to the server. Queues if disconnected. */
  send(msg: ClientMessage) {
    const data = JSON.stringify(msg)
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data)
    } else {
      // Don't queue chat_send to prevent duplicate messages when reconnecting.
      if (msg.type !== 'chat_send') {
        this.pendingMessages.push(data)
      }
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
        // Handled by custom subscription — dispatched below as generic runtime event
        break
      }
    }

    // State / simulation messages.
    if (msg.type === 'state') {
      if (msg.runtime) {
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
