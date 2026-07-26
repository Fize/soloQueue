import type { WorkflowRunSummary } from '@/types'
import { WorkflowStatusBadge } from './WorkflowStatusBadge'
import { Clock, Zap } from 'lucide-react'

// ─── Props ──────────────────────────────────────────────────────────────

interface WorkflowRunCardProps {
  run: WorkflowRunSummary
  onClick: () => void
}

// ─── Component ──────────────────────────────────────────────────────────

export function WorkflowRunCard({ run, onClick }: WorkflowRunCardProps) {
  const isRunning = run.status === 'running'
  const progress = run.node_count > 0
    ? Math.round((run.completed_count / run.node_count) * 100)
    : 0
  const startedAt = new Date(run.started_at)

  return (
    <div
      onClick={onClick}
      className={`group relative flex flex-col gap-2.5 rounded-xl border bg-card/40 hover:bg-card/70 transition-all cursor-pointer px-5 py-3.5 ${
        isRunning
          ? 'border-success/30 hover:border-success/50 shadow-sm shadow-success/5'
          : 'border-border hover:border-border/80'
      }`}
    >
      {/* Top row: status + info */}
      <div className="flex items-center gap-3 min-w-0">
        <WorkflowStatusBadge
          state={run.status}
          size="sm"
          showDot
          showIcon
          showLabel
        />

        <div className="min-w-0 flex-1">
          <h4 className="text-xs font-semibold text-foreground truncate font-mono">
            {run.id}
          </h4>
        </div>

        <div className="flex items-center gap-3 text-[10px] text-muted-foreground font-mono shrink-0">
          <span className="flex items-center gap-1">
            <Clock className="h-3 w-3" />
            {startedAt.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' })}
          </span>
          <span className="flex items-center gap-1">
            <Zap className="h-3 w-3" />
            {run.completed_count}/{run.node_count}
          </span>
        </div>
      </div>

      {/* Progress bar (for running) */}
      {isRunning && (
        <div className="relative h-1 w-full overflow-hidden rounded-full bg-muted">
          <div
            className="h-full bg-signal transition-all duration-700"
            style={{ width: `${Math.max(progress, 5)}%` }}
          />
        </div>
      )}

      {/* Bottom progress bar accent */}
      {isRunning && (
        <div className="absolute bottom-0 left-0 right-0 h-0.5 rounded-b-xl overflow-hidden bg-success/10">
          <div className="h-full bg-success/60 animate-pulse" style={{ width: `${progress}%` }} />
        </div>
      )}
    </div>
  )
}
