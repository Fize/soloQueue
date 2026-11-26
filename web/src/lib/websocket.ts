import type {
  RuntimeStatus,
  AgentListResponse,
  AgentStreamState,
  NotificationPayload,
  ClientMessage,
  TranscriptFrame,
  WSMessage,
  WSStateMessage,
} from '@/types'
import { useRuntimeStore, runtimeSessionId } from '@/stores/runtimeStore'
import { useAgentStore } from '@/stores/agentStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { useChatStore } from '@/stores/chatStore'
import {
  isTranscriptLive,
  setTranscriptTransport,
  transcriptSubscriptions,
  useTranscriptStore,
} from '@/stores/transcriptStore'

type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

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
  private pendingFrames: { sessionId: string; epoch: string; frames: TranscriptFrame[] }[] = []
  private frameFlush: number | null = null

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
      // Resume every open transcript after the last frame it applied. The
      // server replays what was missed or asks for a resync.
      for (const { sessionId, epoch, afterSeq } of transcriptSubscriptions()) {
        this.subscribeTranscript(sessionId, epoch, afterSeq)
      }
    }

    this.ws.onmessage = (event) => {
      if (typeof event.data !== 'string') return
      let msg: WSMessage
      try {
        msg = JSON.parse(event.data)
      } catch {
        return
      }
      this.dispatch(msg)
    }

    this.ws.onclose = () => {
      this.stopPingInterval()
      if (this.intentionalClose) {
        this.setStatus('disconnected')
        return
      }
      this.setStatus('reconnecting')
      this.scheduleReconnect()
    }

    this.ws.onerror = () => {
      // onclose follows onerror and handles reconnection.
    }
  }

  disconnect() {
    this.intentionalClose = true
    this.stopPingInterval()
    // Frames still queued are re-read from the cursor after reconnecting.
    this.dropFrames()
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ws) {
      this.ws.close()
      this.ws = null
    }
    this.setStatus('disconnected')
    this.pendingMessages = []
  }

  /** Send a message; chat_send is never queued so a reconnect cannot duplicate it. */
  send(msg: ClientMessage): boolean {
    const data = JSON.stringify(msg)
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(data)
      return true
    }
    if (msg.type === 'chat_send') return false
    // Subscriptions are re-sent from the transcript store on open.
    if (msg.type !== 'subscribe' && msg.type !== 'unsubscribe') this.pendingMessages.push(data)
    return true
  }

  subscribeTranscript(sessionId: string, epoch: string, afterSeq: number) {
    // The subscription replays everything after this cursor; queued frames are
    // older than it.
    this.dropFrames(sessionId)
    this.send({ type: 'subscribe', session_id: sessionId, epoch, after_seq: afterSeq })
  }

  unsubscribeTranscript(sessionId: string) {
    this.dropFrames(sessionId)
    this.send({ type: 'unsubscribe', session_id: sessionId })
  }

  /**
   * Coalesce transcript frames until the next animation frame.
   *
   * A streamed token arrives as its own message; applying each one as it lands
   * renders the answer as often as the model tokenizes. Merging per displayed
   * frame keeps the render rate tied to the screen instead of the transport.
   */
  private queueFrames(sessionId: string, epoch: string, frames: TranscriptFrame[]) {
    if (frames.length === 0) return
    const last = this.pendingFrames.at(-1)
    if (last && last.sessionId === sessionId && last.epoch === epoch) {
      last.frames.push(...frames)
    } else {
      this.pendingFrames.push({ sessionId, epoch, frames: [...frames] })
    }
    this.scheduleFrameFlush()
  }

  private scheduleFrameFlush() {
    if (this.frameFlush !== null) return
    // A hidden tab never runs animation frames; apply immediately so a queued
    // backlog cannot grow while nothing is painted.
    if (typeof requestAnimationFrame !== 'function' || document.hidden) {
      this.flushTranscriptFrames()
      return
    }
    this.frameFlush = requestAnimationFrame(() => this.flushTranscriptFrames())
  }

  /** Apply every queued transcript frame now, in arrival order. */
  flushTranscriptFrames() {
    if (this.frameFlush !== null) {
      cancelAnimationFrame(this.frameFlush)
      this.frameFlush = null
    }
    if (this.pendingFrames.length === 0) return
    const pending = this.pendingFrames
    this.pendingFrames = []
    const transcripts = useTranscriptStore.getState()
    for (const batch of pending) transcripts.receiveFrames(batch.sessionId, batch.epoch, batch.frames)
  }

  private dropFrames(sessionId?: string) {
    if (this.frameFlush !== null) {
      cancelAnimationFrame(this.frameFlush)
      this.frameFlush = null
    }
    this.pendingFrames = sessionId
      ? this.pendingFrames.filter((batch) => batch.sessionId !== sessionId)
      : []
    if (this.pendingFrames.length > 0) this.scheduleFrameFlush()
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

  /** Exposed for tests: route one decoded server message. */
  dispatch(msg: WSMessage) {
    const transcripts = useTranscriptStore.getState()
    switch (msg.type) {
      case 'frames':
        this.queueFrames(msg.session_id, msg.epoch, msg.frames ?? [])
        return
      case 'resync_required':
        // Queued frames belong to the cursor the resync replaces.
        this.flushTranscriptFrames()
        if (isTranscriptLive(msg.session_id)) void transcripts.resync(msg.session_id)
        return
      case 'chat_ack':
        transcripts.ackLocalInput(msg.request_id, msg.status, msg.error)
        return
      case 'command_result':
        if (msg.session_id) transcripts.addCommandResult(msg.request_id, msg.session_id, msg.content)
        return
      case 'state':
        this.handleState(msg)
        return
      case 'notification': {
        const { notification } = msg
        if (notification) this.handlers.notification.forEach((h) => h(notification))
        return
      }
    }
  }

  private handleState(msg: WSStateMessage) {
    const runtime = msg.runtime
    if (runtime) {
      if (runtime.sessions) {
        const currentSessions = useRuntimeStore.getState().status?.sessions || {}
        const accepted: NonNullable<RuntimeStatus['sessions']> = {}
        for (const [key, next] of Object.entries(runtime.sessions)) {
          const previousRevision = this.sessionRevisions[key]
          // Revisions track lifecycle topology; equal revisions still carry
          // live values such as context usage.
          if (previousRevision === undefined || next.revision >= previousRevision) {
            accepted[key] = next
            this.sessionRevisions[key] = next.revision
          } else if (currentSessions[key]) {
            accepted[key] = currentSessions[key]
          }
        }
        runtime.sessions = accepted
        // Sessions shown in the console follow their transcript; runtime
        // snapshots only drive the activity of the other sessions.
        const activity = new Map<string, { streaming: boolean; delegating: boolean }>()
        for (const [key, session] of Object.entries(accepted)) {
          const sessionId = runtimeSessionId(key, session)
          const current = activity.get(sessionId) ?? { streaming: false, delegating: false }
          current.streaming ||= session.state !== 'idle' && session.state !== 'error'
          current.delegating ||= !!session.delegating
          activity.set(sessionId, current)
        }
        const chat = useChatStore.getState()
        for (const sessionId of new Set([...activity.keys(), ...Object.keys(chat.streamingSessions)])) {
          if (isTranscriptLive(sessionId)) continue
          const current = activity.get(sessionId) ?? { streaming: false, delegating: false }
          chat.setRuntimeActivity(sessionId, current.streaming, current.delegating)
        }
      }
      if (runtime.agent_streams) {
        for (const [id, stream] of Object.entries(runtime.agent_streams)) {
          this.cachedStreams[id] = stream
          this.streamTimestamps[id] = Date.now()
        }
        for (const [id, cachedStream] of Object.entries(this.cachedStreams)) {
          if (!runtime.agent_streams[id]) {
            runtime.agent_streams[id] = { ...cachedStream, processing: false }
          }
        }
        this.pruneCachedStreams()
      }
      useRuntimeStore.getState().setStatus(runtime)
      this.handlers.runtime.forEach((h) => h(runtime))
    }
    if (msg.agents) {
      useAgentStore.getState().setAgents(msg.agents)
      this.handlers.agents.forEach((h) => h(msg.agents!))
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

setTranscriptTransport({
  subscribe: (sessionId, epoch, afterSeq) => wsManager.subscribeTranscript(sessionId, epoch, afterSeq),
  unsubscribe: (sessionId) => wsManager.unsubscribeTranscript(sessionId),
})
