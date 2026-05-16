import { useState, useCallback, useMemo, useRef, useEffect } from 'react'
import { useAgentStore } from '@/stores/agentStore'
import { AgentCard } from './AgentCard'
import { PlaceholderCard } from './AgentsPanel'
import type { AgentInfo, AgentTemplate, TeamInfo } from '@/types'
import { Users, ChevronRight, ChevronDown, Star, FolderOpen, Loader2 } from 'lucide-react'
import { AgentDetailDialog } from './AgentDetailDialog'

interface AgentWithTemplate {
  agents: AgentInfo[]
  template: AgentTemplate | null
}

interface TeamNode {
  team: TeamInfo
  agents: AgentWithTemplate[]
}

export function AgentTreeView() {
  const data = useAgentStore((state) => state.agents)
  const teamsData = useAgentStore((state) => state.teams)
  const fetchTeams = useAgentStore((state) => state.fetchTeams)
  const teamsLoading = useAgentStore((state) => state.teamsLoading)

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

  useEffect(() => {
    fetchTeams()
  }, [fetchTeams])

  // Collapse all by default; expand teams with processing agents
  useEffect(() => {
    if (!data || !teamsData) return

    const sortedTeams = [...teamsData.teams].sort((a, b) => a.name.localeCompare(b.name))

    if (!initializedRef.current) {
      const allTeams = new Set(sortedTeams.map((t) => t.name))
      setCollapsedTeams(allTeams)
      initializedRef.current = true
    }

    const teamsToExpand = new Set<string>()
    const { agents } = data

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

  const { l1Agent, teams } = useMemo(() => {
    if (!data) return { l1Agent: null as AgentInfo | null, teams: [] as TeamNode[] }

    const { agents, supervisors } = data
    const l2Ids = new Set(supervisors.map((sv) => sv.leader_id).filter(Boolean))
    const l3Ids = new Set(supervisors.flatMap((sv) => sv.children_ids))

    const l1Agent =
      agents.find((a) => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)) || null

    const teams: TeamNode[] = []
    const sortedTeams = teamsData
      ? [...teamsData.teams].sort((a, b) => a.name.localeCompare(b.name))
      : []

    for (const team of sortedTeams) {
      const flatAgents: AgentWithTemplate[] = []

      for (const template of team.agents) {
        const matchingAgents = agents.filter((a) => a.id === template.id)
        flatAgents.push({ agents: matchingAgents, template })
      }

      flatAgents.sort((a, b) => {
        if (a.template?.is_leader && !b.template?.is_leader) return -1
        if (!a.template?.is_leader && b.template?.is_leader) return 1
        return 0
      })

      teams.push({ team, agents: flatAgents })
    }

    return { l1Agent, teams }
  }, [data, teamsData])

  const loading = data === null && teamsLoading
  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (!data || data.agents.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <Users className="h-10 w-10" />
        <p className="text-sm">No agents configured</p>
      </div>
    )
  }

  return (
    <>
      <div className="h-full overflow-y-auto space-y-3 px-2 pb-4">
        {/* L1 Agent */}
        <div className="space-y-1.5 pt-1">
          <div className="flex items-center gap-1.5 px-1">
            <Star className="h-4 w-4 text-amber-500 shrink-0" />
            <span className="text-xs font-semibold text-muted-foreground uppercase">
              {l1Agent?.name || 'L1 Agent'}
            </span>
          </div>
          {l1Agent ? (
            <AgentCard agent={l1Agent} onClick={() => handleAgentClick(l1Agent, true)} />
          ) : (
            <PlaceholderCard
              name="L1 Agent"
              onClick={() => {
                setSelectedAgent(null)
                setSelectedTemplateId('main')
                setSelectedTemplateName('L1 Agent')
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
              <button
                onClick={() => toggleTeam(team.name)}
                className="flex items-center gap-1.5 px-1 w-full text-left text-xs font-bold text-muted-foreground uppercase hover:text-foreground transition-colors py-1"
              >
                {isExpanded ? (
                  <ChevronDown className="h-3.5 w-3.5 shrink-0" />
                ) : (
                  <ChevronRight className="h-3.5 w-3.5 shrink-0" />
                )}
                <FolderOpen className="h-4 w-4 text-blue-500 shrink-0" />
                <span className="truncate">{team.name}</span>
                <span className="ml-auto text-[10px] bg-muted px-1.5 py-0.5 rounded shrink-0">
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

      <AgentDetailDialog
        agent={selectedAgent}
        templateId={selectedTemplateId}
        templateName={selectedTemplateName}
        isL1={isL1}
        open={isDetailOpen}
        onOpenChange={setIsDetailOpen}
      />
    </>
  )
}
