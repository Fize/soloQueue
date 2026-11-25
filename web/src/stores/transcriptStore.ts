// Live transcript of the sessions the Web Console is showing.
//
// Each open session holds a reducer state built from a snapshot and advanced by
// ordered frames. The server delivers frames only after a subscription whose
// cursor is the snapshot watermark, so the state never misses or repeats an
// event; when it cannot resume (gap, restart, reset) the session reloads its
// snapshot instead of guessing.

import { create } from 'zustand'
import { fetchTranscript } from '@/lib/api'
import {
  activeRequests as activeTranscriptRequests,
  applyFrames,
  emptyTranscript,
  fromSnapshot,
  orderedItems,
  prependOlder,
  type TranscriptState,
} from '@/lib/transcript/reducer'
import type { CommandResult, LocalInput } from '@/lib/transcript/messages'
import type { ChatRouteInfo, TranscriptFrame, TranscriptSnapshot } from '@/types'
import { useChatStore, type ActiveChatRequest } from './chatStore'

/** Implemented by the WebSocket manager; injected to avoid an import cycle. */
export interface TranscriptTransport {
  subscribe(sessionId: string, epoch: string, afterSeq: number): void
  unsubscribe(sessionId: string): void
}

let transport: TranscriptTransport | null = null

export function setTranscriptTransport(next: TranscriptTransport | null) {
  transport = next
}

export type TranscriptStatus = 'idle' | 'loading' | 'live' | 'resyncing' | 'error'

export interface SessionTranscript {
  state: TranscriptState
  status: TranscriptStatus
  hasMore: boolean
  cursor?: string
  loadingOlder: boolean
  error?: string
}

const PAGE_SIZE = 30
/** Sessions kept subscribed after the user navigates away. */
const MAX_OPEN_SESSIONS = 6

const generations = new Map<string, number>()
const recentSessions: string[] = []
/** Requests started by this browser for a built-in slash command. */
const systemCommandRequests = new Set<string>()

interface TranscriptStoreState {
  sessions: Record<string, SessionTranscript>
  local: Record<string, LocalInput>
  commands: Record<string, CommandResult>

  /** Show a session: resume its subscription or load its snapshot. */
  open: (sessionId: string) => Promise<void>
  /** Rebuild a session from a fresh snapshot (gap, restart, reset). */
  resync: (sessionId: string) => Promise<void>
  /** Stop receiving frames; the state is kept for a fast resume. */
  close: (sessionId: string) => void
  loadOlder: (sessionId: string) => Promise<void>
  receiveFrames: (sessionId: string, epoch: string, frames: TranscriptFrame[]) => void
  addLocalInput: (input: LocalInput) => void
  ackLocalInput: (requestId: string, status: 'accepted' | 'queued' | 'rejected', error?: string) => void
  failLocalInput: (requestId: string, error: string) => void
  addCommandResult: (requestId: string, sessionId: string, text: string) => void
  /** Timestamps of the user item and the replies that follow it. */
  deleteTargets: (sessionId: string, userTimestamps: string[]) => string[]
  forget: (sessionId: string) => void
}

function touch(sessionId: string) {
  const index = recentSessions.indexOf(sessionId)
  if (index >= 0) recentSessions.splice(index, 1)
  recentSessions.push(sessionId)
}

function nextGeneration(sessionId: string) {
  const generation = (generations.get(sessionId) ?? 0) + 1
  generations.set(sessionId, generation)
  return generation
}

/**
 * Requests a snapshot already accounts for. A local input is settled by the
 * frame that commits, queues, drops or ends it, but those frames are skipped
 * while the stream is rebuilt: a reset publishes request_started,
 * transcript_reset and request_end back to back, and the snapshot taken after
 * the reset is already past the end. `ended` is the subset whose request is over.
 */
function accountedFor(snapshot: TranscriptSnapshot) {
  const settled = new Set<string>()
  const ended = new Set<string>()
  for (const item of snapshot.items ?? []) {
    if (item.kind === 'user' && item.request_id) settled.add(item.request_id)
  }
  for (const pending of snapshot.pending ?? []) settled.add(pending.request_id)
  for (const request of snapshot.requests ?? []) {
    if (!request.ended) continue
    for (const id of [request.request_id, ...(request.request_ids ?? [])]) {
      settled.add(id)
      ended.add(id)
    }
  }
  return { settled, ended }
}

export const useTranscriptStore = create<TranscriptStoreState>((set, get) => {
  const patchSession = (sessionId: string, patch: Partial<SessionTranscript>) =>
    set((store) => {
      const current = store.sessions[sessionId]
      if (!current) return store
      return { sessions: { ...store.sessions, [sessionId]: { ...current, ...patch } } }
    })

  const loadSnapshot = async (sessionId: string) => {
    const generation = nextGeneration(sessionId)
    set((store) => {
      const current = store.sessions[sessionId]
      return {
        sessions: {
          ...store.sessions,
          [sessionId]: current
            ? { ...current, status: current.status === 'live' ? 'resyncing' : current.status === 'idle' ? 'loading' : current.status }
            : { state: emptyTranscript(), status: 'loading', hasMore: false, loadingOlder: false },
        },
      }
    })
    try {
      const snapshot = await fetchTranscript(sessionId, { limit: PAGE_SIZE })
      if (generations.get(sessionId) !== generation) return
      set((store) => ({
        sessions: {
          ...store.sessions,
          [sessionId]: {
            state: fromSnapshot(snapshot),
            status: 'live',
            hasMore: snapshot.has_more,
            cursor: snapshot.cursor,
            loadingOlder: false,
          },
        },
      }))
      const { settled, ended } = accountedFor(snapshot)
      for (const id of ended) systemCommandRequests.delete(id)
      removeLocal(settled)
      const chat = useChatStore.getState()
      chat.applyCompactionSnapshot(sessionId, snapshot.compactions ?? [])
      chat.setSessionContextUsage(sessionId, snapshot.ctxwin_used, snapshot.ctxwin_limit)
      transport?.subscribe(sessionId, snapshot.epoch, snapshot.seq)
      syncActivity(sessionId)
    } catch (error) {
      if (generations.get(sessionId) !== generation) return
      patchSession(sessionId, {
        status: 'error',
        error: error instanceof Error ? error.message : String(error),
      })
    }
  }

  const syncActivity = (sessionId: string) => {
    const entry = get().sessions[sessionId]
    if (!entry || (entry.status !== 'live' && entry.status !== 'resyncing')) return
    const state = entry.state
    const running = activeTranscriptRequests(state).sort(
      (a, b) => (a.started_seq ?? 0) - (b.started_seq ?? 0)
    )
    const sending = Object.values(get().local).filter(
      (input) => input.sessionId === sessionId && (input.status === 'sending' || input.status === 'accepted')
    )
    const requests: ActiveChatRequest[] = []
    let route: ChatRouteInfo | undefined
    for (const request of running) {
      const requestRoute: ChatRouteInfo = {
        requestId: request.request_id,
        sessionId,
        taskLevel: request.task_type ?? '',
        modelId: request.model_id ?? '',
        providerId: request.provider_id,
        agentInstanceId: request.agent_instance_id,
      }
      if (request.model_id || !route) route = requestRoute
      requests.push({
        requestId: request.request_id,
        cancelRequestId: request.request_id,
        sessionId,
        status: 'streaming',
        route: requestRoute,
      })
    }
    for (const input of sending) {
      if (requests.some((request) => request.requestId === input.requestId)) continue
      requests.push({ requestId: input.requestId, cancelRequestId: input.requestId, sessionId, status: 'starting' })
    }
    for (const pending of Object.values(state.pending)) {
      if (requests.some((request) => request.requestId === pending.request_id)) continue
      requests.push({ requestId: pending.request_id, cancelRequestId: pending.request_id, sessionId, status: 'queued' })
    }
    useChatStore.getState().setSessionActivity(sessionId, {
      streaming: running.length > 0 || sending.length > 0,
      delegating: running.some((request) => request.delegating),
      systemCommand:
        sending.some((input) => input.systemCommand) ||
        running.some((request) => systemCommandRequests.has(request.request_id)),
      route,
      requests,
    })
  }

  const removeLocal = (ids: Iterable<string>) => {
    const remove = [...ids].filter((id) => get().local[id])
    if (remove.length === 0) return
    set((store) => {
      const local = { ...store.local }
      for (const id of remove) delete local[id]
      return { local }
    })
  }

  /** Command results answer the conversation a reset has just cleared. */
  const dropCommands = (sessionId: string) => {
    const stale = Object.values(get().commands).filter((command) => command.sessionId === sessionId)
    if (stale.length === 0) return
    set((store) => {
      const commands = { ...store.commands }
      for (const command of stale) delete commands[command.requestId]
      return { commands }
    })
  }

  return {
    sessions: {},
    local: {},
    commands: {},

    open: async (sessionId) => {
      if (!sessionId) return
      touch(sessionId)
      const entry = get().sessions[sessionId]
      if (entry && (entry.status === 'live' || entry.status === 'loading' || entry.status === 'resyncing')) {
        return
      }
      // Bound server work: only the most recently shown sessions stay subscribed.
      while (recentSessions.length > MAX_OPEN_SESSIONS) {
        const oldest = recentSessions.shift()!
        if (oldest !== sessionId) get().close(oldest)
      }
      if (entry && entry.status === 'idle' && entry.state.epoch) {
        // Resume after the last applied frame; the server replays the gap or
        // asks for a resync when it no longer retains it.
        patchSession(sessionId, { status: 'live' })
        transport?.subscribe(sessionId, entry.state.epoch, entry.state.headSeq)
        syncActivity(sessionId)
        return
      }
      await loadSnapshot(sessionId)
    },

    resync: (sessionId) => loadSnapshot(sessionId),

    close: (sessionId) => {
      const entry = get().sessions[sessionId]
      if (!entry) return
      nextGeneration(sessionId)
      transport?.unsubscribe(sessionId)
      patchSession(sessionId, { status: 'idle' })
    },

    loadOlder: async (sessionId) => {
      const entry = get().sessions[sessionId]
      if (!entry || !entry.hasMore || entry.loadingOlder || !entry.cursor) return
      patchSession(sessionId, { loadingOlder: true })
      try {
        const page = await fetchTranscript(sessionId, { before: entry.cursor, limit: PAGE_SIZE })
        set((store) => {
          const current = store.sessions[sessionId]
          if (!current) return store
          return {
            sessions: {
              ...store.sessions,
              [sessionId]: {
                ...current,
                state: prependOlder(current.state, page.items ?? []),
                hasMore: page.has_more,
                cursor: page.cursor,
                loadingOlder: false,
              },
            },
          }
        })
      } catch {
        patchSession(sessionId, { loadingOlder: false })
      }
    },

    receiveFrames: (sessionId, epoch, frames) => {
      const entry = get().sessions[sessionId]
      // Frames for a session that is loading belong to a superseded cursor;
      // the subscription made after the snapshot replays them.
      if (!entry || entry.status !== 'live') return
      const result = applyFrames(entry.state, epoch, frames)
      if (result.state !== entry.state) patchSession(sessionId, { state: result.state })

      const settled = new Set<string>()
      const chat = useChatStore.getState()
      for (const frame of frames) {
        if (frame.seq > result.state.headSeq) break
        switch (frame.kind) {
          case 'input_queued':
          case 'input_committed':
          case 'input_dropped':
          case 'request_end':
            if (frame.request_id) settled.add(frame.request_id)
            for (const id of frame.request_ids ?? []) settled.add(id)
            for (const part of frame.parts ?? []) settled.add(part.request_id)
            if (frame.kind === 'request_end') {
              for (const id of [frame.request_id, ...(frame.request_ids ?? [])]) {
                if (id) systemCommandRequests.delete(id)
              }
            }
            break
          case 'transcript_reset':
            dropCommands(sessionId)
            break
          case 'session_meta':
            if (frame.session_name) chat.renameSession(sessionId, frame.session_name)
            if (frame.plans) chat.updateSessionPlans(sessionId, frame.plans)
            break
          case 'compaction_status':
            if (frame.compaction) {
              const owner = Object.values(result.state.requests).find(
                (request) => !request.ended && systemCommandRequests.has(request.request_id)
              )
              chat.setCompactionStatus(sessionId, frame.compaction, owner?.request_id)
            }
            break
        }
      }
      removeLocal(settled)
      syncActivity(sessionId)
      if (result.resync) void get().resync(sessionId)
    },

    addLocalInput: (input) => {
      if (input.systemCommand) systemCommandRequests.add(input.requestId)
      set((store) => ({ local: { ...store.local, [input.requestId]: input } }))
      syncActivity(input.sessionId)
    },

    ackLocalInput: (requestId, status, error) => {
      const input = get().local[requestId]
      if (!input) return
      if (status === 'rejected') {
        get().failLocalInput(requestId, error || 'Message was rejected by the server.')
        return
      }
      set((store) => ({ local: { ...store.local, [requestId]: { ...input, status } } }))
      syncActivity(input.sessionId)
    },

    failLocalInput: (requestId, error) => {
      const input = get().local[requestId]
      if (!input) return
      systemCommandRequests.delete(requestId)
      set((store) => ({ local: { ...store.local, [requestId]: { ...input, status: 'failed', error } } }))
      syncActivity(input.sessionId)
    },

    addCommandResult: (requestId, sessionId, text) => {
      const input = get().local[requestId]
      const entry = get().sessions[sessionId]
      systemCommandRequests.delete(requestId)
      set((store) => {
        const local = { ...store.local }
        delete local[requestId]
        return {
          local,
          commands: {
            ...store.commands,
            [requestId]: {
              requestId,
              sessionId,
              prompt: input?.text ?? '',
              text,
              afterSeq: entry?.state.headSeq ?? 0,
              ts: new Date().toISOString(),
            },
          },
        }
      })
      syncActivity(sessionId)
    },

    deleteTargets: (sessionId, userTimestamps) => {
      const entry = get().sessions[sessionId]
      const targets = new Set(userTimestamps)
      if (!entry) return [...targets]
      const items = orderedItems(entry.state)
      for (const ts of userTimestamps) {
        const index = items.findIndex((item) => item.kind === 'user' && item.ts === ts)
        if (index < 0) continue
        for (const item of items.slice(index + 1)) {
          if (item.kind === 'user') break
          if (item.ts) targets.add(item.ts)
        }
      }
      return [...targets]
    },

    forget: (sessionId) => {
      nextGeneration(sessionId)
      transport?.unsubscribe(sessionId)
      const index = recentSessions.indexOf(sessionId)
      if (index >= 0) recentSessions.splice(index, 1)
      set((store) => {
        const { [sessionId]: _removed, ...sessions } = store.sessions
        const local = Object.fromEntries(
          Object.entries(store.local).filter(([, input]) => input.sessionId !== sessionId)
        )
        const commands = Object.fromEntries(
          Object.entries(store.commands).filter(([, command]) => command.sessionId !== sessionId)
        )
        return { sessions, local, commands }
      })
    },
  }
})

/** Sessions whose cursor must be re-sent after the WebSocket reconnects. */
export function transcriptSubscriptions(): { sessionId: string; epoch: string; afterSeq: number }[] {
  return Object.entries(useTranscriptStore.getState().sessions)
    .filter(([, entry]) => entry.status === 'live' && !!entry.state.epoch)
    .map(([sessionId, entry]) => ({
      sessionId,
      epoch: entry.state.epoch!,
      afterSeq: entry.state.headSeq,
    }))
}

/** Whether frames, not runtime snapshots, drive this session's activity. */
export function isTranscriptLive(sessionId: string): boolean {
  const status = useTranscriptStore.getState().sessions[sessionId]?.status
  return status === 'live' || status === 'resyncing'
}
