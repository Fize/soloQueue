import { useEffect, useState, useCallback, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Plus,
  Trash2,
  Bot,
  FolderOpen,
  ChevronRight,
  ChevronDown,
  ChevronUp,
  Users,
  Loader2,
} from 'lucide-react'
import { useChatStore } from '@/stores/chatStore'
import { useAgentStore } from '@/stores/agentStore'
import { useConnectionStore } from '@/stores/connectionStore'
import { listL2Groups, listProjects, getTeams } from '@/lib/api'
import type { ChatSession, Project } from '@/types'
import { cn } from '@/lib/utils'
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from '@/components/ui/tooltip'
import { useTranslation } from '@/lib/i18n'

interface GroupInfo {
  name: string
  projects: Project[]
}

// pathsMatch compares two paths, handling both ~/ and expanded home directory forms.
function pathsMatch(a: string, b: string): boolean {
  if (a === b) return true
  // Normalize: strip leading ~ or /Users/xxx prefix, and trailing slash
  const norm = (p: string) => p.replace(/^~/, '').replace(/^\/Users\/[^/]+/, '').replace(/\/$/, '')
  return norm(a) === norm(b)
}

export function SessionTree() {
  const navigate = useNavigate()
  const sessions = useChatStore((s) => s.sessions)
  const sessionsLoading = useChatStore((s) => s.sessionsLoading)
  const activeSessionId = useChatStore((s) => s.activeSessionId)
  const streamingSessions = useChatStore((s) => s.streamingSessions)
  const loadSessions = useChatStore((s) => s.loadSessions)
  const createL2Session = useChatStore((s) => s.createL2Session)
  const deleteL2Session = useChatStore((s) => s.deleteL2Session)
  const setActiveSession = useChatStore((s) => s.setActiveSession)

  const fetchLiveAgents = useAgentStore((s) => s.fetchLiveAgents)
  const backendRunning = useConnectionStore((s) => s.backendStatus.running)

  const [groups, setGroups] = useState<GroupInfo[]>([])
  const [creating, setCreating] = useState<string | null>(null)
  // All groups expanded by default; projects collapsed by default.
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const [expandedProjects, setExpandedProjects] = useState<Record<string, boolean>>({})
  // Per-container "show all" disclosure for session lists > 5 items.
  const [expandedSessionLists, setExpandedSessionLists] = useState<Record<string, boolean>>({})
  const focusedSessionPathRef = useRef<string | null>(null)

  const { t } = useTranslation()

  // VISIBLE_SESSION_COUNT: max sessions shown before the "Show N more" affordance appears.
  const VISIBLE_SESSION_COUNT = 5
  const sessionListKey = useCallback(
    (groupName: string, projectId?: string) =>
      `${groupName}::${projectId || '__group__'}`,
    []
  )
  const isListExpanded = useCallback(
    (key: string) => expandedSessionLists[key] === true,
    [expandedSessionLists]
  )
  const toggleList = useCallback((key: string) => {
    setExpandedSessionLists((prev) => ({ ...prev, [key]: !prev[key] }))
  }, [])

  // Refetch when the backend transitions to running. The initial run fires
  // the three loaders in parallel; if the backend isn't ready yet they all
  // fail silently. When the IPC `getBackendStatus()` finally returns
  // `running: true`, this effect re-runs and picks up the data — including
  // loadGroupInfo, which is a local function the App-level auto-retry
  // cannot reach (it's not in a store).
  useEffect(() => {
    loadSessions()
    loadGroupInfo()
    fetchLiveAgents()
  }, [backendRunning])

  // On active session change: open the active group+project, collapse all other
  // groups and projects. Preserves the active session's own "show all" flag.
  useEffect(() => {
    if (!activeSessionId) {
      focusedSessionPathRef.current = null
      return
    }
    if (sessions.length === 0 || groups.length === 0) return
    const s = sessions.find((x) => x.id === activeSessionId)
    if (!s) return
    const activeGroupName = s?.group
    const activeProjectId = s?.project_path
      ? groups
          .flatMap((g) => g.projects)
          .find((p) => pathsMatch(s.project_path!, p.path))?.id
      : undefined
    const focusedPath = `${activeSessionId}:${activeGroupName || ''}:${activeProjectId || ''}`
    if (focusedSessionPathRef.current === focusedPath) return
    focusedSessionPathRef.current = focusedPath

    // Merge-update groups: active → true, others → false.
    setExpandedGroups((prev) => {
      let changed = false
      const next = { ...prev }
      for (const g of groups) {
        const target = g.name === activeGroupName
        if (next[g.name] !== target) {
          next[g.name] = target
          changed = true
        }
      }
      return changed ? next : prev
    })
    // Merge-update projects: active project → true, others → false.
    setExpandedProjects((prev) => {
      let changed = false
      const next = { ...prev }
      for (const g of groups) {
        for (const p of g.projects) {
          const target =
            activeProjectId != null
              ? p.id === activeProjectId && g.name === activeGroupName
              : false
          const k = `${g.name}:${p.id}`
          if (next[k] !== target) {
            next[k] = target
            changed = true
          }
        }
      }
      return changed ? next : prev
    })
    // Scroll after state settles (next frame)
    requestAnimationFrame(() => {
      const el = document.querySelector(`[data-session-id="${activeSessionId}"]`)
      if (el) {
        el.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
      }
    })
  }, [activeSessionId, sessions, groups])

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

  const l2Sessions = sessions.filter((s) => s.type === 'l2')

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
      if (streamingSessions[id]) return
      await deleteL2Session(id)
      if (activeSessionId === id) {
        const remaining = sessions.filter((s) => s.id !== id && s.type === 'l2')
        if (remaining.length > 0) {
          const sorted = [...remaining].sort((a, b) => {
            const timeA = a.createdAt || (a as any).created_at || ''
            const timeB = b.createdAt || (b as any).created_at || ''
            return timeB.localeCompare(timeA)
          })
          navigate(`/chat/${sorted[0].id}`)
        } else {
          navigate('/chat')
        }
      }
    },
    [streamingSessions, deleteL2Session, activeSessionId, sessions, navigate]
  )

  const toggleGroup = (name: string) =>
    setExpandedGroups((prev) => ({ ...prev, [name]: prev[name] === false }))

  const toggleProject = (key: string) =>
    setExpandedProjects((prev) => ({ ...prev, [key]: !prev[key] }))

  const isGroupExp = (name: string) => expandedGroups[name] !== false
  const isProjExp = (key: string) => expandedProjects[key] === true

  // Empty-tree loading state: visible when the first load is still in flight
  // AND we don't have any sessions to show yet. Shown only on the very first
  // mount — once the user has data, a refresh doesn't show this placeholder
  // (the Sidebar button spinner handles that case at the toggle level).
  if (sessionsLoading && sessions.length === 0 && groups.length === 0) {
    return (
      <div
        className="flex items-center gap-2 pl-3 pr-2 py-2 text-[11px] text-muted-foreground/70"
        role="status"
        aria-live="polite"
      >
        <Loader2 className="h-3.5 w-3.5 animate-spin text-signal/70 shrink-0" />
        <span>{t('sessionTree.loading')}</span>
      </div>
    )
  }

  // Stale-empty state: the initial load finished but produced no groups AND
  // the backend isn't running. Without this, the user is left staring at an
  // empty tree with no explanation. (Backend is up but legitimately empty
  // groups are rare and would be visible on the next refresh anyway.)
  if (!sessionsLoading && groups.length === 0 && sessions.length === 0 && !backendRunning) {
    return (
      <div
        className="flex flex-col gap-1.5 pl-3 pr-2 py-2 text-[11px] text-muted-foreground/70"
        role="status"
      >
        <div className="flex items-center gap-1.5">
          <span className="h-1.5 w-1.5 rounded-full bg-amber-500 shrink-0" />
          <span>Backend not connected</span>
        </div>
        <button
          type="button"
          onClick={() => {
            loadSessions()
            loadGroupInfo()
          }}
          className="self-start text-foreground/80 hover:text-foreground underline-offset-2 hover:underline cursor-pointer"
        >
          Retry
        </button>
      </div>
    )
  }

  return (
    <div className="flex flex-col py-1 space-y-1 select-none">
      {/* ─── L2 groups with nested projects ─── */}
      <div className="space-y-2">
        {groups.map((group) => {
          const groupSessions = sessionTree[group.name] || []
          const hasProjects = group.projects.length > 0
          const gExpanded = isGroupExp(group.name)

          return (
            <div key={group.name} className="space-y-0.5">
              {/* Group header */}
              <div className="flex items-center group/header w-full pl-3 pr-2 py-1 rounded-md hover:bg-muted/20 transition-colors">
                <button
                  onClick={() => toggleGroup(group.name)}
                  className="flex-1 flex items-center gap-2 text-[10px] font-bold text-muted-foreground/60 uppercase tracking-wider hover:text-foreground cursor-pointer text-left"
                >
                  {hasProjects || groupSessions.length > 0 ? (
                    gExpanded ? (
                      <ChevronDown className="h-3.5 w-3.5 shrink-0 transition-transform duration-200" />
                    ) : (
                      <ChevronRight className="h-3.5 w-3.5 shrink-0 transition-transform duration-200" />
                    )
                  ) : (
                    <span className="w-3.5 shrink-0" />
                  )}
                  <Users className="h-3.5 w-3.5 shrink-0 opacity-70" />
                  <span className="flex-1 text-left truncate">{group.name || 'UNGROUPED'}</span>
                </button>
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    handleNewSession(group.name)
                  }}
                  disabled={creating === group.name}
                  className="p-0.5 rounded hover:bg-muted-foreground/20 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-30 cursor-pointer opacity-0 group-hover/header:opacity-100"
                  title={`New chat in ${group.name || 'this group'}`}
                >
                  <Plus className="h-3 w-3" />
                </button>
              </div>

              {/* Children */}
              {gExpanded && (
                <div className="space-y-0.5 mt-0.5">
                  {hasProjects ? (
                    <>
                      {/* Projects & their sessions */}
                      {group.projects.map((proj) => {
                        const projKey = `${group.name}:${proj.id}`
                        const projSessions = groupSessions
                          .filter((s) => s.project_path && pathsMatch(s.project_path, proj.path))
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
                              className="group/proj flex items-center gap-2 w-full pl-[28px] pr-2 py-1 text-xs text-muted-foreground hover:text-foreground hover:bg-muted/20 rounded-md transition-colors cursor-pointer relative"
                            >
                              {projSessions.length > 0 ? (
                                pExpanded ? (
                                  <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70 transition-transform duration-200" />
                                ) : (
                                  <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70 transition-transform duration-200" />
                                )
                              ) : (
                                <span className="w-3.5 shrink-0" />
                              )}
                              <FolderOpen className="h-3.5 w-3.5 shrink-0 opacity-70 text-muted-foreground/60" />
                              <span className="flex-1 text-left truncate font-medium">{proj.name}</span>
                              <button
                                onClick={(e) => {
                                  e.stopPropagation()
                                  handleNewSession(group.name, proj.path)
                                }}
                                disabled={creating === projKey}
                                className="p-0.5 rounded hover:bg-muted-foreground/20 text-muted-foreground hover:text-foreground transition-colors disabled:opacity-30 cursor-pointer opacity-0 group-hover/proj:opacity-100"
                                title={`New chat in ${proj.name}`}
                              >
                                <Plus className="h-3 w-3" />
                              </button>
                            </div>

                            {/* Sessions under this project */}
                            {pExpanded && projSessions.length > 0 && (() => {
                              const listKey = sessionListKey(group.name, proj.id)
                              const visible = projSessions.slice(0, VISIBLE_SESSION_COUNT)
                              const hidden = projSessions.slice(VISIBLE_SESSION_COUNT)
                              const listExpanded = isListExpanded(listKey)
                              return (
                                <div className="space-y-0.5 mt-0.5">
                                  {visible.map((s) => (
                                    <TreeItem
                                      key={s.id}
                                      sessionId={s.id}
                                      label={s.name || 'New chat'}
                                      isPast={s.name ? s.name.startsWith('Past') : false}
                                      active={activeSessionId === s.id}
                                      onClick={() => {
                                        setActiveSession(s.id)
                                        navigate(`/chat/${s.id}`)
                                      }}
                                      onDelete={(e) => handleDelete(e, s.id)}
                                      disabled={!!streamingSessions[s.id]}
                                      indent={2}
                                    />
                                  ))}
                                  {listExpanded && hidden.map((s) => (
                                    <TreeItem
                                      key={s.id}
                                      sessionId={s.id}
                                      label={s.name || 'New chat'}
                                      isPast={s.name ? s.name.startsWith('Past') : false}
                                      active={activeSessionId === s.id}
                                      onClick={() => {
                                        setActiveSession(s.id)
                                        navigate(`/chat/${s.id}`)
                                      }}
                                      onDelete={(e) => handleDelete(e, s.id)}
                                      disabled={!!streamingSessions[s.id]}
                                      indent={2}
                                    />
                                  ))}
                                  {hidden.length > 0 && (
                                    <button
                                      type="button"
                                      onClick={() => toggleList(listKey)}
                                      aria-expanded={listExpanded}
                                      aria-label={listExpanded
                                        ? t('sessionTree.showLess')
                                        : t('sessionTree.showMore', { count: hidden.length })}
                                      className="w-full flex items-center gap-1.5 pl-[44px] pr-3 py-1 rounded-md text-[11px] text-muted-foreground/60 hover:text-foreground hover:bg-muted/20 transition-colors cursor-pointer"
                                    >
                                      {listExpanded
                                        ? <ChevronUp className="h-3 w-3 shrink-0" />
                                        : <ChevronDown className="h-3 w-3 shrink-0" />}
                                      <span>
                                        {listExpanded
                                          ? t('sessionTree.showLess')
                                          : t('sessionTree.showMore', { count: hidden.length })}
                                      </span>
                                    </button>
                                  )}
                                </div>
                              )
                            })()}
                          </div>
                        )
                      })}
                      {/* Sessions without a matching project path (unassociated) */}
                      {(() => {
                        const unassociated = groupSessions
                          .filter((s) => !s.project_path || !group.projects.some(p => pathsMatch(s.project_path!, p.path)))
                          .sort((a, b) => {
                            const timeA = a.createdAt || (a as any).created_at || ''
                            const timeB = b.createdAt || (b as any).created_at || ''
                            return timeB.localeCompare(timeA)
                          })
                        if (unassociated.length === 0) return null
                        const listKey = sessionListKey(group.name)
                        const visible = unassociated.slice(0, VISIBLE_SESSION_COUNT)
                        const hidden = unassociated.slice(VISIBLE_SESSION_COUNT)
                        const listExpanded = isListExpanded(listKey)
                        return (
                          <div className="space-y-0.5 mt-0.5">
                            {visible.map((s) => (
                              <TreeItem
                                key={s.id}
                                sessionId={s.id}
                                label={s.name || 'New chat'}
                                isPast={s.name ? s.name.startsWith('Past') : false}
                                active={activeSessionId === s.id}
                                onClick={() => {
                                  setActiveSession(s.id)
                                  navigate(`/chat/${s.id}`)
                                }}
                                onDelete={(e) => handleDelete(e, s.id)}
                                disabled={!!streamingSessions[s.id]}
                                indent={1}
                              />
                            ))}
                            {listExpanded && hidden.map((s) => (
                              <TreeItem
                                key={s.id}
                                sessionId={s.id}
                                label={s.name || 'New chat'}
                                isPast={s.name ? s.name.startsWith('Past') : false}
                                active={activeSessionId === s.id}
                                onClick={() => {
                                  setActiveSession(s.id)
                                  navigate(`/chat/${s.id}`)
                                }}
                                onDelete={(e) => handleDelete(e, s.id)}
                                disabled={!!streamingSessions[s.id]}
                                indent={1}
                              />
                            ))}
                            {hidden.length > 0 && (
                              <button
                                type="button"
                                onClick={() => toggleList(listKey)}
                                aria-expanded={listExpanded}
                                aria-label={listExpanded
                                  ? t('sessionTree.showLess')
                                  : t('sessionTree.showMore', { count: hidden.length })}
                                className="w-full flex items-center gap-1.5 pl-[28px] pr-3 py-1 rounded-md text-[11px] text-muted-foreground/60 hover:text-foreground hover:bg-muted/20 transition-colors cursor-pointer"
                              >
                                {listExpanded
                                  ? <ChevronUp className="h-3 w-3 shrink-0" />
                                  : <ChevronDown className="h-3 w-3 shrink-0" />}
                                <span>
                                  {listExpanded
                                    ? t('sessionTree.showLess')
                                    : t('sessionTree.showMore', { count: hidden.length })}
                                </span>
                              </button>
                            )}
                          </div>
                        )
                      })()}
                    </>
                  ) : (
                    <>
                      {/* No projects: show sessions directly */}
                      {(() => {
                        const listKey = sessionListKey(group.name)
                        const visible = groupSessions.slice(0, VISIBLE_SESSION_COUNT)
                        const hidden = groupSessions.slice(VISIBLE_SESSION_COUNT)
                        const listExpanded = isListExpanded(listKey)
                        return (
                          <div className="space-y-0.5 mt-0.5">
                            {visible.map((s) => (
                              <TreeItem
                                key={s.id}
                                sessionId={s.id}
                                label={s.name || 'New chat'}
                                isPast={s.name ? s.name.startsWith('Past') : false}
                                active={activeSessionId === s.id}
                                onClick={() => {
                                  setActiveSession(s.id)
                                  navigate(`/chat/${s.id}`)
                                }}
                                onDelete={(e) => handleDelete(e, s.id)}
                                disabled={!!streamingSessions[s.id]}
                                indent={1}
                              />
                            ))}
                            {listExpanded && hidden.map((s) => (
                              <TreeItem
                                key={s.id}
                                sessionId={s.id}
                                label={s.name || 'New chat'}
                                isPast={s.name ? s.name.startsWith('Past') : false}
                                active={activeSessionId === s.id}
                                onClick={() => {
                                  setActiveSession(s.id)
                                  navigate(`/chat/${s.id}`)
                                }}
                                onDelete={(e) => handleDelete(e, s.id)}
                                disabled={!!streamingSessions[s.id]}
                                indent={1}
                              />
                            ))}
                            {hidden.length > 0 && (
                              <button
                                type="button"
                                onClick={() => toggleList(listKey)}
                                aria-expanded={listExpanded}
                                aria-label={listExpanded
                                  ? t('sessionTree.showLess')
                                  : t('sessionTree.showMore', { count: hidden.length })}
                                className="w-full flex items-center gap-1.5 pl-[28px] pr-3 py-1 rounded-md text-[11px] text-muted-foreground/60 hover:text-foreground hover:bg-muted/20 transition-colors cursor-pointer"
                              >
                                {listExpanded
                                  ? <ChevronUp className="h-3 w-3 shrink-0" />
                                  : <ChevronDown className="h-3 w-3 shrink-0" />}
                                <span>
                                  {listExpanded
                                    ? t('sessionTree.showLess')
                                    : t('sessionTree.showMore', { count: hidden.length })}
                                </span>
                              </button>
                            )}
                          </div>
                        )
                      })()}
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
  sessionId,
  errorCount,
}: {
  icon?: typeof Bot
  label: string
  active: boolean
  onClick: () => void
  onDelete?: (e: React.MouseEvent) => void
  disabled?: boolean
  indent: number
  showDelete?: boolean
  isPast?: boolean
  state?: string
  sessionId?: string
  errorCount?: number
}) {
  const pl = 28 + (indent - 1) * 16 // indent=1→28px, indent=2→44px
  return (
    <div className="group relative">
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger className="w-full text-left border-none bg-transparent p-0 block cursor-pointer">
            <div
              onClick={onClick}
              data-session-id={sessionId}
              style={{ paddingLeft: `${pl}px` }}
              className={`w-full flex items-center gap-2 pr-8 py-1.5 rounded-md text-xs leading-tight transition-all cursor-pointer ${
                active
                  ? 'bg-primary/10 text-primary dark:bg-primary/20 dark:text-primary-foreground shadow-xs font-semibold'
                  : 'text-muted-foreground hover:bg-muted/20 hover:text-foreground'
              }`}
            >
              <div className="relative flex items-center justify-center shrink-0">
                {Icon && <Icon className="h-3.5 w-3.5 opacity-70" />}
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
                {errorCount != null && errorCount > 0 && (
                  <span className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 rounded-full bg-[var(--destructive)] border border-card" />
                )}
              </div>
              <span className="truncate text-left flex-1 font-medium">
                {label}
                {isPast && (
                  <span className="ml-1.5 align-middle inline-block px-1.5 py-px rounded text-[9px] font-medium bg-amber-500/10 text-amber-600/60">
                    Past
                  </span>
                )}
              </span>
            </div>
          </TooltipTrigger>
          <TooltipContent side="right" sideOffset={12} className="max-w-[280px] break-all z-50">
            {label}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      {showDelete && onDelete && (
        <button
          onClick={onDelete}
          disabled={disabled}
          className="absolute right-1.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-destructive/10 hover:text-destructive text-muted-foreground/30 opacity-0 group-hover:opacity-100 transition-all cursor-pointer disabled:opacity-0 z-10"
          title="Delete chat"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  )
}
