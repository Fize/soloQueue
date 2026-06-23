import { useEffect, useRef, useState, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { ChatMessageView } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { useChatStore } from '@/stores/chatStore'
import { useChatStream } from '@/hooks/useChatStream'
import { useAgentStream } from '@/hooks/useAgentStream'
import { Sparkles, PanelRight, Plus, Loader2 } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { AgentListPage } from '@/components/AgentListPage'
import { cn } from '@/lib/utils'
import type { AgentInfo } from '@/types'
import { L2SessionStatusPanel } from '@/components/L2SessionStatusPanel'
import { listL2Groups } from '@/lib/api'

export function ChatPage() {
  const { sessionId } = useParams<{ sessionId: string }>()
  const navigate = useNavigate()
  const {
    activeSessionId,
    messages,
    streaming,
    delegating,
    sessions,
    historyHasMore,
    historyLoading,
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

  // Agent monitoring integration — default collapsed (P2 content)
  const [showMonitor, setShowMonitor] = useState(false)

  // New L2 Session creation
  const [l2Groups, setL2Groups] = useState<string[]>([])
  const [showNewSessionMenu, setShowNewSessionMenu] = useState(false)
  const [creatingGroup, setCreatingGroup] = useState<string | null>(null)
  const newSessionRef = useRef<HTMLDivElement>(null)
  const createL2Session = useChatStore((s) => s.createL2Session)

  // Close dropdown on outside click and Escape key
  useEffect(() => {
    if (!showNewSessionMenu) return
    const handleOutside = (e: MouseEvent) => {
      if (newSessionRef.current && !newSessionRef.current.contains(e.target as Node)) {
        setShowNewSessionMenu(false)
      }
    }
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setShowNewSessionMenu(false)
    }
    // Delay to avoid the same click that opened it
    const timer = setTimeout(() => {
      document.addEventListener('click', handleOutside, { capture: true })
      document.addEventListener('keydown', handleKey)
    }, 0)
    return () => {
      clearTimeout(timer)
      document.removeEventListener('click', handleOutside, { capture: true })
      document.removeEventListener('keydown', handleKey)
    }
  }, [showNewSessionMenu])

  const handleNewL2Session = async (group: string) => {
    setCreatingGroup(group)
    setShowNewSessionMenu(false)
    try {
      const newId = await createL2Session(group)
      if (newId) {
        navigate(`/chat/${newId}`)
      }
    } finally {
      setCreatingGroup(null)
    }
  }

  const toggleNewSessionMenu = async () => {
    if (showNewSessionMenu) {
      setShowNewSessionMenu(false)
      return
    }
    // Fetch groups on demand
    if (l2Groups.length === 0) {
      try {
        const groups = await listL2Groups()
        setL2Groups(groups)
      } catch {
        setL2Groups([])
      }
    }
    setShowNewSessionMenu(true)
  }
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
      // Find L1 agent by its known ID. Using explicit ID match instead of
      // subtraction heuristic because simulation agents and other non-supervised
      // agents are also in the registry and would erroneously match the old filter.
      let l1 = null
      if (agentsData) {
        l1 = agentsData.agents.find((a) => a.id === 'l1-agent')
      }

      const fallback: AgentInfo = {
        id: 'main',
        instance_id: '',
        name: 'L1 Agent',
        state: 'stopped' as const,
        model_id: 'Expert Model',
        provider_id: '',
        group: 'L1',
        is_leader: true,
        task_level: '',
        error_count: 0,
        last_error: '',
        pending_delegations: 0,
        mailbox_high: 0,
        mailbox_normal: 0,
      }
      return [l1 || fallback]
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
      const placeholder: AgentInfo = {
        id: tmpl.id,
        instance_id: '',
        name: tmpl.name,
        state: 'stopped' as const,
        model_id: tmpl.model_id,
        provider_id: '',
        group: activeGroup,
        is_leader: tmpl.is_leader,
        task_level: '',
        error_count: 0,
        last_error: '',
        pending_delegations: 0,
        mailbox_high: 0,
        mailbox_normal: 0,
      }
      return live || placeholder
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

  // Keep L1 session history in sync, but only after agent finishes processing.
  // Reloading history while the agent is still processing creates a duplicate-render
  // window: the history snapshot can already contain the user message + a partial
  // assistant entry, while the virtualMessage stream also contains that same content.
  const prevL1AgentState = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (isL1Session && l1AgentState) {
      const wasProcessing = prevL1AgentState.current === 'processing'
      const isDoneProcessing = l1AgentState !== 'processing'
      if (wasProcessing && isDoneProcessing) {
        loadHistory('l1')
      } else if (!prevL1AgentState.current) {
        // Initial load when we first get the agent state
        loadHistory('l1')
      }
    }
    prevL1AgentState.current = l1AgentState
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
      // Strip trailing assistant messages from the history snapshot to prevent
      // duplicates: the history may already contain the user's last message and
      // a partial assistant entry that overlaps with the live stream content.
      let base = currentMessages
      while (base.length > 0 && base[base.length - 1].role === 'assistant') {
        base = base.slice(0, -1)
      }
      // Also strip the user message that was just sent if it is already present
      // in the stream context (identified by being the last user message).
      // The stream segments represent the current in-flight turn; the history
      // snapshot may duplicate the triggering user turn.
      const virtualMessage = {
        id: `msg-virtual-stream`,
        role: 'assistant' as const,
        segments: streamChatSegments,
        timestamp: new Date().toISOString(),
      }
      return [...base, virtualMessage]
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

          <div className="flex items-center gap-2">
            {streaming && (
              <div className="flex items-center gap-1.5 text-xs text-violet-500/70">
                <span className="inline-block h-1.5 w-1.5 rounded-full bg-violet-500 animate-pulse" />
                Generating
              </div>
            )}

            {/* New L2 Session */}
            {isL1Session && (
              <div className="relative" ref={newSessionRef}>
                <button
                  onClick={toggleNewSessionMenu}
                  disabled={creatingGroup !== null}
                  className="flex items-center gap-1.5 rounded-lg border border-border/80 bg-muted/40 px-2.5 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground hover:bg-muted/60 transition-colors cursor-pointer disabled:opacity-50"
                  title="Create a new L2 session for team collaboration"
                  aria-expanded={showNewSessionMenu}
                  aria-haspopup="menu"
                >
                  {creatingGroup ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Plus className="h-3.5 w-3.5" />
                  )}
                  <span className="hidden sm:inline">New L2</span>
                </button>

                {/* Dropdown menu */}
                {showNewSessionMenu && (
                  <div
                    className="absolute right-0 top-full mt-1 z-50 min-w-[160px] rounded-xl border border-border bg-card shadow-xl animate-in fade-in zoom-in-95 duration-150"
                    role="menu"
                  >
                    <div className="p-1.5">
                      <p className="px-2.5 py-1.5 text-[10px] font-semibold text-muted-foreground/60 uppercase tracking-wider">
                        Select a Team
                      </p>
                      {l2Groups.length === 0 ? (
                        <p className="px-2.5 py-3 text-xs text-muted-foreground/40 text-center">
                          No groups available
                        </p>
                      ) : (
                        l2Groups.map((group) => (
                          <button
                            key={group}
                            onClick={() => handleNewL2Session(group)}
                            className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-xs text-foreground hover:bg-muted/60 transition-colors text-left cursor-pointer"
                            role="menuitem"
                          >
                            <div className="h-5 w-5 rounded-md bg-violet-500/10 flex items-center justify-center">
                              <Plus className="h-3 w-3 text-violet-500" />
                            </div>
                            <span className="font-medium">{group}</span>
                          </button>
                        ))
                      )}
                    </div>
                  </div>
                )}
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
          ) : historyLoading[activeSessionId || ''] && finalMessages.length === 0 ? (
            <div className="px-4 py-3 space-y-5">
              {/* User message skeleton */}
              <div className="flex justify-end">
                <div className="max-w-[70%] space-y-2">
                  <div className="h-2.5 w-12 bg-muted/20 rounded animate-pulse" />
                  <div className="rounded-2xl rounded-br-md bg-muted/20 p-4 animate-pulse space-y-2">
                    <div className="h-3 w-48 bg-muted/30 rounded" />
                  </div>
                </div>
              </div>
              {/* Assistant message skeleton */}
              <div className="flex gap-3">
                <div className="h-7 w-7 shrink-0 rounded-full bg-violet-500/10 animate-pulse" />
                <div className="flex-1 space-y-2">
                  <div className="h-2.5 w-20 bg-muted/20 rounded animate-pulse" />
                  <div className="rounded-2xl rounded-bl-md bg-muted/20 p-4 animate-pulse space-y-2">
                    <div className="h-3 w-full bg-muted/30 rounded" />
                    <div className="h-3 w-3/4 bg-muted/30 rounded" />
                    <div className="h-3 w-1/2 bg-muted/30 rounded" />
                    <div className="h-3 w-5/6 bg-muted/30 rounded" />
                  </div>
                  {/* Tool call skeleton */}
                  <div className="rounded-xl border border-border/40 bg-muted/10 p-3 animate-pulse space-y-2">
                    <div className="flex items-center gap-2">
                      <div className="h-3 w-3 rounded-full bg-muted/30" />
                      <div className="h-2.5 w-40 bg-muted/30 rounded" />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          ) : finalMessages.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full gap-5 text-muted-foreground px-4">
              <div className="h-16 w-16 rounded-2xl bg-violet-500/5 flex items-center justify-center ring-1 ring-violet-500/10">
                <Sparkles className="h-8 w-8 text-violet-500/30" />
              </div>
              <div className="text-center max-w-sm">
                <p className="text-sm font-medium text-foreground/80 mb-1">
                  {activeSession?.type === 'l1'
                    ? 'L1 Coordinator — ready to help'
                    : `Start a conversation with ${activeSession?.group || 'this agent'}`}
                </p>
                {activeSession?.type === 'l1' ? (
                  <div className="space-y-3">
                    <p className="text-xs text-muted-foreground/40">
                      L1 is the primary coordinator that can browse files, edit code, run commands,
                      and delegate sub-tasks to specialized agents.
                    </p>
                    <div className="flex flex-wrap justify-center gap-1.5">
                      <span className="inline-flex items-center gap-1 rounded-md bg-muted/40 px-2 py-1 text-[10px] font-medium text-muted-foreground/70">
                        <span className="h-1.5 w-1.5 rounded-full bg-violet-400/60" />
                        Files &amp; Code
                      </span>
                      <span className="inline-flex items-center gap-1 rounded-md bg-muted/40 px-2 py-1 text-[10px] font-medium text-muted-foreground/70">
                        <span className="h-1.5 w-1.5 rounded-full bg-violet-400/60" />
                        Commands
                      </span>
                      <span className="inline-flex items-center gap-1 rounded-md bg-muted/40 px-2 py-1 text-[10px] font-medium text-muted-foreground/70">
                        <span className="h-1.5 w-1.5 rounded-full bg-violet-400/60" />
                        Delegation
                      </span>
                    </div>
                    <p className="text-[11px] text-muted-foreground/30">
                      For team workflows, create an L2 session via the sidebar or the button above.
                    </p>
                  </div>
                ) : (
                  <p className="text-xs text-muted-foreground/40">Send a message to get started.</p>
                )}
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
          delegating={delegating}
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
