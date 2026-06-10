import { create } from 'zustand'
import type { ChatSession, ChatMessage, ChatSegment } from '@/types'
import { listSessions, createL2Session, deleteL2Session, fetchSessionHistory } from '@/lib/api'
import type { SessionHistoryMessage, SessionHistorySegment } from '@/types'

interface ChatState {
  sessions: ChatSession[]
  activeSessionId: string | null
  messages: Record<string, ChatMessage[]>  // keyed by session id
  streaming: boolean
  titleGenerated: Record<string, boolean>  // track which sessions already had title generated
  historyLoading: Record<string, boolean>  // track which sessions are loading history

  loadSessions: () => Promise<void>
  createL2Session: (group: string, workDir?: string) => Promise<string | null>
  deleteL2Session: (id: string) => Promise<void>
  setActiveSession: (id: string) => void
  loadHistory: (sessionId: string) => Promise<void>
  renameSession: (id: string, name: string) => void
  markTitleGenerated: (id: string) => void

  addMessage: (message: ChatMessage) => void
  updateLastAssistantSegment: (segment: ChatSegment) => void
  appendToLastAssistantContent: (text: string) => void
  appendToLastAssistantThinking: (text: string) => void
  updateToolCallResult: (callId: string, result: string, error?: string, durationMs?: number) => void
  setStreaming: (v: boolean) => void
  removeLastEmptyAssistantMessage: () => void
  addDelegationSegment: (delegation: { agentName: string; task: string }) => void
  completeLastDelegation: (agentName: string, durationMs?: number, resultContent?: string) => void
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: [],
  activeSessionId: null,
  messages: {},
  streaming: false,
  titleGenerated: {},
  historyLoading: {},

  loadSessions: async () => {
    try {
      const data = await listSessions()
      set({ sessions: data.sessions })
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
      set((s) => ({
        sessions: s.sessions.filter((sess) => sess.id !== id),
        activeSessionId: s.activeSessionId === id ? null : s.activeSessionId,
      }))
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
      const data = await fetchSessionHistory(sessionId)
      const msgs: ChatMessage[] = data.messages.map((hm: SessionHistoryMessage) => ({
        id: hm.id,
        role: hm.role as 'user' | 'assistant',
        segments: hm.segments.map(convertHistorySegment),
        timestamp: hm.timestamp,
      }))
      set((s) => ({
        messages: { ...s.messages, [sessionId]: msgs },
      }))
    } catch {
      // Timeline may not exist yet for new sessions; that's fine.
      set((s) => ({
        messages: { ...s.messages, [sessionId]: [] },
      }))
    } finally {
      set((s) => ({
        historyLoading: { ...s.historyLoading, [sessionId]: false },
      }))
    }
  },

  renameSession: (id: string, name: string) => {
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.id === id ? { ...sess, name } : sess
      ),
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
      return { messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] } }
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
      return { messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] } }
    })
  },

  updateToolCallResult: (callId: string, result: string, error?: string, durationMs?: number) => {
    set((s) => {
      const sid = s.activeSessionId || ''
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const segs = last.segments.map((seg) => {
        if (seg.type === 'tool_call' && seg.callId === callId) {
          return { ...seg, result, error, durationMs, done: true }
        }
        return seg
      })
      return { messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: segs }] } }
    })
  },

  setStreaming: (v: boolean) => set({ streaming: v }),

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
        messages: { ...s.messages, [sid]: [...msgs.slice(0, -1), { ...last, segments: [...last.segments, seg] }] },
      }
    })
  },

  completeLastDelegation: (agentName: string, durationMs?: number, resultContent?: string) => {
    set((s) => {
      const sid = s.activeSessionId || ''
      const msgs = [...(s.messages[sid] || [])]
      const last = msgs[msgs.length - 1]
      if (!last || last.role !== 'assistant') return s
      const segs = last.segments.map((seg) => {
        if (seg.type === 'delegation' && seg.agentName === agentName && seg.status === 'running') {
          return { ...seg, status: 'completed' as const, durationMs, resultContent }
        }
        // If no matching running delegation, mark the last running one
        if (seg.type === 'delegation' && seg.status === 'running') {
          return { ...seg, status: 'completed' as const, durationMs, resultContent }
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
    case 'error':
      return { type: 'error', text: seg.text || '' }
    default:
      return { type: 'error', text: 'Unknown segment type' }
  }
}
