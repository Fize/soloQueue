import { Bot, Cpu, FileText, AlertCircle } from 'lucide-react'
import { MetricCard } from './MetricCard'
import { useTranslation } from '../i18n'

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

interface MetricsRowProps {
  isConnected: boolean
  runningAgents: number
  totalAgents: number
  idleAgents: number
  totalTokens: number
  promptTokens: number
  outputTokens: number
  contextPct: number
  totalErrors: number
  phase?: string
}

export function MetricsRow({
  isConnected,
  runningAgents,
  totalAgents,
  idleAgents,
  totalTokens,
  promptTokens,
  outputTokens,
  contextPct,
  totalErrors,
  phase,
}: MetricsRowProps) {
  const { t } = useTranslation()

  return (
    <section className="metric-grid">
      <MetricCard
        title={t('metrics.activeAgents.title')}
        icon={<Bot className="h-5 w-5" />}
        iconColor={{ color: 'var(--color-primary)' }}
        mainValue={isConnected ? `${runningAgents}` : undefined}
        subValue={isConnected ? t('metrics.activeAgents.sub', { count: totalAgents }) : undefined}
        detail={isConnected ? t('metrics.activeAgents.detail', { running: runningAgents, idle: idleAgents }) : undefined}
        isEmpty={!isConnected}
      />

      <MetricCard
        title={t('metrics.tokenUsage.title')}
        icon={<Cpu className="h-5 w-5" />}
        iconColor={{ color: 'var(--color-accent)' }}
        mainValue={isConnected ? formatTokenCount(totalTokens) : undefined}
        subValue={isConnected ? t('metrics.tokenUsage.sub') : undefined}
        detail={isConnected ? t('metrics.tokenUsage.detail', { input: formatTokenCount(promptTokens), output: formatTokenCount(outputTokens) }) : undefined}
        isEmpty={!isConnected}
      />

      <MetricCard
        title={t('metrics.contextOccupancy.title')}
        icon={<FileText className="h-5 w-5" />}
        iconColor={{ color: 'var(--color-muted-foreground)' }}
        mainValue={isConnected ? `${contextPct}%` : undefined}
        subValue={isConnected ? t('metrics.contextOccupancy.sub') : undefined}
        progress={isConnected ? contextPct : undefined}
        progressColor={
          contextPct > 85
            ? 'var(--color-destructive)'
            : contextPct > 60
              ? 'var(--color-warning)'
              : 'var(--color-primary)'
        }
        isEmpty={!isConnected}
      />

      <MetricCard
        title={t('metrics.systemErrors.title')}
        icon={<AlertCircle className="h-5 w-5" />}
        iconColor={totalErrors > 0 ? { color: 'var(--color-destructive)' } : { color: 'var(--color-muted-foreground)' }}
        mainValue={isConnected ? totalErrors : undefined}
        subValue={isConnected ? t('metrics.systemErrors.sub') : undefined}
        detail={
          isConnected && totalErrors > 0 && phase
            ? t('metrics.systemErrors.detail', { phase })
            : undefined
        }
        isEmpty={!isConnected}
      />
    </section>
  )
}
