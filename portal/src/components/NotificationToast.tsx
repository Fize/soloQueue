
export interface NotificationPayload {
  category: string
  level: 'info' | 'success' | 'warning' | 'error'
  title: string
  body: string
  timestamp: string
}

interface NotificationToastProps {
  notifications: NotificationPayload[]
}

const levelStyles: Record<string, { bg: string; fg: string; border: string }> = {
  info: {
    bg: 'color-mix(in srgb, var(--color-info) 12%, transparent)',
    fg: 'var(--color-info)',
    border: 'var(--color-info)',
  },
  success: {
    bg: 'color-mix(in srgb, var(--color-success) 12%, transparent)',
    fg: 'var(--color-success)',
    border: 'var(--color-success)',
  },
  warning: {
    bg: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
    fg: 'var(--color-warning)',
    border: 'var(--color-warning)',
  },
  error: {
    bg: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)',
    fg: 'var(--color-destructive)',
    border: 'var(--color-destructive)',
  },
}

export function NotificationToast({ notifications }: NotificationToastProps) {
  if (notifications.length === 0) return null

  return (
    <div className="toast-container">
      {notifications.map((n, i) => {
        const s = levelStyles[n.level] || levelStyles.info
        return (
          <div
            key={`${n.timestamp}-${i}`}
            className="toast-item animate-slide-up"
            style={{
              backgroundColor: 'var(--color-card)',
              borderColor: s.border,
              animationDelay: `${i * 50}ms`,
            }}
          >
            <div
              className="toast-level-bar"
              style={{ backgroundColor: s.border }}
            />
            <div className="flex-1 min-w-0">
              <p className="text-sm font-semibold truncate" style={{ color: s.fg }}>
                {n.title}
              </p>
              <p className="text-xs mt-0.5 line-clamp-2" style={{ color: 'var(--color-muted-foreground)' }}>
                {n.body}
              </p>
            </div>
          </div>
        )
      })}
    </div>
  )
}
