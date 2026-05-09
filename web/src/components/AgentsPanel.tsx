import { useAgents } from '@/hooks/useAgents';
import { AgentCard } from './AgentCard';
import type { AgentInfo } from '@/types';
import { Users } from 'lucide-react';

export function AgentsPanel() {
  const data = useAgents();

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

  // Group by group name
  const grouped = agents.reduce<Record<string, AgentInfo[]>>((acc, agent) => {
    const group = agent.group || 'Session';
    if (!acc[group]) acc[group] = [];
    acc[group].push(agent);
    return acc;
  }, {});

  return (
    <aside className="flex h-full w-[260px] shrink-0 flex-col border-r-2 border-border bg-card">
      <div className="border-b-2 border-border px-3 py-2.5">
        <h2 className="text-xs font-bold uppercase text-muted-foreground tracking-wide">
          Agents ({agents.length})
        </h2>
      </div>
      <div className="flex-1 overflow-y-auto p-2 space-y-3">
        {Object.entries(grouped).map(([group, groupAgents]) => (
          <div key={group} className="space-y-1.5">
            <h3 className="flex items-center gap-1.5 px-1 text-[11px] font-bold text-muted-foreground uppercase">
              <span>📂</span> {group}
            </h3>
            <div className="space-y-1.5">
              {groupAgents.map((agent) => (
                <AgentCard key={agent.instance_id} agent={agent} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </aside>
  );
}
