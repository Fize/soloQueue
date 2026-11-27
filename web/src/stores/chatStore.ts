import { create } from 'zustand'
import type {
  ChatSession,
  ChatRouteInfo,
  CompactionStatus,
  SessionModelSelection,
  SessionModelState,
} from '@/types'
import {
  listSessions,
  createL2Session,
  deleteL2Session,
  rewindSession as apiRewindSession,
  deleteSessionMessages as apiDeleteSessionMessages,
  setSessionModel,
} from '@/lib/api'
import { useInspectorStateStore } from './inspectorStateStore'

// Conversation content lives in transcriptStore. This store keeps the session
// list, compaction notices and the per-session activity flags derived from the
// transcript (or from runtime snapshots for sessions that are not open).

export type ChatRequestStatus = 'starting' | 'streaming' | 'queued' | 'cancelling'

export interface ActiveChatRequest {
  requestId: string
  /** Transport/root request that owns backend cancellation after queue hand-off. */
  cancelRequestId?: string
  sessionId: string
  status: ChatRequestStatus
  route?: ChatRouteInfo
}

export interface SessionActivity {
  streaming: boolean
  delegating: boolean
  systemCommand: boolean
  route?: ChatRouteInfo
  requests: ActiveChatRequest[]
}

interface ChatState {
  sessions: ChatSession[]
  /**
   * Placeholder sessions shown in the tree while the user has not sent a single
   * message yet. They exist only on the client: no server session, timeline or
   * metadata is created until the first message is sent, so abandoning a new
   * chat leaves nothing behind to clean up.
   */
  drafts: ChatSession[]
  /**
   * Model chosen on the composer before any chat exists (the welcome screen).
   * The first message of the chat started there applies it, exactly like a
   * placeholder's choice; a session created from it clears the field.
   */
  pendingModel: SessionModelSelection | null
  /**
   * Last server-reported model state per session. A composer that remounts
   * (empty transcript, session switch) reads this instead of showing "Auto"
   * until the fetch returns.
   */
  sessionModels: Record<string, SessionModelState>
  activeSessionId: string | null
  compactions: Record<string, CompactionStatus[]>
  compactionClearWatermarks: Record<string, string>
  activeRequests: Record<string, ActiveChatRequest>
  streamingSessions: Record<string, boolean>
  /** True while a built-in slash command runs; hides routing chips. */
  systemCommandSessions: Record<string, boolean>
  /** True while async delegation is in progress. */
  delegatingSessions: Record<string, boolean>
  routeSessions: Record<string, ChatRouteInfo | undefined>
  titleGenerated: Record<string, boolean>
  /** True while loadSessions() is in flight. */
  sessionsLoading: boolean
  /** True once a listSessions call has succeeded. */
  sessionsLoaded: boolean

  loadSessions: () => Promise<void>
  createL2Session: (
    group: string,
    workDir?: string,
    model?: SessionModelSelection | null,
  ) => Promise<string | null>
  deleteL2Session: (id: string) => Promise<void>
  setActiveSession: (id: string) => void
  /** Creates a client-side placeholder session and returns its id. */
  beginDraft: (group: string, projectPath?: string) => string
  /** Keeps a model chosen before the placeholder's session exists; null clears it. */
  setDraftModel: (id: string, selection: SessionModelSelection | null) => void
  /** Keeps a model chosen while no chat exists yet; null clears it. */
  setPendingModel: (selection: SessionModelSelection | null) => void
  /** Records the last known model state of a real session. */
  setSessionModelState: (id: string, state: SessionModelState) => void
  /** Drops one placeholder session without touching the backend. */
  clearDraft: (id: string) => void
  /** Marks a session as just active so the tree reorders without a refetch. */
  touchSession: (id: string) => void
  renameSession: (id: string, name: string) => void
  updateSessionPlans: (id: string, plans: string[]) => void
  markTitleGenerated: (id: string) => void
  setSessionContextUsage: (id: string, used?: number, limit?: number) => void

  setCompactionStatus: (sessionId: string, status: CompactionStatus, requestId?: string) => void
  applyCompactionSnapshot: (sessionId: string, statuses: CompactionStatus[]) => void
  clearCompactions: (sessionId: string) => void

  /** Replace every activity flag of one session. */
  setSessionActivity: (sessionId: string, activity: SessionActivity) => void
  /** Runtime-derived flags for sessions without a live transcript. */
  setRuntimeActivity: (sessionId: string, streaming: boolean, delegating: boolean) => void
  setStreaming: (v: boolean, sessionId?: string | null) => void
  setDelegating: (v: boolean, sessionId?: string | null) => void
  setSystemCommandRunning: (v: boolean, sessionId?: string | null) => void
  removeRequest: (requestId: string) => void

  rewindSession: (sessionId: string, targetTs: string) => Promise<void>
  deleteSessionMessages: (sessionId: string, targetTsList: string[]) => Promise<void>
}

const COMPACTION_ERROR_NOTICE_MS = 30_000

// ─── Session ordering ───────────────────────────────────────────────────────
//
// One ordering rule for the whole console: most recently active first, then
// newest created, then id. Timestamps are compared numerically — ordering ISO
// strings with localeCompare is not a time comparison and it silently fails for
// mixed timezone offsets ("…10:00:00+08:00" vs "…05:00:00Z") or ragged
// fractional seconds. The comparator mirrors the server's total order, so a
// locally touched session and a refetched list agree.

export const DRAFT_SESSION_PREFIX = 'draft:'

export function isDraftSessionId(id: string | null | undefined): boolean {
  return !!id && id.startsWith(DRAFT_SESSION_PREFIX)
}

function parseTimestamp(value?: string): number {
  if (!value) return 0
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : 0
}

function sessionActivity(session: ChatSession): number {
  return parseTimestamp(session.last_active_at) || parseTimestamp(session.createdAt)
}

export function compareSessions(a: ChatSession, b: ChatSession): number {
  const activity = sessionActivity(b) - sessionActivity(a)
  if (activity !== 0) return activity
  const created = parseTimestamp(b.createdAt) - parseTimestamp(a.createdAt)
  if (created !== 0) return created
  return a.id.localeCompare(b.id)
}

/** Returns a new array ordered by compareSessions. */
export function sortSessions(sessions: ChatSession[]): ChatSession[] {
  return [...sessions].sort(compareSessions)
}

function isAfterCompactionClear(status: CompactionStatus, watermark?: string) {
  if (!watermark) return true
  const startedAt = Date.parse(status.started_at)
  const clearedAt = Date.parse(watermark)
  return Number.isFinite(startedAt) && Number.isFinite(clearedAt) && startedAt >= clearedAt
}

function mergeCompactionStatuses(snapshot: CompactionStatus[], current: CompactionStatus[], watermark?: string) {
  const terminal = new Set(['completed', 'failed', 'interrupted'])
  const merged = new Map(
    snapshot.filter((status) => isAfterCompactionClear(status, watermark)).map((status) => [status.id, status])
  )
  for (const live of current.filter((status) => isAfterCompactionClear(status, watermark))) {
    const saved = merged.get(live.id)
    if (!saved) {
      merged.set(live.id, live)
      continue
    }
    const savedTerminal = terminal.has(saved.status)
    const liveTerminal = terminal.has(live.status)
    if (liveTerminal && !savedTerminal) {
      merged.set(live.id, live)
      continue
    }
    if (liveTerminal && savedTerminal && (live.finished_at || '') > (saved.finished_at || '')) {
      merged.set(live.id, live)
    }
    // A snapshot may update the lifecycle, but cannot restart a live notice or
    // discard the request ownership used to suppress a duplicate error.
    const selected = merged.get(live.id)!
    merged.set(live.id, { ...selected, noticeExpiresAt: live.noticeExpiresAt, requestId: live.requestId })
  }
  return Array.from(merged.values())
}

// Coalesce concurrent loadSessions() calls.
let inflightSessionsLoad: Promise<void> | null = null

function newDraftSessionId(): string {
  // randomUUID needs a secure context; a plain-HTTP origin falls back to a
  // timestamp id. The value is local-only, so collision resistance is enough.
  const uuid =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
  return `${DRAFT_SESSION_PREFIX}${uuid}`
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: [],
  drafts: [],
  pendingModel: null,
  sessionModels: {},
  activeSessionId: null,
  compactions: {},
  compactionClearWatermarks: {},
  activeRequests: {},
  streamingSessions: {},
  systemCommandSessions: {},
  delegatingSessions: {},
  routeSessions: {},
  titleGenerated: {},
  sessionsLoading: false,
  sessionsLoaded: false,

  loadSessions: async () => {
    if (inflightSessionsLoad) return inflightSessionsLoad
    set({ sessionsLoading: true })
    const applyResult = (data: { sessions?: any[] }) => {
      const mapped: ChatSession[] = (data.sessions || []).map((s: any) => ({
        ...s,
        createdAt: s.createdAt || s.created_at || new Date().toISOString(),
        last_active_at: s.last_active_at || s.lastActiveAt || undefined,
      }))
      set((prev) => {
        // Preserve the active session if the list raced its creation.
        if (prev.activeSessionId && !mapped.some((m) => m.id === prev.activeSessionId)) {
          const activeFromStore = prev.sessions.find((ss) => ss.id === prev.activeSessionId)
          if (activeFromStore) mapped.push(activeFromStore)
        }
        // The server already returns this order; sorting again keeps a session
        // touched locally in place if the list predates it.
        return { sessions: sortSessions(mapped), sessionsLoaded: true }
      })
    }
    inflightSessionsLoad = (async () => {
      try {
        applyResult(await listSessions())
      } catch {
        // The backend may still be starting; retry once.
        try {
          applyResult(await listSessions())
        } catch {
          // Both attempts failed; sessions stay empty.
        }
      } finally {
        set({ sessionsLoading: false })
        inflightSessionsLoad = null
      }
    })()
    return inflightSessionsLoad
  },

  createL2Session: async (group: string, workDir?: string, model?: SessionModelSelection | null) => {
    try {
      const info = await createL2Session(group, workDir || '')
      const session: ChatSession = {
        id: `l2:${info.id}`,
        type: 'l2',
        name: '',
        group: info.group,
        agent_name: info.agent_name,
        project_path: info.project_path || workDir || '',
        design_dir: info.design_dir || '',
        createdAt: info.created_at,
        last_active_at: info.created_at,
      }
      // Apply a model chosen while the chat was only a placeholder before the
      // session becomes active: the composer reads its model from the server,
      // so activating first would show the default for a moment and never
      // refresh. A failed write keeps the default instead of blocking the chat.
      if (model) {
        try {
          await setSessionModel(info.id, model)
        } catch {
          // Fall through: the session exists with the configured default model.
        }
      }
      set((s) => ({
        sessions: sortSessions([session, ...s.sessions]),
        activeSessionId: session.id,
        // The placeholder is now backed by a real session.
        drafts: [],
        // A choice made on the welcome screen belonged to this chat.
        pendingModel: null,
      }))
      return session.id
    } catch {
      return null
    }
  },

  beginDraft: (group: string, projectPath?: string) => {
    const now = new Date().toISOString()
    const draft: ChatSession = {
      id: newDraftSessionId(),
      type: 'l2',
      name: '',
      group,
      project_path: projectPath || '',
      createdAt: now,
      last_active_at: now,
      draft: true,
    }
    // Placeholders are per new-chat action: a second "new chat" replaces the
    // previous unstarted one instead of accumulating empty rows. The choice made
    // on the welcome screen belonged to the chat that was never started, and a
    // placeholder keeps its own choice, so it must not leak into this one.
    set(() => ({ drafts: [draft], activeSessionId: draft.id, pendingModel: null }))
    return draft.id
  },

  clearDraft: (id: string) =>
    set((s) => {
      const drafts = s.drafts.filter((d) => d.id !== id)
      if (drafts.length === s.drafts.length) return s
      return {
        drafts,
        activeSessionId: s.activeSessionId === id ? null : s.activeSessionId,
      }
    }),

  touchSession: (id: string) =>
    set((s) => {
      // Draft rows are made active by being sent, at which point they become a
      // real session; nothing to touch yet.
      if (isDraftSessionId(id)) return s
      const found = s.sessions.find((sess) => sess.id === id)
      if (!found) return s
      const touched = { ...found, last_active_at: new Date().toISOString() }
      return {
        sessions: sortSessions([touched, ...s.sessions.filter((sess) => sess.id !== id)]),
      }
    }),

  deleteL2Session: async (id: string) => {
    // A placeholder never reached the backend: dropping it locally is the
    // whole cleanup.
    if (isDraftSessionId(id)) {
      set((s) => ({
        drafts: s.drafts.filter((d) => d.id !== id),
        activeSessionId: s.activeSessionId === id ? null : s.activeSessionId,
      }))
      return
    }
    const uuid = id.replace('l2:', '')
    try {
      await deleteL2Session(uuid)
      set((s) => {
        const without = <T,>(record: Record<string, T>) => {
          const { [id]: _removed, ...rest } = record
          return rest
        }
        return {
          sessions: s.sessions.filter((sess) => sess.id !== id),
          activeSessionId: s.activeSessionId === id ? null : s.activeSessionId,
          titleGenerated: without(s.titleGenerated),
          routeSessions: without(s.routeSessions),
          streamingSessions: without(s.streamingSessions),
          systemCommandSessions: without(s.systemCommandSessions),
          delegatingSessions: without(s.delegatingSessions),
          compactions: without(s.compactions),
          activeRequests: Object.fromEntries(
            Object.entries(s.activeRequests).filter(([, request]) => request.sessionId !== id)
          ),
        }
      })
      useInspectorStateStore.getState().removeSession(id)
    } catch {
      // ignore
    }
  },

  setDraftModel: (id: string, selection: SessionModelSelection | null) =>
    set((s) => ({
      drafts: s.drafts.map((d) => (d.id === id ? { ...d, model: selection ?? undefined } : d)),
    })),

  setPendingModel: (selection: SessionModelSelection | null) =>
    set({ pendingModel: selection }),

  setSessionModelState: (id, state) =>
    set((s) => ({ sessionModels: { ...s.sessionModels, [id]: state } })),

  // Switching away from an unstarted placeholder abandons it: a new chat that
  // never produced a message must not linger in the tree. An empty id is not a
  // switch — the router reports "no session" for one render while it catches up
  // with a placeholder beginDraft() just activated, and dropping the row there
  // would lose the context (and any model) chosen for the pending chat.
  setActiveSession: (id: string) =>
    set((s) => {
      if (!id) return { activeSessionId: id }
      const toDraft = s.drafts.some((d) => d.id === id)
      return {
        activeSessionId: id,
        drafts: toDraft ? s.drafts : [],
        // The welcome-screen choice belonged to the chat that never started.
        pendingModel: toDraft ? s.pendingModel : null,
      }
    }),

  renameSession: (id: string, name: string) =>
    set((s) => ({ sessions: s.sessions.map((sess) => (sess.id === id ? { ...sess, name } : sess)) })),

  updateSessionPlans: (id: string, plans: string[]) =>
    set((s) => ({ sessions: s.sessions.map((sess) => (sess.id === id ? { ...sess, plans } : sess)) })),

  markTitleGenerated: (id: string) => set((s) => ({ titleGenerated: { ...s.titleGenerated, [id]: true } })),

  setSessionContextUsage: (id, used, limit) =>
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.id === id
          ? {
              ...sess,
              ctxwin_used: used !== undefined ? used : sess.ctxwin_used,
              ctxwin_limit: limit !== undefined ? limit : sess.ctxwin_limit,
            }
          : sess
      ),
    })),

  setCompactionStatus: (sessionId, status, requestId) =>
    set((s) => {
      if (!isAfterCompactionClear(status, s.compactionClearWatermarks[sessionId])) return {}
      const rows = [...(s.compactions[sessionId] || [])]
      const i = rows.findIndex((r) => r.id === status.id)
      const previous = rows[i]
      if (previous && ['completed', 'failed', 'interrupted'].includes(previous.status)) return {}
      const row: CompactionStatus = {
        ...status,
        requestId: previous?.requestId ?? (status.reason === 'user_command' ? requestId : undefined),
        noticeExpiresAt:
          status.status === 'failed' || status.status === 'interrupted'
            ? Date.now() + COMPACTION_ERROR_NOTICE_MS
            : undefined,
      }
      if (i < 0) rows.push(row)
      else rows[i] = row
      return { compactions: { ...s.compactions, [sessionId]: rows } }
    }),

  applyCompactionSnapshot: (sessionId, statuses) =>
    set((s) => ({
      compactions: {
        ...s.compactions,
        [sessionId]: mergeCompactionStatuses(
          statuses,
          s.compactions[sessionId] || [],
          s.compactionClearWatermarks[sessionId]
        ),
      },
    })),

  clearCompactions: (sessionId) =>
    set((s) => ({
      compactions: { ...s.compactions, [sessionId]: [] },
      compactionClearWatermarks: { ...s.compactionClearWatermarks, [sessionId]: new Date().toISOString() },
    })),

  setSessionActivity: (sessionId, activity) =>
    set((s) => {
      const activeRequests = Object.fromEntries(
        Object.entries(s.activeRequests).filter(([, request]) => request.sessionId !== sessionId)
      )
      for (const request of activity.requests) activeRequests[request.requestId] = request
      const routeSessions = { ...s.routeSessions }
      if (activity.route) routeSessions[sessionId] = activity.route
      else delete routeSessions[sessionId]
      return {
        activeRequests,
        routeSessions,
        streamingSessions: { ...s.streamingSessions, [sessionId]: activity.streaming },
        delegatingSessions: { ...s.delegatingSessions, [sessionId]: activity.delegating },
        systemCommandSessions: { ...s.systemCommandSessions, [sessionId]: activity.systemCommand },
      }
    }),

  setRuntimeActivity: (sessionId, streaming, delegating) =>
    set((s) => {
      if (!!s.streamingSessions[sessionId] === streaming && !!s.delegatingSessions[sessionId] === delegating) {
        return s
      }
      return {
        streamingSessions: { ...s.streamingSessions, [sessionId]: streaming },
        delegatingSessions: { ...s.delegatingSessions, [sessionId]: delegating },
      }
    }),

  setStreaming: (v, sessionId) =>
    set((s) => {
      const id = sessionId || s.activeSessionId
      if (!id) return s
      return { streamingSessions: { ...s.streamingSessions, [id]: v } }
    }),

  setDelegating: (v, sessionId) =>
    set((s) => {
      const id = sessionId || s.activeSessionId
      if (!id) return s
      return { delegatingSessions: { ...s.delegatingSessions, [id]: v } }
    }),

  setSystemCommandRunning: (v, sessionId) =>
    set((s) => {
      const id = sessionId || s.activeSessionId
      if (!id) return s
      return { systemCommandSessions: { ...s.systemCommandSessions, [id]: v } }
    }),

  removeRequest: (requestId) =>
    set((s) => {
      if (!s.activeRequests[requestId]) return s
      const { [requestId]: _removed, ...activeRequests } = s.activeRequests
      return { activeRequests }
    }),

  // The server publishes a transcript reset after both operations; the open
  // transcript reloads its snapshot from it.
  rewindSession: async (sessionId, targetTs) => {
    await apiRewindSession(sessionId, targetTs)
  },

  deleteSessionMessages: async (sessionId, targetTsList) => {
    await apiDeleteSessionMessages(sessionId, targetTsList)
  },
}))
