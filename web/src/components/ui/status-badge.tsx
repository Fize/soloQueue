import { cn } from '@/lib/utils'
import type { AgentState } from '@/types'

const stateConfig: Record<
  AgentState,
  { label: string; dotColor: string; bgColor: string; textColor: string }
> = {
  processing: {
    label: 'Running',
    dotColor: 'bg-[var(--color-signal)]',
    bgColor: 'bg-[var(--color-signal)]/10 dark:bg-[var(--color-signal)]/15',
    textColor: 'text-[var(--color-signal)]',
  },
  idle: {
    label: 'Idle',
    dotColor: 'bg-[var(--success)]',
    bgColor: 'bg-[var(--success)]/10 dark:bg-[var(--success)]/15',
    textColor: 'text-[var(--success)]',
  },
  stopping: {
    label: 'Stopping',
    dotColor: 'bg-[var(--warning)]',
    bgColor: 'bg-[var(--warning)]/10 dark:bg-[var(--warning)]/15',
    textColor: 'text-[var(--warning)]',
  },
  stopped: {
    label: 'Stopped',
    dotColor: 'bg-muted-foreground',
    bgColor: 'bg-muted-foreground/10 dark:bg-muted-foreground/15',
    textColor: 'text-muted-foreground',
  },
}

export interface StatusBadgeProps {
  state: AgentState
  className?: string
  showLabel?: boolean
  size?: 'sm' | 'md'
  errorCount?: number
  iteration?: number
}

export function StatusBadge({ state, className, showLabel = true, size = 'md', errorCount, iteration }: StatusBadgeProps) {
  const config = stateConfig[state]
  const isProcessing = state === 'processing'

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded font-medium',
        config.bgColor,
        config.textColor,
        size === 'sm' ? 'px-2 py-0.5 text-[10px]' : 'px-2.5 py-1 text-xs',
        className
      )}
    >
      <span
        className={cn(
          'rounded-full shrink-0',
          config.dotColor,
          size === 'sm' ? 'h-1.5 w-1.5' : 'h-2 w-2',
          isProcessing && 'animate-pulse'
        )}
      />
      {showLabel && config.label}
      {iteration != null && iteration > 0 && (
        <span className="ml-1 opacity-70">#{iteration}</span>
      )}
      {errorCount != null && errorCount > 0 && (
        <span className="ml-1 inline-flex items-center gap-0.5 text-[var(--destructive)]">
          <span className="h-1.5 w-1.5 rounded-full bg-[var(--destructive)]" />
          <span className="text-[10px] font-semibold">{errorCount}</span>
        </span>
      )}
    </span>
  )
}

/** Dot-only indicator for compact displays */
export function StatusDot({ state, className, errorCount }: { state: AgentState; className?: string; errorCount?: number }) {
  const config = stateConfig[state]
  const dotColor = errorCount != null && errorCount > 0 ? 'bg-[var(--destructive)]' : config.dotColor
  return (
    <span
      className={cn(
        'h-2.5 w-2.5 rounded-full shrink-0 ring-2 ring-background',
        dotColor,
        state === 'processing' && 'animate-pulse',
        className
      )}
    />
  )
}
