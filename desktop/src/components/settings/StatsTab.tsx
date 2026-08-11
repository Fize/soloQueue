import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  Clock3,
  Database,
  Gauge,
  RefreshCw,
  Sparkles,
  Zap,
} from 'lucide-react'
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from 'recharts'

import { ActivityHeatmap, type ActivityDay } from '@/components/ActivityHeatmap'
import { GlassCard } from '@/components/ui/glass-card'
import { Select } from '@/components/ui/select'
import {
  getStatsActivity,
  getStatsBreakdown,
  getStatsEvents,
  getStatsFilters,
  getStatsOverview,
  type StatsActivity,
  type StatsBreakdown,
  type StatsDimension,
  type StatsEvent,
  type StatsEvents,
  type StatsFilters,
  type StatsMetrics,
  type StatsOverview,
  type StatsQuery,
} from '@/lib/api/stats-api'
import { useTranslation } from '@/lib/i18n'
import { cn } from '@/lib/utils'

type RangeKey = '24h' | '7d' | '30d' | 'custom'
type TrendMetric = 'tokens' | 'calls' | 'errors' | 'latency'

interface DashboardFilters {
  team: string
  origin: string
  usageType: string
  taskType: string
  model: string
  status: string
}

const EMPTY_FILTERS: DashboardFilters = {
  team: '',
  origin: '',
  usageType: '',
  taskType: '',
  model: '',
  status: '',
}

const RANGE_HOURS: Record<Exclude<RangeKey, 'custom'>, number> = {
  '24h': 24,
  '7d': 24 * 7,
  '30d': 24 * 30,
}

const BREAKDOWN_DIMENSIONS: StatsDimension[] = [
  'model',
  'usage_type',
  'task_type',
  'origin',
  'status',
]

function initialRange(): RangeKey {
  const value = new URLSearchParams(window.location.search).get('stats_range')
  return value === '24h' || value === '7d' || value === 'custom' ? value : '30d'
}

function rangeToQuery(range: RangeKey, customFrom: string, customTo: string) {
  const now = new Date()
  if (range === 'custom' && customFrom && customTo) {
    return { from: new Date(customFrom).toISOString(), to: new Date(customTo).toISOString() }
  }
  const hours = range === 'custom' ? RANGE_HOURS['30d'] : RANGE_HOURS[range]
  return { from: new Date(now.getTime() - hours * 3_600_000).toISOString(), to: now.toISOString() }
}

function formatCompact(value: number): string {
  return Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(
    value
  )
}

function formatDuration(value: number | null): string {
  if (value === null) return 'N/A'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)} s`
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(value >= 0.1 ? 1 : 2)}%`
}

function formatDelta(value: number | null | undefined): string {
  if (value === null || value === undefined) return '—'
  return `${value > 0 ? '+' : ''}${value.toFixed(1)}%`
}

function metricValue(metrics: StatsMetrics, metric: TrendMetric): number {
  switch (metric) {
    case 'calls':
      return metrics.request_count
    case 'errors':
      return metrics.error_count + metrics.timeout_count
    case 'latency':
      return metrics.p95_duration_ms ?? 0
    default:
      return metrics.total_tokens
  }
}

function chartLabel(value: string, bucketSize: string, timezone: string): string {
  const date = new Date(value)
  const options: Intl.DateTimeFormatOptions =
    bucketSize === 'hour'
      ? { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZone: timezone }
      : { month: 'short', day: 'numeric', timeZone: timezone }
  return new Intl.DateTimeFormat(undefined, options).format(date)
}

function KpiCard({
  label,
  value,
  delta,
  icon,
  hint,
}: {
  label: string
  value: string
  delta?: number | null
  icon: React.ReactNode
  hint?: string
}) {
  return (
    <GlassCard variant="flat" className="min-w-0 p-4" data-testid="stats-kpi">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="truncate text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {label}
          </p>
          <p className="mt-2 text-2xl font-semibold tracking-tight text-foreground tabular-nums">
            {value}
          </p>
        </div>
        <span className="rounded-lg border border-border/70 bg-muted/40 p-2 text-primary">
          {icon}
        </span>
      </div>
      <div className="mt-2 min-h-4 text-[11px] text-muted-foreground">
        {delta !== undefined ? (
          <span className={cn(delta !== null && delta > 0 && 'text-amber-500')}>
            {formatDelta(delta)} {hint}
          </span>
        ) : (
          hint
        )}
      </div>
    </GlassCard>
  )
}

function BreakdownList({ title, data }: { title: string; data?: StatsBreakdown }) {
  const maximum = Math.max(...(data?.items.map((item) => item.metrics.total_tokens) ?? [0]), 1)
  return (
    <GlassCard variant="flat" className="p-4">
      <h3 className="text-sm font-semibold text-foreground">{title}</h3>
      <div className="mt-4 space-y-3">
        {data?.items.slice(0, 6).map((item) => (
          <div key={item.key}>
            <div className="mb-1.5 flex items-center justify-between gap-3 text-xs">
              <span className="truncate text-foreground" title={item.label}>
                {item.label}
              </span>
              <span className="shrink-0 font-mono text-muted-foreground tabular-nums">
                {formatCompact(item.metrics.total_tokens)}
              </span>
            </div>
            <div className="h-1.5 overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary/80"
                style={{ width: `${Math.max((item.metrics.total_tokens / maximum) * 100, 2)}%` }}
              />
            </div>
          </div>
        ))}
        {(!data || data.items.length === 0) && (
          <p className="py-8 text-center text-xs text-muted-foreground">
            No data for this selection.
          </p>
        )}
      </div>
    </GlassCard>
  )
}

function EventTable({ events }: { events: StatsEvent[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[760px] text-left text-xs">
        <thead className="border-b border-border text-[10px] uppercase tracking-wider text-muted-foreground">
          <tr>
            <th className="px-3 py-2 font-medium">Time</th>
            <th className="px-3 py-2 font-medium">Model</th>
            <th className="px-3 py-2 font-medium">Context</th>
            <th className="px-3 py-2 font-medium">Status</th>
            <th className="px-3 py-2 text-right font-medium">Tokens</th>
            <th className="px-3 py-2 text-right font-medium">Duration</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {events.map((event) => (
            <tr key={event.call_id} className="hover:bg-muted/30">
              <td className="whitespace-nowrap px-3 py-2.5 font-mono text-muted-foreground">
                {new Date(event.finished_at).toLocaleString()}
              </td>
              <td className="max-w-[220px] truncate px-3 py-2.5 text-foreground">
                {event.provider_id ? `${event.provider_id}/` : ''}
                {event.model_id || 'Unknown'}
              </td>
              <td className="px-3 py-2.5 text-muted-foreground">
                {event.origin} · {event.usage_type} · {event.task_type}
              </td>
              <td className="px-3 py-2.5">
                <span
                  className={cn(
                    'inline-flex rounded-full border px-2 py-0.5 text-[10px] font-medium',
                    event.status === 'success'
                      ? 'border-emerald-500/20 bg-emerald-500/10 text-emerald-500'
                      : event.status === 'unknown'
                        ? 'border-border bg-muted text-muted-foreground'
                        : 'border-destructive/20 bg-destructive/10 text-destructive'
                  )}
                >
                  {event.legacy ? 'Legacy' : event.status}
                </span>
              </td>
              <td className="px-3 py-2.5 text-right font-mono tabular-nums">
                {event.total_tokens.toLocaleString()}
              </td>
              <td className="px-3 py-2.5 text-right font-mono text-muted-foreground tabular-nums">
                {event.legacy ? 'N/A' : formatDuration(event.duration_ms)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function StatsTab() {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>(initialRange)
  const [customFrom, setCustomFrom] = useState('')
  const [customTo, setCustomTo] = useState('')
  const [filters, setFilters] = useState<DashboardFilters>(() => ({
    ...EMPTY_FILTERS,
    team: new URLSearchParams(window.location.search).get('stats_team') || '',
  }))
  const [options, setOptions] = useState<StatsFilters | null>(null)
  const [overview, setOverview] = useState<StatsOverview | null>(null)
  const [breakdowns, setBreakdowns] = useState<Partial<Record<StatsDimension, StatsBreakdown>>>({})
  const [activity, setActivity] = useState<ActivityDay[]>([])
  const [events, setEvents] = useState<StatsEvent[]>([])
  const [nextCursor, setNextCursor] = useState<string | null>(null)
  const [trendMetric, setTrendMetric] = useState<TrendMetric>('tokens')
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [partialError, setPartialError] = useState(false)
  const [refreshKey, setRefreshKey] = useState(0)

  const timezone = useMemo(() => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', [])
  const rangeValues = useMemo(
    () => rangeToQuery(range, customFrom, customTo),
    [range, customFrom, customTo]
  )
  const query = useMemo<StatsQuery>(
    () => ({
      ...rangeValues,
      timezone,
      team_id: filters.team || undefined,
      origin: filters.origin || undefined,
      usage_type: filters.usageType || undefined,
      task_type: filters.taskType || undefined,
      model_id: filters.model || undefined,
      status: filters.status || undefined,
    }),
    [rangeValues, timezone, filters]
  )

  const refresh = useCallback(() => setRefreshKey((value) => value + 1), [])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    params.set('stats_range', range)
    if (filters.team) params.set('stats_team', filters.team)
    else params.delete('stats_team')
    window.history.replaceState(null, '', `${window.location.pathname}?${params}`)
  }, [range, filters.team])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    async function load() {
      setRefreshing(true)
      setError('')
      setPartialError(false)
      try {
        const nextOverview = await getStatsOverview(query, controller.signal)
        if (!active) return
        setOverview(nextOverview)

        const rangeOnly = { from: query.from, to: query.to, timezone: query.timezone }
        const activityQuery = { ...query }
        delete activityQuery.from
        delete activityQuery.to
        const results = await Promise.allSettled([
          getStatsFilters(rangeOnly, controller.signal),
          ...BREAKDOWN_DIMENSIONS.map((dimension) =>
            getStatsBreakdown(dimension, query, controller.signal)
          ),
          getStatsActivity(activityQuery, 365, controller.signal),
          getStatsEvents(query, undefined, 25, controller.signal),
        ])
        if (!active) return
        const [filterResult, ...rest] = results
        if (filterResult.status === 'fulfilled') setOptions(filterResult.value)
        const nextBreakdowns: Partial<Record<StatsDimension, StatsBreakdown>> = {}
        BREAKDOWN_DIMENSIONS.forEach((dimension, index) => {
          const result = rest[index]
          if (result.status === 'fulfilled')
            nextBreakdowns[dimension] = result.value as StatsBreakdown
        })
        setBreakdowns(nextBreakdowns)
        const activityResult = rest[BREAKDOWN_DIMENSIONS.length]
        if (activityResult.status === 'fulfilled') {
          const activityData = activityResult.value as StatsActivity
          setActivity(
            activityData.points
              .filter((point) => point.request_count > 0)
              .map((point) => ({ date: point.date, count: point.total_tokens, level: point.level }))
          )
        }
        const eventsResult = rest[BREAKDOWN_DIMENSIONS.length + 1]
        if (eventsResult.status === 'fulfilled') {
          const eventData = eventsResult.value as StatsEvents
          setEvents(eventData.items)
          setNextCursor(eventData.next_cursor)
        }
        setPartialError(results.some((result) => result.status === 'rejected'))
      } catch (reason) {
        if (!active || controller.signal.aborted) return
        setError(reason instanceof Error ? reason.message : t('common.error'))
      } finally {
        if (active) {
          setLoading(false)
          setRefreshing(false)
        }
      }
    }
    void load()
    return () => {
      active = false
      controller.abort()
    }
  }, [query, refreshKey, t])

  useEffect(() => {
    const timer = window.setInterval(refresh, 60_000)
    return () => window.clearInterval(timer)
  }, [refresh])

  const loadMore = async () => {
    if (!nextCursor) return
    try {
      const page = await getStatsEvents(query, nextCursor, 25)
      setEvents((current) => [...current, ...page.items])
      setNextCursor(page.next_cursor)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : t('common.error'))
    }
  }

  const updateFilter = (key: keyof DashboardFilters, value: string) => {
    setFilters((current) => ({ ...current, [key]: value }))
  }

  const clearFilters = () => setFilters(EMPTY_FILTERS)
  const hasFilters = Object.values(filters).some(Boolean)
  const summary = overview?.summary
  const chartData =
    overview?.series.map((point) => ({
      period: chartLabel(point.start, overview.meta.bucket_size, overview.meta.timezone),
      value: metricValue(point.metrics, trendMetric),
    })) ?? []

  const optionList = (items: { value: string; label: string }[] | undefined, allLabel: string) => [
    { value: '', label: allLabel },
    ...(items ?? []),
  ]

  return (
    <div className="space-y-5 pb-10">
      <header className="flex flex-col gap-4 border-b border-border/70 pb-5 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-primary" />
            <h2 className="text-xl font-semibold tracking-tight text-foreground">
              {t('stats.title')}
            </h2>
            {overview && overview.meta.coverage.legacy_rows > 0 && (
              <span className="rounded-full border border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-500">
                {overview.meta.coverage.legacy_rows.toLocaleString()} legacy
              </span>
            )}
          </div>
          <p className="mt-1 text-sm text-muted-foreground">{t('stats.subtitle')}</p>
          {overview && (
            <p className="mt-2 font-mono text-[10px] text-muted-foreground/70">
              {t('stats.updated')} {new Date(overview.meta.generated_at).toLocaleTimeString()} ·{' '}
              {overview.meta.timezone}
            </p>
          )}
        </div>
        <button
          type="button"
          onClick={refresh}
          disabled={refreshing}
          className="inline-flex h-8 items-center justify-center gap-2 rounded-lg border border-border bg-card px-3 text-xs font-medium text-foreground transition-colors hover:bg-muted disabled:opacity-50"
        >
          <RefreshCw className={cn('h-3.5 w-3.5', refreshing && 'animate-spin')} />
          {t('stats.refresh')}
        </button>
      </header>

      <section
        aria-label="Statistics filters"
        className="rounded-xl border border-border/70 bg-card/70 p-3"
      >
        <div className="flex flex-wrap items-center gap-2">
          {(['24h', '7d', '30d', 'custom'] as RangeKey[]).map((key) => (
            <button
              key={key}
              type="button"
              aria-pressed={range === key}
              onClick={() => setRange(key)}
              className={cn(
                'h-8 rounded-lg px-3 text-xs font-medium transition-colors',
                range === key
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )}
            >
              {key === 'custom' ? t('stats.custom') : key}
            </button>
          ))}
          <span className="mx-1 hidden h-5 w-px bg-border sm:block" />
          <Select
            value={filters.team}
            onChange={(value) => updateFilter('team', value)}
            options={optionList(options?.teams, t('stats.allTeams'))}
            className="w-[150px]"
          />
          <details className="group relative">
            <summary className="flex h-8 cursor-pointer list-none items-center rounded-lg border border-border px-3 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground">
              {t('stats.moreFilters')}
              {hasFilters ? ' ·' : ''}
            </summary>
            <div className="absolute right-0 z-20 mt-2 grid w-[440px] max-w-[80vw] grid-cols-2 gap-3 rounded-xl border border-border bg-card p-4 shadow-xl">
              <Select
                value={filters.origin}
                onChange={(value) => updateFilter('origin', value)}
                options={optionList(options?.origins, t('stats.allOrigins'))}
              />
              <Select
                value={filters.usageType}
                onChange={(value) => updateFilter('usageType', value)}
                options={optionList(options?.usage_types, t('stats.allUsageTypes'))}
              />
              <Select
                value={filters.taskType}
                onChange={(value) => updateFilter('taskType', value)}
                options={optionList(options?.task_types, t('stats.allTaskTypes'))}
              />
              <Select
                value={filters.model}
                onChange={(value) => updateFilter('model', value)}
                options={optionList(options?.models, t('stats.allModels'))}
              />
              <Select
                value={filters.status}
                onChange={(value) => updateFilter('status', value)}
                options={optionList(options?.statuses, t('stats.allStatuses'))}
              />
              <button
                type="button"
                onClick={clearFilters}
                className="h-8 rounded-lg border border-border text-xs text-muted-foreground hover:bg-muted hover:text-foreground"
              >
                {t('stats.clearFilters')}
              </button>
            </div>
          </details>
          {range === 'custom' && (
            <div className="flex flex-wrap items-center gap-2 border-l border-border pl-3">
              <input
                aria-label="From"
                type="datetime-local"
                value={customFrom}
                onChange={(event) => setCustomFrom(event.target.value)}
                className="h-8 rounded-lg border border-border bg-muted/30 px-2 text-xs text-foreground"
              />
              <span className="text-xs text-muted-foreground">–</span>
              <input
                aria-label="To"
                type="datetime-local"
                value={customTo}
                onChange={(event) => setCustomTo(event.target.value)}
                className="h-8 rounded-lg border border-border bg-muted/30 px-2 text-xs text-foreground"
              />
            </div>
          )}
        </div>
      </section>

      {error && (
        <div
          role="alert"
          className="flex items-start justify-between gap-3 rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive"
        >
          <span>{error}</span>
          <button
            type="button"
            onClick={refresh}
            className="font-medium underline underline-offset-2"
          >
            {t('stats.retry')}
          </button>
        </div>
      )}
      {partialError && !error && (
        <div className="flex items-center gap-2 rounded-lg border border-amber-500/20 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
          <AlertTriangle className="h-3.5 w-3.5" /> {t('stats.partialData')}
        </div>
      )}

      {loading && !overview ? (
        <div className="grid grid-cols-2 gap-3 xl:grid-cols-6">
          {Array.from({ length: 6 }).map((_, index) => (
            <div
              key={index}
              className="h-28 animate-pulse rounded-xl border border-border bg-muted/30"
            />
          ))}
        </div>
      ) : overview && summary ? (
        <>
          {summary.request_count === 0 && (
            <GlassCard variant="ghost" className="py-10 text-center">
              <Database className="mx-auto h-6 w-6 text-muted-foreground" />
              <p className="mt-3 text-sm font-medium text-foreground">{t('stats.emptyTitle')}</p>
              <p className="mt-1 text-xs text-muted-foreground">{t('stats.emptyDesc')}</p>
              {hasFilters && (
                <button
                  type="button"
                  onClick={clearFilters}
                  className="mt-3 text-xs font-medium text-primary hover:underline"
                >
                  {t('stats.clearFilters')}
                </button>
              )}
            </GlassCard>
          )}

          <div className="grid grid-cols-2 gap-3 xl:grid-cols-5">
            <KpiCard
              label={t('stats.totalTokens')}
              value={formatCompact(summary.total_tokens)}
              delta={overview.comparison.total_tokens?.change_pct}
              hint={t('stats.vsPrevious')}
              icon={<Zap className="h-4 w-4" />}
            />
            <KpiCard
              label={t('stats.requests')}
              value={summary.request_count.toLocaleString()}
              delta={overview.comparison.request_count?.change_pct}
              hint={t('stats.vsPrevious')}
              icon={<Activity className="h-4 w-4" />}
            />
            <KpiCard
              label={t('stats.successRate')}
              value={
                overview.meta.coverage.legacy_rows === overview.meta.coverage.total_rows
                  ? 'N/A'
                  : formatPercent(summary.success_rate)
              }
              hint={t('stats.knownCallsOnly')}
              icon={<CheckCircle2 className="h-4 w-4" />}
            />
            <KpiCard
              label={t('stats.p95Latency')}
              value={formatDuration(summary.p95_duration_ms)}
              delta={overview.comparison.p95_duration_ms?.change_pct}
              hint={t('stats.lowerIsBetter')}
              icon={<Clock3 className="h-4 w-4" />}
            />
            <KpiCard
              label={t('stats.cacheHits')}
              value={formatPercent(summary.cache_hit_rate)}
              hint={`${overview.meta.coverage.cache_coverage_pct.toFixed(0)}% ${t('stats.coverage')}`}
              icon={<Gauge className="h-4 w-4" />}
            />
          </div>

          {overview.insights.length > 0 && (
            <div className="grid gap-2 lg:grid-cols-2">
              {overview.insights.map((insight) => (
                <div
                  key={insight.id}
                  className={cn(
                    'flex items-start gap-3 rounded-xl border px-4 py-3',
                    insight.severity === 'critical' || insight.severity === 'warning'
                      ? 'border-amber-500/20 bg-amber-500/10'
                      : 'border-primary/20 bg-primary/5'
                  )}
                >
                  <Sparkles className="mt-0.5 h-4 w-4 shrink-0 text-primary" />
                  <div>
                    <p className="text-xs font-semibold text-foreground">{insight.title}</p>
                    <p className="mt-0.5 text-xs text-muted-foreground">{insight.detail}</p>
                  </div>
                </div>
              ))}
            </div>
          )}

          <GlassCard variant="flat" className="p-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div>
                <h3 className="text-sm font-semibold text-foreground">{t('stats.trend')}</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">{t('stats.trendDesc')}</p>
              </div>
              <div className="flex rounded-lg bg-muted/50 p-1">
                {(['tokens', 'calls', 'errors', 'latency'] as TrendMetric[]).map((metric) => (
                  <button
                    key={metric}
                    type="button"
                    onClick={() => setTrendMetric(metric)}
                    className={cn(
                      'rounded-md px-2.5 py-1 text-[11px] font-medium capitalize',
                      trendMetric === metric
                        ? 'bg-card text-foreground shadow-sm'
                        : 'text-muted-foreground'
                    )}
                  >
                    {t(`stats.${metric}` as 'stats.tokens')}
                  </button>
                ))}
              </div>
            </div>
            <div className="mt-4 h-[280px] text-muted-foreground">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartData} margin={{ top: 8, right: 12, left: 8, bottom: 0 }}>
                  <CartesianGrid
                    strokeDasharray="3 3"
                    vertical={false}
                    stroke="currentColor"
                    strokeOpacity={0.12}
                  />
                  <XAxis
                    dataKey="period"
                    tick={{ fontSize: 10, fill: 'currentColor' }}
                    tickLine={false}
                    axisLine={false}
                    minTickGap={24}
                  />
                  <YAxis
                    width={54}
                    tickFormatter={formatCompact}
                    tick={{ fontSize: 10, fill: 'currentColor' }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <RechartsTooltip
                    formatter={(value) => Number(value).toLocaleString()}
                    contentStyle={{
                      background: 'var(--color-card)',
                      border: '1px solid var(--color-border)',
                      borderRadius: 10,
                      fontSize: 12,
                    }}
                  />
                  <Legend wrapperStyle={{ fontSize: 11 }} />
                  <Line
                    type="monotone"
                    dataKey="value"
                    name={t(`stats.${trendMetric}` as 'stats.tokens')}
                    stroke="var(--color-primary)"
                    strokeWidth={2}
                    dot={chartData.length < 20 ? { r: 2 } : false}
                    activeDot={{ r: 4 }}
                  />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </GlassCard>

          <div className="grid gap-4 xl:grid-cols-3">
            <BreakdownList title={t('stats.topModels')} data={breakdowns.model} />
            <BreakdownList title={t('stats.usageTypes')} data={breakdowns.usage_type} />
            <BreakdownList title={t('stats.taskTypes')} data={breakdowns.task_type} />
            <BreakdownList title={t('stats.origins')} data={breakdowns.origin} />
            <BreakdownList title={t('stats.reliability')} data={breakdowns.status} />
            <GlassCard variant="flat" className="p-4">
              <h3 className="text-sm font-semibold text-foreground">{t('stats.dataCoverage')}</h3>
              <div className="mt-4 space-y-3 text-xs">
                {[
                  [t('stats.cache'), overview.meta.coverage.cache_coverage_pct],
                  [t('stats.reasoning'), overview.meta.coverage.reasoning_coverage_pct],
                ].map(([label, value]) => (
                  <div key={String(label)}>
                    <div className="mb-1 flex justify-between">
                      <span className="text-muted-foreground">{label}</span>
                      <span className="font-mono text-foreground">{Number(value).toFixed(0)}%</span>
                    </div>
                    <div className="h-1.5 rounded-full bg-muted">
                      <div
                        className="h-full rounded-full bg-primary/70"
                        style={{ width: `${value}%` }}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </GlassCard>
          </div>

          <details className="rounded-xl border border-border bg-card" open={false}>
            <summary className="cursor-pointer list-none px-4 py-3 text-sm font-semibold text-foreground">
              {t('stats.activityHeatmap')}{' '}
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                {t('stats.activityDesc')}
              </span>
            </summary>
            <div className="border-t border-border p-4">
              <ActivityHeatmap data={activity} days={365} />
            </div>
          </details>

          <GlassCard variant="flat" size="none" className="overflow-hidden">
            <div className="flex items-center justify-between border-b border-border px-4 py-3">
              <div>
                <h3 className="text-sm font-semibold text-foreground">{t('stats.recentCalls')}</h3>
                <p className="mt-0.5 text-xs text-muted-foreground">{t('stats.recentCallsDesc')}</p>
              </div>
              <span className="text-[10px] text-muted-foreground">
                {events.length} {t('stats.loaded')}
              </span>
            </div>
            {events.length > 0 ? (
              <EventTable events={events} />
            ) : (
              <p className="py-10 text-center text-xs text-muted-foreground">
                {t('stats.noEvents')}
              </p>
            )}
            {nextCursor && (
              <div className="border-t border-border p-3 text-center">
                <button
                  type="button"
                  onClick={loadMore}
                  className="rounded-lg border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
                >
                  {t('stats.loadMore')}
                </button>
              </div>
            )}
          </GlassCard>
        </>
      ) : null}
    </div>
  )
}
