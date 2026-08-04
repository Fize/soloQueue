import { useEffect, useState } from 'react'
import { BarChart3, Database, AlertCircle, Loader2, ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from '../i18n'
import { formatTokenCount } from '../App'

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

type PresetKey = '24h' | 'today' | '3d' | '7d'

interface PresetOption {
  key: PresetKey
  labelKey: string
}

const PRESET_OPTIONS: PresetOption[] = [
  { key: '24h', labelKey: 'tokenStats.preset24h' },
  { key: 'today', labelKey: 'tokenStats.presetToday' },
  { key: '3d', labelKey: 'tokenStats.preset3d' },
  { key: '7d', labelKey: 'tokenStats.preset7d' },
]

function toSQLiteUTCString(d: Date): string {
  const year = d.getUTCFullYear()
  const month = String(d.getUTCMonth() + 1).padStart(2, '0')
  const day = String(d.getUTCDate()).padStart(2, '0')
  const hours = String(d.getUTCHours()).padStart(2, '0')
  const minutes = String(d.getUTCMinutes()).padStart(2, '0')
  const seconds = String(d.getUTCSeconds()).padStart(2, '0')
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}

function getQueryParams(preset: PresetKey): { timeframe: string; from: string } {
  const now = new Date()
  if (preset === '24h') {
    // Rolling 24h window: current time minus 24 hours
    const from = new Date(now.getTime() - 24 * 3600 * 1000)
    return { timeframe: 'hourly', from: toSQLiteUTCString(from) }
  }
  if (preset === 'today') {
    // Calendar today: start of current local day (00:00:00)
    const from = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0, 0)
    return { timeframe: 'hourly', from: toSQLiteUTCString(from) }
  }
  if (preset === '3d') {
    // Start of local day 3 days ago
    const from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 2, 0, 0, 0, 0)
    return { timeframe: 'daily', from: toSQLiteUTCString(from) }
  }
  // 7d: Start of local day 7 days ago
  const from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6, 0, 0, 0, 0)
  return { timeframe: 'daily', from: toSQLiteUTCString(from) }
}

function formatDateLabel(periodStr: string, timeframe?: string, preset?: PresetKey): string {
  if (!periodStr) return ''
  const normalized = periodStr.includes('T') ? periodStr : periodStr.replace(' ', 'T') + 'Z'
  const d = new Date(normalized)
  if (isNaN(d.getTime())) return periodStr.slice(11, 16) || periodStr.slice(0, 10)
  if (timeframe === 'hourly') {
    const hours = String(d.getHours()).padStart(2, '0') + ':00'
    if (preset === '24h') {
      return `${d.getMonth() + 1}/${d.getDate()} ${hours}`
    }
    return hours
  }
  return `${d.getMonth() + 1}/${d.getDate()}`
}

export function TokenStats() {
  const { t } = useTranslation()
  const [data, setData] = useState<AggregatedTokenUsage[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState(true)
  const [preset, setPreset] = useState<PresetKey>('24h')

  useEffect(() => {
    let cancelled = false
    const fetchStats = async () => {
      try {
        setLoading(true)
        const { timeframe, from } = getQueryParams(preset)
        const res = await fetch(`/api/stats/tokens?timeframe=${timeframe}&from=${encodeURIComponent(from)}`)
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

    // 60s polling
    const interval = setInterval(fetchStats, 60_000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [preset])

  // Aggregate totals
  const totalPrompt = data.reduce((s, d) => s + d.prompt_tokens, 0)
  const totalCompletion = data.reduce((s, d) => s + d.completion_tokens, 0)
  const totalAll = totalPrompt + totalCompletion
  const totalCacheHit = data.reduce((s, d) => s + d.cache_hit_tokens, 0)
  const totalCacheTokens = totalCacheHit + data.reduce((s, d) => s + d.cache_miss_tokens, 0)
  const cacheHitRate = totalCacheTokens > 0 ? (totalCacheHit / totalCacheTokens) * 100 : 0

  // Chart items sorted ascending
  const chartItems = [...data].sort((a, b) => a.period.localeCompare(b.period))
  const maxTokens = Math.max(...chartItems.map(d => d.total_tokens), 1)
  const isHourly = preset === '24h' || preset === 'today'

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

  // ── Loading ──
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

  // ── Error states ──
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

  return (
    <section
      className="rounded-xl overflow-hidden animate-slide-up shadow-sm"
      style={{ backgroundColor: 'var(--color-card)' }}
    >
      {/* Section header with title, toggle & quick time presets */}
      <div
        className="px-4 sm:px-6 py-3 border-b flex items-center justify-between flex-wrap gap-2 select-none"
        style={{ borderColor: 'var(--color-border)' }}
      >
        <div
          className="flex items-center gap-2 cursor-pointer"
          onClick={() => setCollapsed(!collapsed)}
        >
          <BarChart3 className="h-4 w-4" style={{ color: 'var(--color-accent)' }} />
          <h2 className="text-sm font-semibold" style={{ color: 'var(--color-foreground)' }}>
            {t('tokenStats.title')}
          </h2>
          {collapsed ? <ChevronRight className="h-4 w-4" style={{ color: 'var(--color-muted-foreground)' }} /> : <ChevronDown className="h-4 w-4" style={{ color: 'var(--color-muted-foreground)' }} />}
        </div>

        {/* Quick preset time filters */}
        <div className="flex items-center gap-1.5 flex-wrap">
          {PRESET_OPTIONS.map((p) => {
            const isActive = preset === p.key
            return (
              <button
                key={p.key}
                onClick={(e) => {
                  e.stopPropagation()
                  setPreset(p.key)
                }}
                className={`px-2.5 py-1 rounded-full text-xs font-medium cursor-pointer transition-all ${
                  isActive ? 'font-semibold' : 'hover:opacity-80'
                }`}
                style={{
                  backgroundColor: isActive
                    ? 'color-mix(in srgb, var(--color-accent) 15%, transparent)'
                    : 'var(--color-surface-secondary)',
                  color: isActive ? 'var(--color-accent)' : 'var(--color-muted-foreground)',
                  border: isActive
                    ? '1px solid color-mix(in srgb, var(--color-accent) 40%, transparent)'
                    : '1px solid var(--color-border)',
                }}
              >
                {t(p.labelKey)}
              </button>
            )
          })}
        </div>
      </div>

      {/* Summary row — always visible */}
      <div className="px-4 sm:px-6 py-4 grid grid-cols-5 gap-3 border-b" style={{ borderColor: 'var(--color-border)' }}>
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
            {t('tokenStats.summaryTotal')}
          </span>
          <span className="text-lg font-bold tabular-nums" style={{ color: 'var(--color-foreground)' }}>
            {formatTokenCount(totalAll)}
          </span>
        </div>
        <div className="flex flex-col items-center gap-1">
          <span className="text-xs font-medium" style={{ color: 'var(--color-muted-foreground)' }}>
            {t('tokenStats.summaryCacheHitAbs')}
          </span>
          <span className="text-lg font-bold tabular-nums" style={{ color: 'var(--color-success)' }}>
            {formatTokenCount(totalCacheHit)}
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

      {/* Expanded content */}
      {!collapsed && (
        <>
          {/* Bar chart */}
          <div className="px-4 sm:px-6 py-5 border-b" style={{ borderColor: 'var(--color-border)' }}>
            <h3 className="text-xs font-semibold mb-4" style={{ color: 'var(--color-muted-foreground)' }}>
              {isHourly ? t('tokenStats.chartTitleHourly') : t('tokenStats.chartTitleDaily')}
            </h3>
            <div className="flex items-end gap-2 h-32 overflow-x-auto">
              {chartItems.map((d) => {
                const inputPct = maxTokens > 0 ? (d.prompt_tokens / maxTokens) * 100 : 0
                const outputPct = maxTokens > 0 ? (d.completion_tokens / maxTokens) * 100 : 0
                return (
                  <div key={d.period} className="flex-1 min-w-[24px] flex flex-col items-center gap-1 h-full justify-end">
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
                    <span className="text-[10px] font-mono mt-1 whitespace-nowrap" style={{ color: 'var(--color-muted-foreground)' }}>
                      {formatDateLabel(d.period, isHourly ? 'hourly' : 'daily', preset)}
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
                  {modelEntries.map(([model, stats]) => {
                    const slashIdx = model.indexOf('/')
                    const hasProvider = slashIdx > 0
                    const provider = hasProvider ? model.slice(0, slashIdx) : ''
                    const modelName = hasProvider ? model.slice(slashIdx + 1) : model

                    return (
                      <tr key={model} style={{ borderTop: '1px solid var(--color-border)' }}>
                        <td className="py-2 pr-4 font-medium font-mono" style={{ color: 'var(--color-foreground)' }}>
                          {hasProvider ? (
                            <span title={model}>
                              <span style={{ color: 'var(--color-muted-foreground)' }}>{provider}/</span>
                              <span style={{ color: 'var(--color-foreground)' }}>{modelName}</span>
                            </span>
                          ) : (
                            model
                          )}
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
                    )
                  })}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </section>
  )
}
