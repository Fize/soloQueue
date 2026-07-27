import { useEffect, useState, useRef, useCallback } from 'react'
import {
  Bot,
  Cpu,
  Activity,
  AlertCircle,
  ShieldAlert,
  Terminal,
  FileText,
  CheckCircle2,
  Wifi,
  WifiOff,
  Loader2,
  Database,
  Layers,
  RefreshCw,
  X,
  BarChart3,
  Hash,
  Clock,
} from 'lucide-react'
import { ThemeToggle } from './theme'
import { useTranslation } from './i18n'

// ─── Types ───
type AgentState = 'idle' | 'processing' | 'stopping' | 'stopped'
type ConnectionStatus = 'connected' | 'disconnected' | 'reconnecting'

interface AgentInfo {
  id: string
  instance_id: string
  name: string
  state: AgentState
  model_id: string
  provider_id: string
  group: string
  is_leader: boolean
  task_level: string
  error_count: number
  last_error: string
  iteration?: number
}

interface Segment {
  type: 'thinking' | 'content' | 'tool_call'
  text?: string
  call_id?: string
  name?: string
  args?: string
  result?: string
  error?: string
  done?: boolean
  duration_ms?: number
}

interface AgentStreamState {
  agent_id: string
  processing: boolean
  segments: Segment[]
  iteration: number
  error?: string
}

interface CronTaskStatus {
  id: string
  title: string
  task_level: 'L0' | 'L1' | 'L2' | 'L3'
  expression: string
  instruction: string
  target_agent: string
  status: string
  last_run_at: string | null
  next_run_at: string
  is_one_time: boolean
}

interface RuntimeStatus {
  phase: string
  prompt_tokens: number
  output_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
  context_pct: number
  total_agents: number
  running_agents: number
  idle_agents: number
  total_errors: number
  agent_streams: Record<string, AgentStreamState>
}

interface AggregatedTokenUsage {
  period: string
  usage_type: string
  team_id: string
  model_name: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
}

// ─── Constants ───
const RECONNECT_DELAY = 2000

// ─── Helpers ───
function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

function formatDateLabel(periodStr: string): string {
  if (!periodStr) return ''
  const d = new Date(periodStr + 'Z')
  if (isNaN(d.getTime())) return periodStr.slice(0, 10)
  return `${d.getMonth() + 1}/${d.getDate()}`
}

function resolveAgentName(agents: AgentInfo[], id: string) {
  return agents.find(a => a.id === id)?.name || id.slice(0, 8)
}

// ─── Metric Card ───
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

function MetricCard({ title, icon, iconColor, mainValue, subValue, detail, progress, progressColor, isEmpty }: MetricCardProps) {
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

// ─── Connection Badge ───
function ConnectionBadge({ status }: { status: ConnectionStatus }) {
  const { t } = useTranslation()
  if (status === 'connected') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--color-success) 12%, transparent)',
          color: 'var(--color-success)',
        }}
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
        {t('connection.connected')}
      </span>
    )
  }

  if (status === 'reconnecting') {
    return (
      <span
        className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold"
        style={{
          backgroundColor: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
          color: 'var(--color-warning)',
        }}
      >
        <RefreshCw className="h-3.5 w-3.5 animate-spin" />
        {t('connection.reconnecting')}
      </span>
    )
  }

  return (
    <span
      className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-semibold"
      style={{
        backgroundColor: 'color-mix(in srgb, var(--color-muted-foreground) 12%, transparent)',
        color: 'var(--color-muted-foreground)',
      }}
    >
      <WifiOff className="h-3.5 w-3.5" />
      {t('connection.disconnected')}
    </span>
  )
}

// ─── Agent State Badge ───
function AgentStateBadge({ state }: { state: AgentState }) {
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

  return (
    <span
      className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-xs font-semibold"
      style={{
        backgroundColor: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)',
        color: 'var(--color-destructive)',
      }}
    >
      <span className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: 'var(--color-destructive)' }} />
      {t('table.badges.stopped')}
    </span>
  )
}

// ─── Empty State ───
function EmptyState({ icon, title, description }: { icon: React.ReactNode; title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3 text-center px-6">
      <div
        className="w-12 h-12 rounded-full flex items-center justify-center"
        style={{ backgroundColor: 'var(--color-surface-secondary)' }}
      >
        <span style={{ color: 'var(--color-muted-foreground)' }}>{icon}</span>
      </div>
      <span className="text-base font-medium" style={{ color: 'var(--color-foreground)' }}>
        {title}
      </span>
      <span className="text-sm max-w-xs" style={{ color: 'var(--color-muted-foreground)' }}>
        {description}
      </span>
    </div>
  )
}

// ─── Token Stats Section ───
function TokenStatsSection({ t }: { t: (key: string, v?: Record<string, string | number>) => string }) {
  const [data, setData] = useState<AggregatedTokenUsage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    const fetchStats = async () => {
      try {
        setLoading(true)
        const res = await fetch('/api/stats/tokens?timeframe=daily')
        if (!res.ok) {
          if (res.status === 503) {
            if (!cancelled) setError('db_unavailable')
            return
          }
          throw new Error(`HTTP ${res.status}`)
        }
        const json: AggregatedTokenUsage[] = await res.json()
        if (!cancelled) {
          setData(json)
          setError(null)
        }
      } catch (err) {
        if (!cancelled) setError(String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    fetchStats()
    return () => { cancelled = true }
  }, [])

  if (loading) {
    return (
      <section
        className="rounded-xl overflow-hidden animate-slide-up"
        style={{ backgroundColor: 'var(--color-card)' }}
      >
        <div className="flex flex-col items-center justify-center py-12 gap-3">
          <Loader2 className="h-6 w-6 animate-spin" style={{ color: 'var(--color-muted-foreground)' }} />
          <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.loading')}
          </span>
        </div>
      </section>
    )
  }

  if (error === 'db_unavailable') {
    return (
      <section
        className="rounded-xl overflow-hidden animate-slide-up"
        style={{ backgroundColor: 'var(--color-card)' }}
      >
        <div className="flex flex-col items-center justify-center py-12 gap-3">
          <Database className="h-6 w-6" style={{ color: 'var(--color-muted-foreground)' }} />
          <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.dbUnavailable')}
          </span>
        </div>
      </section>
    )
  }

  if (error) {
    return (
      <section
        className="rounded-xl overflow-hidden animate-slide-up"
        style={{ backgroundColor: 'var(--color-card)' }}
      >
        <div className="flex flex-col items-center justify-center py-12 gap-3">
          <AlertCircle className="h-6 w-6" style={{ color: 'var(--color-destructive)' }} />
          <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
            {error}
          </span>
        </div>
      </section>
    )
  }

  if (data.length === 0) {
    return (
      <section
        className="rounded-xl overflow-hidden animate-slide-up"
        style={{ backgroundColor: 'var(--color-card)' }}
      >
        <div className="flex flex-col items-center justify-center py-12 gap-3">
          <BarChart3 className="h-6 w-6" style={{ color: 'var(--color-muted-foreground)' }} />
          <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.noData')}
          </span>
        </div>
      </section>
    )
  }

  // Aggregate totals
  const totalPrompt = data.reduce((s, d) => s + d.prompt_tokens, 0)
  const totalCompletion = data.reduce((s, d) => s + d.completion_tokens, 0)
  const totalCacheHit = data.reduce((s, d) => s + d.cache_hit_tokens, 0)
  const totalCacheTokens = totalCacheHit + data.reduce((s, d) => s + d.cache_miss_tokens, 0)
  const cacheHitRate = totalCacheTokens > 0 ? (totalCacheHit / totalCacheTokens) * 100 : 0

  // Get last 7 days sorted ascending
  const last7Days = [...data]
    .sort((a, b) => a.period.localeCompare(b.period))
    .slice(-7)
  const maxTokens = Math.max(...last7Days.map(d => d.total_tokens), 1)

  // Aggregate by model
  const byModel: Record<string, { prompt: number; completion: number; cacheHit: number; total: number }> = {}
  data.forEach(d => {
    const m = d.model_name || 'Unknown'
    if (!byModel[m]) byModel[m] = { prompt: 0, completion: 0, cacheHit: 0, total: 0 }
    byModel[m].prompt += d.prompt_tokens
    byModel[m].completion += d.completion_tokens
    byModel[m].cacheHit += d.cache_hit_tokens
    byModel[m].total += d.total_tokens
  })
  const modelEntries = Object.entries(byModel).sort((a, b) => b[1].total - a[1].total)

  return (
    <section
      className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
      style={{ backgroundColor: 'var(--color-card)' }}
    >
      {/* Section header */}
      <div
        className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
        style={{ borderColor: 'var(--color-border)' }}
      >
        <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--color-foreground)' }}>
          <BarChart3 className="h-4 w-4" style={{ color: 'var(--color-accent)' }} />
          {t('tokenStats.title')}
        </h2>
      </div>

      {/* Summary row */}
      <div className="px-4 sm:px-6 py-4 grid grid-cols-3 gap-4 border-b" style={{ borderColor: 'var(--color-border)' }}>
        <div className="flex flex-col items-center gap-1">
          <span className="text-xs font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.summaryInput')}
          </span>
          <span className="text-lg font-bold tabular-nums" style={{ color: 'var(--color-primary)' }}>
            {formatTokenCount(totalPrompt)}
          </span>
        </div>
        <div className="flex flex-col items-center gap-1">
          <span className="text-xs font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.summaryOutput')}
          </span>
          <span className="text-lg font-bold tabular-nums" style={{ color: 'var(--color-accent)' }}>
            {formatTokenCount(totalCompletion)}
          </span>
        </div>
        <div className="flex flex-col items-center gap-1">
          <span className="text-xs font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.summaryCacheHit')}
          </span>
          <span className="text-lg font-bold tabular-nums" style={{ color: 'var(--color-success)' }}>
            {cacheHitRate.toFixed(1)}%
          </span>
        </div>
      </div>

      {/* Bar chart */}
      <div className="px-4 sm:px-6 py-5 border-b" style={{ borderColor: 'var(--color-border)' }}>
        <h3 className="text-xs font-semibold mb-4" style={{ color: 'var(--color-muted-foreground)' }}>
          {t('tokenStats.chartTitleDaily')}
        </h3>
        <div className="flex items-end gap-2 h-32">
          {last7Days.map((d) => {
            const inputPct = maxTokens > 0 ? (d.prompt_tokens / maxTokens) * 100 : 0
            const outputPct = maxTokens > 0 ? (d.completion_tokens / maxTokens) * 100 : 0
            return (
              <div key={d.period} className="flex-1 flex flex-col items-center gap-1 h-full justify-end">
                <span className="text-[10px] font-mono tabular-nums" style={{ color: 'var(--color-muted-foreground)' }}>
                  {formatTokenCount(d.total_tokens)}
                </span>
                <div className="w-full flex flex-col justify-end rounded-sm overflow-hidden" style={{ height: '100%', backgroundColor: 'var(--color-surface-secondary)' }}>
                  <div
                    className="w-full rounded-t-sm transition-all duration-500 ease-out bar-grow"
                    style={{
                      height: `${inputPct}%`,
                      backgroundColor: 'var(--color-primary)',
                      ['--bar-height' as string]: `${inputPct}%`,
                      opacity: inputPct > 0 ? 0.85 : 0,
                    }}
                  />
                  <div
                    className="w-full rounded-t-sm transition-all duration-500 ease-out bar-grow"
                    style={{
                      height: `${outputPct}%`,
                      backgroundColor: 'var(--color-accent)',
                      ['--bar-height' as string]: `${outputPct}%`,
                      opacity: outputPct > 0 ? 0.85 : 0,
                    }}
                  />
                </div>
                <span className="text-[10px] font-mono mt-1" style={{ color: 'var(--color-muted-foreground)' }}>
                  {formatDateLabel(d.period)}
                </span>
              </div>
            )
          })}
        </div>
        {/* Legend */}
        <div className="flex items-center gap-4 mt-4 justify-center">
          <span className="flex items-center gap-1.5 text-[11px]" style={{ color: 'var(--color-muted-foreground)' }}>
            <span className="w-3 h-3 rounded-sm inline-block" style={{ backgroundColor: 'var(--color-primary)' }} />
            {t('tokenStats.chartInput')}
          </span>
          <span className="flex items-center gap-1.5 text-[11px]" style={{ color: 'var(--color-muted-foreground)' }}>
            <span className="w-3 h-3 rounded-sm inline-block" style={{ backgroundColor: 'var(--color-accent)' }} />
            {t('tokenStats.chartOutput')}
          </span>
        </div>
      </div>

      {/* By model table */}
      <div className="px-4 sm:px-6 py-4">
        <h3 className="text-xs font-semibold mb-3" style={{ color: 'var(--color-muted-foreground)' }}>
          {t('tokenStats.modelTitle')}
        </h3>
        <div className="table-scroll">
          <table className="w-full text-left text-xs border-collapse">
            <thead>
              <tr>
                <th className="py-2 pr-4 font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>
                  {t('tokenStats.modelCol')}
                </th>
                <th className="py-2 pr-4 font-semibold text-right" style={{ color: 'var(--color-muted-foreground)' }}>
                  {t('tokenStats.promptCol')}
                </th>
                <th className="py-2 pr-4 font-semibold text-right" style={{ color: 'var(--color-muted-foreground)' }}>
                  {t('tokenStats.completionCol')}
                </th>
                <th className="py-2 pr-4 font-semibold text-right" style={{ color: 'var(--color-muted-foreground)' }}>
                  {t('tokenStats.cacheCol')}
                </th>
                <th className="py-2 font-semibold text-right" style={{ color: 'var(--color-muted-foreground)' }}>
                  {t('tokenStats.totalCol')}
                </th>
              </tr>
            </thead>
            <tbody>
              {modelEntries.map(([model, stats]) => (
                <tr key={model} style={{ borderTop: '1px solid var(--color-border)' }}>
                  <td className="py-2 pr-4 font-medium font-mono" style={{ color: 'var(--color-foreground)' }}>
                    {model}
                  </td>
                  <td className="py-2 pr-4 text-right font-mono tabular-nums" style={{ color: 'var(--color-primary)' }}>
                    {formatTokenCount(stats.prompt)}
                  </td>
                  <td className="py-2 pr-4 text-right font-mono tabular-nums" style={{ color: 'var(--color-accent)' }}>
                    {formatTokenCount(stats.completion)}
                  </td>
                  <td className="py-2 pr-4 text-right font-mono tabular-nums" style={{ color: 'var(--color-success)' }}>
                    {formatTokenCount(stats.cacheHit)}
                  </td>
                  <td className="py-2 text-right font-mono font-semibold tabular-nums" style={{ color: 'var(--color-foreground)' }}>
                    {formatTokenCount(stats.total)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  )
}

// ─── Agent Detail Modal ───
interface AgentModalProps {
  agent: AgentInfo
  stream: AgentStreamState | undefined
  onClose: () => void
  t: (key: string, v?: Record<string, string | number>) => string
}

function AgentModal({ agent, stream, onClose, t }: AgentModalProps) {
  const overlayRef = useRef<HTMLDivElement>(null)
  const [closing, setClosing] = useState(false)

  // Lock body scroll
  useEffect(() => {
    document.body.classList.add('modal-open')
    return () => document.body.classList.remove('modal-open')
  }, [])

  // Close on ESC
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') handleClose()
    }
    document.addEventListener('keydown', handler)
    return () => document.removeEventListener('keydown', handler)
  })

  const handleClose = useCallback(() => {
    setClosing(true)
    setTimeout(() => onClose(), 150)
  }, [onClose])

  const handleOverlayClick = (e: React.MouseEvent) => {
    if (e.target === overlayRef.current) handleClose()
  }

  const thinkingSegments = stream?.segments?.filter(s => s.type === 'thinking') || []
  const contentSegments = stream?.segments?.filter(s => s.type === 'content') || []
  const toolSegments = stream?.segments?.filter(s => s.type === 'tool_call') || []
  const hasStreamData = thinkingSegments.length > 0 || contentSegments.length > 0 || toolSegments.length > 0

  return (
    <div
      ref={overlayRef}
      onClick={handleOverlayClick}
      className={`fixed inset-0 z-[100] flex items-center justify-center p-4 sm:p-6 ${closing ? 'modal-overlay-exiting' : 'modal-overlay-entering'}`}
      style={{
        backgroundColor: 'rgba(0,0,0,0.6)',
        backdropFilter: 'blur(4px)',
      }}
    >
      <div
        className={`w-full max-w-2xl max-h-[85vh] flex flex-col rounded-xl overflow-hidden shadow-2xl ${closing ? 'modal-content-exiting' : 'modal-content-entering'}`}
        style={{ backgroundColor: 'var(--color-card)' }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal header */}
        <div
          className="px-5 py-4 border-b flex items-center justify-between shrink-0"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <div className="flex items-center gap-3 min-w-0">
            <div
              className="h-9 w-9 rounded-lg flex items-center justify-center shrink-0"
              style={{
                backgroundColor: 'color-mix(in srgb, var(--color-primary) 12%, transparent)',
                color: 'var(--color-primary)',
              }}
            >
              <Bot className="h-5 w-5" />
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-bold truncate" style={{ color: 'var(--color-foreground)' }}>
                {agent.name}
              </h2>
              {agent.is_leader && (
                <span
                  className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-bold mt-0.5"
                  style={{
                    backgroundColor: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
                    color: 'var(--color-warning)',
                  }}
                >
                  {t('table.badges.leader')}
                </span>
              )}
            </div>
          </div>
          <button
            onClick={handleClose}
            className="shrink-0 w-8 h-8 rounded-lg flex items-center justify-center cursor-pointer transition-colors"
            style={{
              color: 'var(--color-muted-foreground)',
            }}
            onMouseEnter={(e) => { e.currentTarget.style.backgroundColor = 'var(--color-surface-secondary)' }}
            onMouseLeave={(e) => { e.currentTarget.style.backgroundColor = 'transparent' }}
          >
            <X className="h-5 w-5" />
          </button>
        </div>

        {/* Agent info grid */}
        <div
          className="px-5 py-4 grid grid-cols-2 sm:grid-cols-4 gap-4 border-b shrink-0"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.state')}
            </span>
            <AgentStateBadge state={agent.state} />
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.model')}
            </span>
            <span className="text-sm font-mono truncate" style={{ color: 'var(--color-foreground)' }} title={agent.model_id}>
              {agent.model_id}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.group')}
            </span>
            <span className="text-sm font-mono" style={{ color: 'var(--color-foreground)' }}>
              {agent.group || 'Global'}
            </span>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-[11px] font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.level')}
            </span>
            <span
              className="inline-flex items-center px-2 py-0.5 rounded text-xs font-mono w-fit"
              style={{
                backgroundColor: 'var(--color-surface-secondary)',
                color: 'var(--color-foreground)',
              }}
            >
              {agent.task_level}
            </span>
          </div>
        </div>

        {/* Stats row */}
        <div
          className="px-5 py-3 flex items-center gap-6 border-b shrink-0"
          style={{ borderColor: 'var(--color-border)' }}
        >
          <span className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
            <Hash className="h-3.5 w-3.5" />
            {t('modal.iteration')}: <span className="font-semibold font-mono" style={{ color: 'var(--color-foreground)' }}>{stream?.iteration ?? (agent.iteration ?? 0)}</span>
          </span>
          <span className="flex items-center gap-1.5 text-xs" style={{ color: 'var(--color-muted-foreground)' }}>
            <AlertCircle className="h-3.5 w-3.5" style={{ color: agent.error_count > 0 ? 'var(--color-destructive)' : undefined }} />
            {t('modal.errors')}: <span className="font-semibold font-mono" style={{ color: agent.error_count > 0 ? 'var(--color-destructive)' : 'var(--color-foreground)' }}>{agent.error_count}</span>
          </span>
        </div>

        {/* Error display */}
        {agent.last_error && (
          <div
            className="px-5 py-3 border-b shrink-0"
            style={{ borderColor: 'var(--color-border)', backgroundColor: 'color-mix(in srgb, var(--color-destructive) 8%, transparent)' }}
          >
            <span className="text-[11px] font-semibold block" style={{ color: 'var(--color-destructive)' }}>
              {t('modal.lastError')}:
            </span>
            <span className="text-xs font-mono whitespace-pre-wrap break-all mt-0.5 block" style={{ color: 'var(--color-destructive)' }}>
              {agent.last_error}
            </span>
          </div>
        )}

        {/* Live stream */}
        <div className="flex-1 overflow-y-auto p-5">
          <div className="flex items-center gap-2 mb-3">
            <Terminal className="h-4 w-4" style={{ color: agent.state === 'processing' ? 'var(--color-signal)' : 'var(--color-muted-foreground)' }} />
            <span className="text-xs font-semibold" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('modal.streamTitle')}
            </span>
          </div>

          {agent.state !== 'processing' && !hasStreamData ? (
            <div className="flex flex-col items-center justify-center py-8 gap-2">
              <Clock className="h-5 w-5" style={{ color: 'var(--color-muted-foreground)' }} />
              <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
                {t('modal.idle')}
              </span>
            </div>
          ) : !hasStreamData ? (
            <div className="flex items-center gap-2 py-8 justify-center">
              <Loader2 className="h-4 w-4 animate-spin" style={{ color: 'var(--color-muted-foreground)' }} />
              <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
                {t('modal.noStream')}
              </span>
            </div>
          ) : (
            <div className="font-mono text-xs leading-relaxed space-y-3">
              {thinkingSegments.map((seg, i) => (
                <div
                  key={`think-${i}`}
                  className="border-l-2 pl-3"
                  style={{ borderColor: 'color-mix(in srgb, var(--color-signal) 50%, transparent)' }}
                >
                  <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--color-signal)' }}>
                    {t('stream.thinking')}
                  </span>
                  <span className="whitespace-pre-wrap" style={{ color: 'var(--color-muted-foreground)' }}>
                    {seg.text}
                  </span>
                </div>
              ))}
              {contentSegments.map((seg, i) => (
                <div
                  key={`content-${i}`}
                  className="border-l-2 pl-3"
                  style={{ borderColor: 'color-mix(in srgb, var(--color-accent) 50%, transparent)' }}
                >
                  <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--color-accent)' }}>
                    {t('stream.content')}
                  </span>
                  <span className="whitespace-pre-wrap" style={{ color: 'var(--color-foreground)' }}>
                    {seg.text}
                  </span>
                </div>
              ))}
              {toolSegments.map((seg, i) => (
                <div
                  key={`tool-${i}`}
                  className="border-l-2 pl-3"
                  style={{ borderColor: seg.error ? 'var(--color-destructive)' : 'color-mix(in srgb, var(--color-primary) 50%, transparent)' }}
                >
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    <span
                      className="font-semibold text-xs"
                      style={{ color: seg.error ? 'var(--color-destructive)' : 'var(--color-primary)' }}
                    >
                      {t('stream.toolCall')}: {seg.name}
                    </span>
                    {seg.done && !seg.error && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded" style={{
                        backgroundColor: 'color-mix(in srgb, var(--color-success) 12%, transparent)',
                        color: 'var(--color-success)',
                      }}>
                        {t('stream.toolDone')}
                      </span>
                    )}
                    {seg.error && (
                      <span className="text-[10px] px-1.5 py-0.5 rounded" style={{
                        backgroundColor: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)',
                        color: 'var(--color-destructive)',
                      }}>
                        {t('stream.toolError')}
                      </span>
                    )}
                    {seg.duration_ms !== undefined && (
                      <span className="text-[10px]" style={{ color: 'var(--color-muted-foreground)' }}>
                        {t('stream.toolDuration', { ms: seg.duration_ms })}
                      </span>
                    )}
                  </div>
                  {seg.args && (
                    <details className="mt-1">
                      <summary className="text-[10px] cursor-pointer" style={{ color: 'var(--color-muted-foreground)' }}>
                        Arguments
                      </summary>
                      <pre className="text-[10px] mt-1 p-2 rounded overflow-x-auto" style={{
                        backgroundColor: 'var(--color-surface-secondary)',
                        color: 'var(--color-muted-foreground)',
                        maxHeight: '120px',
                      }}>
                        {seg.args}
                      </pre>
                    </details>
                  )}
                  {seg.result && !seg.error && (
                    <details className="mt-1">
                      <summary className="text-[10px] cursor-pointer" style={{ color: 'var(--color-muted-foreground)' }}>
                        Result
                      </summary>
                      <pre className="text-[10px] mt-1 p-2 rounded overflow-x-auto" style={{
                        backgroundColor: 'var(--color-surface-secondary)',
                        color: 'var(--color-foreground)',
                        maxHeight: '120px',
                      }}>
                        {seg.result}
                      </pre>
                    </details>
                  )}
                  {seg.error && (
                    <pre className="text-[10px] mt-1 p-2 rounded overflow-x-auto" style={{
                      backgroundColor: 'color-mix(in srgb, var(--color-destructive) 8%, transparent)',
                      color: 'var(--color-destructive)',
                      maxHeight: '120px',
                    }}>
                      {seg.error}
                    </pre>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// ════════════════════════════════════════════════════════════
//  App
// ════════════════════════════════════════════════════════════
export default function App() {
  const { t, language, setLanguage } = useTranslation()
  const [connStatus, setConnStatus] = useState<ConnectionStatus>('disconnected')
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null)
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [cronTasks, setCronTasks] = useState<CronTaskStatus[]>([])
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null)

  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const mountedRef = useRef(true)

  const connect = async () => {
    if (wsRef.current) return

    let token = ''
    try {
      const res = await fetch('/api/auth/token')
      if (res.ok) {
        const data = await res.json()
        token = data.token
      }
    } catch {
      // server might not have auth, continue without token
    }

    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    let url = `${proto}//${window.location.host}/ws`
    if (token) url += `?token=${encodeURIComponent(token)}`

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      if (mountedRef.current) setConnStatus('connected')
    }

    ws.onmessage = (event) => {
      if (event.data === 'ping') {
        ws.send('pong')
        return
      }
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'state') {
          if (msg.runtime) setRuntime(msg.runtime)
          if (msg.agents?.agents) setAgents(msg.agents.agents)
          if (msg.cron_tasks) setCronTasks(msg.cron_tasks)
        }
      } catch {
        // ignore malformed messages
      }
    }

    ws.onclose = () => {
      wsRef.current = null
      if (mountedRef.current) {
        setConnStatus('reconnecting')
        scheduleReconnect()
      }
    }

    ws.onerror = () => {
      ws.close()
    }
  }

  const scheduleReconnect = () => {
    if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    reconnectTimerRef.current = setTimeout(() => {
      if (mountedRef.current) connect()
    }, RECONNECT_DELAY)
  }

  useEffect(() => {
    mountedRef.current = true
    connect()
    return () => {
      mountedRef.current = false
      if (wsRef.current) wsRef.current.close()
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const isConnected = connStatus === 'connected'
  const activeStreams = runtime?.agent_streams
    ? Object.entries(runtime.agent_streams).filter(([_, s]) => s.processing)
    : []

  // Calculate token totals
  const promptTokens = runtime?.prompt_tokens ?? 0
  const outputTokens = runtime?.output_tokens ?? 0
  const totalTokens = promptTokens + outputTokens
  const contextPct = runtime?.context_pct ?? 0
  const totalErrors = runtime?.total_errors ?? 0
  const runningAgents = runtime?.running_agents ?? 0
  const totalAgents = runtime?.total_agents ?? 0

  const selectedAgent = agents.find(a => a.instance_id === selectedAgentId)
  const selectedStream = selectedAgentId ? runtime?.agent_streams?.[selectedAgentId] : undefined

  return (
    <div
      className="min-h-screen flex flex-col transition-colors duration-250"
      style={{ backgroundColor: 'var(--color-background)', color: 'var(--color-foreground)' }}
    >
      {/* ═══ Header ═══ */}
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
            onClick={() => setLanguage(language === 'en' ? 'zh' : 'en')}
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

      {/* ═══ Main Content ═══ */}
      <main className="flex-1 w-full max-w-7xl mx-auto px-4 sm:px-6 py-6 space-y-6">
        {/* ─── Metric Cards ─── */}
        <section className="metric-grid">
          <MetricCard
            title={t('metrics.activeAgents.title')}
            icon={<Bot className="h-5 w-5" />}
            iconColor={{ color: 'var(--color-primary)' }}
            mainValue={
              isConnected
                ? `${runningAgents}`
                : undefined
            }
            subValue={isConnected ? t('metrics.activeAgents.sub', { count: totalAgents }) : undefined}
            detail={isConnected ? t('metrics.activeAgents.detail', { running: runningAgents, idle: runtime?.idle_agents ?? 0 }) : undefined}
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
              isConnected && totalErrors > 0 && runtime?.phase
                ? t('metrics.systemErrors.detail', { phase: runtime.phase })
                : undefined
            }
            isEmpty={!isConnected}
          />
        </section>

        {/* ─── Token Usage Statistics ─── */}
        {isConnected && <TokenStatsSection t={t} />}

        {/* ─── Live Inference Stream ─── */}
        {isConnected && activeStreams.length > 0 && (
          <section className="space-y-3 animate-slide-up">
            <h2
              className="text-sm font-semibold tracking-wider uppercase flex items-center gap-2"
              style={{ color: 'var(--color-muted-foreground)' }}
            >
              <Terminal className="h-4 w-4" style={{ color: 'var(--color-signal)' }} />
              {t('stream.title')}
              <span
                className="text-xs font-normal normal-case"
                style={{ color: 'var(--color-muted-foreground)' }}
              >
                {t('stream.activeAgents', { count: activeStreams.length })}
              </span>
            </h2>

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              {activeStreams.map(([id, stream], idx) => {
                const agentName = resolveAgentName(agents, id)
                const thinkingSegments = stream.segments
                  ?.filter(s => s.type === 'thinking')
                  .map(s => s.text)
                  .join('') || ''
                const contentSegments = stream.segments
                  ?.filter(s => s.type === 'content')
                  .map(s => s.text)
                  .join('') || ''

                return (
                  <div
                    key={id}
                    className="rounded-xl overflow-hidden flex flex-col h-80 animate-slide-up shadow-sm"
                    style={{
                      backgroundColor: 'var(--color-card)',
                      animationDelay: `${idx * 80}ms`,
                    }}
                  >
                    {/* Stream header */}
                    <div
                      className="px-4 py-3 border-b flex items-center justify-between shrink-0"
                      style={{ borderColor: 'var(--color-border)', backgroundColor: 'var(--color-surface-secondary)' }}
                    >
                      <span className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--color-foreground)' }}>
                        <span className="relative flex h-2 w-2">
                          <span
                            className="absolute inline-flex h-full w-full rounded-full opacity-75 animate-ping"
                            style={{ backgroundColor: 'var(--color-signal)' }}
                          />
                          <span
                            className="relative inline-flex h-2 w-2 rounded-full"
                            style={{ backgroundColor: 'var(--color-signal)' }}
                          />
                        </span>
                        {agentName}
                      </span>
                      <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
                        {t('stream.iteration', { iteration: stream.iteration })}
                      </span>
                    </div>

                    {/* Stream body */}
                    <div className="flex-1 p-4 overflow-y-auto font-mono text-xs leading-relaxed space-y-3">
                      {thinkingSegments && (
                        <div
                          className="border-l-2 pl-3"
                          style={{ borderColor: 'color-mix(in srgb, var(--color-signal) 50%, transparent)' }}
                        >
                          <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--color-signal)' }}>
                            {t('stream.thinking')}
                          </span>
                          <span className="whitespace-pre-wrap" style={{ color: 'var(--color-muted-foreground)' }}>
                            {thinkingSegments}
                          </span>
                        </div>
                      )}
                      {contentSegments && (
                        <div
                          className="border-l-2 pl-3"
                          style={{ borderColor: 'color-mix(in srgb, var(--color-accent) 50%, transparent)' }}
                        >
                          <span className="font-semibold block mb-1 text-xs" style={{ color: 'var(--color-accent)' }}>
                            {t('stream.content')}
                          </span>
                          <span className="whitespace-pre-wrap" style={{ color: 'var(--color-foreground)' }}>
                            {contentSegments}
                          </span>
                        </div>
                      )}
                      {!thinkingSegments && !contentSegments && (
                        <div className="flex items-center gap-2 h-full justify-center" style={{ color: 'var(--color-muted-foreground)' }}>
                          <Loader2 className="h-4 w-4 animate-spin" />
                          <span>{t('stream.waiting')}</span>
                        </div>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          </section>
        )}

        {/* ─── Agent Status Table ─── */}
        <section
          className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
          style={{ backgroundColor: 'var(--color-card)' }}
        >
          {/* Table header */}
          <div
            className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
            style={{ borderColor: 'var(--color-border)' }}
          >
            <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--color-foreground)' }}>
              <Database className="h-4 w-4" style={{ color: 'var(--color-primary)' }} />
              {t('table.title')}
            </h2>
            <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
              {t('table.totalRegistered', { count: isConnected ? agents.length : 0 })}
            </span>
          </div>

          {/* Table body */}
          <div className="table-scroll">
            {!isConnected ? (
              <div className="flex flex-col items-center justify-center py-16 gap-3">
                <Loader2 className="h-6 w-6 animate-spin" style={{ color: 'var(--color-muted-foreground)' }} />
                <span className="text-sm" style={{ color: 'var(--color-muted-foreground)' }}>
                  {t('metrics.connecting')}
                </span>
              </div>
            ) : agents.length === 0 ? (
              <EmptyState
                icon={<Bot className="h-6 w-6" />}
                title={t('table.emptyTitle')}
                description={t('table.emptyDesc')}
              />
            ) : (
              <table className="w-full text-left text-sm border-collapse">
                <thead>
                  <tr style={{ backgroundColor: 'var(--color-surface-secondary)' }}>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                      style={{ color: 'var(--color-muted-foreground)' }}
                    >
                      {t('table.cols.name')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap"
                      style={{ color: 'var(--color-muted-foreground)' }}
                    >
                      {t('table.cols.status')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden sm:table-cell"
                      style={{ color: 'var(--color-muted-foreground)' }}
                    >
                      {t('table.cols.group')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden md:table-cell"
                      style={{ color: 'var(--color-muted-foreground)' }}
                    >
                      {t('table.cols.model')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap hidden lg:table-cell"
                      style={{ color: 'var(--color-muted-foreground)' }}
                    >
                      {t('table.cols.level')}
                    </th>
                    <th
                      className="px-4 sm:px-6 py-3 text-xs font-semibold uppercase tracking-wider whitespace-nowrap text-right"
                      style={{ color: 'var(--color-muted-foreground)' }}
                    >
                      {t('table.cols.errors')}
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {agents.map((agent, idx) => (
                    <tr
                      key={agent.instance_id}
                      className="transition-colors duration-150 cursor-pointer"
                      style={{
                        borderTop: '1px solid var(--color-border)',
                        animationDelay: `${idx * 30}ms`,
                      }}
                      onClick={() => setSelectedAgentId(agent.instance_id)}
                      onMouseEnter={(e) => {
                        e.currentTarget.style.backgroundColor = 'color-mix(in srgb, var(--color-foreground) 4%, transparent)'
                      }}
                      onMouseLeave={(e) => {
                        e.currentTarget.style.backgroundColor = 'transparent'
                      }}
                    >
                      <td className="px-4 sm:px-6 py-4">
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-sm" style={{ color: 'var(--color-foreground)' }}>
                            {agent.name}
                          </span>
                          {agent.is_leader && (
                            <span
                              className="px-1.5 py-0.5 rounded text-[10px] font-bold"
                              style={{
                                backgroundColor: 'color-mix(in srgb, var(--color-warning) 12%, transparent)',
                                color: 'var(--color-warning)',
                              }}
                            >
                              {t('table.badges.leader')}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-4 sm:px-6 py-4">
                        <AgentStateBadge state={agent.state} />
                      </td>
                      <td
                        className="px-4 sm:px-6 py-4 text-xs font-mono hidden sm:table-cell"
                        style={{ color: 'var(--color-muted-foreground)' }}
                      >
                        {agent.group || 'Global'}
                      </td>
                      <td
                        className="px-4 sm:px-6 py-4 text-xs font-mono hidden md:table-cell max-w-[160px] truncate"
                        style={{ color: 'var(--color-muted-foreground)' }}
                        title={agent.model_id}
                      >
                        {agent.model_id}
                      </td>
                      <td className="px-4 sm:px-6 py-4 hidden lg:table-cell">
                        <span
                          className="px-2 py-0.5 rounded text-xs font-mono"
                          style={{
                            backgroundColor: 'var(--color-surface-secondary)',
                            color: 'var(--color-foreground)',
                          }}
                        >
                          {agent.task_level}
                        </span>
                      </td>
                      <td className="px-4 sm:px-6 py-4 text-right">
                        {agent.error_count > 0 ? (
                          <span
                            className="inline-flex items-center gap-1 text-xs font-semibold"
                            style={{ color: 'var(--color-destructive)' }}
                            title={agent.last_error || undefined}
                          >
                            <ShieldAlert className="h-3.5 w-3.5" />
                            {agent.error_count}
                          </span>
                        ) : (
                          <span
                            className="inline-flex items-center gap-1 text-xs"
                            style={{ color: 'var(--color-muted-foreground)' }}
                          >
                            <CheckCircle2 className="h-3.5 w-3.5" style={{ color: 'var(--color-success)' }} />
                            0
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </section>

        {/* ─── Scheduled Tasks ─── */}
        {isConnected && cronTasks.length > 0 && (
          <section
            className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
            style={{ backgroundColor: 'var(--color-card)' }}
          >
            {/* Section header */}
            <div
              className="px-4 sm:px-6 py-4 border-b flex items-center justify-between flex-wrap gap-2"
              style={{ borderColor: 'var(--color-border)' }}
            >
              <h2 className="text-sm font-semibold flex items-center gap-2" style={{ color: 'var(--color-foreground)' }}>
                <RefreshCw className="h-4 w-4" style={{ color: 'var(--color-primary)' }} />
                {t('cron.title')}
              </h2>
              <span className="text-xs font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
                {cronTasks.length === 1 ? t('cron.tasksCount', { count: 1 }) : t('cron.tasksCountPlural', { count: cronTasks.length })}
              </span>
            </div>

            {/* Task cards */}
            <div className="divide-y" style={{ borderColor: 'var(--color-border)' }}>
              {cronTasks.map((task) => {
                const statusColors: Record<string, { bg: string; fg: string }> = {
                  active:    { bg: 'color-mix(in srgb, var(--color-success) 12%, transparent)', fg: 'var(--color-success)' },
                  paused:    { bg: 'color-mix(in srgb, var(--color-warning) 12%, transparent)', fg: 'var(--color-warning)' },
                  completed: { bg: 'color-mix(in srgb, var(--color-muted-foreground) 12%, transparent)', fg: 'var(--color-muted-foreground)' },
                  running:   { bg: 'color-mix(in srgb, var(--color-signal) 12%, transparent)', fg: 'var(--color-signal)' },
                  failed:    { bg: 'color-mix(in srgb, var(--color-destructive) 12%, transparent)', fg: 'var(--color-destructive)' },
                }
                const sc = statusColors[task.status] || statusColors.completed
                const isL1 = task.target_agent === 'L1'
                const agentColor = isL1
                  ? { bg: 'color-mix(in srgb, var(--color-primary) 12%, transparent)', fg: 'var(--color-primary)' }
                  : { bg: 'color-mix(in srgb, var(--color-accent) 12%, transparent)', fg: 'var(--color-accent)' }

                return (
                  <div
                    key={task.id}
                    className="px-4 sm:px-6 py-3 flex items-start gap-3"
                    style={{ animationDelay: `${cronTasks.indexOf(task) * 30}ms` }}
                  >
                    {/* Instruction */}
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium truncate" style={{ color: 'var(--color-foreground)' }}>
                        {task.title}
                      </p>
                      <p className="text-xs truncate mt-0.5" style={{ color: 'var(--color-muted-foreground)' }}>
                        {task.instruction}
                      </p>
                      <div className="flex items-center gap-2 mt-1.5 flex-wrap">
                        <span
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                          style={{
                            backgroundColor: 'color-mix(in srgb, var(--color-primary) 12%, transparent)',
                            color: 'var(--color-primary)',
                          }}
                        >
                          {task.task_level}
                        </span>
                        <span
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                          style={{
                            backgroundColor: agentColor.bg,
                            color: agentColor.fg,
                          }}
                        >
                          {task.target_agent}
                        </span>
                        <span
                          className="inline-flex items-center px-2 py-0.5 rounded text-[10px] font-semibold"
                          style={{
                            backgroundColor: sc.bg,
                            color: sc.fg,
                          }}
                        >
                          {task.status}
                        </span>
                        <span className="text-[10px] font-mono" style={{ color: 'var(--color-muted-foreground)' }}>
                          {t('cron.nextRun', { time: task.next_run_at || '--' })}
                        </span>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          </section>
        )}

        {/* ─── Footer Info ─── */}
        {isConnected && runtime && (
          <div
            className="flex flex-wrap items-center gap-x-4 gap-y-1 px-1 py-2 text-xs"
            style={{ color: 'var(--color-muted-foreground)' }}
          >
            <span className="flex items-center gap-1">
              <Layers className="h-3 w-3" />
              {t('footerInfo.phase', { phase: runtime.phase || 'Running' })}
            </span>
            <span className="flex items-center gap-1">
              <Database className="h-3 w-3" />
              {t('footerInfo.cache', { hit: formatTokenCount(runtime.cache_hit_tokens), miss: formatTokenCount(runtime.cache_miss_tokens) })}
            </span>
          </div>
        )}
      </main>

      {/* ═══ Footer ═══ */}
      <footer
        className="border-t px-6 py-4 text-center text-xs"
        style={{ borderColor: 'var(--color-border)', color: 'var(--color-muted-foreground)' }}
      >
        <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-center justify-between gap-1">
          <span>{t('footer.banner')}</span>
          <span>
            {isConnected ? t('footer.summary', { agents: totalAgents, tokens: formatTokenCount(totalTokens) }) : t('footer.disconnected')}
          </span>
        </div>
      </footer>

      {/* ═══ Agent Detail Modal ═══ */}
      {selectedAgent && (
        <AgentModal
          agent={selectedAgent}
          stream={selectedStream}
          onClose={() => setSelectedAgentId(null)}
          t={t}
        />
      )}
    </div>
  )
}
