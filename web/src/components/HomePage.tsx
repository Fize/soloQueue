import { useState } from 'react'
import { Board } from './Board';
import { AgentsPanel } from './AgentsPanel';
import { SlideOver } from '@/components/ui/SlideOver';
import { useAgents } from '@/hooks/useAgents';
import { Users } from 'lucide-react';

export function HomePage() {
  const [agentsOpen, setAgentsOpen] = useState(false)
  const agentData = useAgents()
  const agentCount = agentData?.agents?.length ?? 0

  return (
    <div className="flex h-full gap-4">
      {/* Desktop: Agent Panel */}
      <div className="hidden md:block">
        <AgentsPanel />
      </div>

      {/* Mobile: SlideOver with AgentsPanel */}
      <SlideOver open={agentsOpen} onClose={() => setAgentsOpen(false)} title="Agents">
        <AgentsPanel />
      </SlideOver>

      {/* Right: Kanban Board */}
      <div className="flex-1 overflow-hidden">
        <Board />
      </div>

      {/* Mobile FAB */}
      <button
        onClick={() => setAgentsOpen(true)}
        className="fixed bottom-6 right-6 z-30 flex h-12 w-12 items-center justify-center rounded-full bg-primary text-primary-foreground shadow-lg md:hidden"
      >
        <Users className="h-5 w-5" />
        {agentCount > 0 && (
          <span className="absolute -right-1 -top-1 flex h-5 w-5 items-center justify-center rounded-full bg-status-running text-[10px] font-bold text-white">
            {agentCount}
          </span>
        )}
      </button>
    </div>
  );
}
