import { useMemo } from 'react'
import { Board } from './Board'
import { AgentsPanel } from './AgentsPanel'
import { usePlanStore } from '@/stores/planStore'
import { useRuntime } from '@/hooks/useRuntime'
import { useAgentStore } from '@/stores/agentStore'
import { cn } from '@/lib/utils'

function StatsCard({
  title,
  value,
  className,
}: {
  title: string
  value: string | number
  className?: string
}) {
  return (
    <div className={cn('rounded-xl border bg-card px-5 py-4 shadow-sm', className)}>
      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wider">
        {title}
      </p>
      <p className="mt-1 text-2xl font-bold text-foreground tabular-nums">{value}</p>
    </div>
  )
}

export function Dashboard() {
  const plans = usePlanStore((s) => s.plans)
  const runtime = useRuntime()
  const agentsData = useAgentStore((s) => s.agents)

  const stats = useMemo(
    () => ({
      total: plans.length,
      running: plans.filter((p) => p.status === 'running').length,
      done: plans.filter((p) => p.status === 'done').length,
      agentsActive: runtime?.running_agents ?? agentsData?.agents?.length ?? 0,
    }),
    [plans, runtime, agentsData]
  )

  return (
    <div className="flex h-full flex-col">
      {/* Stats Cards */}
      <div className="grid grid-cols-4 gap-4 px-6 pt-6 pb-4">
        <StatsCard title="Total Plans" value={stats.total} />
        <StatsCard title="Running" value={stats.running} />
        <StatsCard title="Completed" value={stats.done} />
        <StatsCard title="Active Agents" value={stats.agentsActive} />
      </div>

      {/* Board + Agents */}
      <div className="flex flex-1 min-h-0 gap-4 px-6 pb-6">
        <AgentsPanel />
        <div className="flex-1 min-w-0">
          <Board />
        </div>
      </div>
    </div>
  )
}
