import { Loader2, Info, Clock } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import type { PlanItem } from '@/types'

interface AgentPlanTabProps {
  plan: { schedule: PlanItem[] } | null
  planLoading: boolean
  planError: string | null
}

const PLAN_STATUS_LABELS: Record<string, string> = {
  pending: 'Pending',
  in_progress: 'In Progress',
  completed: 'Completed',
  cancelled: 'Cancelled',
}

export function AgentPlanTab({ plan, planLoading, planError }: AgentPlanTabProps) {
  const { t } = useTranslation()

  if (planLoading) {
    return (
      <div className="flex h-32 items-center justify-center text-xs text-muted-foreground font-mono">
        <Loader2 className="mr-2 h-4 w-4 animate-spin text-primary" /> {t('common.loading')}
      </div>
    )
  }
  if (planError) {
    return <div className="text-center text-xs font-mono text-rose-500 py-6">{planError}</div>
  }
  if (!plan || !plan.schedule || plan.schedule.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-6 text-muted-foreground gap-2 border border-dashed border-border/80 rounded-xl bg-card/5">
        <Info className="h-6 w-6 opacity-30" />
        <span className="text-xs">{t('chat.noPlans')}</span>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-wider font-mono flex items-center gap-1.5 border-b border-border/40 pb-1.5">
        <Clock className="h-3.5 w-3.5" />
        {t('chat.planPanel')}
      </h4>
      <div className="relative border-l border-border/80 ml-2.5 pl-5 space-y-5">
        {plan.schedule.map((item: any, idx: number) => {
          const startStr = new Date(item.start_time).toLocaleTimeString([], {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          })
          const endStr = new Date(item.end_time).toLocaleTimeString([], {
            hour: '2-digit',
            minute: '2-digit',
            hour12: false,
          })

          let statusColor = 'bg-muted-foreground/30 border-muted-foreground/40'
          if (item.status === 'in_progress') {
            statusColor = 'bg-primary border-primary animate-pulse'
          } else if (item.status === 'completed') {
            statusColor = 'bg-success border-success'
          } else if (item.status === 'cancelled') {
            statusColor = 'bg-rose-500 border-rose-500'
          }

          return (
            <div key={idx} className="relative group">
              {/* Timeline Dot */}
              <span
                className={`absolute left-[-26px] top-1.5 w-3.5 h-3.5 rounded-full border-2 bg-background transition-colors ${statusColor}`}
              />

              <div className="flex flex-col gap-1 rounded-xl bg-card/25 border border-border/60 p-3.5 hover:border-primary/40 transition-colors">
                <div className="flex items-center justify-between text-[10px] font-mono text-muted-foreground">
                  <span className="font-semibold text-primary/95 bg-primary/5 border border-primary/10 rounded px-1.5 py-0.5">
                    {startStr} - {endStr}
                  </span>
                  <span className="capitalize">
                    {PLAN_STATUS_LABELS[item.status] || item.status}
                  </span>
                </div>
                <div className="text-xs font-semibold text-foreground/90 mt-1">
                  {item.activity}
                </div>
                <div className="text-[10px] text-muted-foreground/90 font-mono mt-0.5">
                  📍 {item.location}
                </div>
                {item.description && (
                  <div className="text-[10px] text-muted-foreground/80 leading-normal border-t border-border/20 pt-1.5 mt-1.5 italic">
                    {item.description}
                  </div>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
