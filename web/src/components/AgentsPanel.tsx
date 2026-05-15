import { useState, useCallback, useMemo, useRef, useEffect } from 'react'
import { useAgents } from '@/hooks/useAgents'
import { useTeams } from '@/hooks/useTeams'
import { AgentCard } from './AgentCard'
import { Badge } from '@/components/ui/badge'
import type { AgentInfo, AgentTemplate, TeamInfo } from '@/types'
import { Users, ChevronRight, ChevronDown, Star, Circle, FolderOpen } from 'lucide-react'
import { AgentDetailDialog } from './AgentDetailDialog'

interface AgentWithTemplate {
  agents: AgentInfo[]
  template: AgentTemplate | null
}

interface TeamNode {
  team: TeamInfo
  agents: AgentWithTemplate[]
}

// 占位组件
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
    <div
      className="rounded-lg border-2 border-dashed border-border/50 bg-muted/30 p-3 cursor-pointer hover:bg-muted/50 transition-colors"
      onClick={onClick}
    >
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2 text-muted-foreground">
          <Circle className="h-3 w-3" />
          <span className="text-xs font-medium">{name || 'Unassigned'}</span>
        </div>
        <div className="flex items-center gap-1.5">
          {isLeader && (
            <Badge variant="default" className="text-[10px]">
              Leader
            </Badge>
          )}
          <span className="text-[10px] text-muted-foreground bg-muted px-1.5 py-0.5 rounded uppercase">
            Idle
          </span>
        </div>
      </div>
    </div>
  )
}

export function AgentsPanel() {
  const data = useAgents()
  const teamsData = useTeams()
  const [collapsedTeams, setCollapsedTeams] = useState<Set<string>>(new Set())
  const [selectedAgent, setSelectedAgent] = useState<AgentInfo | null>(null)
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null)
  const [selectedTemplateName, setSelectedTemplateName] = useState<string | null>(null)
  const [isDetailOpen, setIsDetailOpen] = useState(false)
  const [isL1, setIsL1] = useState(false)

  const toggleTeam = useCallback((team: string) => {
    setCollapsedTeams((prev) => {
      const next = new Set(prev)
      if (next.has(team)) {
        next.delete(team)
      } else {
        next.add(team)
      }
      return next
    })
  }, [])

  const handleAgentClick = useCallback((agent: AgentInfo, l1Flag: boolean = false) => {
    setSelectedAgent(agent)
    setSelectedTemplateId(null)
    setSelectedTemplateName(null)
    setIsL1(l1Flag)
    setIsDetailOpen(true)
  }, [])

  const handlePlaceholderClick = useCallback((tmpl: AgentTemplate) => {
    setSelectedAgent(null)
    setSelectedTemplateId(tmpl.id)
    setSelectedTemplateName(tmpl.name)
    setIsL1(false)
    setIsDetailOpen(true)
  }, [])

  const initializedRef = useRef(false)

  // 默认折叠所有团队，有 agent 被调度时展开对应团队
  useEffect(() => {
    if (!data || !teamsData.data) return

    const sortedTeams = [...teamsData.data.teams].sort((a, b) => a.name.localeCompare(b.name))

    if (!initializedRef.current) {
      const allTeams = new Set(sortedTeams.map((t) => t.name))
      setCollapsedTeams(allTeams)
      initializedRef.current = true
    }

    // 展开有活跃 agent 的团队
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
  }, [data, teamsData.data])

  // 构建数据
  const { l1Agent, teams } = useMemo(() => {
    if (!data) return { l1Agent: null as AgentInfo | null, teams: [] as TeamNode[] }

    const { agents, supervisors } = data
    const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
    const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))

    // 找 L1 主 agent（不在任何 supervisor 中）
    const l1Agent =
      agents.find((a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)) || null

    // 构建 teams
    const teams: TeamNode[] = []

    // 构建 teams 时按名称排序
    const sortedTeams = teamsData.data
      ? [...teamsData.data.teams].sort((a, b) => a.name.localeCompare(b.name))
      : []

    for (const team of sortedTeams) {
      const flatAgents: AgentWithTemplate[] = []

      for (const template of team.agents) {
        const matchingAgents = agents.filter((a) => a.id === template.id)
        flatAgents.push({ agents: matchingAgents, template })
      }

      // leader 排在前面
      flatAgents.sort((a, b) => {
        if (a.template?.is_leader && !b.template?.is_leader) return -1
        if (!a.template?.is_leader && b.template?.is_leader) return 1
        return 0
      })

      teams.push({ team, agents: flatAgents })
    }

    return { l1Agent, teams }
  }, [data, teamsData.data])

  if (!data || data.agents.length === 0) {
    return (
      <aside className="flex h-full w-[260px] shrink-0 flex-col border-r-2 border-border bg-card">
        <div className="border-b-2 border-border px-3 py-2.5">
          <h2 className="text-xs font-bold uppercase text-muted-foreground tracking-wide">
            Agents
          </h2>
        </div>
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
          <Users className="h-8 w-8" />
          <p className="text-xs">No agents</p>
        </div>
      </aside>
    )
  }

  const { agents } = data

  return (
    <aside className="flex h-full w-[260px] shrink-0 flex-col border-r-2 border-border bg-card">
      <div className="border-b-2 border-border px-3 py-2.5">
        <h2 className="text-xs font-bold uppercase text-muted-foreground tracking-wide">
          Agents ({agents.length})
        </h2>
      </div>
      <div className="flex-1 overflow-y-auto p-2 space-y-3">
        {/* L1 主 Agent - 固定显示 */}
        <div className="space-y-1.5">
          <div className="flex items-center gap-1.5 px-1">
            <Star className="h-3.5 w-3.5 text-amber-500" />
            <span className="text-[10px] font-semibold text-muted-foreground uppercase">
              L1 Main Agent
            </span>
          </div>
          {l1Agent ? (
            <AgentCard agent={l1Agent} onClick={() => handleAgentClick(l1Agent, true)} />
          ) : (
            <PlaceholderCard
              name="Main Agent"
              onClick={() => {
                setSelectedAgent(null)
                setSelectedTemplateId('main')
                setSelectedTemplateName('Main Agent')
                setIsL1(true)
                setIsDetailOpen(true)
              }}
            />
          )}
        </div>

        {/* Teams */}
        {teams.map((teamNode) => {
          const team = teamNode.team
          const isExpanded = !collapsedTeams.has(team.name)

          return (
            <div key={team.name} className="space-y-1.5">
              {/* Team header */}
              <button
                onClick={() => toggleTeam(team.name)}
                className="flex items-center gap-1.5 px-1 text-[11px] font-bold text-muted-foreground uppercase hover:text-foreground transition-colors w-full text-left"
              >
                {isExpanded ? (
                  <ChevronDown className="h-3 w-3" />
                ) : (
                  <ChevronRight className="h-3 w-3" />
                )}
                <FolderOpen className="h-3.5 w-3.5 text-blue-500" />
                <span>{team.name}</span>
                <span className="ml-auto text-[10px] bg-muted px-1.5 py-0.5 rounded">
                  {teamNode.agents.length}
                </span>
              </button>

              {isExpanded && (
                <div className="space-y-1.5 pl-1">
                  {teamNode.agents.map((item) => {
                    const key = item.template?.id || 'unknown'
                    if (item.agents.length > 0) {
                      return item.agents.map((a) => (
                        <AgentCard
                          key={a.instance_id}
                          agent={a}
                          onClick={() => handleAgentClick(a, false)}
                        />
                      ))
                    }
                    return (
                      <PlaceholderCard
                        key={key}
                        name={item.template?.name}
                        isLeader={item.template?.is_leader}
                        onClick={
                          item.template ? () => handlePlaceholderClick(item.template!) : undefined
                        }
                      />
                    )
                  })}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* Agent detail dialog */}
      <AgentDetailDialog
        agent={selectedAgent}
        templateId={selectedTemplateId}
        templateName={selectedTemplateName}
        isL1={isL1}
        open={isDetailOpen}
        onOpenChange={setIsDetailOpen}
      />
    </aside>
  )
}
