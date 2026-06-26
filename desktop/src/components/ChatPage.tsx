import { useEffect, useRef, useState, useMemo, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { ChatMessageView } from '@/components/ChatMessage'
import { ChatInput } from '@/components/ChatInput'
import { useChatStore } from '@/stores/chatStore'
import { useChatStream } from '@/hooks/useChatStream'
import { useAgentStream } from '@/hooks/useAgentStream'
import { PanelRight, Loader2, Activity, Bot, Users, FolderOpen, Layers } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { useRuntimeStore } from '@/stores/runtimeStore'
import { cn } from '@/lib/utils'
import type { AgentInfo, Project } from '@/types'
import { L2SessionStatusPanel } from '@/components/L2SessionStatusPanel'
import { SessionFilePanel } from '@/components/SessionFilePanel'
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
  const [showInspector, setShowInspector] = useState(false)
  const [inspectorTab, setInspectorTab] = useState<'files' | 'changes'>('files')

  const toggleInspector = (open: boolean) => {
    useRuntimeStore.getState().setInspectorPanelWidth(open ? panelWidth : 0)
    setShowInspector(open)
  }
  const sidebarCollapsed = useRuntimeStore((s) => s.sidebarCollapsed)

  // Resizable inspector panel
  const MIN_AREA_WIDTH = 200
  const [panelWidth, setPanelWidth] = useState(300)
  const [isResizing, setIsResizing] = useState(false)
  const splitContainerRef = useRef<HTMLDivElement>(null)

  const handleResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault()
    setIsResizing(true)
  }, [])

  useEffect(() => {
    if (!isResizing) return
    const handleMouseMove = (e: MouseEvent) => {
      if (!splitContainerRef.current) return
      const rect = splitContainerRef.current.getBoundingClientRect()
      const newWidth = rect.right - e.clientX
      const maxWidth = Math.floor(rect.width * 0.6)
      const clamped = Math.max(
        MIN_AREA_WIDTH,
        Math.min(newWidth, rect.width - MIN_AREA_WIDTH, maxWidth)
      )
      setPanelWidth(clamped)
      useRuntimeStore.getState().setInspectorPanelWidth(clamped)
    }
    const handleMouseUp = () => setIsResizing(false)
    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
    }
  }, [isResizing])

  // Track split container width for responsive content sizing
  const [containerWidth, setContainerWidth] = useState(0)
  useEffect(() => {
    const el = splitContainerRef.current
    if (!el) return
    const ro = new ResizeObserver(([entry]) => {
      setContainerWidth(entry.contentRect.width)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

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
    if (sessionId && sessionId !== 'l1') {
      if (sessionId !== activeSessionId) {
        setActiveSession(sessionId)
      }
    } else {
      // Find the most recent L2 session
      const l2Sessions = sessions.filter((s) => s.type === 'l2')
      if (l2Sessions.length > 0) {
        const sorted = [...l2Sessions].sort((a, b) => {
          const timeA = a.createdAt || (a as any).created_at || ''
          const timeB = b.createdAt || (b as any).created_at || ''
          return timeB.localeCompare(timeA)
        })
        const latest = sorted[0].id
        setActiveSession(latest)
        navigate(`/chat/${latest}`, { replace: true })
      } else {
        // No L2 sessions exist
        if (activeSessionId) {
          setActiveSession('')
        }
        if (sessionId === 'l1') {
          navigate('/chat', { replace: true })
        }
      }
    }
  }, [sessionId, activeSessionId, sessions, setActiveSession, navigate])

  const currentMessages = messages[activeSessionId || ''] || []
  const activeSession = sessions.find((s) => s.id === activeSessionId)
  const activeGroup = activeSession?.group
  const isL1Session = activeSessionId === 'l1'

  // Dynamic message max-width: scales with main content area, capped at original 3xl (768px)
  const MESSAGE_MAX_W = 768
  const messageMaxWidth = useMemo(() => {
    const panelVisible = showInspector && activeSession
    const mainContentWidth = containerWidth - (panelVisible ? panelWidth + 4 : 0) // 4px = handle width
    if (mainContentWidth <= 0) return MESSAGE_MAX_W
    return Math.max(MIN_AREA_WIDTH - 32, Math.min(mainContentWidth * 0.85, MESSAGE_MAX_W))
  }, [showInspector, activeSession, containerWidth, panelWidth])

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

  const agentDisplayName = useMemo(() => {
    if (isL1Session) return 'L1 Agent'
    return activeSession?.agent_name || activeAgent?.name || 'Assistant'
  }, [isL1Session, activeSession, activeAgent])

  useEffect(() => {
    if (anyRunning) {
      toggleInspector(true)
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

    if (!isL1Session && group) {
      if (!activeSessionId) {
        // No session exists — auto-create one on first send
        const newId = await createL2Session(group, projectPath || '')
        if (newId) {
          targetSessionId = newId
          navigate(`/chat/${newId}`)
        }
      } else if (currentMessages.length === 0 && activeSession) {
        // Session exists but no messages — recreate if context changed
        const currentProjPath = activeSession.project_path || ''
        const currentGroup = activeSession.group || ''

        if (group !== currentGroup || projectPath !== currentProjPath) {
          const newId = await createL2Session(group, projectPath || '')
          if (newId) {
            if (activeSessionId !== newId) {
              await deleteL2Session(activeSessionId)
            }
            targetSessionId = newId
            navigate(`/chat/${newId}`)
          }
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

  const lastScrolledSessionId = useRef<string | null>(null)

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

      const hasMore = activeSessionId ? historyHasMore[activeSessionId] : false
      const isLoading = activeSessionId ? historyLoading[activeSessionId] : false

      if (scrollTop < 50 && hasMore && !isLoading && !loadingMore.current) {
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
  }, [activeSessionId, historyHasMore, historyLoading, loadMoreHistory])

  useEffect(() => {
    if (userScrolledUp.current) return
    
    const shouldScrollInstant = !streaming && (lastScrolledSessionId.current !== activeSessionId)
    
    if (shouldScrollInstant) {
      bottomRef.current?.scrollIntoView({ behavior: 'auto' })
      if (finalMessages.length > 0) {
        lastScrolledSessionId.current = activeSessionId
      }
    } else {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [contentSum, streaming, activeSessionId, finalMessages])

  if (!activeSessionId) {
    return (
      <div className="flex-1 flex flex-col items-center justify-center p-8 overflow-y-auto bg-background select-none h-full w-full">
        <div
          className="w-full flex flex-col items-center space-y-8"
          style={{ maxWidth: messageMaxWidth }}
        >
          <div className="text-center space-y-3">
            <div className="h-16 w-16 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center text-primary mx-auto mb-2 shadow-inner">
              <Bot className="h-8 w-8 animate-pulse" />
            </div>
            <h1 className="text-3xl font-extrabold tracking-tight text-foreground bg-gradient-to-r from-foreground to-foreground/75 bg-clip-text">
              欢迎使用 SoloQueue 协作空间
            </h1>
            <p className="text-sm text-muted-foreground max-w-md mx-auto text-center">
              选择团队和项目，与多智能体系统开始协同编程。
            </p>
          </div>

          {/* ChatInput with selectors — available immediately for composing first message */}
          <div className="w-full">
            <ChatInput
              onSend={handleSend}
              onCancel={cancel}
              streaming={streaming}
              delegating={delegating}
              disabled={streaming || delegating}
              activeSessionId={undefined}
              showL2Selectors={true}
              groups={l2Groups}
              projects={projects}
              teamProjectsMap={teamProjectsMap}
              selectedGroup={selectedGroup}
              selectedProjectPath={selectedProjectPath}
              onGroupChange={setSelectedGroup}
              onProjectChange={setSelectedProjectPath}
              ctxwinUsed={0}
              ctxwinLimit={0}
            />
          </div>

          {/* Team cards — click to pre-fill selectors above */}
          <div className="w-full grid grid-cols-1 md:grid-cols-2 gap-4">
            {l2Groups.map((group) => {
              const groupProjects = teamProjectsMap[group] || []
              return (
                <div
                  key={group}
                  onClick={() => {
                    setSelectedGroup(group)
                    if (groupProjects.length > 0) setSelectedProjectPath(groupProjects[0].path)
                  }}
                  className={cn(
                    "border border-border/45 bg-card/40 rounded-xl p-5 hover:border-border/80 hover:bg-card/60 transition-all cursor-pointer",
                    selectedGroup === group && "border-primary/50 bg-primary/5 ring-1 ring-primary/20"
                  )}
                >
                  <div className="space-y-1.5">
                    <h3 className="text-sm font-bold text-foreground flex items-center gap-2">
                      <Users className="h-3.5 w-3.5 shrink-0 opacity-70" />
                      <span className="tracking-wider uppercase">{group} 团队</span>
                    </h3>
                    <p className="text-xs text-muted-foreground">
                      {groupProjects.length > 0
                        ? `关联项目: ${groupProjects.map(p => p.name).join(', ')}`
                        : '无关联项目'}
                    </p>
                  </div>
                </div>
              )
            })}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      {/* Pane 3: Chat conversation bubble stream */}
      <div className="flex flex-1 flex-col overflow-hidden h-full bg-background relative">
        {/* Chat header — split into chat section + panel section when inspector is open */}
        <header className={cn(
          "flex h-12 items-center border-b border-border/30 select-none bg-card/20 shrink-0",
          sidebarCollapsed && "pl-[115px]"
        )}>
          {/* Left section: chat header area — fills remaining space */}
          <div className={cn(
            "flex items-center gap-3 px-6 h-full",
            showInspector ? "flex-1 justify-between" : "flex-1 justify-between"
          )}>
            <h1 className="text-xs font-bold text-foreground truncate font-mono">
              {activeSession?.name || (isL1Session ? '通用问答 (L1)' : `${activeGroup} 团队`)}
            </h1>
            <div className="flex items-center gap-2 electron-no-drag">
              {streaming && (
                <button
                  onClick={cancel}
                  className="px-2.5 py-1 rounded bg-rose-500/10 text-rose-500 border border-rose-500/20 hover:bg-rose-500 hover:text-white text-[10px] font-semibold transition-all cursor-pointer"
                >
                  停止生成
                </button>
              )}
              {!showInspector && (
                <button
                  onClick={() => toggleInspector(true)}
                  className="p-1.5 rounded-md hover:bg-foreground/5 transition-all cursor-pointer text-muted-foreground"
                  title="显示任务状态面板"
                >
                  <PanelRight className="h-4 w-4" />
                </button>
              )}
            </div>
          </div>

          {/* Right section: panel header area — aligned to inspector width */}
          {showInspector && (
            <div
              className="shrink-0 flex items-center justify-between h-full border-l border-border/30 bg-card/20 px-3"
              style={{ width: panelWidth }}
            >
              <div className="flex items-center gap-1">
                <button
                  onClick={() => setInspectorTab('files')}
                  className={cn(
                    'flex shrink-0 items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors cursor-pointer',
                    inspectorTab === 'files'
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
                  )}
                >
                  <FolderOpen className="h-3.5 w-3.5" />
                  文件
                </button>
                <button
                  onClick={() => setInspectorTab('changes')}
                  className={cn(
                    'flex shrink-0 items-center gap-1.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-colors cursor-pointer',
                    inspectorTab === 'changes'
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground hover:text-foreground hover:bg-foreground/5'
                  )}
                >
                  <Layers className="h-3.5 w-3.5" />
                  变更
                </button>
              </div>
              <button
                onClick={() => toggleInspector(false)}
                className="shrink-0 p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-foreground/5 transition-colors cursor-pointer"
                title="关闭面板"
              >
                <PanelRight className="h-3.5 w-3.5" />
              </button>
            </div>
          )}
        </header>

        {/* Outer container for chat content + inspector split layout */}
        <div ref={splitContainerRef} className={cn(
          'flex flex-1 min-h-0 overflow-hidden relative',
          isResizing && 'select-none'
        )}>
          
          {/* Conversation stream */}
          <div className="flex-1 flex flex-col min-w-0 h-full overflow-hidden bg-background">
            {finalMessages.length === 0 ? (
              <div className="flex-1 flex flex-col items-center justify-center p-6 overflow-y-auto bg-background">
                <div
                  className="w-full flex flex-col items-center space-y-8 select-none"
                  style={{ maxWidth: messageMaxWidth }}
                >
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
                      disabled={streaming || delegating}
                      activeSessionId={activeSessionId || undefined}
                      showL2Selectors={!isL1Session}
                      groups={l2Groups}
                      projects={projects}
                      teamProjectsMap={teamProjectsMap}
                      selectedGroup={selectedGroup}
                      selectedProjectPath={selectedProjectPath}
                      onGroupChange={setSelectedGroup}
                      onProjectChange={setSelectedProjectPath}
                      ctxwinUsed={activeSession?.ctxwin_used ?? 0}
                      ctxwinLimit={activeSession?.ctxwin_limit ?? 0}
                    />
                  </div>
                </div>
              </div>
            ) : (
              <>
                <div ref={scrollRef} className="flex-1 overflow-y-auto p-6">
                  <div
                    className="mx-auto w-full space-y-6 px-4"
                    style={{ maxWidth: messageMaxWidth }}
                  >
                    {activeSessionId && historyLoading[activeSessionId] && (
                      <div className="flex items-center justify-center py-4">
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        <span className="text-xs text-muted-foreground font-mono ml-2">正在载入历史...</span>
                      </div>
                    )}
                    
                    {finalMessages.map((msg) => (
                      <ChatMessageView key={msg.id} message={msg} agentName={agentDisplayName} />
                    ))}

                    {delegating && (
                      <div className="flex items-center gap-2.5 text-xs text-muted-foreground bg-secondary/30 p-3 rounded-lg border border-border/25 font-mono animate-pulse">
                        <Activity className="h-3.5 w-3.5 text-primary animate-spin" />
                        <span>团队正在协作分发中，请稍候...</span>
                      </div>
                    )}
                    
                    <div ref={bottomRef} className="h-2" />
                  </div>
                </div>

                <ChatInput
                  onSend={handleSend}
                  onCancel={cancel}
                  streaming={streaming}
                  delegating={delegating}
                  disabled={streaming || delegating}
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
                  ctxwinUsed={activeSession?.ctxwin_used ?? 0}
                  ctxwinLimit={activeSession?.ctxwin_limit ?? 0}
                />
              </>
            )}
          </div>

          {/* Right Inspector panel (Plan lists, checklist, MCP status details) */}
          {showInspector && activeSession && (
            <>
              {/* Resize handle */}
              <div
                onMouseDown={handleResizeStart}
                className={cn(
                  'w-1 shrink-0 cursor-col-resize hover:bg-primary/40 active:bg-primary/40 transition-colors',
                  isResizing && 'bg-primary/40'
                )}
              />
              <div
                className="shrink-0 border-l border-border/30 h-full overflow-hidden bg-card/5 flex flex-col"
                style={{ width: panelWidth }}
              >
                {/* Panel content */}
                <div className="flex-1 min-h-0 overflow-hidden">
                  {inspectorTab === 'files' ? (
                    activeSession.project_path ? (
                      <SessionFilePanel projectPath={activeSession.project_path} panelWidth={panelWidth} />
                    ) : (
                      <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
                        当前会话未关联项目
                      </div>
                    )
                  ) : (
                    <div className="h-full overflow-y-auto">
                      <L2SessionStatusPanel session={activeSession} activeAgent={activeAgent} />
                    </div>
                  )}
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
