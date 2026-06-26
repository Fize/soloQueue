import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { ChatMessageView } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { Loader2, Sparkles } from 'lucide-react'
import { wsManager } from '@/lib/websocket'
import { fetchSessionHistory } from '@/lib/api'
import { useAgentStore } from '@/stores/agentStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import type { ChatHandler } from '@/lib/websocket'
import type { ChatMessage, ChatSegment, SessionHistoryMessage, SessionHistorySegment } from '@/types'

// ─── Helpers ────────────────────────────────────────────────────────────────

function generateRequestId(): string {
  return `req-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

// ─── Helpers ────────────────────────────────────────────────────────────────

function toChatMessage(hm: SessionHistoryMessage): ChatMessage {
  return {
    id: hm.id,
    role: hm.role as 'user' | 'assistant',
    segments: hm.segments.map(toChatSegment),
    timestamp: hm.timestamp,
  }
}

function toChatSegment(seg: SessionHistorySegment): ChatSegment {
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

// ─── Component ──────────────────────────────────────────────────────────────

const HISTORY_PAGE_SIZE = 30

export function AssistantPage() {
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [streaming, setStreaming] = useState(false)
  const [historyHasMore, setHistoryHasMore] = useState(false)
  const [historyCursor, setHistoryCursor] = useState<string | null>(null)
  const [historyLoadingMore, setHistoryLoadingMore] = useState(false)
  const activeRequestRef = useRef<string | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const loadingMoreRef = useRef(false)
  const userScrolledUpRef = useRef(false)

  // Agent name from runtime data (same source as ChatPage)
  const agentsData = useAgentStore((state) => state.agents)
  const runtimeStatus = useRuntimeStore((state) => state.status)

  const agentName = useMemo(() => {
    const l1 = agentsData?.agents.find((a) => a.id === 'l1-agent')
    return l1?.name || 'L1 Agent'
  }, [agentsData])

  // Context window from runtime (broadcast every 3s from backend CW().TokenUsage())
  const ctxwinUsed = runtimeStatus?.current_tokens ?? 0
  const ctxwinLimit = runtimeStatus?.max_tokens ?? 0

  // ── Load history on mount ──────────────────────────────────────────────────
  const [historyLoading, setHistoryLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setHistoryLoading(true)

    fetchSessionHistory('l1', undefined, HISTORY_PAGE_SIZE)
      .then((data) => {
        if (cancelled) return
        const msgs: ChatMessage[] = data.messages.map(toChatMessage)
        setMessages(msgs)
        setHistoryHasMore(data.has_more || false)
        setHistoryCursor(data.cursor || null)
      })
      .catch(() => {
        // Timeline may not exist yet for a fresh session; that's fine.
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [])

  // ── Load more history when scrolling up ───────────────────────────────────
  const loadMoreHistory = useCallback(async () => {
    if (!historyCursor || historyLoadingMore) return
    setHistoryLoadingMore(true)
    try {
      const data = await fetchSessionHistory('l1', historyCursor, HISTORY_PAGE_SIZE)
      const olderMsgs: ChatMessage[] = data.messages.map(toChatMessage)
      setMessages((prev) => [...olderMsgs, ...prev])
      setHistoryHasMore(data.has_more || false)
      setHistoryCursor(data.cursor || null)
    } catch {
      // keep cursor so user can retry
    } finally {
      setHistoryLoadingMore(false)
    }
  }, [historyCursor, historyLoadingMore])

  // Scroll-up detection → load more history
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 100
      userScrolledUpRef.current = !isNearBottom

      if (scrollTop < 50 && historyHasMore && !historyLoadingMore && !loadingMoreRef.current) {
        loadingMoreRef.current = true
        const prevHeight = scrollHeight
        loadMoreHistory().then(() => {
          if (scrollRef.current) {
            const diff = scrollRef.current.scrollHeight - prevHeight
            scrollRef.current.scrollTop = diff
          }
          loadingMoreRef.current = false
        })
      }
    }
    el.addEventListener('scroll', handleScroll)
    return () => el.removeEventListener('scroll', handleScroll)
  }, [historyHasMore, historyLoadingMore, loadMoreHistory])

  // Auto-scroll to bottom — instant for history loads, smooth for live streaming
  useEffect(() => {
    if (userScrolledUpRef.current) return
    bottomRef.current?.scrollIntoView({ behavior: streaming ? 'smooth' : 'auto' })
  }, [messages, streaming])

  // ── Send ──────────────────────────────────────────────────────────────────

  const handleSend = useCallback(
    (text: string, files?: { name: string; path: string }[], _group?: string, _projectPath?: string) => {
      if (!text.trim() || streaming) return

      const requestId = generateRequestId()
      activeRequestRef.current = requestId

      const msgId = `asst-msg-${Date.now()}`
      const asstId = `asst-msg-${Date.now() + 1}`

      // Add user message
      setMessages((prev) => [
        ...prev,
        {
          id: msgId,
          role: 'user',
          segments: [{ type: 'content' as const, text }],
          timestamp: new Date().toISOString(),
          files,
        },
      ])

      // Add empty assistant placeholder
      setMessages((prev) => [
        ...prev,
        {
          id: asstId,
          role: 'assistant',
          segments: [],
          timestamp: new Date().toISOString(),
        },
      ])

      setStreaming(true)

      // Accumulators (mutable locals avoid stale closures)
      let currentContent = ''
      let currentThinking = ''

      const buildSegments = (): ChatSegment[] => {
        const segs: ChatSegment[] = []
        if (currentThinking) segs.push({ type: 'thinking' as const, text: currentThinking })
        if (currentContent) segs.push({ type: 'content' as const, text: currentContent })
        return segs
      }

      const finish = () => {
        setStreaming(false)
        activeRequestRef.current = null
        wsManager.unregisterChat(requestId)
      }

      const handler: ChatHandler = {
        onChunk: (delta: string) => {
          currentContent += delta
          const segs = buildSegments()
          setMessages((prev) => {
            const msgs = [...prev]
            const last = msgs[msgs.length - 1]
            if (last?.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, segments: segs }
            }
            return msgs
          })
        },
        onReasoning: (delta: string) => {
          currentThinking += delta
          const segs = buildSegments()
          setMessages((prev) => {
            const msgs = [...prev]
            const last = msgs[msgs.length - 1]
            if (last?.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, segments: segs }
            }
            return msgs
          })
        },
        onDone: () => finish(),
        onError: (error: string) => {
          const segs = buildSegments()
          const finalSegs: ChatSegment[] = segs.length > 0
            ? [...segs, { type: 'error' as const, text: error }]
            : [{ type: 'error' as const, text: error }]
          setMessages((prev) => {
            const msgs = [...prev]
            const last = msgs[msgs.length - 1]
            if (last?.role === 'assistant') {
              msgs[msgs.length - 1] = { ...last, segments: finalSegs }
            }
            return msgs
          })
          finish()
        },
        onClose: () => {
          if (activeRequestRef.current === requestId) finish()
        },
      }

      wsManager.registerChat(requestId, handler)
      wsManager.send({
        type: 'chat_send',
        request_id: requestId,
        session_id: 'l1',
        prompt: text,
        files,
      })
    },
    [streaming]
  )

  // ── Cancel ────────────────────────────────────────────────────────────────

  const handleCancel = useCallback(() => {
    const rid = activeRequestRef.current
    if (!rid) return
    wsManager.send({ type: 'chat_cancel', request_id: rid, session_id: 'l1' })
    setStreaming(false)
    activeRequestRef.current = null
    wsManager.unregisterChat(rid)
  }, [])

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      <div className="flex flex-1 flex-col overflow-hidden h-full bg-background relative">
        {/* Header — matches ChatPage header style */}
        <header className="flex h-12 shrink-0 items-center border-b border-border/30 bg-card/20 px-6 select-none">
          <div className="flex flex-1 items-center gap-3">
            <h1 className="text-xs font-bold text-foreground font-mono truncate">{agentName}</h1>
          </div>
          {streaming && (
            <div className="flex items-center gap-1.5 text-[10px] text-violet-500/70">
              <Loader2 className="h-3 w-3 animate-spin" />
              生成中...
            </div>
          )}
        </header>

        {/* Messages — conditional overflow to avoid scrollbar when empty */}
        <div
          ref={scrollRef}
          className={messages.length > 0 ? 'flex-1 overflow-y-auto' : 'flex-1'}
        >
          {messages.length === 0 && historyLoading ? (
            <div className="flex h-full flex-col items-center justify-center gap-4 px-6 select-none">
              <Loader2 className="h-7 w-7 animate-spin text-violet-500/70" />
              <p className="text-xs text-muted-foreground font-mono">正在载入历史...</p>
            </div>
          ) : messages.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-4 px-6 select-none">
              <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-violet-500/10 border border-violet-500/20">
                <Sparkles className="h-7 w-7 text-violet-500" />
              </div>
              <h2 className="text-lg font-semibold text-foreground/80">{agentName}</h2>
              <p className="max-w-xs text-center text-xs text-muted-foreground">
                向 L1 智能体发送消息，获取即时响应。
              </p>
            </div>
          ) : (
            <div className="mx-auto max-w-3xl">
              {historyLoadingMore && (
                <div className="flex items-center justify-center py-4">
                  <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  <span className="text-xs text-muted-foreground font-mono ml-2">正在载入更多历史...</span>
                </div>
              )}
              {messages.map((msg) => (
                <ChatMessageView key={msg.id} message={msg} agentName={agentName} />
              ))}
            </div>
          )}
          <div ref={bottomRef} className="h-2" />
        </div>

        {/* Input — same ChatInput as ChatPage */}
        <ChatInput
          onSend={handleSend}
          onCancel={handleCancel}
          streaming={streaming}
          delegating={false}
          disabled={streaming}
          showL2Selectors={false}
          ctxwinUsed={ctxwinUsed}
          ctxwinLimit={ctxwinLimit}
        />
      </div>
    </div>
  )
}
