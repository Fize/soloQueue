import { useEffect, useState, useCallback } from 'react'
import { Plus, Trash2, Bot, FolderOpen, ChevronRight, ChevronDown, MessageSquare } from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { listL2Groups, listProjects, getTeams } from '@/lib/api'
import type { ChatSession, Project } from '@/types'

interface GroupInfo {
  name: string
  projects: Project[]
}

export function SessionSidebar() {
  const sessions = useChatStore((s) => s.sessions)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const streaming = useChatStore((s) => s.streaming)
  const loadSessions = useChatStore((s) => s.loadSessions)
  const createL2Session = useChatStore((s) => s.createL2Session)
  const deleteL2Session = useChatStore((s) => s.deleteL2Session)
  const setActiveSession = useChatStore((s) => s.setActiveSession)

  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [creating, setCreating] = useState<string | null>(null)
  // All groups expanded by default; projects collapsed by default.
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [expandedProjects, setExpandedProjects] = useState<Record<string, boolean>>({})

  useEffect(() => {
    loadSessions()
    loadGroupInfo()
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
        groupNames.map((name) => ({
          name,
          projects: groupProjects[name] || [],
        })),
      )
    } catch {
      try {
        const names = await listL2Groups()
        setGroups(names.map((name) => ({ name, projects: [] })))
      } catch {
        setGroups([])
      }
    }
  }

  const l1Session = sessions.find((s) => s.type === 'l1')
  const l2Sessions = sessions.filter((s) => s.type === 'l2')

  // Build tree: sessions nested under their parent (group or project).
  const buildSessionTree = useCallback(() => {
    const grouped: Record<string, ChatSession[]> = {}
    for (const s of l2Sessions) {
      const bucket = s.group || 'unknown'
      if (!grouped[bucket]) grouped[bucket] = []
      grouped[bucket].push(s)
    }
    return grouped
  }, [l2Sessions])

  const sessionTree = buildSessionTree()

  const handleNewSession = useCallback(async (group: string, workDir?: string) => {
    setCreating(group)
    try {
      await createL2Session(group, workDir || '')
    } finally {
      setCreating(null)
    }
  }, [createL2Session])

  const handleDelete = useCallback(async (e: React.MouseEvent, id: string) => {
    e.stopPropagation()
    if (streaming) return
    await deleteL2Session(id)
  }, [streaming, deleteL2Session])

  const toggleGroup = (name: string) =>
    setExpandedGroups((prev) => ({ ...prev, [name]: prev[name] === false ? true : false }))

  const toggleProject = (key: string) =>
    setExpandedProjects((prev) => ({ ...prev, [key]: prev[key] === false ? true : false }))

  const isGroupExp = (name: string) => expandedGroups[name] !== false
  const isProjExp = (key: string) => expandedProjects[key] === true

  return (
    <div className="flex flex-col h-full bg-sidebar border-r border-sidebar-border">
      {/* Header */}
      <div className="px-4 py-3 border-b border-sidebar-border">
        <h3 className="text-xs font-semibold text-sidebar-foreground/70 uppercase tracking-wider">
          Sessions
        </h3>
      </div>

      <div className="flex-1 overflow-y-auto py-0.5">
        {/* ─── L1 ─── */}
        {l1Session && (
          <SidebarItem
            icon={Bot}
            label="L1 Orchestrator"
            active={activeSessionId === 'l1'}
            onClick={() => setActiveSession('l1')}
            indent={0}
            showDelete={false}
          />
        )}

        {/* ─── L2 groups with nested projects ─── */}
        <div className="mt-2">
          {groups.map((group) => {
            const groupSessions = sessionTree[group.name] || []
            const hasProjects = group.projects.length > 0
            const gExpanded = isGroupExp(group.name)

            return (
              <div key={group.name}>
                {/* Group header */}
                <button
                  onClick={() => toggleGroup(group.name)}
                  className="flex items-center gap-1.5 w-full px-3 py-1.5 text-[11px] font-semibold text-sidebar-foreground/50 uppercase tracking-wider hover:text-sidebar-foreground/80 transition-colors"
                >
                  {hasProjects ? (
                    gExpanded ? <ChevronDown className="h-3 w-3 shrink-0" /> : <ChevronRight className="h-3 w-3 shrink-0" />
                  ) : (
                    <span className="w-3 shrink-0" />
                  )}
                  <span className="flex-1 text-left">{group.name}</span>
                </button>

                {/* Children */}
                {gExpanded && (
                  <div>
                    {hasProjects ? (
                      <>
                        {/* Projects & their sessions */}
                        {group.projects.map((proj) => {
                          const projKey = `${group.name}:${proj.id}`
                          const projSessions = groupSessions.filter(
                            (s) => s.project_path === proj.path,
                          )
                          const pExpanded = isProjExp(projKey)

                          return (
                            <div key={proj.id}>
                              {/* Project row */}
                              <button
                                onClick={() => toggleProject(projKey)}
                                className="flex items-center gap-1.5 w-full px-5 py-1.5 text-xs text-sidebar-foreground/60 hover:text-sidebar-foreground/80 transition-colors"
                              >
                                {pExpanded ? (
                                  <ChevronDown className="h-3 w-3 shrink-0" />
                                ) : (
                                  <ChevronRight className="h-3 w-3 shrink-0" />
                                )}
                                <FolderOpen className="h-3.5 w-3.5 shrink-0" />
                                <span className="flex-1 text-left truncate">{proj.name}</span>
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    handleNewSession(group.name, proj.path)
                                  }}
                                  disabled={creating === projKey || streaming}
                                  className="p-0.5 rounded hover:bg-sidebar-accent text-sidebar-foreground/40 hover:text-sidebar-foreground transition-colors disabled:opacity-30"
                                  title={`New session in ${proj.name}`}
                                >
                                  <Plus className="h-3 w-3" />
                                </button>
                              </button>

                              {/* Sessions under this project */}
                              {pExpanded &&
                                projSessions.map((s) => (
                                  <SidebarItem
                                    key={s.id}
                                    icon={MessageSquare}
                                    label={s.name || 'New session'}
                                    active={activeSessionId === s.id}
                                    onClick={() => setActiveSession(s.id)}
                                    onDelete={(e) => handleDelete(e, s.id)}
                                    disabled={streaming}
                                    indent={2}
                                  />
                                ))}
                              {!pExpanded && projSessions.length > 0 && (
                                <div className="pl-10 py-0.5 text-[10px] text-sidebar-foreground/30">
                                  {projSessions.length} session{projSessions.length !== 1 ? 's' : ''}
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
                          <SidebarItem
                            key={s.id}
                            icon={MessageSquare}
                            label={s.name || 'New session'}
                            active={activeSessionId === s.id}
                            onClick={() => setActiveSession(s.id)}
                            onDelete={(e) => handleDelete(e, s.id)}
                            disabled={streaming}
                            indent={1}
                          />
                        ))}
                        <button
                          onClick={() => handleNewSession(group.name)}
                          disabled={creating === group.name || streaming}
                          className="flex items-center gap-1.5 w-full px-5 py-1.5 text-xs text-sidebar-foreground/50 hover:text-sidebar-foreground/80 transition-colors disabled:opacity-30"
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
    </div>
  )
}

// ─── Reusable sidebar item ─────────────────────────────────────────────────
function SidebarItem({
  icon: Icon,
  label,
  active,
  onClick,
  onDelete,
  disabled,
  indent,
  showDelete = true,
}: {
  icon: typeof Bot
  label: string
  active: boolean
  onClick: () => void
  onDelete?: (e: React.MouseEvent) => void
  disabled?: boolean
  indent: number
  showDelete?: boolean
}) {
  const pl = 12 + indent * 12
  return (
    <div className="group relative">
      <button
        onClick={onClick}
        style={{ paddingLeft: `${pl}px` }}
        className={`w-full flex items-center gap-2 pr-2 py-1.5 text-[13px] leading-tight transition-colors ${
          active
            ? 'bg-sidebar-accent text-sidebar-accent-foreground font-medium'
            : 'text-sidebar-foreground/75 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground'
        }`}
      >
        <Icon className="h-3.5 w-3.5 shrink-0 opacity-60" />
        <span className="truncate text-left">{label}</span>
      </button>
      {showDelete && onDelete && (
        <button
          onClick={onDelete}
          disabled={disabled}
          className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded opacity-0 group-hover:opacity-100 hover:bg-destructive/10 hover:text-destructive text-sidebar-foreground/30 transition-all disabled:opacity-0"
          title="Delete session"
        >
          <Trash2 className="h-3 w-3" />
        </button>
      )}
    </div>
  )
}
