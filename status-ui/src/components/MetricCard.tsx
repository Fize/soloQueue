import { Loader2 } from 'lucide-react'

interface MetricCardProps {
  title: string
  icon: React.ReactNode
  iconColor: React.CSSProperties
  mainValue: string | number | undefined
  subValue: string | undefined
  detail?: string
  progress?: number
  progressColor?: string
  isEmpty?: boolean
}

export function MetricCard({ title, icon, iconColor, mainValue, subValue, detail, progress, progressColor, isEmpty }: MetricCardProps) {
  return (
    <div
      className="rounded-xl p-5 flex flex-col gap-2 animate-slide-up shadow-sm"
      style={{ backgroundColor: 'var(--color-card)' }}
    >
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
          {title}
        </span>
        <span style={iconColor}>{icon}</span>
      </div>

      <div className="flex flex-col gap-0.5">
        <span
          className={`text-2xl font-bold tabular-nums tracking-tight ${
            isEmpty ? 'opacity-40' : ''
          }`}
          style={{ color: 'var(--color-foreground)' }}
        >
          {isEmpty ? (
            <span className="inline-flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span className="text-base font-normal opacity-60">Waiting for connection...</span>
            </span>
          ) : (
            mainValue
          )}
        </span>
        <span className="text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
          {isEmpty ? 'Auto-reconnect after startup' : subValue}
        </span>
      </div>

      {detail && !isEmpty && (
        <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
          {detail}
        </span>
      )}

      {progress !== undefined && !isEmpty && (
        <div className="w-full h-1.5 rounded-full overflow-hidden mt-1" style={{ backgroundColor: 'var(--color-surface-secondary)' }}>
          <div
            className="h-full rounded-full transition-all duration-500 ease-out"
            style={{
              width: `${Math.min(progress, 100)}%`,
              backgroundColor: progressColor ?? 'var(--color-primary)',
            }}
          />
        </div>
      )}
    </div>
  )
}
