import { useState, useEffect, useMemo, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAgentStore } from '@/stores/agentStore'
import { useIsMobile } from '@/hooks/useMediaQuery'
import { AgentCard } from './AgentCard'
import { Badge } from '@/components/ui/badge'
import { StatusBadge, StatusDot } from '@/components/ui/status-badge'
import { GlassCard } from '@/components/ui/glass-card'
import type { AgentInfo, AgentTemplate, TeamInfo } from '@/types'
import {
  Search,
  Star,
  FolderOpen,
  ChevronDown,
  ChevronRight,
  Loader2,
  AlertCircle,
  Mail,
  Bot,
  Users,
} from 'lucide-react'

interface AgentWithTemplate {
  agents: AgentInfo[]
  template: AgentTemplate | null
}

interface TeamNode {
  team: TeamInfo
  agents: AgentWithTemplate[]
}

export function AgentListPage() {
  const navigate = useNavigate()
  const isMobile = useIsMobile()

  // Store data
  const data = useAgentStore((state) => state.agents)
  const teamsData = useAgentStore((state) => state.teams)
  const fetchTeams = useAgentStore((state) => state.fetchTeams)
  const teamsLoading = useAgentStore((state) => state.teamsLoading)

  // UI state
  const [searchQuery, setSearchQuery] = useState('')
  const [activeFilter, setActiveFilter] = useState<
    'all' | 'processing' | 'idle' | 'stopped' | 'errors'
  >('all')
  const [collapsedTeams, setCollapsedTeams] = useState<Set<string>>(new Set())
  const teamsInitializedRef = useRef(false)

  // Load teams
  useEffect(() => {
    fetchTeams()
  }, [fetchTeams])

  // L1 Agent and Team Grouping
  const { l1Agent, teamNodes } = useMemo(() => {
    if (!data) return { l1Agent: null as AgentInfo | null, teamNodes: [] as TeamNode[] }

    const { agents, supervisors } = data
    const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
    const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))

    // L1 is the agent not supervised by anyone
    const l1Agent =
      agents.find((a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)) || null

    const nodes: TeamNode[] = []
    const sortedTeams = teamsData
      ? [...teamsData.teams].sort((a, b) => a.name.localeCompare(b.name))
      : []

    for (const team of sortedTeams) {
      const teamAgents: AgentWithTemplate[] = []

      for (const template of team.agents) {
        const matchingAgents = agents.filter((a) => a.id === template.id)
        teamAgents.push({ agents: matchingAgents, template })
      }

      // Leaders first
      teamAgents.sort((a, b) => {
        if (a.template?.is_leader && !b.template?.is_leader) return -1
        if (!a.template?.is_leader && b.template?.is_leader) return 1
        return 0
      })

      nodes.push({ team, agents: teamAgents })
    }

    return { l1Agent, teamNodes: nodes }
  }, [data, teamsData])

  // Collapsible teams auto-expand with processing agents
  useEffect(() => {
    if (!data || !teamsData) return

    const sortedTeams = [...teamsData.teams].sort((a, b) => a.name.localeCompare(b.name))

    if (!teamsInitializedRef.current) {
      const allTeams = new Set(sortedTeams.map((t) => t.name))
      setCollapsedTeams(allTeams)
      teamsInitializedRef.current = true
    }

    const { agents } = data
    const teamsToExpand = new Set<string>()

    for (const agent of agents) {
      if (agent.state === 'processing') {
        for (const team of sortedTeams) {
          if (team.agents.some((t) => t.id === agent.id)) {
            teamsToExpand.add(team.name)
            break
          }
        }
      }
    }

    if (teamsToExpand.size > 0) {
      setCollapsedTeams((prev) => {
        const next = new Set(prev)
        for (const t of teamsToExpand) next.delete(t)
        return next
      })
    }
  }, [data, teamsData])

  const toggleTeam = (teamName: string) => {
    setCollapsedTeams((prev) => {
      const next = new Set(prev)
      if (next.has(teamName)) {
        next.delete(teamName)
      } else {
        next.add(teamName)
      }
      return next
    })
  }

  // Filter & Search Logic
  const filteredTeamNodes = useMemo(() => {
    return teamNodes
      .map((node) => {
        const filteredAgents = node.agents.filter((item) => {
          // 1. Search Query Filter
          const query = searchQuery.toLowerCase()
          const matchesSearch =
            searchQuery === '' ||
            (item.template?.name && item.template.name.toLowerCase().includes(query)) ||
            item.agents.some(
              (a) =>
                a.name.toLowerCase().includes(query) || a.model_id.toLowerCase().includes(query)
            )

          if (!matchesSearch) return false

          // 2. Status Tab Filter
          if (activeFilter === 'all') return true

          // If no running instance, it matches 'stopped' filter
          if (item.agents.length === 0) {
            return activeFilter === 'stopped'
          }

          // Otherwise, filter by actual running instance states
          return item.agents.some((a) => {
            if (activeFilter === 'errors') return a.error_count > 0
            return a.state === activeFilter
          })
        })

        return {
          ...node,
          agents: filteredAgents,
        }
      })
      .filter((node) => node.agents.length > 0 || searchQuery === '') // Hide empty teams during search/filter
  }, [teamNodes, searchQuery, activeFilter])

  // Check if L1 Agent matches filters
  const matchesL1Filter = useMemo(() => {
    // Search filter
    const query = searchQuery.toLowerCase()
    const matchesSearch =
      searchQuery === '' ||
      (l1Agent && l1Agent.name.toLowerCase().includes(query)) ||
      (l1Agent && l1Agent.model_id.toLowerCase().includes(query)) ||
      (!l1Agent && 'l1 agent'.includes(query))

    if (!matchesSearch) return false

    // Status filter
    if (activeFilter === 'all') return true
    if (!l1Agent) return activeFilter === 'stopped' // Placeholder is stopped

    if (activeFilter === 'errors') return l1Agent.error_count > 0
    return l1Agent.state === activeFilter
  }, [l1Agent, searchQuery, activeFilter])

  // Count Stats
  const stats = useMemo(() => {
    if (!data) return { total: 0, active: 0, idle: 0, errors: 0 }
    const { agents } = data
    return {
      total: agents.length,
      active: agents.filter((a) => a.state === 'processing').length,
      idle: agents.filter((a) => a.state === 'idle').length,
      errors: agents.filter((a) => a.error_count > 0).length,
    }
  }, [data])

  if (!data && teamsLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="flex flex-col items-center gap-2 text-muted-foreground text-sm">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
          <span>Loading agents...</span>
        </div>
      </div>
    )
  }

  return (
    <div className="flex-1 space-y-6 px-4 py-6 md:px-8 md:py-8 max-w-6xl mx-auto pb-20 md:pb-8">
      {/* Page Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Bot className="h-6 w-6 text-primary" />
            Obsidian Console
          </h1>
          <p className="text-sm text-muted-foreground">Monitor and manage active team workflows</p>
        </div>

        {/* Search */}
        <div className="relative w-full sm:max-w-xs">
          <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search agents..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="w-full h-9 pl-9 pr-4 text-sm bg-muted/40 border border-border/80 rounded-lg outline-none focus:border-primary/55 focus:bg-muted/10 transition-all placeholder:text-muted-foreground"
          />
        </div>
      </div>

      {/* Filter Tabs */}
      <div className="flex border-b border-border/60 overflow-x-auto no-scrollbar gap-2 scroll-smooth py-1">
        {(['all', 'processing', 'idle', 'stopped', 'errors'] as const).map((filter) => {
          const isActive = activeFilter === filter
          const countMap = {
            all: stats.total,
            processing: stats.active,
            idle: stats.idle,
            stopped: stats.total - stats.active - stats.idle,
            errors: stats.errors,
          }
          return (
            <button
              key={filter}
              onClick={() => setActiveFilter(filter)}
              className={`px-3 py-1.5 text-xs font-semibold rounded-md border transition-all duration-200 capitalize whitespace-nowrap ${
                isActive
                  ? 'bg-primary text-primary-foreground border-primary shadow-sm'
                  : 'bg-card/40 border-border/80 text-muted-foreground hover:text-foreground hover:bg-muted/40'
              }`}
            >
              {filter === 'processing' ? 'running' : filter}
              <span
                className={`ml-1.5 px-1 py-0.25 text-[9px] rounded-full font-bold ${
                  isActive
                    ? 'bg-primary-foreground/20 text-primary-foreground'
                    : 'bg-muted text-muted-foreground'
                }`}
              >
                {countMap[filter]}
              </span>
            </button>
          )
        })}
      </div>

      {/* Main Content Area */}
      <div className="space-y-6">
        {/* L1 Agent Section */}
        {matchesL1Filter && (
          <div className="space-y-3">
            <h2 className="text-xs font-bold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
              <Star className="h-3.5 w-3.5 fill-primary text-primary" />
              L1 Root Coordinator
            </h2>
            {l1Agent ? (
              <div className="max-w-md">
                <AgentCard
                  agent={l1Agent}
                  onClick={() => navigate(`/agents/${l1Agent.instance_id}`)}
                />
              </div>
            ) : (
              <div className="max-w-md">
                <PlaceholderCard
                  name="L1 Agent"
                  isLeader
                  onClick={() => navigate('/agents/main')}
                />
              </div>
            )}
          </div>
        )}

        {/* Collapsible Teams */}
        <div className="space-y-4">
          {filteredTeamNodes.map((node) => {
            const isCollapsed = collapsedTeams.has(node.team.name)
            const activeInstances = node.agents.filter((a) => a.agents.length > 0).length
            const totalTeamAgents = node.team.agents.length

            return (
              <GlassCard
                key={node.team.name}
                variant="flat"
                size="none"
                className="overflow-hidden border border-border/80 bg-card/60 backdrop-blur-sm"
              >
                {/* Team Header */}
                <button
                  onClick={() => toggleTeam(node.team.name)}
                  className="flex w-full items-center gap-3 px-4 py-3.5 text-left border-b border-border/60 hover:bg-muted/20 active:bg-muted/40 transition-colors"
                >
                  <FolderOpen className="h-4.5 w-4.5 text-primary shrink-0" />
                  <span className="font-semibold text-foreground text-sm flex-1">
                    {node.team.name}
                  </span>

                  <div className="flex items-center gap-2">
                    <Badge
                      variant="outline"
                      className="text-[10px] font-medium text-muted-foreground gap-1.5 py-0 px-2"
                    >
                      <Users className="h-3 w-3" />
                      {activeInstances}/{totalTeamAgents}
                    </Badge>
                    {isCollapsed ? (
                      <ChevronRight className="h-4 w-4 text-muted-foreground" />
                    ) : (
                      <ChevronDown className="h-4 w-4 text-muted-foreground" />
                    )}
                  </div>
                </button>

                {/* Team Content */}
                {!isCollapsed && (
                  <div className="p-3">
                    {isMobile ? (
                      /* Mobile Layout: Stacked Cards */
                      <div className="grid grid-cols-1 gap-2.5">
                        {node.agents.map((item) => {
                          const key = item.template?.id || 'unknown'
                          if (item.agents.length > 0) {
                            return item.agents.map((a) => (
                              <AgentCard
                                key={a.instance_id}
                                agent={a}
                                onClick={() => navigate(`/agents/${a.instance_id}`)}
                              />
                            ))
                          }
                          return (
                            <PlaceholderCard
                              key={key}
                              name={item.template?.name}
                              isLeader={item.template?.is_leader}
                              onClick={
                                item.template
                                  ? () => navigate(`/agents/${item.template!.id}`)
                                  : undefined
                              }
                            />
                          )
                        })}
                      </div>
                    ) : (
                      /* Desktop Layout: Sleek Grid/Table */
                      <div className="overflow-x-auto">
                        <table className="w-full text-left border-collapse text-sm">
                          <thead>
                            <tr className="border-b border-border/40 text-muted-foreground font-medium text-xs">
                              <th className="py-2.5 px-3">Agent</th>
                              <th className="py-2.5 px-3">Model</th>
                              <th className="py-2.5 px-3">Status</th>
                              <th className="py-2.5 px-3">Task Level</th>
                              <th className="py-2.5 px-3">Mailbox</th>
                              <th className="py-2.5 px-3 text-right">Errors</th>
                            </tr>
                          </thead>
                          <tbody>
                            {node.agents.map((item) => {
                              const key = item.template?.id || 'unknown'
                              if (item.agents.length > 0) {
                                return item.agents.map((a) => {
                                  const hasMail = a.mailbox_high > 0 || a.mailbox_normal > 0
                                  return (
                                    <tr
                                      key={a.instance_id}
                                      onClick={() => navigate(`/agents/${a.instance_id}`)}
                                      className="border-b border-border/20 last:border-0 hover:bg-muted/10 cursor-pointer transition-colors group"
                                    >
                                      <td className="py-3 px-3">
                                        <div className="flex items-center gap-2.5 min-w-0">
                                          <StatusDot state={a.state} />
                                          <span className="font-semibold text-foreground truncate group-hover:text-primary transition-colors">
                                            {a.name}
                                          </span>
                                          {a.is_leader && (
                                            <Badge
                                              variant="primary"
                                              className="text-[9px] uppercase tracking-wider py-0 px-1.5"
                                            >
                                              Leader
                                            </Badge>
                                          )}
                                        </div>
                                      </td>
                                      <td className="py-3 px-3 font-mono text-xs text-muted-foreground truncate max-w-[150px]">
                                        {a.model_id}
                                      </td>
                                      <td className="py-3 px-3">
                                        <StatusBadge state={a.state} size="sm" />
                                      </td>
                                      <td className="py-3 px-3 text-muted-foreground font-medium text-xs">
                                        {a.task_level ? `Level ${a.task_level}` : '-'}
                                      </td>
                                      <td className="py-3 px-3">
                                        {hasMail ? (
                                          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
                                            <Mail className="h-3.5 w-3.5" />
                                            <span className="font-mono">
                                              {a.mailbox_high > 0 ? `${a.mailbox_high}H/` : ''}
                                              {a.mailbox_normal}N
                                            </span>
                                          </div>
                                        ) : (
                                          <span className="text-muted-foreground/45">-</span>
                                        )}
                                      </td>
                                      <td className="py-3 px-3 text-right">
                                        {a.error_count > 0 ? (
                                          <Badge
                                            variant="destructive"
                                            className="font-bold text-[10px] py-0 px-2 gap-1"
                                          >
                                            <AlertCircle className="h-3 w-3" />
                                            {a.error_count}
                                          </Badge>
                                        ) : (
                                          <span className="text-muted-foreground/45">-</span>
                                        )}
                                      </td>
                                    </tr>
                                  )
                                })
                              }
                              return (
                                <tr
                                  key={key}
                                  onClick={
                                    item.template
                                      ? () => navigate(`/agents/${item.template!.id}`)
                                      : undefined
                                  }
                                  className="border-b border-border/20 last:border-0 hover:bg-muted/10 cursor-pointer transition-colors opacity-55 text-muted-foreground"
                                >
                                  <td className="py-3 px-3" colSpan={6}>
                                    <div className="flex items-center gap-2">
                                      <div className="h-2.5 w-2.5 rounded-full border border-dashed border-border-light shrink-0" />
                                      <span className="font-medium text-xs">
                                        {item.template?.name || 'Unassigned placeholder'}
                                      </span>
                                      {item.template?.is_leader && (
                                        <Badge
                                          variant="outline"
                                          className="text-[9px] uppercase tracking-wider py-0 px-1 text-muted-foreground/60 border-dashed"
                                        >
                                          Leader
                                        </Badge>
                                      )}
                                      <span className="text-[10px] italic ml-auto text-muted-foreground/60">
                                        Offline (Click to configure)
                                      </span>
                                    </div>
                                  </td>
                                </tr>
                              )
                            })}
                          </tbody>
                        </table>
                      </div>
                    )}
                  </div>
                )}
              </GlassCard>
            )
          })}
        </div>

        {/* Empty State */}
        {filteredTeamNodes.length === 0 && !matchesL1Filter && (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <Bot className="h-10 w-10 text-muted-foreground/50 mb-3" />
            <p className="font-semibold text-foreground text-sm">No agents match filters</p>
            <p className="text-xs text-muted-foreground mt-1">
              Try adjusting your search query or status filter.
            </p>
          </div>
        )}
      </div>
    </div>
  )
}

// Local PlaceholderCard helper component for mobile/L1 views
function PlaceholderCard({
  name,
  isLeader,
  onClick,
}: {
  name?: string | null
  isLeader?: boolean
  onClick?: () => void
}) {
  return (
    <GlassCard
      variant="ghost"
      interactive={!!onClick}
      onClick={onClick}
      className="group relative select-none border-dashed border-border/60 hover:border-primary/40 hover:bg-muted/10"
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-muted-foreground">
          <div className="h-2 w-2 rounded-full border border-dashed border-muted-foreground/50" />
          <span className="text-xs font-semibold group-hover:text-primary transition-colors">
            {name || 'Unassigned'}
          </span>
          {isLeader && (
            <Badge
              variant="outline"
              className="text-[9px] uppercase tracking-wider py-0 px-1 border-dashed text-muted-foreground/60"
            >
              Leader
            </Badge>
          )}
        </div>
        <span className="text-[10px] text-muted-foreground/60 group-hover:text-primary transition-colors">
          Offline
        </span>
      </div>
    </GlassCard>
  )
}
