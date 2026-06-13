import type {
  WSMessage,
  RuntimeStatus,
  AgentListResponse,
  AgentStreamState,
  SimulationEvent,
  SimulationProgress,
} from '@/types'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { useAgentStore } from '@/stores/agentStore'

type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

type MessageHandler = {
  runtime: Set<(data: RuntimeStatus) => void>
  agents: Set<(data: AgentListResponse) => void>
  status: Set<(status: ConnectionStatus) => void>
  simulation_event: Set<(data: SimulationEvent) => void>
  simulation_progress: Set<(data: SimulationProgress) => void>
}

function wsBase(): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}/ws`
}

class WebSocketManager {
  private ws: WebSocket | null = null
  private cachedStreams: Record<string, AgentStreamState> = {}
  private streamTimestamps: Record<string, number> = {}
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

    // Fetch temporary handshake token via standard HTTP request (browser automatically attaches basic auth)
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

// Singleton instance
export const wsManager = new WebSocketManager()
