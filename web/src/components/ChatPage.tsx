import { useEffect, useRef, useState, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { ChatMessageView } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { useChatStore } from '@/stores/chatStore'
import { useChatStream } from '@/hooks/useChatStream'
import { useAgentStream } from '@/hooks/useAgentStream'
import { Sparkles, PanelRight } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { AgentListPage } from '@/components/AgentListPage'
import { cn } from '@/lib/utils'
import { L2SessionStatusPanel } from '@/components/L2SessionStatusPanel'

export function ChatPage() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const {
    activeSessionId,
    messages,
    streaming,
    sessions,
    historyHasMore,
    loadMoreHistory,
    loadSessions,
    setActiveSession,
    loadHistory,
  } = useChatStore()
  const { send, cancel } = useChatStream()
  const scrollRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const userScrolledUp = useRef(false)
  const loadingMore = useRef(false)

  // Agent monitoring integration
  const [showMonitor, setShowMonitor] = useState(true)
  const agentsData = useAgentStore((state) => state.agents)
  const teamsData = useAgentStore((state) => state.teams)
  const fetchLiveAgents = useAgentStore((state) => state.fetchLiveAgents)
  const fetchTeams = useAgentStore((state) => state.fetchTeams)

  useEffect(() => {
    fetchLiveAgents()
    fetchTeams()
  }, [fetchLiveAgents, fetchTeams])

  // Load sessions on mount and when streaming completes
  useEffect(() => {
    loadSessions()
  }, [loadSessions, streaming])

  // Sync activeSessionId with the URL parameter sessionId
  useEffect(() => {
    if (sessionId) {
      if (sessionId !== activeSessionId) {
        setActiveSession(sessionId)
      }
    } else if (activeSessionId) {
      navigate(`/chat/${activeSessionId}`, { replace: true })
    } else {
      // Default to l1 session if none is active
      navigate('/chat/l1', { replace: true })
    }
  }, [sessionId, activeSessionId, setActiveSession, navigate])

  const currentMessages = messages[activeSessionId || ''] || []
  const noSession = !activeSessionId
  const activeSession = sessions.find((s) => s.id === activeSessionId)

  const activeGroup = activeSession?.group
  const isL1Session = activeSessionId === 'l1'

  const groupAgents = useMemo(() => {
    if (isL1Session) {
      // Find L1 agent in live agents
      let l1 = null
      if (agentsData) {
        const { agents, supervisors } = agentsData
        const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
        const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))
        l1 = agents.find((a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id))
      }

      return [
        l1 || {
          id: 'main',
          instance_id: '',
          name: 'L1 Agent',
          state: 'stopped' as const,
          model_id: 'Expert Model',
          group: 'L1',
          is_leader: true,
          task_level: '',
          error_count: 0,
          last_error: '',
          pending_delegations: 0,
          mailbox_high: 0,
          mailbox_normal: 0,
        },
      ]
    }

    if (!activeGroup) return []

    // Get all templates for this group (case-insensitive match)
    const team = teamsData?.teams.find((t) => t.name.toLowerCase() === activeGroup.toLowerCase())
    if (!team) {
      // Fallback: if no team metadata loaded yet, just return live agents in this group (case-insensitive)
      return agentsData
        ? agentsData.agents.filter((a) => a.group?.toLowerCase() === activeGroup.toLowerCase())
        : []
    }

    // Map each template to live instance or static placeholder
    return team.agents.map((tmpl) => {
      const live = agentsData?.agents.find((a) => a.id === tmpl.id)
      return (
        live || {
          id: tmpl.id,
          instance_id: '',
          name: tmpl.name,
          state: 'stopped' as const,
          model_id: tmpl.model_id,
          group: activeGroup,
          is_leader: tmpl.is_leader,
          task_level: '',
          error_count: 0,
          last_error: '',
          pending_delegations: 0,
          mailbox_high: 0,
          mailbox_normal: 0,
        }
      )
    })
  }, [agentsData, teamsData, activeGroup, isL1Session])

  // Auto-expand monitor if any agent in the group is processing/running
  const anyRunning = useMemo(() => {
    return groupAgents.some((a) => a.state === 'processing')
  }, [groupAgents])

  useEffect(() => {
    if (anyRunning) {
      setShowMonitor(true)
    }
  }, [anyRunning])

  const l1Agent = isL1Session ? groupAgents[0] : null
  const l1AgentState = l1Agent?.state
  const l1AgentInstanceId = l1Agent?.instance_id || null
  const stream = useAgentStream(l1AgentInstanceId)

  // Keep L1 session history in sync when L1 agent state changes
  useEffect(() => {
    if (isL1Session && l1AgentState) {
      loadHistory('l1')
    }
  }, [isL1Session, l1AgentState, loadHistory])

  const streamChatSegments = useMemo(() => {
    if (!stream?.segments) return []
    return stream.segments.map((seg) => {
      if (seg.type === 'tool_call') {
        return {
          type: 'tool_call' as const,
          callId: seg.call_id,
          name: seg.name,
          args: seg.args,
          result: seg.result || undefined,
          error: seg.error || undefined,
          durationMs: seg.duration_ms || undefined,
          done: seg.done,
        }
      }
      return seg
    })
  }, [stream])

  const finalMessages = useMemo(() => {
    if (
      isL1Session &&
      l1AgentState === 'processing' &&
      !streaming &&
      streamChatSegments.length > 0
    ) {
      const virtualMessage = {
        id: `msg-virtual-stream`,
        role: 'assistant' as const,
        segments: streamChatSegments,
        timestamp: new Date().toISOString(),
      }
      return [...currentMessages, virtualMessage]
    }
    return currentMessages
  }, [currentMessages, isL1Session, l1AgentState, streaming, streamChatSegments])

  // Content checksum: changes on every text append within any segment (captures streaming content updates)
  const contentSum = finalMessages.reduce((acc, msg) => {
    let sum = 0
    for (const seg of msg.segments) {
      if ('text' in seg && typeof (seg as any).text === 'string') {
        sum += (seg as any).text.length
      }
    }
    return acc + sum + msg.segments.length
  }, 0)

  // Track scroll position: detect when user manually scrolls up.
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return

    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 100
      userScrolledUp.current = !isNearBottom

      // Load more history when scrolling near top
      if (scrollTop < 50 && !loadingMore.current) {
        const sid = activeSessionId
        if (sid && historyHasMore[sid]) {
          loadingMore.current = true
          loadMoreHistory(sid)
          setTimeout(() => {
            loadingMore.current = false
          }, 500)
        }
      }
    }

    el.addEventListener('scroll', handleScroll, { passive: true })
    return () => el.removeEventListener('scroll', handleScroll)
  }, [])

  // Auto-scroll to bottom (only when user hasn't scrolled up).
  // Dependencies include contentSum to capture streaming text appends within existing segments.
  useEffect(() => {
    if (!userScrolledUp.current) {
      bottomRef.current?.scrollIntoView({ behavior: 'instant' })
    }
  }, [finalMessages.length, contentSum, streaming])

  if (noSession) {
    return (
      <div className="flex-1 h-full overflow-hidden bg-background">
        <AgentListPage />
      </div>
    )
  }

  return (
    <div className="flex h-full bg-background overflow-hidden w-full">
      {/* Chat area */}
      <div className="flex-1 flex flex-col min-w-0 h-full border-r border-border/30">
        {/* Header */}
        <div className="shrink-0 flex items-center justify-between px-4 py-2.5 border-b border-border/50 bg-card/30 backdrop-blur-sm">
          <div className="flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-lg bg-violet-500/10 flex items-center justify-center">
              <Sparkles className="h-3.5 w-3.5 text-violet-500" />
            </div>
            <div>
              <h1 className="text-sm font-semibold text-foreground">
                {activeSession ? activeSession.name || l1Agent?.name || 'New session' : 'Chat'}
              </h1>
              {activeSession && activeSession.group && (
                <p className="text-[11px] text-muted-foreground/60">{activeSession.group}</p>
              )}
            </div>
          </div>

          <div className="flex items-center gap-3">
            {streaming && (
              <div className="flex items-center gap-1.5 text-xs text-violet-500/70">
                <span className="inline-block h-1.5 w-1.5 rounded-full bg-violet-500 animate-pulse" />
                Generating
              </div>
            )}

            {activeSession && (
              <button
                onClick={() => setShowMonitor(!showMonitor)}
                className={cn(
                  'p-1.5 rounded-lg border transition-all cursor-pointer',
                  showMonitor
                    ? 'bg-primary/10 text-primary border-primary/20'
                    : 'bg-muted/40 text-muted-foreground hover:text-foreground border-border/80'
                )}
                title={showMonitor ? 'Hide Agent Monitor' : 'Show Agent Monitor'}
              >
                <PanelRight className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        </div>

        {/* Messages */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto">
          {noSession ? (
            <div className="flex flex-col items-center justify-center h-full gap-4 text-muted-foreground">
              <div className="h-16 w-16 rounded-2xl bg-muted/50 flex items-center justify-center">
                <Sparkles className="h-8 w-8 text-muted-foreground/30" />
              </div>
              <div className="text-center">
                <p className="text-sm font-medium text-muted-foreground/70">No session selected</p>
                <p className="text-xs text-muted-foreground/40 mt-1">
                  Choose a session from the sidebar or create a new one
                </p>
              </div>
            </div>
          ) : finalMessages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full gap-4 text-muted-foreground">
              <div className="h-16 w-16 rounded-2xl bg-violet-500/5 flex items-center justify-center ring-1 ring-violet-500/10">
                <Sparkles className="h-8 w-8 text-violet-500/30" />
              </div>
              <div className="text-center max-w-sm">
                <p className="text-sm font-medium text-foreground/80 mb-1">
                  {activeSession?.type === 'l1'
                    ? 'Ask L1 to coordinate complex tasks'
                    : `Start a new conversation with ${activeSession?.group || 'this agent'}`}
                </p>
                <p className="text-xs text-muted-foreground/40">
                  The agent can browse files, edit code, run commands, and delegate work.
                </p>
              </div>
            </div>
          ) : (
            <div>
              {finalMessages.map((msg) => (
                <ChatMessageView key={msg.id} message={msg} agentName={activeSession?.agent_name} />
              ))}
            </div>
          )}
          <div ref={bottomRef} />
        </div>

        {/* Input */}
        <ChatInput
          onSend={(text, files) => send(text, files)}
          onCancel={cancel}
          streaming={streaming}
          disabled={noSession}
          activeSessionId={activeSessionId || undefined}
        />
      </div>

      {/* Agent Monitor / Status panel */}
      {showMonitor && activeSession && (
        <div className="w-[320px] md:w-[380px] shrink-0 h-full border-l border-border bg-card/10 backdrop-blur-sm animate-in slide-in-from-right duration-200">
          <L2SessionStatusPanel
            session={activeSession}
            activeAgent={
              isL1Session
                ? groupAgents[0] || null
                : agentsData?.agents.find(
                    (a) => a.instance_id === activeSession.agent_instance_id
                  ) || null
            }
          />
        </div>
      )}
    </div>
  )
}
