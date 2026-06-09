import { create } from 'zustand'
import type { ChatSession, ChatMessage, ChatSegment } from '@/types'
import { listSessions, createL2Session, deleteL2Session } from '@/lib/api'

interface ChatState {
  sessions: ChatSession[]
  activeSessionId: string | null
  messages: Record<string, ChatMessage[]>  // keyed by session id
  streaming: boolean
  titleGenerated: Record<string, boolean>  // track which sessions already had title generated

  loadSessions: () => Promise<void>
  createL2Session: (group: string, workDir?: string) => Promise<string | null>
  deleteL2Session: (id: string) => Promise<void>
  setActiveSession: (id: string) => void
  renameSession: (id: string, name: string) => void
  markTitleGenerated: (id: string) => void

  addMessage: (message: ChatMessage) => void
  updateLastAssistantSegment: (segment: ChatSegment) => void
  appendToLastAssistantContent: (text: string) => void
  appendToLastAssistantThinking: (text: string) => void
  updateToolCallResult: (callId: string, result: string, error?: string, durationMs?: number) => void
  setStreaming: (v: boolean) => void
}

export const useChatStore = create<ChatState>((set) => ({
  sessions: [],
  activeSessionId: null,
  messages: {},
  streaming: false,
  titleGenerated: {},

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
}))
