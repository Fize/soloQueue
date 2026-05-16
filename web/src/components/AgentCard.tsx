import type { AgentInfo, AgentState } from '@/types'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

const stateColors: Record<AgentState, string> = {
  processing: '#4DABF7',
  idle: '#69DB7C',
  stopping: '#FFD43B',
  stopped: '#BBBBBB',
}

const stateBadgeVariant: Record<AgentState, 'default' | 'secondary' | 'outline' | 'destructive'> = {
  processing: 'default',
  idle: 'secondary',
  stopping: 'outline',
  stopped: 'outline',
}

interface AgentCardProps {
  agent: AgentInfo
  onClick?: () => void
}

export function AgentCard({ agent, onClick }: AgentCardProps) {
  const borderColor = stateColors[agent.state] || '#BBBBBB'

  return (
    <div
      className={cn(
        'relative rounded-lg border bg-card p-3.5 shadow-sm hover:shadow-md hover:-translate-y-0.5 cursor-pointer'
      )}
      style={{ borderLeftWidth: '4px', borderLeftColor: borderColor }}
      onClick={onClick}
    >
      {/* Name + State */}
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm font-bold text-card-foreground truncate">{agent.name}</h3>
        <Badge variant={stateBadgeVariant[agent.state]} className="text-[10px] capitalize">
          {agent.state}
        </Badge>
      </div>

      {/* Model + Instance */}
      <div className="space-y-1">
        <p className="text-xs text-muted-foreground truncate">{agent.model_id}</p>
        <p className="font-mono text-[10px] text-muted-foreground truncate">{agent.instance_id}</p>
      </div>

      {/* Badges row */}
      <div className="mt-2 flex items-center gap-1.5 flex-wrap">
        {agent.is_leader && (
          <Badge variant="default" className="text-[10px]">
            Leader
          </Badge>
        )}
        {agent.task_level && (
          <Badge variant="secondary" className="text-[10px]">
            {agent.task_level}
          </Badge>
        )}
        {agent.error_count > 0 && (
          <Badge variant="destructive" className="text-[10px]">
            ✗{agent.error_count}
          </Badge>
        )}
      </div>
    </div>
  )
}
