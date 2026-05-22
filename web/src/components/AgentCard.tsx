import type { AgentInfo } from '@/types'
import { Badge } from '@/components/ui/badge'
import { GlassCard } from '@/components/ui/glass-card'
import { StatusBadge } from '@/components/ui/status-badge'
import { Mail, AlertCircle } from 'lucide-react'

interface AgentCardProps {
  agent: AgentInfo
  onClick?: () => void
}

export function AgentCard({ agent, onClick }: AgentCardProps) {
  const hasMail = agent.mailbox_high > 0 || agent.mailbox_normal > 0

  return (
    <GlassCard
      variant={
        agent.state === 'processing' ? 'active' : agent.error_count > 0 ? 'error' : 'default'
      }
      interactive={!!onClick}
      onClick={onClick}
      className="group relative select-none"
    >
      <div className="flex items-start justify-between gap-4">
        {/* Name, Model & ID */}
        <div className="min-w-0 flex-1 space-y-1">
          <div className="flex items-center gap-2">
            <h3 className="font-semibold text-foreground text-sm truncate group-hover:text-primary transition-colors">
              {agent.name}
            </h3>
            {agent.is_leader && (
              <Badge variant="primary" className="text-[9px] uppercase tracking-wider py-0 px-1">
                Leader
              </Badge>
            )}
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="font-mono text-[10px] text-muted-foreground truncate">
              {agent.model_id}
            </span>
            <span className="font-mono text-[9px] text-muted-foreground/60 truncate">
              {agent.instance_id}
            </span>
          </div>
        </div>

        {/* Status Badge & Counters */}
        <div className="flex flex-col items-end gap-1.5 shrink-0">
          <StatusBadge state={agent.state} size="sm" />

          <div className="flex items-center gap-1.5">
            {/* Mailbox Indicators */}
            {hasMail && (
              <div
                className="flex items-center gap-1 text-[10px] px-1.5 py-0.5 rounded-sm bg-muted text-muted-foreground font-medium"
                title={`Mailbox: ${agent.mailbox_high} High / ${agent.mailbox_normal} Normal`}
              >
                <Mail className="h-3 w-3" />
                <span className="tabular-nums">
                  {agent.mailbox_high > 0 ? `${agent.mailbox_high}H/` : ''}
                  {agent.mailbox_normal}N
                </span>
              </div>
            )}

            {/* Error Counter */}
            {agent.error_count > 0 && (
              <div
                className="flex items-center gap-0.5 text-[10px] font-bold text-destructive bg-destructive/10 dark:bg-destructive/15 px-1.5 py-0.5 rounded-sm"
                title={`Errors: ${agent.error_count}. Last error: ${agent.last_error}`}
              >
                <AlertCircle className="h-3 w-3" />
                <span className="tabular-nums">{agent.error_count}</span>
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Footer Badges */}
      {agent.task_level && (
        <div className="mt-2.5 pt-2 border-t border-border/40 flex items-center gap-1.5">
          <Badge variant="outline" className="text-[10px] text-muted-foreground py-0 px-1.5">
            Level {agent.task_level}
          </Badge>
          {agent.pending_delegations > 0 && (
            <span className="text-[10px] text-muted-foreground/80">
              {agent.pending_delegations} active delegations
            </span>
          )}
        </div>
      )}
    </GlassCard>
  )
}
