import { useAgents } from '@/hooks/useAgents';
import { AgentCard } from './AgentCard';
import type { AgentInfo } from '@/types';
import { Users } from 'lucide-react';

export function AgentsView() {
  const data = useAgents();

  if (!data) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="flex flex-col items-center gap-3 text-muted-foreground">
          <Users className="h-10 w-10" />
          <p className="text-sm">No agents connected</p>
        </div>
      </div>
    );
  }

  const { agents } = data;

  if (agents.length === 0) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="flex flex-col items-center gap-3 text-muted-foreground">
          <Users className="h-10 w-10" />
          <p className="text-sm">No agents connected</p>
        </div>
      </div>
    );
  }

  // Group by group name
  const grouped = agents.reduce<Record<string, AgentInfo[]>>((acc, agent) => {
    const group = agent.group || 'default';
    if (!acc[group]) acc[group] = [];
    acc[group].push(agent);
    return acc;
  }, {});

  return (
    <div className="h-full overflow-y-auto p-6">
      <div className="mx-auto max-w-4xl space-y-6">
        {Object.entries(grouped).map(([group, groupAgents]) => (
          <div key={group} className="space-y-3">
            <h2 className="flex items-center gap-2 text-sm font-bold text-foreground">
              <span>📂</span> {group}
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                ({groupAgents.length} agents)
              </span>
            </h2>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {groupAgents.map((agent) => (
                <AgentCard key={agent.id} agent={agent} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
