import { useEffect, useRef, useState, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { ChatMessageView } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { useChatStore } from '@/stores/chatStore'
import { useChatStream } from '@/hooks/useChatStream'
import { useAgentStream } from '@/hooks/useAgentStream'
import { PanelRight, Loader2, Activity } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { cn } from '@/lib/utils'
import type { AgentInfo, Project } from '@/types'
import { L2SessionStatusPanel } from '@/components/L2SessionStatusPanel'
import { listL2Groups, listProjects, getTeams } from '@/lib/api'

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
    createL2Session,
    deleteL2Session,
  } = useChatStore()
  const { send, cancel } = useChatStream()
  const scrollRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const userScrolledUp = useRef(false)
  const loadingMore = useRef(false)

  // macOS Inspector state
  const [showInspector, setShowInspector] = useState(true)

  // L2 redesign states
  const [l2Groups, setL2Groups] = useState<string[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [teamProjectsMap, setTeamProjectsMap] = useState<Record<string, Project[]>>({})

  const [selectedGroup, setSelectedGroup] = useState<string>('')
  const [selectedProjectPath, setSelectedProjectPath] = useState<string>('')

  // Load L2 groups, projects, teams
  useEffect(() => {
    let active = true
    async function loadInitialData() {
      try {
        const [groupNames, projs, teamsData] = await Promise.all([
          listL2Groups(),
          listProjects(),
          getTeams().catch(() => ({ teams: [] })),
        ])

        if (!active) return

        setL2Groups(groupNames)
        setProjects(projs)

        const projectMap = new Map(projs.map((p) => [p.id, p]))
        const groupProjects: Record<string, Project[]> = {}
        for (const team of (teamsData as any).teams || []) {
          if (team.projects && Array.isArray(team.projects)) {
            for (const pid of team.projects) {
              const proj = projectMap.get(pid)
              if (proj) {
                if (!groupProjects[team.name]) groupProjects[team.name] = []
                groupProjects[team.name].push(proj)
              }
            }
          }
        }
        setTeamProjectsMap(groupProjects)
      } catch (err) {
        console.error('Failed to load welcome screen options:', err)
      }
    }
    loadInitialData()
    return () => {
      active = false
    }
  }, [])

  const agentsData = useAgentStore((state) => state.agents)
  const teamsData = useAgentStore((state) => state.teams)
  const fetchLiveAgents = useAgentStore((state) => state.fetchLiveAgents)
  const fetchTeams = useAgentStore((state) => state.fetchTeams)

  useEffect(() => {
    fetchLiveAgents()
    fetchTeams()
  }, [fetchLiveAgents, fetchTeams])

  useEffect(() => {
    loadSessions()
  }, [loadSessions, streaming])

  useEffect(() => {
    if (sessionId) {
      if (sessionId !== activeSessionId) {
        setActiveSession(sessionId)
      }
    } else if (activeSessionId) {
      navigate(`/chat/${activeSessionId}`, { replace: true })
    } else {
      navigate('/chat/l1', { replace: true })
    }
  }, [sessionId, activeSessionId, setActiveSession, navigate])

  const currentMessages = messages[activeSessionId || ''] || []
  const activeSession = sessions.find((s) => s.id === activeSessionId)
  const activeGroup = activeSession?.group
  const isL1Session = activeSessionId === 'l1'

  // Sync selected group and project path when activeSession changes
  useEffect(() => {
    if (activeSession) {
      setSelectedGroup(activeSession.group || '')
      setSelectedProjectPath(activeSession.project_path || '')
    } else if (l2Groups.length > 0) {
      setSelectedGroup(l2Groups[0])
    }
  }, [activeSession, l2Groups])

  // Sync first project of selected group when selectedGroup changes
  useEffect(() => {
    if (selectedGroup) {
      const groupProjs = teamProjectsMap[selectedGroup] || []
      const currentProjValid = groupProjs.some((p) => p.path === selectedProjectPath)
      if (!currentProjValid) {
        if (groupProjs.length > 0) {
          setSelectedProjectPath(groupProjs[0].path)
        } else if (projects.length > 0) {
          setSelectedProjectPath(projects[0].path)
        }
      }
    }
  }, [selectedGroup, teamProjectsMap, projects, selectedProjectPath])

  const selectedProject = projects.find((p) => p.path === selectedProjectPath)

  const groupAgents = useMemo(() => {
    if (isL1Session) {
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

    const team = teamsData?.teams.find((t) => t.name.toLowerCase() === activeGroup.toLowerCase())
    if (!team) {
      return agentsData
        ? agentsData.agents.filter((a) => a.group?.toLowerCase() === activeGroup.toLowerCase())
        : []
    }

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

  const anyRunning = useMemo(() => {
    return groupAgents.some((a) => a.state === 'processing')
  }, [groupAgents])

  const activeAgent = useMemo(() => {
    return groupAgents.find((a) => a.is_leader) || groupAgents[0] || null
  }, [groupAgents])

  useEffect(() => {
    if (anyRunning) {
      setShowInspector(true)
    }
  }, [anyRunning])

  const l1Agent = isL1Session ? groupAgents[0] : null
  const l1AgentState = l1Agent?.state
  const l1AgentInstanceId = l1Agent?.instance_id || null
  const stream = useAgentStream(l1AgentInstanceId)

  const prevL1AgentState = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (isL1Session && l1AgentState) {
      const wasProcessing = prevL1AgentState.current === 'processing'
      const isDoneProcessing = l1AgentState !== 'processing'
      if (wasProcessing && isDoneProcessing) {
        loadHistory('l1')
      } else if (!prevL1AgentState.current) {
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
      let base = currentMessages
      while (base.length > 0 && base[base.length - 1].role === 'assistant') {
        base = base.slice(0, -1)
      }
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

  const handleSend = async (
    text: string,
    files?: { name: string; path: string }[],
    group?: string,
    projectPath?: string
  ) => {
    let targetSessionId = activeSessionId || undefined

    if (!isL1Session && currentMessages.length === 0 && group && activeSession) {
      const currentProjPath = activeSession.project_path || ''
      const currentGroup = activeSession.group || ''

      if (group !== currentGroup || projectPath !== currentProjPath) {
        const newId = await createL2Session(group, projectPath || '')
        if (newId) {
          if (activeSessionId && activeSessionId !== newId) {
            await deleteL2Session(activeSessionId)
          }
          targetSessionId = newId
          navigate(`/chat/${newId}`)
        }
      }
    }

    await send(text, files, targetSessionId)
  }

  const contentSum = finalMessages.reduce((acc, msg) => {
    let sum = 0
    for (const seg of msg.segments) {
      if ('text' in seg && typeof (seg as any).text === 'string') {
        sum += (seg as any).text.length
      }
    }
    return acc + sum + msg.segments.length
  }, 0)

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    const handleScroll = () => {
      const { scrollTop, scrollHeight, clientHeight } = el
      const isNearBottom = scrollHeight - scrollTop - clientHeight < 100
      if (isNearBottom) {
        userScrolledUp.current = false
      } else {
        userScrolledUp.current = true
      }

      if (scrollTop < 50 && historyHasMore && !historyLoading && !loadingMore.current) {
        loadingMore.current = true
        const prevHeight = scrollHeight
        loadMoreHistory(activeSessionId || '')
        setTimeout(() => {
          if (scrollRef.current) {
            const diff = scrollRef.current.scrollHeight - prevHeight
            scrollRef.current.scrollTop = diff
          }
          loadingMore.current = false
        }, 0)
      }
    }
    el.addEventListener('scroll', handleScroll)
    return () => el.removeEventListener('scroll', handleScroll)
  }, [historyHasMore, historyLoading, loadMoreHistory])

  useEffect(() => {
    if (userScrolledUp.current) return
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [contentSum, streaming])

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      {/* Pane 3: Chat conversation bubble stream */}
      <div className="flex flex-1 flex-col overflow-hidden h-full bg-background relative">
        {/* Chat header */}
        <header className="flex h-12 items-center justify-between border-b border-border/30 px-6 select-none bg-card/20 shrink-0">
          {/* Header left: info and status */}
          <div className="flex items-center gap-3">
            <h1 className="text-xs font-bold text-foreground truncate font-mono">
              {activeSession?.name || (isL1Session ? '通用问答 (L1)' : `${activeGroup} 团队`)}
            </h1>
            <span className="text-[10px] text-muted-foreground font-mono bg-secondary px-1.5 py-0.5 rounded border border-border/20">
              {isL1Session ? 'L1 fast-edit mode' : `L2 multi-agent workspace`}
            </span>
          </div>

          {/* Header right: Actions */}
          <div className="flex items-center gap-2">
            {streaming && (
              <button
                onClick={cancel}
                className="px-2.5 py-1 rounded bg-rose-500/10 text-rose-500 border border-rose-500/20 hover:bg-rose-500 hover:text-white text-[10px] font-semibold transition-all cursor-pointer"
              >
                停止生成
              </button>
            )}
            
            <button
              onClick={() => setShowInspector(!showInspector)}
              className={cn(
                'p-1.5 rounded-md hover:bg-foreground/5 transition-all cursor-pointer',
                showInspector ? 'text-primary' : 'text-muted-foreground'
              )}
              title="显示/隐藏 任务状态面板"
            >
              <PanelRight className="h-4 w-4" />
            </button>
          </div>
        </header>

        {/* Outer container for chat content + inspector split layout */}
        <div className="flex flex-1 min-h-0 overflow-hidden relative">
          
          {/* Conversation stream */}
          <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-background">
            {finalMessages.length === 0 ? (
              <div className="flex-1 flex flex-col items-center justify-center p-6 overflow-y-auto bg-background">
                <div className="w-full max-w-3xl flex flex-col items-center space-y-8 select-none">
                  {/* Centered Heading */}
                  <h1 className="text-3xl font-semibold text-foreground tracking-tight text-center">
                    {isL1Session 
                      ? 'What should we build with L1 Orchestrator?' 
                      : `What should we build in ${selectedProject?.name || 'soloQueue'}?`
                    }
                  </h1>

                  {/* Redesigned Input Card */}
                  <div className="w-full">
                    <ChatInput
                      onSend={handleSend}
                      onCancel={cancel}
                      streaming={streaming}
                      delegating={delegating}
                      disabled={!activeSessionId || streaming || delegating}
                      activeSessionId={activeSessionId || undefined}
                      showL2Selectors={!isL1Session}
                      groups={l2Groups}
                      projects={projects}
                      teamProjectsMap={teamProjectsMap}
                      selectedGroup={selectedGroup}
                      selectedProjectPath={selectedProjectPath}
                      onGroupChange={setSelectedGroup}
                      onProjectChange={setSelectedProjectPath}
                    />
                  </div>
                </div>
              </div>
            ) : (
              <>
                <div ref={scrollRef} className="flex-1 overflow-y-auto p-6 space-y-6">
                  {historyLoading && (
                    <div className="flex items-center justify-center py-4">
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                      <span className="text-xs text-muted-foreground font-mono ml-2">正在载入历史...</span>
                    </div>
                  )}
                  
                  {finalMessages.map((msg) => (
                    <ChatMessageView key={msg.id} message={msg} />
                  ))}

                  {delegating && (
                    <div className="flex items-center gap-2.5 text-xs text-muted-foreground bg-secondary/30 p-3 rounded-lg border border-border/25 font-mono animate-pulse">
                      <Activity className="h-3.5 w-3.5 text-primary animate-spin" />
                      <span>团队正在协作分发中，请稍候...</span>
                    </div>
                  )}
                  
                  <div ref={bottomRef} className="h-2" />
                </div>

                <ChatInput
                  onSend={handleSend}
                  onCancel={cancel}
                  streaming={streaming}
                  delegating={delegating}
                  disabled={!activeSessionId || streaming || delegating}
                  activeSessionId={activeSessionId || undefined}
                  showL2Selectors={!isL1Session}
                  readOnlySelectors={true}
                  groups={l2Groups}
                  projects={projects}
                  teamProjectsMap={teamProjectsMap}
                  selectedGroup={selectedGroup}
                  selectedProjectPath={selectedProjectPath}
                  onGroupChange={setSelectedGroup}
                  onProjectChange={setSelectedProjectPath}
                />
              </>
            )}
          </div>

          {/* Right Inspector panel (Plan lists, checklist, MCP status details) */}
          {showInspector && activeSession && (
            <div className="w-[300px] shrink-0 border-l border-border/30 h-full overflow-y-auto bg-card/5">
              <L2SessionStatusPanel session={activeSession} activeAgent={activeAgent} />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
