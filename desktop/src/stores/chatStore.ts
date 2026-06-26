import { create } from 'zustand'
import type { ChatSession, ChatMessage, ChatSegment } from '@/types'
import { listSessions, createL2Session, deleteL2Session, fetchSessionHistory } from '@/lib/api'
import type { SessionHistoryMessage, SessionHistorySegment } from '@/types'

interface ChatState {
  sessions: ChatSession[]
  activeSessionId: string | null
  messages: Record<string, ChatMessage[]> // keyed by session id
  streaming: boolean
  delegating: boolean // true when async delegation is in progress (L1 waiting for L2)
  titleGenerated: Record<string, boolean> // track which sessions already had title generated
  historyLoading: Record<string, boolean> // track which sessions are loading history
  historyHasMore: Record<string, boolean> // track which sessions have more history to load
  historyCursor: Record<string, string | null> // cursor for next load-more page

  loadSessions: () => Promise<void>
  createL2Session: (group: string, workDir?: string) => Promise<string | null>
  deleteL2Session: (id: string) => Promise<void>
  setActiveSession: (id: string) => void
  loadHistory: (sessionId: string) => Promise<void>
  loadMoreHistory: (sessionId: string) => Promise<void>
  renameSession: (id: string, name: string) => void
  markTitleGenerated: (id: string) => void

  addMessage: (message: ChatMessage) => void
  updateLastAssistantSegment: (segment: ChatSegment) => void
  appendToLastAssistantContent: (text: string) => void
  appendToLastAssistantThinking: (text: string) => void
  updateToolCallResult: (
    callId: string,
    result: string,
    error?: string,
    durationMs?: number
  ) => void
  setStreaming: (v: boolean) => void
  setDelegating: (v: boolean) => void
  removeLastEmptyAssistantMessage: () => void
  addDelegationSegment: (delegation: { agentName: string; task: string }) => void
  completeLastDelegation: (agentName: string, durationMs?: number, resultContent?: string) => void
  resolveToolConfirm: (callId: string, choice: string) => void
}

const PAGE_SIZE = 30 // number of messages to load per page

export const useChatStore = create<ChatState>((set) => ({
  sessions: [],
  activeSessionId: null,
  messages: {},
  streaming: false,
  delegating: false,
  titleGenerated: {},
  historyLoading: {},
  historyHasMore: {},
  historyCursor: {},

  loadSessions: async () => {
    try {
      const data = await listSessions()
      const mapped = (data.sessions || []).map((s: any) => ({
        ...s,
        createdAt: s.createdAt || s.created_at || new Date().toISOString(),
      }))
      set({ sessions: mapped })
    } catch {
      // Server may not be running; sessions remain empty.
    }
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
        return {
          sessions: s.sessions.filter((sess) => sess.id !== id),
          activeSessionId: s.activeSessionId === id ? null : s.activeSessionId,
          messages: restMessages,
          titleGenerated: restTitle,
          historyLoading: restLoading,
          historyHasMore: restHasMore,
          historyCursor: restCursor,
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
      set((s) => ({
        messages: { ...s.messages, [sessionId]: msgs },
        historyHasMore: { ...s.historyHasMore, [sessionId]: data.has_more || false },
        historyCursor: { ...s.historyCursor, [sessionId]: data.cursor || null },
      }))
    } catch {
      // Timeline may not exist yet for new sessions; that's fine.
      set((s) => ({
        messages: { ...s.messages, [sessionId]: [] },
        historyHasMore: { ...s.historyHasMore, [sessionId]: false },
        historyCursor: { ...s.historyCursor, [sessionId]: null },
      }))
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
        return {
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

  markTitleGenerated: (id: string) => {
    set((s) => ({
      titleGenerated: { ...s.titleGenerated, [id]: true },
    }))
  },

  addMessage: (message: ChatMessage) => {
    set((s) => {
      const msgs = s.messages[s.activeSessionId || ''] || []
      return {
        messages: {
          ...s.messages,
          [s.activeSessionId || '']: [...msgs, message],
        },
      }
    })
  },

  updateLastAssistantSegment: (segment: ChatSegment) => {
    set((s) => {
      const sid = s.activeSessionId || ''
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const updated = { ...last, segments: [...last.segments, segment] }
      return { messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), updated] } }
    })
  },

  appendToLastAssistantContent: (text: string) => {
    set((s) => {
      const sid = s.activeSessionId || ''
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const segs = [...last.segments]
      const lastSeg = segs[segs.length - 1]
      if (lastSeg && lastSeg.type === 'content') {
        segs[segs.length - 1] = { ...lastSeg, text: lastSeg.text + text }
      } else {
        segs.push({ type: 'content', text })
      }
      return {
        messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] },
      }
    })
  },

  appendToLastAssistantThinking: (text: string) => {
    set((s) => {
      const sid = s.activeSessionId || ''
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
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

  updateToolCallResult: (callId: string, result: string, error?: string, durationMs?: number) => {
    set((s) => {
      const sid = s.activeSessionId || ''
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

  setStreaming: (v: boolean) => set({ streaming: v }),
  setDelegating: (v: boolean) => set({ delegating: v }),

  removeLastEmptyAssistantMessage: () => {
    set((s) => {
      const sid = s.activeSessionId || ''
      const msgs = s.messages[sid] || []
      if (msgs.length === 0) return s
      const last = msgs[msgs.length - 1]
      if (last.role !== 'assistant' || last.segments.length > 0) return s
      return {
        messages: { ...s.messages, [sid]: msgs.slice(0, -1) },
      }
    })
  },

  addDelegationSegment: (delegation: { agentName: string; task: string }) => {
    set((s) => {
      const sid = s.activeSessionId || ''
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

  completeLastDelegation: (agentName: string, durationMs?: number, resultContent?: string) => {
    set((s) => {
      const sid = s.activeSessionId || ''
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

  resolveToolConfirm: (callId: string, choice: string) => {
    set((s) => {
      const sid = s.activeSessionId || ''
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
}))

// convertHistorySegment maps backend history segment format to frontend ChatSegment.
function convertHistorySegment(seg: SessionHistorySegment): ChatSegment {
  switch (seg.type) {
    case 'content':
      return { type: 'content', text: seg.text || '' }
    case 'thinking':
      return { type: 'thinking', text: seg.text || '' }
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
