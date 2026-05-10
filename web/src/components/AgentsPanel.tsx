import { useState, useCallback, useMemo } from 'react';
import { useAgents } from '@/hooks/useAgents';
import { useTeams } from '@/hooks/useTeams';
import { AgentCard } from './AgentCard';
import type { AgentInfo, AgentTemplate, TeamInfo } from '@/types';
import { Users, ChevronRight, ChevronDown, Star, Circle, FolderOpen, Bot, UserCog } from 'lucide-react';
import { AgentDetailDialog } from './AgentDetailDialog';

interface AgentWithTemplate {
  agent: AgentInfo | null;
  template: AgentTemplate | null;
}

interface TeamNode {
  team: TeamInfo;
  l2: AgentWithTemplate[];
  l3: AgentWithTemplate[];
}

// 占位组件
function PlaceholderCard({ name, onClick }: { name?: string | null; onClick?: () => void }) {
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
        <span className="text-[10px] text-muted-foreground bg-muted px-1.5 py-0.5 rounded uppercase">Idle</span>
      </div>
    </div>
  );
}

export function AgentsPanel() {
  const data = useAgents();
  const teamsData = useTeams();
  const [collapsedTeams, setCollapsedTeams] = useState<Set<string>>(new Set());
  const [expandedL2, setExpandedL2] = useState<Set<string>>(new Set());
  const [selectedAgent, setSelectedAgent] = useState<AgentInfo | null>(null);
  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null);
  const [selectedTemplateName, setSelectedTemplateName] = useState<string | null>(null);
  const [isDetailOpen, setIsDetailOpen] = useState(false);
  const [isL1, setIsL1] = useState(false);

  const toggleTeam = useCallback((team: string) => {
    setCollapsedTeams(prev => {
      const next = new Set(prev);
      if (next.has(team)) {
        next.delete(team);
      } else {
        next.add(team);
      }
      return next;
    });
  }, []);

  const toggleL2 = useCallback((key: string) => {
    setExpandedL2(prev => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const handleAgentClick = useCallback((agent: AgentInfo, l1Flag: boolean = false) => {
    setSelectedAgent(agent);
    setSelectedTemplateId(null);
    setSelectedTemplateName(null);
    setIsL1(l1Flag);
    setIsDetailOpen(true);
  }, []);

  const handlePlaceholderClick = useCallback((tmpl: AgentTemplate) => {
    setSelectedAgent(null);
    setSelectedTemplateId(tmpl.id);
    setSelectedTemplateName(tmpl.name);
    setIsL1(false);
    setIsDetailOpen(true);
  }, []);

  // 构建数据
  const { l1Agent, teams } = useMemo(() => {
    if (!data) return { l1Agent: null as AgentInfo | null, teams: [] as TeamNode[] };

    const { agents, supervisors } = data;
    const l2Ids = new Set(supervisors.map(sv => sv.leader_id).filter(Boolean));
    const l3Ids = new Set(supervisors.flatMap(sv => sv.children_ids));

    // 找 L1 主 agent（不在任何 supervisor 中）
    const l1Agent = agents.find(a => !l2Ids.has(a.instance_id) && !l3Ids.has(a.instance_id)) || null;

    // 构建 teams
    const teams: TeamNode[] = [];

    // 构建 teams 时按名称排序
    const sortedTeams = teamsData.data
      ? [...teamsData.data.teams].sort((a, b) => a.name.localeCompare(b.name))
      : [];

    for (const team of sortedTeams) {
      const l2: AgentWithTemplate[] = [];
      const l3: AgentWithTemplate[] = [];

      for (const template of team.agents) {
        // 用 template.id 匹配实际 agent
        const agent = agents.find(a => a.id === template.id) || null;

        if (template.is_leader) {
          l2.push({ agent, template });
        } else {
          l3.push({ agent, template });
        }
      }

      teams.push({ team, l2, l3 });
    }

    return { l1Agent, teams };
  }, [data, teamsData.data]);

  if (!data || data.agents.length === 0) {
    return (
      <aside className="flex h-full w-[260px] shrink-0 flex-col border-r-2 border-border bg-card">
        <div className="border-b-2 border-border px-3 py-2.5">
          <h2 className="text-xs font-bold uppercase text-muted-foreground tracking-wide">Agents</h2>
        </div>
        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
          <Users className="h-8 w-8" />
          <p className="text-xs">No agents</p>
        </div>
      </aside>
    );
  }

  const { agents } = data;

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
            <span className="text-[10px] font-semibold text-muted-foreground uppercase">L1 主 Agent</span>
          </div>
          {l1Agent ? (
            <AgentCard agent={l1Agent} onClick={() => handleAgentClick(l1Agent, true)} />
          ) : (
            <PlaceholderCard name="主 Agent" />
          )}
        </div>

        {/* Teams */}
        {teams.map((teamNode) => {
          const team = teamNode.team;
          const isExpanded = !collapsedTeams.has(team.name);

          return (
            <div key={team.name} className="space-y-1.5">
              {/* Team header */}
              <button
                onClick={() => toggleTeam(team.name)}
                className="flex items-center gap-1.5 px-1 text-[11px] font-bold text-muted-foreground uppercase hover:text-foreground transition-colors w-full text-left"
              >
                {isExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                <FolderOpen className="h-3.5 w-3.5 text-blue-500" />
                <span>{team.name}</span>
                <span className="ml-auto text-[10px] bg-muted px-1.5 py-0.5 rounded">
                  {teamNode.l2.length + teamNode.l3.length}
                </span>
              </button>

              {isExpanded && (
                <div className="space-y-2 pl-1">
                  {/* L2 Leaders */}
                  {teamNode.l2.map((l2, index) => {
                    const l2Key = `${team.name}-l2-${index}`;
                    const isL2Expanded = !expandedL2.has(l2Key);

                    return (
                      <div key={l2Key} className="space-y-1">
                        {/* L2 Header */}
                        <button
                          onClick={() => toggleL2(l2Key)}
                          className="flex items-center gap-1.5 px-1 text-[10px] font-semibold text-muted-foreground uppercase hover:text-foreground transition-colors w-full text-left"
                        >
                          {isL2Expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                          <UserCog className="h-3.5 w-3.5 text-amber-500" />
                          <span>{l2.template?.name || 'Leader'}</span>
                        </button>

                        {isL2Expanded && (
                          <div className="space-y-1.5 pl-4">
                            {/* L2 Agent */}
                            {l2.agent ? (
                              <AgentCard agent={l2.agent} onClick={() => handleAgentClick(l2.agent!, false)} />
                            ) : (
                              <PlaceholderCard name={l2.template?.name} onClick={l2.template ? () => handlePlaceholderClick(l2.template!) : undefined} />
                            )}

                            {/* L3 Workers */}
                            <div className="pl-4 space-y-1">
                              <div className="flex items-center gap-1.5 px-1">
                                <Bot className="h-3 w-3 text-green-500" />
                                <span className="text-[10px] font-medium text-muted-foreground uppercase">Workers</span>
                              </div>
                              {teamNode.l3.map((l3, idx) => (
                                <div key={idx}>
                                  {l3.agent ? (
                                    <AgentCard agent={l3.agent} onClick={() => handleAgentClick(l3.agent!, false)} />
                                  ) : (
                                    <PlaceholderCard name={l3.template?.name} onClick={l3.template ? () => handlePlaceholderClick(l3.template!) : undefined} />
                                  )}
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          );
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
  );
}
