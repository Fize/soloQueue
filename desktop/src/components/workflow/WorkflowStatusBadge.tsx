import { cn } from '@/lib/utils'
import { cva, type VariantProps } from 'class-variance-authority'
import {
  CircleDot,
  Loader2,
  CheckCircle2,
  AlertCircle,
  Ban,
  Clock,
  Pause,
} from 'lucide-react'
import type { NodeRunState, RunStatus } from '@/types'

// ─── Combined state type for both node and workflow states ──────────────

export type BadgeState = NodeRunState | RunStatus

// ─── Config maps ────────────────────────────────────────────────────────

const iconMap: Record<string, React.ComponentType<{ className?: string }>> = {
  queued: CircleDot,
  running: Loader2,
  succeeded: CheckCircle2,
  failed: AlertCircle,
  cancelled: Ban,
  timed_out: Clock,
  pending: Clock,
  completed: CheckCircle2,
  preparing_worktree: Clock,
  pause_requested: Clock,
  paused: Pause,
  resuming: Loader2,
  interrupted: AlertCircle,
  blocked: AlertCircle,
  abandoned: Ban,
}

const labelMap: Record<string, string> = {
  queued: 'Queued',
  running: 'Running',
  succeeded: 'Succeeded',
  failed: 'Failed',
  cancelled: 'Cancelled',
  timed_out: 'Timed Out',
  pending: 'Pending',
  completed: 'Completed',
  preparing_worktree: 'Preparing worktree',
  pause_requested: 'Pause requested',
  paused: 'Paused',
  resuming: 'Resuming',
  interrupted: 'Interrupted',
  blocked: 'Blocked',
  abandoned: 'Abandoned',
}

// ─── CVA variants ───────────────────────────────────────────────────────

const badgeVariants = cva(
  'inline-flex items-center gap-1.5 rounded font-medium shrink-0',
  {
    variants: {
      state: {
        queued: 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25',
        running: 'bg-signal/10 text-signal border border-signal/25',
        succeeded: 'bg-success/10 text-success border border-success/25',
        failed: 'bg-rose-500/10 text-rose-500 border border-rose-500/25',
        cancelled: 'bg-warning/10 text-warning border border-warning/25',
        timed_out: 'bg-rose-400/10 text-rose-400 border border-rose-400/25',
        pending: 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25',
        completed: 'bg-primary/10 text-primary border border-primary/25',
        preparing_worktree: 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25',
        pause_requested: 'bg-warning/10 text-warning border border-warning/25',
        paused: 'bg-warning/10 text-warning border border-warning/25',
        resuming: 'bg-signal/10 text-signal border border-signal/25',
        interrupted: 'bg-rose-500/10 text-rose-500 border border-rose-500/25',
        blocked: 'bg-warning/10 text-warning border border-warning/25',
        abandoned: 'bg-muted-foreground/10 text-muted-foreground border border-muted-foreground/25',
      } satisfies Record<BadgeState, string>,
      size: {
        sm: 'px-1.5 py-0.5 text-[9px]',
        md: 'px-2 py-1 text-[10px]',
        lg: 'px-2.5 py-1 text-xs',
      },
    },
    defaultVariants: { state: 'pending', size: 'md' },
  }
)

const dotVariants = cva('h-2 w-2 shrink-0 rounded-full', {
  variants: {
    state: {
      queued: 'bg-muted-foreground/40',
      running: 'bg-signal',
      succeeded: 'bg-success',
      failed: 'bg-rose-500',
      cancelled: 'bg-warning',
      timed_out: 'bg-rose-400',
      pending: 'bg-muted-foreground/40',
      completed: 'bg-primary',
      preparing_worktree: 'bg-muted-foreground/40',
      pause_requested: 'bg-warning',
      paused: 'bg-warning',
      resuming: 'bg-signal',
      interrupted: 'bg-rose-500',
      blocked: 'bg-warning',
      abandoned: 'bg-muted-foreground/40',
    } satisfies Record<BadgeState, string>,
  },
  defaultVariants: { state: 'pending' },
})

// ─── Props ──────────────────────────────────────────────────────────────

interface WorkflowStatusBadgeProps extends VariantProps<typeof badgeVariants> {
  state: BadgeState
  className?: string
  showDot?: boolean
  showIcon?: boolean
  showLabel?: boolean
  label?: string
}

// ─── Component ──────────────────────────────────────────────────────────

export function WorkflowStatusBadge({
  state,
  size = 'md',
  className,
  showDot = false,
  showIcon = true,
  showLabel = true,
  label,
}: WorkflowStatusBadgeProps) {
  const Icon = iconMap[state]
  const isRunning = state === 'running'

  return (
    <span className={cn(badgeVariants({ state, size }), className)}>
      {showDot && (
        isRunning ? (
          <span className="relative flex h-2 w-2 shrink-0">
            <span className="absolute inset-0 rounded-full bg-signal animate-ping opacity-60" />
            <span className={cn(dotVariants({ state }), 'absolute inset-0.5')} />
          </span>
        ) : (
          <span className={dotVariants({ state })} />
        )
      )}
      {showIcon && Icon && (
        <Icon className={cn('h-3 w-3 shrink-0', isRunning && 'animate-spin')} />
      )}
      {showLabel && (
        <span>{label || labelMap[state] || state}</span>
      )}
    </span>
  )
}

// ─── State color helpers (for border-l use in timeline, etc.) ───────────

export function getStateBorderClass(state: BadgeState): string {
  switch (state) {
    case 'queued':
    case 'pending':
      return 'border-muted-foreground/40'
    case 'running':
      return 'border-signal'
    case 'preparing_worktree':
    case 'pause_requested':
    case 'resuming':
      return 'border-signal'
    case 'paused':
    case 'blocked':
      return 'border-warning'
    case 'interrupted':
      return 'border-rose-500'
    case 'abandoned':
      return 'border-muted-foreground/40'
    case 'succeeded':
    case 'completed':
      return state === 'completed' ? 'border-primary' : 'border-success'
    case 'failed':
      return 'border-rose-500'
    case 'cancelled':
      return 'border-warning'
    case 'timed_out':
      return 'border-rose-400'
    default:
      return 'border-muted-foreground/40'
  }
}

export function getStateBgClass(state: BadgeState): string {
  switch (state) {
    case 'queued':
    case 'pending':
      return 'bg-muted-foreground/5'
    case 'running':
      return 'bg-signal/5'
    case 'succeeded':
    case 'completed':
      return state === 'completed' ? 'bg-primary/5' : 'bg-success/5'
    case 'failed':
      return 'bg-rose-500/5'
    case 'cancelled':
      return 'bg-warning/5'
    case 'timed_out':
      return 'bg-rose-400/5'
    default:
      return 'bg-muted-foreground/5'
  }
}
