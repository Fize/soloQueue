import { Wifi, WifiOff, RefreshCw } from 'lucide-react'
import { useTranslation } from '../i18n'

type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

interface ConnectionBadgeProps {
  status: ConnectionStatus
}

export function ConnectionBadge({ status }: ConnectionBadgeProps) {
  const { t } = useTranslation()

  if (status === 'connected') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-2.5 sm:px-3 py-1 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--color-success) 12%, transparent)',
          color: 'var(--color-success)',
        }}
        title={t('connection.connected')}
      >
        <span className="relative flex h-2 w-2">
          <span
            className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
            style={{ backgroundColor: 'var(--color-success)' }}
          />
          <span
            className="relative inline-flex h-2 w-2 rounded-full"
            style={{ backgroundColor: 'var(--color-success)' }}
          />
        </span>
        <Wifi className="h-3.5 w-3.5" />
        <span className="hidden sm:inline">{t('connection.connected')}</span>
      </span>
    )
  }

  if (status === 'reconnecting') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-2.5 sm:px-3 py-1 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
          color: 'var(--color-warning)',
        }}
        title={t('connection.reconnecting')}
      >
        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
        <span className="hidden sm:inline">{t('connection.reconnecting')}</span>
      </span>
    )
  }

  return (
    <span
      className="inline-flex items-center gap-1.5 px-2.5 sm:px-3 py-1 rounded-full text-xs font-semibold"
      style={{
        backgroundColor: 'color-mix(in srgb, var(--color-muted-foreground) 12%, transparent)',
        color: 'var(--color-muted-foreground)',
      }}
      title={t('connection.disconnected')}
    >
      <WifiOff className="h-3.5 w-3.5" />
      <span className="hidden sm:inline">{t('connection.disconnected')}</span>
    </span>
  )
}
