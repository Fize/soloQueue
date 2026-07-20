import { create } from 'zustand'
import type { ChatSession, ChatMessage, ChatSegment, ChatRouteInfo } from '@/types'
import { listSessions, createL2Session, deleteL2Session, fetchSessionHistory, rewindSession as apiRewindSession, deleteSessionMessages as apiDeleteSessionMessages } from '@/lib/api'
import type { SessionHistoryMessage, SessionHistorySegment } from '@/types'

interface ChatState {
  sessions: ChatSession[]
  activeSessionId: string | null
  messages: Record<string, ChatMessage[]> // keyed by session id
  streamingSessions: Record<string, boolean>
  systemCommandSessions: Record<string, boolean> // keyed by session id, true while a built-in system slash command (/clear, /compact, /cancel, /help, /version) is being executed. Used to suppress the L0–L3 / model chips in the working indicator, since those commands don't run a routed task.
  delegatingSessions: Record<string, boolean> // keyed by session id, true when async delegation is in progress (L1 waiting for L2)
  routeSessions: Record<string, ChatRouteInfo | undefined>
  titleGenerated: Record<string, boolean> // track which sessions already had title generated
  historyLoading: Record<string, boolean> // track which sessions are loading history
  historyHasMore: Record<string, boolean> // track which sessions have more history to load
  historyCursor: Record<string, string | null> // cursor for next load-more page
  sessionsLoading: boolean // true while loadSessions() is in flight — drives the "Loading sessions…" UI

  loadSessions: () => Promise<void>
  createL2Session: (group: string, workDir?: string) => Promise<string | null>
  deleteL2Session: (id: string) => Promise<void>
  setActiveSession: (id: string) => void
  loadHistory: (sessionId: string) => Promise<void>
  loadMoreHistory: (sessionId: string) => Promise<void>
  renameSession: (id: string, name: string) => void
  updateSessionPlans: (id: string, plans: string[]) => void
  markTitleGenerated: (id: string) => void

  addMessage: (sessionId: string, message: ChatMessage) => void
  updateLastAssistantSegment: (sessionId: string, segment: ChatSegment) => void
  appendToLastAssistantContent: (sessionId: string, text: string) => void
  appendToLastAssistantThinking: (sessionId: string, text: string) => void
  appendToLastAssistantCompact: (sessionId: string, text: string) => void
  updateToolCallResult: (
    sessionId: string,
    callId: string,
    result: string,
    error?: string,
    durationMs?: number
  ) => void
  setStreaming: (v: boolean, sessionId?: string | null) => void
  setSystemCommandRunning: (v: boolean, sessionId?: string | null) => void
  setDelegating: (v: boolean, sessionId?: string | null) => void
  setRoute: (route: ChatRouteInfo) => void
  clearRoute: (sessionId: string, requestId: string) => void
  removeLastEmptyAssistantMessage: (sessionId: string) => void
  addDelegationSegment: (sessionId: string, delegation: { agentName: string; task: string }) => void
  completeLastDelegation: (sessionId: string, agentName: string, durationMs?: number, resultContent?: string) => void
  resolveToolConfirm: (sessionId: string, callId: string, choice: string) => void
  
  rewindSession: (sessionId: string, targetTs: string) => Promise<void>
  deleteSessionMessages: (sessionId: string, targetTsList: string[]) => Promise<void>
}

const PAGE_SIZE = 30 // number of messages to load per page

// Coalesce concurrent loadSessions() calls (e.g. App.tsx auto-retry effect
// AND SessionTree mount AND focus/visibility handler all firing in the same
// render tick). The first call kicks off the fetch; subsequent callers await
// the same in-flight promise instead of issuing duplicate requests.
let inflightSessionsLoad: Promise<void> | null = null

export const useChatStore = create<ChatState>((set) => ({
  sessions: [],
  activeSessionId: null,
  messages: {},
  streamingSessions: {},
  systemCommandSessions: {},
  delegatingSessions: {},
  routeSessions: {},
  titleGenerated: {},
  historyLoading: {},
  historyHasMore: {},
  historyCursor: {},
  sessionsLoading: false,

  loadSessions: async () => {
    if (inflightSessionsLoad) return inflightSessionsLoad

    set({ sessionsLoading: true })
    const applyResult = (data: { sessions?: any[] }) => {
      const mapped = (data.sessions || []).map((s: any) => ({
        ...s,
        createdAt: s.createdAt || s.created_at || new Date().toISOString(),
      }))
      set((prev) => {
        // Preserve the active session if missing from API response
        // (race: list request sent before create completed).
        if (prev.activeSessionId) {
          const hasActive = mapped.some((m) => m.id === prev.activeSessionId)
          if (!hasActive) {
            const activeFromStore = prev.sessions.find((ss) => ss.id === prev.activeSessionId)
            if (activeFromStore) {
              mapped.push(activeFromStore)
            }
          }
        }
        return { sessions: mapped }
      })
    }
    inflightSessionsLoad = (async () => {
      try {
        applyResult(await listSessions())
      } catch {
        // First attempt failed — most often because the backend was still
        // starting when this fired. Retry once. If the backend is genuinely
        // down the retry will also fail and the caller is back to the
        // empty state, ready for the next backendRunning transition.
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

  createL2Session: async (group: string, workDir?: string) => {
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
      }
      set((s) => ({ sessions: [...s.sessions, session], activeSessionId: session.id }))
      return session.id
    } catch {
      return null
    }
  },

  deleteL2Session: async (id: string) => {
    const uuid = id.replace('l2:', '')
    try {
      await deleteL2Session(uuid)
      set((s) => {
        const { [id]: _msg, ...restMessages } = s.messages
        const { [id]: _title, ...restTitle } = s.titleGenerated
        const { [id]: _loading, ...restLoading } = s.historyLoading
        const { [id]: _more, ...restHasMore } = s.historyHasMore
        const { [id]: _cursor, ...restCursor } = s.historyCursor
        const { [id]: _route, ...restRoutes } = s.routeSessions
        return {
          sessions: s.sessions.filter((sess) => sess.id !== id),
          activeSessionId: s.activeSessionId === id ? null : s.activeSessionId,
          messages: restMessages,
          titleGenerated: restTitle,
          historyLoading: restLoading,
          historyHasMore: restHasMore,
          historyCursor: restCursor,
          routeSessions: restRoutes,
        }
      })
    } catch {
      // ignore
    }
  },

  setActiveSession: (id: string) => {
    set({ activeSessionId: id })
    // If no messages cached for this session, load history.
    const state = useChatStore.getState()
    const existing = state.messages[id]
    if (!existing || existing.length === 0) {
      state.loadHistory(id)
    }
  },

  loadHistory: async (sessionId: string) => {
    const state = useChatStore.getState()
    if (state.streamingSessions[sessionId]) {
      return
    }
    set((s) => ({
      historyLoading: { ...s.historyLoading, [sessionId]: true },
    }))
    try {
      const data = await fetchSessionHistory(sessionId, undefined, PAGE_SIZE)
      const msgs: ChatMessage[] = data.messages.map((hm: SessionHistoryMessage) => ({
        id: hm.id,
        role: hm.role as 'user' | 'assistant',
        segments: hm.segments.map(convertHistorySegment),
        timestamp: hm.timestamp,
      }))
      set((s) => {
        // Don't overwrite messages that were added by a racing send().
        const current = s.messages[sessionId]
        if (current && current.length > 0 && s.streamingSessions[sessionId]) {
          return {}
        }
        const updatedSessions = s.sessions.map((sess) => {
          if (sess.id === sessionId) {
            return {
              ...sess,
              ctxwin_used: data.ctxwin_used !== undefined ? data.ctxwin_used : sess.ctxwin_used,
              ctxwin_limit: data.ctxwin_limit !== undefined ? data.ctxwin_limit : sess.ctxwin_limit,
            }
          }
          return sess
        })
        return {
          sessions: updatedSessions,
          messages: { ...s.messages, [sessionId]: msgs },
          historyHasMore: { ...s.historyHasMore, [sessionId]: data.has_more || false },
          historyCursor: { ...s.historyCursor, [sessionId]: data.cursor || null },
        }
      })
    } catch {
      // Timeline may not exist yet for new sessions; that's fine.
      set((s) => {
        // Don't clear messages that were added by a racing send().
        const current = s.messages[sessionId]
        if (current && current.length > 0 && s.streamingSessions[sessionId]) {
          return {}
        }
        return {
          messages: { ...s.messages, [sessionId]: [] },
          historyHasMore: { ...s.historyHasMore, [sessionId]: false },
          historyCursor: { ...s.historyCursor, [sessionId]: null },
        }
      })
    } finally {
      set((s) => ({
        historyLoading: { ...s.historyLoading, [sessionId]: false },
      }))
    }
  },

  loadMoreHistory: async (sessionId: string) => {
    const cursor = useChatStore.getState().historyCursor[sessionId]
    if (!cursor) return // no more to load

    set((s) => ({
      historyLoading: { ...s.historyLoading, [sessionId]: true },
    }))
    try {
      const data = await fetchSessionHistory(sessionId, cursor, PAGE_SIZE)
      const olderMsgs: ChatMessage[] = data.messages.map((hm: SessionHistoryMessage) => ({
        id: hm.id,
        role: hm.role as 'user' | 'assistant',
        segments: hm.segments.map(convertHistorySegment),
        timestamp: hm.timestamp,
      }))
      set((s) => {
        const current = s.messages[sessionId] || []
        const updatedSessions = s.sessions.map((sess) => {
          if (sess.id === sessionId) {
            return {
              ...sess,
              ctxwin_used: data.ctxwin_used !== undefined ? data.ctxwin_used : sess.ctxwin_used,
              ctxwin_limit: data.ctxwin_limit !== undefined ? data.ctxwin_limit : sess.ctxwin_limit,
            }
          }
          return sess
        })
        return {
          sessions: updatedSessions,
          messages: { ...s.messages, [sessionId]: [...olderMsgs, ...current] },
          historyHasMore: { ...s.historyHasMore, [sessionId]: data.has_more || false },
          historyCursor: { ...s.historyCursor, [sessionId]: data.cursor || null },
        }
      })
    } catch {
      // If the request fails, keep the current cursor so the user can retry
    } finally {
      set((s) => ({
        historyLoading: { ...s.historyLoading, [sessionId]: false },
      }))
    }
  },

  renameSession: (id: string, name: string) => {
    set((s) => ({
      sessions: s.sessions.map((sess) => (sess.id === id ? { ...sess, name } : sess)),
    }))
  },

  updateSessionPlans: (id: string, plans: string[]) => {
    set((s) => ({
      sessions: s.sessions.map((sess) => (sess.id === id ? { ...sess, plans } : sess)),
    }))
  },

  markTitleGenerated: (id: string) => {
    set((s) => ({
      titleGenerated: { ...s.titleGenerated, [id]: true },
    }))
  },

  addMessage: (sessionId: string, message: ChatMessage) => {
    set((s) => {
      const msgs = s.messages[sessionId] || []
      return {
        messages: {
          ...s.messages,
          [sessionId]: [...msgs, message],
        },
      }
    })
  },

  updateLastAssistantSegment: (sessionId: string, segment: ChatSegment) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const updated = { ...last, segments: [...last.segments, segment] }
      return { messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), updated] } }
    })
  },

  appendToLastAssistantContent: (sessionId: string, text: string) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') {
        // Diagnostic: log when content is silently dropped. If this fires,
        // it means the last message is not an assistant — likely due to a
        // WebSocket reconnect clearing the handler or loadHistory overwrite.
        if (text && text.trim()) {
          const lastRole = last ? last.role : 'none'
          console.warn(
            `[chatStore] Dropping content delta (${text.length} chars) for session ${sid}: ` +
            `lastMessageRole=${lastRole}, msgCount=${msgs.length}`
          )
        }
        return s
      }
      const segs = [...last.segments]
      const lastSeg = segs[segs.length - 1]
      if (lastSeg && lastSeg.type === 'content') {
        segs[segs.length - 1] = { ...lastSeg, text: lastSeg.text + text }
      } else {
        segs.push({ type: 'content', text })
      }
      // Build the new per-session messages list without spreading the
      // previous array; .concat creates one new array containing all
      // existing references plus the freshly-cloned last message. The
      // earlier `messages: [...msgs.slice(0, -1), { ...last, ... }]`
      // pattern also created one new array but allocated an intermediate
      // slice; .concat is more direct.
      return {
        messages: {
          ...s.messages,
          [sid]: msgs.slice(0, -1).concat({ ...last, segments: segs }),
        },
      }
    })
  },

  appendToLastAssistantThinking: (sessionId: string, text: string) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') {
        if (text && text.trim()) {
          const lastRole = last ? last.role : 'none'
          console.warn(
            `[chatStore] Dropping thinking delta (${text.length} chars) for session ${sid}: ` +
            `lastMessageRole=${lastRole}, msgCount=${msgs.length}`
          )
        }
        return s
      }
      const segs = [...last.segments]
      const lastSeg = segs[segs.length - 1]
      if (lastSeg && lastSeg.type === 'thinking') {
        segs[segs.length - 1] = { ...lastSeg, text: lastSeg.text + text }
      } else {
        segs.push({ type: 'thinking', text })
      }
      return {
        messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] },
      }
    })
  },

  appendToLastAssistantCompact: (sessionId: string, text: string) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const segs = [...last.segments]
      const lastSeg = segs[segs.length - 1]
      if (lastSeg && lastSeg.type === 'compact') {
        segs[segs.length - 1] = { ...lastSeg, text: lastSeg.text + text }
      } else {
        segs.push({ type: 'compact', text })
      }
      return {
        messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] },
      }
    })
  },

  updateToolCallResult: (sessionId: string, callId: string, result: string, error?: string, durationMs?: number) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]

      let found = false
      const updated = [...msgs]
      for (let i = updated.length - 1; i >= 0; i--) {
        const msg = updated[i]
        if (msg.role !== 'assistant') continue

        let segFound = false
        const segs = msg.segments.map((seg) => {
          if (seg.type === 'tool_call' && seg.callId === callId) {
            segFound = true
            found = true
            return { ...seg, result, error, durationMs, done: true }
          }
          return seg
        })

        if (segFound) {
          updated[i] = { ...msg, segments: segs }
          break
        }
      }

      if (!found) return s
      return {
        messages: { ...s.messages, [sid]: updated },
      }
    })
  },

  setStreaming: (v: boolean, sessionId?: string | null) =>
    set((s) => {
      const id = sessionId || s.activeSessionId
      if (!id) return s
      return {
        streamingSessions: {
          ...s.streamingSessions,
          [id]: v,
        },
      }
    }),
  setSystemCommandRunning: (v: boolean, sessionId?: string | null) =>
    set((s) => {
      const id = sessionId || s.activeSessionId
      if (!id) return s
      return {
        systemCommandSessions: {
          ...s.systemCommandSessions,
          [id]: v,
        },
      }
    }),
  setDelegating: (v: boolean, sessionId?: string | null) =>
    set((s) => {
      const id = sessionId || s.activeSessionId
      if (!id) return s
      return {
        delegatingSessions: {
          ...s.delegatingSessions,
          [id]: v,
        },
      }
    }),
  setRoute: (route: ChatRouteInfo) =>
    set((s) => ({
      routeSessions: {
        ...s.routeSessions,
        [route.sessionId]: route,
      },
      sessions: route.agentInstanceId
        ? s.sessions.map((session) =>
            session.id === route.sessionId && session.agent_instance_id !== route.agentInstanceId
              ? { ...session, agent_instance_id: route.agentInstanceId }
              : session
          )
        : s.sessions,
    })),
  clearRoute: (sessionId: string, requestId: string) =>
    set((s) => {
      if (s.routeSessions[sessionId]?.requestId !== requestId) return s
      const { [sessionId]: _route, ...routeSessions } = s.routeSessions
      return { routeSessions }
    }),

  removeLastEmptyAssistantMessage: (sessionId: string) => {
    set((s) => {
      const sid = sessionId
      const msgs = s.messages[sid] || []
      if (msgs.length === 0) return s
      const last = msgs[msgs.length - 1]
      if (last.role !== 'assistant' || last.segments.length > 0) return s
      return {
        messages: { ...s.messages, [sid]: msgs.slice(0, -1) },
      }
    })
  },

  addDelegationSegment: (sessionId: string, delegation: { agentName: string; task: string }) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const seg: ChatSegment = {
        type: 'delegation',
        agentName: delegation.agentName,
        task: delegation.task,
        status: 'running',
      }
      return {
        messages: {
          ...s.messages,
          [sid]: [...msgs.slice(0, -1), { ...last, segments: [...last.segments, seg] }],
        },
      }
    })
  },

  completeLastDelegation: (sessionId: string, agentName: string, durationMs?: number, resultContent?: string) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s

      const normalize = (n: string) => n.toLowerCase().replace(/[\s_]/g, '')
      const target = normalize(agentName)

      const segs = last.segments.map((seg) => {
        if (seg.type !== 'delegation' || seg.status !== 'running') return seg
        if (target && normalize(seg.agentName) === target) {
          return { ...seg, status: 'completed' as const, durationMs, resultContent }
        }
        return seg
      })

      // Fallback: if no exact match, complete the last running delegation.
      let lastRunningIdx = -1
      for (let i = segs.length - 1; i >= 0; i--) {
        const seg = segs[i]
        if (seg.type === 'delegation' && seg.status === 'running') {
          lastRunningIdx = i
          break
        }
      }
      if (lastRunningIdx >= 0) {
        const seg = segs[lastRunningIdx]
        if (seg.type === 'delegation') {
          segs[lastRunningIdx] = {
            ...seg,
            status: 'completed' as const,
            durationMs,
            resultContent,
          }
        }
      }

      return {
        messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] },
      }
    })
  },

  resolveToolConfirm: (sessionId: string, callId: string, choice: string) => {
    set((s) => {
      const sid = sessionId
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const segs = last.segments.map((seg) => {
        if (seg.type === 'tool_confirm' && seg.callId === callId) {
          return { ...seg, resolved: true, choice }
        }
        return seg
      })
      return {
        messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] },
      }
    })
  },

  rewindSession: async (sessionId: string, targetTs: string) => {
    try {
      await apiRewindSession(sessionId, targetTs)
      set((s) => {
        const msgs = s.messages[sessionId] || []
        const filtered = msgs.filter((m) => {
          if (!m.timestamp) return true
          return m.timestamp < targetTs
        })
        return {
          messages: { ...s.messages, [sessionId]: filtered },
        }
      })
    } catch (e) {
      console.error('Failed to rewind session', e)
      throw e
    }
  },

  deleteSessionMessages: async (sessionId: string, targetTsList: string[]) => {
    try {
      // Expand targetTsList to include paired assistant messages
      const msgs = useChatStore.getState().messages[sessionId] || []
      const expandedSet = new Set(targetTsList)
      
      targetTsList.forEach(ts => {
        const idx = msgs.findIndex((m: ChatMessage) => m.timestamp === ts)
        if (idx !== -1 && msgs[idx].role === 'user') {
          // Find subsequent assistant messages until the next user message
          for (let i = idx + 1; i < msgs.length; i++) {
            if (msgs[i].role === 'user') break
            if (msgs[i].timestamp) expandedSet.add(msgs[i].timestamp)
          }
        }
      })
      
      const expandedTsList = Array.from(expandedSet)

      await apiDeleteSessionMessages(sessionId, expandedTsList)
      set((s) => {
        const sMsgs = s.messages[sessionId] || []
        const filtered = sMsgs.filter((m: ChatMessage) => {
          if (!m.timestamp) return true
          return !expandedSet.has(m.timestamp)
        })
        return {
          messages: { ...s.messages, [sessionId]: filtered },
        }
      })
    } catch (e) {
      console.error('Failed to delete messages', e)
      throw e
    }
  },
}))

// convertHistorySegment maps backend history segment format to frontend ChatSegment.
function convertHistorySegment(seg: SessionHistorySegment): ChatSegment {
  switch (seg.type) {
    case 'content':
      return { type: 'content', text: seg.text || '' }
    case 'thinking':
      return { type: 'thinking', text: seg.text || '' }
    case 'compact':
      return { type: 'compact', text: seg.text || '' }
    case 'tool_call':
      return {
        type: 'tool_call',
        callId: seg.call_id || '',
        name: seg.name || '',
        args: seg.args || '',
        result: seg.result,
        error: seg.error,
        durationMs: seg.duration_ms,
        done: seg.done ?? true,
      }
    case 'delegation':
      return {
        type: 'delegation',
        agentName: seg.agent_name || seg.name || '',
        task: seg.task || '',
        status: (seg.status as 'running' | 'completed' | 'failed') || 'completed',
        durationMs: seg.duration_ms,
        resultContent: seg.result,
      }
    case 'tool_confirm':
      return {
        type: 'tool_confirm',
        callId: seg.call_id || '',
        name: seg.name || '',
        prompt: seg.prompt || '',
        allowInSession: seg.allow_in_session ?? false,
        resolved: seg.resolved ?? true,
        choice: seg.choice,
      }
    case 'error':
      return { type: 'error', text: seg.text || '' }
    default:
      return { type: 'error', text: 'Unknown segment type' }
  }
}
