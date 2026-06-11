import { useEffect, useState, useCallback, useMemo } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Plus,
  Trash2,
  Bot,
  FolderOpen,
  ChevronRight,
  ChevronDown,
  MessageSquare,
} from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { useAgentStore } from '@/stores/agentStore'
import { listL2Groups, listProjects, getTeams } from '@/lib/api'
import type { ChatSession, Project } from '@/types'
import { cn } from '@/lib/utils'

interface GroupInfo {
  name: string
  projects: Project[]
}

export function SessionTree() {
  const navigate = useNavigate()
  const sessions = useChatStore((s) => s.sessions)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const streaming = useChatStore((s) => s.streaming)
  const loadSessions = useChatStore((s) => s.loadSessions)
  const createL2Session = useChatStore((s) => s.createL2Session)
  const deleteL2Session = useChatStore((s) => s.deleteL2Session)
  const setActiveSession = useChatStore((s) => s.setActiveSession)

  const agentsData = useAgentStore((s) => s.agents)
  const fetchLiveAgents = useAgentStore((s) => s.fetchLiveAgents)

  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [creating, setCreating] = useState<string | null>(null)
  // All groups expanded by default; projects collapsed by default.
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [expandedProjects, setExpandedProjects] = useState<Record<string, boolean>>({})

  useEffect(() => {
    loadSessions()
    loadGroupInfo()
    fetchLiveAgents()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const loadGroupInfo = async () => {
    try {
      const [groupNames, projects, teamsData] = await Promise.all([
        listL2Groups(),
        listProjects(),
        getTeams().catch(() => ({ teams: [] })),
      ])
      const projectMap = new Map(projects.map((p) => [p.id, p]))
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
      setGroups(
        groupNames
          .map((name) => ({
            name,
            projects: groupProjects[name] || [],
          }))
          .sort((a, b) => a.name.localeCompare(b.name))
      )
    } catch {
      try {
        const names = await listL2Groups()
        setGroups(
          names.map((name) => ({ name, projects: [] })).sort((a, b) => a.name.localeCompare(b.name))
        )
      } catch {
        setGroups([])
      }
    }
  }

  const l1Session = sessions.find((s) => s.type === 'l1')
  const l2Sessions = sessions.filter((s) => s.type === 'l2')

  // Find L1 agent from list
  const l1Agent = useMemo(() => {
    if (!agentsData) return null
    const { agents, supervisors } = agentsData
    const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
    const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))
    return agents.find((a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)) || null
  }, [agentsData])

  // Build tree: sessions nested under their parent (group or project).
  const buildSessionTree = useCallback(() => {
    const grouped: Record<string, ChatSession[]> = {}
    for (const s of l2Sessions) {
      const bucket = s.group || 'unknown'
      if (!grouped[bucket]) grouped[bucket] = []
      grouped[bucket].push(s)
    }
    for (const bucket of Object.keys(grouped)) {
      grouped[bucket].sort((a, b) => {
        const timeA = a.createdAt || (a as any).created_at || ''
        const timeB = b.createdAt || (b as any).created_at || ''
        return timeB.localeCompare(timeA)
      })
    }
    return grouped
  }, [l2Sessions])

  const sessionTree = buildSessionTree()

  const handleNewSession = useCallback(
    async (group: string, workDir?: string) => {
      setCreating(group)
      try {
        const newId = await createL2Session(group, workDir || '')
        if (newId) {
          navigate(`/chat/${newId}`)
        }
      } finally {
        setCreating(null)
      }
    },
    [createL2Session, navigate]
  )

  const handleDelete = useCallback(
    async (e: React.MouseEvent, id: string) => {
      e.stopPropagation()
      if (streaming) return
      await deleteL2Session(id)
      if (activeSessionId === id) {
        navigate('/chat/l1')
      }
    },
    [streaming, deleteL2Session, activeSessionId, navigate]
  )

  const toggleGroup = (name: string) =>
    setExpandedGroups((prev) => ({ ...prev, [name]: prev[name] === false }))

  const toggleProject = (key: string) =>
    setExpandedProjects((prev) => ({ ...prev, [key]: !prev[key] }))

  const isGroupExp = (name: string) => expandedGroups[name] !== false
  const isProjExp = (key: string) => expandedProjects[key] === true

  return (
    <div className="flex flex-col py-1 space-y-1 select-none">
      {/* ─── L1 ─── */}
      {l1Session && (
        <TreeItem
          icon={Bot}
          label="L1 Orchestrator"
          active={activeSessionId === 'l1'}
          state={l1Agent?.state}
          onClick={() => {
            setActiveSession('l1')
            navigate('/chat/l1')
          }}
          indent={0}
          showDelete={false}
        />
      )}

      {/* ─── L2 groups with nested projects ─── */}
      <div className="space-y-1.5">
        {groups.map((group) => {
          const groupSessions = sessionTree[group.name] || []
          const hasProjects = group.projects.length > 0
          const gExpanded = isGroupExp(group.name)

          return (
            <div key={group.name} className="space-y-0.5">
              {/* Group header */}
              <button
                onClick={() => toggleGroup(group.name)}
                className="flex items-center gap-1.5 w-full px-3 py-1 text-[10px] font-semibold text-muted-foreground/60 uppercase tracking-wider hover:text-foreground/80 hover:bg-muted/30 rounded-md transition-colors cursor-pointer"
              >
                {hasProjects ? (
                  gExpanded ? (
                    <ChevronDown className="h-3 w-3 shrink-0" />
                  ) : (
                    <ChevronRight className="h-3 w-3 shrink-0" />
                  )
                ) : (
                  <span className="w-3 shrink-0" />
                )}
                <span className="flex-1 text-left truncate">{group.name}</span>
              </button>

              {/* Children */}
              {gExpanded && (
                <div className="space-y-0.5">
                  {hasProjects ? (
                    <>
                      {/* Projects & their sessions */}
                      {group.projects.map((proj) => {
                        const projKey = `${group.name}:${proj.id}`
                        const projSessions = groupSessions
                          .filter((s) => s.project_path === proj.path)
                          .sort((a, b) => {
                            const timeA = a.createdAt || (a as any).created_at || ''
                            const timeB = b.createdAt || (b as any).created_at || ''
                            return timeB.localeCompare(timeA)
                          })
                        const pExpanded = isProjExp(projKey)

                        return (
                          <div key={proj.id} className="space-y-0.5">
                            {/* Project row */}
                            <div
                              onClick={() => toggleProject(projKey)}
                              className="flex items-center gap-1.5 w-full px-5 py-1 text-xs text-muted-foreground/80 hover:text-foreground hover:bg-muted/30 rounded-md transition-colors cursor-pointer"
                            >
                              {pExpanded ? (
                                <ChevronDown className="h-3 w-3 shrink-0" />
                              ) : (
                                <ChevronRight className="h-3 w-3 shrink-0" />
                              )}
                              <FolderOpen className="h-3.5 w-3.5 shrink-0 opacity-60" />
                              <span className="flex-1 text-left truncate">{proj.name}</span>
                              <button
                                onClick={(e) => {
                                  e.stopPropagation()
                                  handleNewSession(group.name, proj.path)
                                }}
                                disabled={creating === projKey || streaming}
                                className="p-0.5 rounded hover:bg-muted-foreground/10 text-muted-foreground/50 hover:text-foreground transition-colors disabled:opacity-30 cursor-pointer"
                                title={`New session in ${proj.name}`}
                              >
                                <Plus className="h-3 w-3" />
                              </button>
                            </div>

                            {/* Sessions under this project */}
                            {pExpanded &&
                              projSessions.map((s) => (
                                <TreeItem
                                  key={s.id}
                                  icon={MessageSquare}
                                  label={s.name || 'New session'}
                                  isPast={s.name ? s.name.startsWith('Past') : false}
                                  active={activeSessionId === s.id}
                                  onClick={() => {
                                    setActiveSession(s.id)
                                    navigate(`/chat/${s.id}`)
                                  }}
                                  onDelete={(e) => handleDelete(e, s.id)}
                                  disabled={streaming}
                                  indent={2}
                                />
                              ))}
                            {!pExpanded && projSessions.length > 0 && (
                              <div className="pl-12 py-0.5 text-[10px] text-muted-foreground/40 font-medium select-none">
                                {projSessions.length} session
                                {projSessions.length !== 1 ? 's' : ''}
                              </div>
                            )}
                          </div>
                        )
                      })}
                    </>
                  ) : (
                    <>
                      {/* No projects: show sessions directly + new session button */}
                      {groupSessions.map((s) => (
                        <TreeItem
                          key={s.id}
                          icon={MessageSquare}
                          label={s.name || 'New session'}
                          isPast={s.name ? s.name.startsWith('Past') : false}
                          active={activeSessionId === s.id}
                          onClick={() => {
                            setActiveSession(s.id)
                            navigate(`/chat/${s.id}`)
                          }}
                          onDelete={(e) => handleDelete(e, s.id)}
                          disabled={streaming}
                          indent={1}
                        />
                      ))}
                      <button
                        onClick={() => handleNewSession(group.name)}
                        disabled={creating === group.name || streaming}
                        className="flex items-center gap-1.5 w-full px-5 py-1 text-xs text-muted-foreground/50 hover:text-foreground hover:bg-muted/30 rounded-md transition-colors disabled:opacity-30 cursor-pointer"
                      >
                        <Plus className="h-3 w-3" />
                        <span>New session</span>
                      </button>
                    </>
                  )}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

// ─── Reusable tree item ───────────────────────────────────────────────────
function TreeItem({
  icon: Icon,
  label,
  active,
  onClick,
  onDelete,
  disabled,
  indent,
  showDelete = true,
  isPast = false,
  state,
}: {
  icon: typeof Bot
  label: string
  active: boolean
  onClick: () => void
  onDelete?: (e: React.MouseEvent) => void
  disabled?: boolean
  indent: number
  showDelete?: boolean
  isPast?: boolean
  state?: string
}) {
  const pl = 12 + indent * 12
  return (
    <div className="group relative">
      <button
        onClick={onClick}
        style={{ paddingLeft: `${pl}px` }}
        className={`w-full flex items-center gap-2 pr-8 py-1 rounded-md text-[13px] leading-tight transition-colors cursor-pointer ${
          active
            ? 'bg-primary/10 text-primary font-medium'
            : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
        }`}
      >
        <div className="relative flex items-center justify-center shrink-0">
          <Icon className="h-3.5 w-3.5 opacity-60" />
          {state && (
            <span
              className={cn(
                'absolute -bottom-0.5 -right-0.5 w-1.5 h-1.5 rounded-full border border-card',
                state === 'processing'
                  ? 'bg-[var(--success)]'
                  : state === 'idle'
                    ? 'bg-amber-500'
                    : 'bg-muted-foreground/40'
              )}
            />
          )}
        </div>
        <span className="truncate text-left flex-1">
          {label}
          {isPast && (
            <span className="ml-1.5 align-middle inline-block px-1.5 py-px rounded text-[9px] font-medium bg-amber-500/10 text-amber-600/60">
              Past
            </span>
          )}
        </span>
      </button>
      {showDelete && onDelete && (
        <button
          onClick={onDelete}
          disabled={disabled}
          className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1 rounded hover:bg-destructive/10 hover:text-destructive text-muted-foreground/30 opacity-0 group-hover:opacity-100 transition-all cursor-pointer disabled:opacity-0"
          title="Delete session"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  )
}
