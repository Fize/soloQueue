import { Activity } from 'lucide-react'
import { ThemeToggle } from '../theme'
import { ConnectionBadge } from './ConnectionBadge'
import { useTranslation } from '../i18n'

type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'
type Language = 'en' | 'zh'

interface HeaderProps {
  connStatus: ConnectionStatus
  language: Language
  onToggleLanguage: () => void
}

export function Header({ connStatus, language, onToggleLanguage }: HeaderProps) {
  const { t } = useTranslation()

  return (
    <header
      className="sticky top-0 z-50 px-4 sm:px-6 py-3 flex items-center justify-between border-b transition-colors duration-250"
      style={{
        backgroundColor: 'color-mix(in srgb, var(--color-card) 80%, transparent)',
        borderColor: 'var(--color-border)',
        backdropFilter: 'blur(12px)',
      }}
    >
      <div className="flex items-center gap-3 min-w-0">
        <div
          className="h-9 w-9 rounded-xl flex items-center justify-center shrink-0"
          style={{
            backgroundColor: 'color-mix(in srgb, var(--color-primary) 12%, transparent)',
            color: 'var(--color-primary)',
          }}
        >
          <Activity className="h-5 w-5" />
        </div>
        <div className="min-w-0">
          <h1 className="text-base font-bold tracking-tight truncate" style={{ color: 'var(--color-foreground)' }}>
            {t('header.title')}
          </h1>
          <p className="text-xs font-mono truncate" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('header.desc')}
          </p>
        </div>
      </div>

      <div className="flex items-center gap-2 shrink-0">
        <button
          onClick={onToggleLanguage}
          className="px-2.5 py-1 text-xs font-semibold rounded-full cursor-pointer transition-all select-none"
          style={{
            borderColor: 'var(--color-border)',
            border: '1px solid var(--color-border)',
            backgroundColor: 'var(--color-surface-secondary)',
            color: 'var(--color-foreground)',
          }}
        >
          {language === 'en' ? 'EN' : '中文'}
        </button>
        <ThemeToggle />
        <ConnectionBadge status={connStatus} />
      </div>
    </header>
  )
}
