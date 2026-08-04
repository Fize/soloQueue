import { RefreshCw } from 'lucide-react'

interface CronTaskStatus {
  id: string
  title: string
  task_type: 'L0' | 'L1' | 'L2' | 'L3'
  expression: string
  instruction: string
  target_agent: string
  status: string
  last_run_at: string | null
  next_run_at: string
  is_one_time: boolean
}

interface CronSectionProps {
  tasks: CronTaskStatus[]
  t: (key: string, v?: Record<string, string | number>) => string
}

function EmptyState({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <div
        className="w-12 h-12 rounded-full flex items-center justify-center"
        style={{ backgroundColor: 'var(--color-surface-secondary)' }}
      >
        <span style={{ color: 'var(--color-muted-foreground)' }}>{icon}</span>
      </div>
      <span className="text-base font-medium" style={{ color: 'var(--color-foreground)' }}>
        {title}
      </span>
      <span className="text-sm max-w-xs" style={{ color: 'var(--color-muted-foreground)' }}>
        {description}
      </span>
    </div>
  )
}

export function CronSection({ tasks, t }: CronSectionProps) {
  return (
    <section
      className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
      style={{ backgroundColor: 'var(--color-card)' }}
    >
      {/* Section header */}
      <div
        className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
        style={{ borderColor: 'var(--color-border)' }}
      >
        <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--color-foreground)' }}>
          <RefreshCw className="h-4 w-4" style={{ color: 'var(--color-primary)' }} />
          {t('cron.title')}
        </h2>
        <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
          {tasks.length === 1 ? t('cron.tasksCount', { count: 1 }) : t('cron.tasksCountPlural', { count: tasks.length })}
        </span>
      </div>

      {/* Task cards or empty */}
      {tasks.length === 0 ? (
        <EmptyState
          icon={<RefreshCw className="h-6 w-6" />}
          title={t('cron.emptyTitle')}
          description={t('cron.emptyDesc')}
        />
      ) : (
        <div className="divide-y" style={{ borderColor: 'var(--color-border)' }}>
          {tasks.map((task) => {
            const statusColors: Record<string, { bg: string; fg: string }> = {
              active: { bg: 'color-mix(in srgb, var(--color-success) 12%, transparent)', fg: 'var(--color-success)' },
              paused: { bg: 'color-mix(in srgb, var(--color-warning) 12%, transparent)', fg: 'var(--color-warning)' },
              completed: { bg: 'color-mix(in srgb, var(--color-muted-foreground) 12%, transparent)', fg: 'var(--color-muted-foreground)' },
              running: { bg: 'color-mix(in srgb, var(--color-signal) 12%, transparent)', fg: 'var(--color-signal)' },
              failed: { bg: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)', fg: 'var(--color-destructive)' },
            }
            const sc = statusColors[task.status] || statusColors.completed
            const isL1 = task.target_agent === 'L1'
            const agentColor = isL1
              ? { bg: 'color-mix(in srgb, var(--color-primary) 12%, transparent)', fg: 'var(--color-primary)' }
              : { bg: 'color-mix(in srgb, var(--color-accent) 12%, transparent)', fg: 'var(--color-accent)' }

            return (
              <div
                key={task.id}
                className="px-4 sm:px-6 py-3 flex items-start gap-3"
                style={{ animationDelay: `${tasks.indexOf(task) * 30}ms` }}
              >
                {/* Instruction */}
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate" style={{ color: 'var(--color-foreground)' }}>
                    {task.title}
                  </p>
                  <p className="text-xs truncate mt-0.5" style={{ color: 'var(--color-muted-foreground)' }}>
                    {task.instruction}
                  </p>
                  <div className="flex items-center gap-2 mt-1.5 flex-wrap">
                    <span
                      className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                      style={{
                        backgroundColor: 'color-mix(in srgb, var(--color-primary) 12%, transparent)',
                        color: 'var(--color-primary)',
                      }}
                    >
                      {task.task_type}
                    </span>
                    <span
                      className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                      style={{
                        backgroundColor: agentColor.bg,
                        color: agentColor.fg,
                      }}
                    >
                      {task.target_agent}
                    </span>
                    <span
                      className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                      style={{
                        backgroundColor: sc.bg,
                        color: sc.fg,
                      }}
                    >
                      {task.status}
                    </span>
                    <span className="text-[10px] font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
                      {t('cron.nextRun', { time: task.next_run_at || '--' })}
                    </span>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
