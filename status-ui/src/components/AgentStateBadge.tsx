import { useTranslation } from '../i18n'

type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'

interface AgentStateBadgeProps {
  state: AgentState
}

export function AgentStateBadge({ state }: AgentStateBadgeProps) {
  const { t } = useTranslation()

  if (state === 'processing') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--color-signal) 12%, transparent)',
          color: 'var(--color-signal)',
        }}
      >
        <span className="relative flex h-1.5 w-1.5">
          <span
            className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
            style={{ backgroundColor: 'var(--color-signal)' }}
          />
          <span className="relative inline-flex h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--color-signal)' }} />
        </span>
        {t('table.badges.processing')}
      </span>
    )
  }

  if (state === 'idle') {
    return (
      <span
        className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--color-muted-foreground) 8%, transparent)',
          color: 'var(--color-muted-foreground)',
        }}
      >
        <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--color-muted-foreground)' }} />
        {t('table.badges.idle')}
      </span>
    )
  }

  // stopping → yellow/warning, stopped → red/destructive
  const color = state === 'stopping' ? 'var(--color-warning)' : 'var(--color-destructive)'
  const label = state === 'stopping' ? t('table.badges.stopping') : t('table.badges.stopped')

  return (
    <span
      className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold"
      style={{
        backgroundColor: `color-mix(in srgb, ${color} 12%, transparent)`,
        color,
      }}
    >
      <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: color }} />
      {label}
    </span>
  )
}
