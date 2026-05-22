import { cn } from '@/lib/utils'
import type { AgentState } from '@/types'

const stateConfig: Record<
  AgentState,
  { label: string; dotColor: string; bgColor: string; textColor: string }
> = {
  processing: {
    label: 'Running',
    dotColor: 'bg-teal-400',
    bgColor: 'bg-teal-400/10 dark:bg-teal-400/15',
    textColor: 'text-teal-700 dark:text-teal-300',
  },
  idle: {
    label: 'Idle',
    dotColor: 'bg-emerald-400',
    bgColor: 'bg-emerald-400/10 dark:bg-emerald-400/15',
    textColor: 'text-emerald-700 dark:text-emerald-300',
  },
  stopping: {
    label: 'Stopping',
    dotColor: 'bg-amber-400',
    bgColor: 'bg-amber-400/10 dark:bg-amber-400/15',
    textColor: 'text-amber-700 dark:text-amber-300',
  },
  stopped: {
    label: 'Stopped',
    dotColor: 'bg-zinc-400',
    bgColor: 'bg-zinc-400/10 dark:bg-zinc-400/15',
    textColor: 'text-zinc-600 dark:text-zinc-400',
  },
}

interface StatusBadgeProps {
  state: AgentState
  className?: string
  showLabel?: boolean
  size?: 'sm' | 'md'
}

export function StatusBadge({ state, className, showLabel = true, size = 'md' }: StatusBadgeProps) {
  const config = stateConfig[state]
  const isProcessing = state === 'processing'

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full font-medium',
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
    </span>
  )
}

/** Dot-only indicator for compact displays */
export function StatusDot({ state, className }: { state: AgentState; className?: string }) {
  const config = stateConfig[state]
  return (
    <span
      className={cn(
        'h-2.5 w-2.5 rounded-full shrink-0 ring-2 ring-background',
        config.dotColor,
        state === 'processing' && 'animate-pulse',
        className
      )}
    />
  )
}
