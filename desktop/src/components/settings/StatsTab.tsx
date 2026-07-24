import { useEffect, useState, useMemo, useCallback, useRef } from 'react'
import {
  LineChart,
  Line,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as RechartsTooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import { getTokenStats, getRouterStats, getStatTeams, type TokenStat, type RouterStat } from '@/lib/api'
import { getClassifierStats, type ClassifierStat } from '@/lib/api'
import { ActivityHeatmap, type ActivityDay } from '@/components/ActivityHeatmap'
import { TrendingUp, GitCommitHorizontal } from 'lucide-react'
import { Select } from '@/components/ui/select'
import { GlassCard } from '@/components/ui/glass-card'
import { useTranslation } from '@/lib/i18n'

const DATE_FMT = (d: Date) => {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

type RangeOffset = { from: Date; to: Date }

const PRESETS = [
  { labelKey: 'stats.presetAll' as const, suggestedTimeframe: '', offset: (): RangeOffset | null => null },
  { labelKey: 'stats.presetToday' as const, suggestedTimeframe: 'hourly', offset: (): RangeOffset | null => {
    const now = new Date()
    const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
    return { from: start, to: now }
  }},
  { labelKey: 'stats.preset24h' as const, suggestedTimeframe: 'hourly', offset: (): RangeOffset | null => {
    const now = new Date()
    return { from: new Date(now.getTime() - 24 * 3600_000), to: now }
  }},
  { labelKey: 'stats.preset3d' as const, suggestedTimeframe: 'hourly', offset: (): RangeOffset | null => {
    const now = new Date()
    return { from: new Date(now.getTime() - 3 * 24 * 3600_000), to: now }
  }},
  { labelKey: 'stats.preset7d' as const, suggestedTimeframe: 'daily', offset: (): RangeOffset | null => {
    const now = new Date()
    return { from: new Date(now.getTime() - 7 * 24 * 3600_000), to: now }
  }},
  { labelKey: 'stats.preset30d' as const, suggestedTimeframe: 'daily', offset: (): RangeOffset | null => {
    const now = new Date()
    return { from: new Date(now.getTime() - 30 * 24 * 3600_000), to: now }
  }},
]

const DEFAULT_PRESETS: Record<string, string> = {
  minutely: 'stats.preset24h',
  hourly: 'stats.presetToday',
  daily: 'stats.preset30d',
}

function toInputVal(dateStr: string) {
  return dateStr.replace(' ', 'T').slice(0, 16)
}

function alignToBucket(d: Date, timeframe: string): Date {
  const a = new Date(d)
  a.setSeconds(0, 0)
  if (timeframe === 'minutely') {
    return a
  }
  a.setMinutes(0)
  if (timeframe === 'hourly') {
    return a
  }
  a.setHours(0)
  if (timeframe === 'daily') {
    return a
  }
  if (timeframe === 'weekly') {
    const dow = a.getDay() || 7
    a.setDate(a.getDate() - dow + 1)
    return a
  }
  if (timeframe === 'monthly') {
    a.setDate(1)
    return a
  }
  return a
}

function generateBuckets(timeframe: string, from: Date, to: Date): string[] {
  const buckets: string[] = []
  const cur = alignToBucket(from, timeframe)
  const end = to
  while (cur <= end) {
    buckets.push(DATE_FMT(cur))
    if (timeframe === 'minutely') cur.setMinutes(cur.getMinutes() + 1)
    else if (timeframe === 'hourly') cur.setHours(cur.getHours() + 1)
    else if (timeframe === 'daily') cur.setDate(cur.getDate() + 1)
    else if (timeframe === 'weekly') cur.setDate(cur.getDate() + 7)
    else if (timeframe === 'monthly') cur.setMonth(cur.getMonth() + 1)
    else break
  }
  return buckets
}

/* ── custom tooltip ─────────────────────────────────────────── */

function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-lg border border-border bg-card px-3 py-2 shadow-md">
      <p className="text-[11px] font-mono text-muted-foreground mb-1">{label}</p>
      {payload.map((p: any, i: number) => (
        <div key={i} className="flex items-center gap-2 text-[11px]">
          <span className="h-2 w-2 rounded-full shrink-0" style={{ backgroundColor: p.color }} />
          <span className="text-muted-foreground">{p.name}:</span>
          <span className="font-mono font-semibold text-foreground tabular-nums">
            {typeof p.value === 'number' ? p.value.toLocaleString() : p.value}
          </span>
        </div>
      ))}
    </div>
  )
}

/* ── main component ────────────────────────────────────────── */

export function StatsTab() {
  const [timeframe, setTimeframe] = useState('daily')
  const [teamFilter, setTeamFilter] = useState('all')
  const [tokenStats, setTokenStats] = useState<TokenStat[]>([])
  const [routerStats, setRouterStats] = useState<RouterStat[]>([])
  const [classifierStats, setClassifierStats] = useState<ClassifierStat[]>([])
  const [teams, setTeams] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const { t } = useTranslation()

  const [activePreset, setActivePreset] = useState('')
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const mounted = useRef(false)

  const applyPreset = useCallback((key: string) => {
    setActivePreset(key)
    const preset = PRESETS.find(p => p.labelKey === key)
    if (!preset) return
    if (preset.suggestedTimeframe) {
      setTimeframe(preset.suggestedTimeframe)
    }
    const range = preset.offset()
    if (!range) {
      setFromDate('')
      setToDate('')
      return
    }
    setFromDate(DATE_FMT(range.from))
    setToDate(DATE_FMT(range.to))
  }, [])

  const handleCustomFrom = useCallback((val: string) => {
    setActivePreset('')
    const d = val ? new Date(val) : new Date(0)
    setFromDate(isNaN(d.getTime()) ? '' : DATE_FMT(d))
  }, [])

  const handleCustomTo = useCallback((val: string) => {
    setActivePreset('')
    const d = val ? new Date(val) : new Date(0)
    setToDate(isNaN(d.getTime()) ? '' : DATE_FMT(d))
  }, [])

  useEffect(() => {
    if (mounted.current) return
    mounted.current = true
    applyPreset(DEFAULT_PRESETS[timeframe] || 'stats.preset30d')
  }, [])

  useEffect(() => {
    let active = true
    const fetchData = async () => {
      setLoading(true); setError('')
      try {
        const teamParam = teamFilter === 'all' ? undefined : teamFilter
        const [tokenData, routerData, classifierData, teamsData] = await Promise.all([
          getTokenStats(timeframe, teamParam, fromDate || undefined, toDate || undefined),
          getRouterStats(timeframe, teamParam, fromDate || undefined, toDate || undefined),
          getClassifierStats(timeframe, fromDate || undefined, toDate || undefined),
          getStatTeams(),
        ])
        if (!active) return
        setTokenStats(tokenData || [])
        setRouterStats(routerData || [])
        setClassifierStats(classifierData || [])
        setTeams(teamsData || [])
      } catch (err: any) {
        if (active) setError(err.message || t('common.error'))
      } finally {
        if (active) setLoading(false)
      }
    }
    fetchData()
    return () => { active = false }
  }, [timeframe, teamFilter, fromDate, toDate, t])

  const tokenChartData = useMemo(() => {
    const grouped = new Map<string, { period: string; prompt: number; completion: number; cache: number }>()
    for (const row of tokenStats) {
      const p = row.period
      if (!grouped.has(p)) grouped.set(p, { period: p, prompt: 0, completion: 0, cache: 0 })
      const g = grouped.get(p)!
      g.prompt += row.prompt_tokens
      g.completion += row.completion_tokens
      g.cache += row.cache_hit_tokens
    }
    if (fromDate && toDate) {
      const fromD = new Date(fromDate.replace(' ', 'T'))
      const toD = new Date(toDate.replace(' ', 'T'))
      const buckets = generateBuckets(timeframe, fromD, toD)
      for (const b of buckets) {
        if (!grouped.has(b)) grouped.set(b, { period: b, prompt: 0, completion: 0, cache: 0 })
      }
    }
    return Array.from(grouped.values()).sort((a, b) => a.period.localeCompare(b.period))
  }, [tokenStats, timeframe, fromDate, toDate])

  const routerChartData = useMemo(() => {
    const grouped = new Map<string, { period: string; local: number; remote: number; error: number }>()
    for (const row of routerStats) {
      const p = row.period
      if (!grouped.has(p)) grouped.set(p, { period: p, local: 0, remote: 0, error: 0 })
      const g = grouped.get(p)!
      if (row.classification_source === 'local') {
        g.local += row.count
      } else if (row.classification_source === 'local-fallback' || row.classification_source === 'error') {
        g.error += row.count
      } else {
        g.remote += row.count
      }
    }
    if (fromDate && toDate) {
      const fromD = new Date(fromDate.replace(' ', 'T'))
      const toD = new Date(toDate.replace(' ', 'T'))
      const buckets = generateBuckets(timeframe, fromD, toD)
      for (const b of buckets) {
        if (!grouped.has(b)) grouped.set(b, { period: b, local: 0, remote: 0, error: 0 })
      }
    }
    return Array.from(grouped.values()).sort((a, b) => a.period.localeCompare(b.period))
  }, [routerStats, timeframe, fromDate, toDate])

  const classifierChartData = useMemo(() => {
    return classifierStats.map(s => ({
      period: s.period,
      ft: s.ft_count,
      llm: s.llm_count,
      error: s.llm_error_count,
      agreed: s.agreed_count,
      total: s.total_count,
      agreement: s.llm_count > 0 ? Math.round((s.agreed_count / s.llm_count) * 100) : 0,
      avgFtConf: s.avg_ft_conf,
      avgLlmConf: s.avg_llm_conf,
    })).sort((a, b) => a.period.localeCompare(b.period))
  }, [classifierStats])

  const [heatmapData, setHeatmapData] = useState<ActivityDay[]>([])
  const [heatmapLoading, setHeatmapLoading] = useState(true)

  useEffect(() => {
    let active = true
    async function load() {
      setHeatmapLoading(true)
      try {
        const teamParam = teamFilter === 'all' ? undefined : teamFilter
        const data = await getTokenStats('daily', teamParam)
        if (!active) return
        const grouped = new Map<string, number>()
        for (const row of data) {
          const date = row.period.split(' ')[0]
          grouped.set(date, (grouped.get(date) || 0) + row.total_tokens)
        }
        const entries: ActivityDay[] = []
        for (const [date, count] of grouped) entries.push({ date, count, level: 0 })
        entries.sort((a, b) => a.date.localeCompare(b.date))
        const counts = entries.map(e => e.count).filter(c => c > 0).sort((a, b) => a - b)
        if (counts.length > 0) {
          const q25 = counts[Math.floor(counts.length * 0.25)]
          const q50 = counts[Math.floor(counts.length * 0.5)]
          const q75 = counts[Math.floor(counts.length * 0.75)]
          for (const e of entries) {
            if (e.count === 0) e.level = 0
            else if (e.count <= q25) e.level = 1
            else if (e.count <= q50) e.level = 2
            else if (e.count <= q75) e.level = 3
            else e.level = 4
          }
        }
        setHeatmapData(entries)
      } catch { if (active) setHeatmapData([]) }
      finally { if (active) setHeatmapLoading(false) }
    }
    load()
    return () => { active = false }
  }, [teamFilter])

  const teamOptions = useMemo(() => {
    const opts: { value: string; label: string }[] = [
      { value: 'all', label: t('stats.allTeams') },
      { value: '__solo__', label: t('stats.soloL1') },
    ]
    for (const t of teams) opts.push({ value: t, label: t.charAt(0).toUpperCase() + t.slice(1) })
    return opts
  }, [teams, t])

  const fmtLabel = (p: string) => {
    const [date, time] = p.split(' ')
    const parts = date.split('-')
    const dateStr = parts.length === 3 ? `${parts[1]}/${parts[2]}` : p
    if (time) {
      const h = time.slice(0, 2), m = time.slice(3, 5)
      if (m !== '00') return `${dateStr} ${h}:${m}`
      if (h !== '00') return `${dateStr} ${h}:00`
    }
    return dateStr
  }

  const fmtTick = (v: number) => {
    if (v >= 1_000_000) return `${Number((v / 1_000_000).toFixed(1))}M`
    if (v >= 1_000) return `${Number((v / 1_000).toFixed(1))}k`
    return String(v)
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
          <TrendingUp className="h-4 w-4 text-primary" />
          {t('stats.title')}
        </h2>
        <p className="text-sm text-muted-foreground">
          {t('stats.title')}
        </p>
      </div>

      <div className="inline-flex w-fit flex-wrap items-center gap-3 rounded-xl border border-border/70 bg-card/70 px-3 py-2 shadow-sm">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">{t('stats.timeFrame')}</span>
          <Select value={timeframe} onChange={setTimeframe} options={[
            { value: 'minutely', label: t('stats.minutely') },
            { value: 'hourly', label: t('stats.hourly') },
            { value: 'daily', label: t('stats.daily') },
            { value: 'weekly', label: t('stats.weekly') },
            { value: 'monthly', label: t('stats.monthly') },
          ]} className="w-[116px]" />
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">{t('stats.teamFilter')}</span>
          <Select value={teamFilter} onChange={setTeamFilter} options={teamOptions} className="w-[140px]" />
        </div>
        <span className="h-5 w-px bg-border/70" />
        {PRESETS.map(p => (
          <button
            key={p.labelKey}
            className={`px-2 py-1 text-xs rounded-md transition-colors ${
              activePreset === p.labelKey
                ? 'bg-primary text-primary-foreground'
                : 'bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground'
            }`}
            onClick={() => applyPreset(p.labelKey)}
          >
            {t(p.labelKey as any)}
          </button>
        ))}
        <span className="h-5 w-px bg-border/70" />
        <div className="flex items-center gap-1.5">
          <input
            type="datetime-local"
            key={`from-${fromDate}`}
            value={fromDate ? toInputVal(fromDate) : ''}
            onChange={e => handleCustomFrom(e.target.value)}
            className="h-7 w-[160px] text-[11px] bg-muted/50 border border-border rounded-md px-1.5 text-foreground [color-scheme:dark]"
          />
          <span className="text-[10px] text-muted-foreground">-</span>
          <input
            type="datetime-local"
            key={`to-${toDate}`}
            value={toDate ? toInputVal(toDate) : ''}
            onChange={e => handleCustomTo(e.target.value)}
            className="h-7 w-[160px] text-[11px] bg-muted/50 border border-border rounded-md px-1.5 text-foreground [color-scheme:dark]"
          />
        </div>
      </div>

      {error && (
        <div className="p-3 text-sm text-destructive bg-destructive/10 rounded-md border border-destructive/20">{error}</div>
      )}

      {/* Activity Heatmap */}
      <GlassCard className="flex flex-col gap-3">
        <div>
          <h3 className="font-semibold text-foreground flex items-center gap-2">
            <GitCommitHorizontal className="h-4 w-4 text-primary" />
            {t('stats.activityHeatmap')}
          </h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            {t('stats.activityDesc')}
          </p>
        </div>
        <ActivityHeatmap data={heatmapData} days={365} loading={heatmapLoading} />
      </GlassCard>

      {!loading && !error && (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          {/* Token Consumption */}
          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div>
              <h3 className="font-semibold text-foreground">{t('stats.tokenConsumption')}</h3>
            </div>
            {tokenChartData.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                {t('stats.noTokenData')}
              </div>
            ) : (
              <div className="flex-1 h-[250px] text-muted-foreground">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={tokenChartData} margin={{ top: 10, right: 18, left: 8, bottom: 6 }}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" strokeOpacity={0.15} />
                    <XAxis dataKey="period" tickFormatter={fmtLabel} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                    <YAxis width={56} tickFormatter={fmtTick} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                    <RechartsTooltip content={<ChartTooltip />} cursor={{ fill: 'currentColor', fillOpacity: 0.08 }} />
                    <Legend wrapperStyle={{ fontSize: '12px' }} />
                    <Bar dataKey="prompt" name={t('stats.prompt')} fill="var(--color-chart-1)" radius={[3, 3, 0, 0]} />
                    <Bar dataKey="completion" name={t('stats.completion')} fill="var(--color-chart-2)" radius={[3, 3, 0, 0]} />
                    <Bar dataKey="cache" name={t('stats.cacheHits')} fill="var(--color-chart-3)" radius={[3, 3, 0, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </GlassCard>

          {/* Router Classifications */}
          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div>
              <h3 className="font-semibold text-foreground">{t('stats.routerClassifications')}</h3>
            </div>
            {routerChartData.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                {t('stats.noRouterData')}
              </div>
            ) : (
              <div className="flex-1 h-[250px] text-muted-foreground">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={routerChartData} margin={{ top: 10, right: 18, left: 8, bottom: 6 }}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" strokeOpacity={0.15} />
                    <XAxis dataKey="period" tickFormatter={fmtLabel} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                    <YAxis width={44} tickFormatter={fmtTick} allowDecimals={false} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                    <RechartsTooltip content={<ChartTooltip />} cursor={{ stroke: 'currentColor', strokeOpacity: 0.2 }} />
                    <Legend wrapperStyle={{ fontSize: '12px' }} />
                  <Line type="monotone" dataKey="local" name={t('stats.local')} stroke="var(--color-chart-4)" strokeWidth={2} dot={routerChartData.length > 24 ? false : { r: 3 }} activeDot={{ r: 5 }} />
                  <Line type="monotone" dataKey="remote" name={t('stats.remote')} stroke="var(--color-chart-5)" strokeWidth={2} dot={routerChartData.length > 24 ? false : { r: 3 }} activeDot={{ r: 5 }} />
                  <Line type="monotone" dataKey="error" name={t('stats.error')} stroke="var(--color-destructive)" strokeWidth={2} dot={routerChartData.length > 24 ? false : { r: 3 }} activeDot={{ r: 5 }} />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </GlassCard>
        </div>
      )}

      {/* Classifier Performance */}
      {!loading && !error && classifierChartData.length > 0 && (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div>
              <h3 className="font-semibold text-foreground">{t('stats.classifierDecisions')}</h3>
              <p className="text-xs text-muted-foreground mt-0.5">{t('stats.classifierDesc')}</p>
            </div>
            <div className="flex-1 h-[250px] text-muted-foreground">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={classifierChartData} margin={{ top: 10, right: 18, left: 8, bottom: 6 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" strokeOpacity={0.15} />
                  <XAxis dataKey="period" tickFormatter={fmtLabel} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                  <YAxis width={44} tickFormatter={fmtTick} allowDecimals={false} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                  <RechartsTooltip content={<ChartTooltip />} cursor={{ fill: 'currentColor', fillOpacity: 0.08 }} />
                  <Legend wrapperStyle={{ fontSize: '12px' }} />
                  <Bar dataKey="ft" name={t('stats.ftOnly')} stackId="a" fill="var(--color-chart-4)" radius={[3, 3, 0, 0]} />
                  <Bar dataKey="llm" name={t('stats.llmCall')} stackId="a" fill="var(--color-chart-2)" radius={[3, 3, 0, 0]} />
                  <Bar dataKey="error" name={t('stats.llmError')} stackId="a" fill="var(--color-destructive)" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </GlassCard>

          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div>
              <h3 className="font-semibold text-foreground">{t('stats.classifierAgreement')}</h3>
              <p className="text-xs text-muted-foreground mt-0.5">{t('stats.agreementDesc')}</p>
            </div>
            <div className="flex-1 h-[250px] text-muted-foreground">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={classifierChartData} margin={{ top: 10, right: 18, left: 8, bottom: 6 }}>
                  <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" strokeOpacity={0.15} />
                  <XAxis dataKey="period" tickFormatter={fmtLabel} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                  <YAxis domain={[0, 100]} width={44} tickFormatter={(v: number) => `${v}%`} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                  <RechartsTooltip content={<ChartTooltip />} cursor={{ stroke: 'currentColor', strokeOpacity: 0.2 }} />
                  <Legend wrapperStyle={{ fontSize: '12px' }} />
                  <Line type="monotone" dataKey="agreement" name={t('stats.agreement')} stroke="var(--color-chart-1)" strokeWidth={2} dot={classifierChartData.length > 24 ? false : { r: 3 }} activeDot={{ r: 5 }} />
                  <Line type="monotone" dataKey="avgFtConf" name={t('stats.avgFtConf')} stroke="var(--color-warning)" strokeWidth={1.5} strokeDasharray="4 3" dot={false} />
                  <Line type="monotone" dataKey="avgLlmConf" name={t('stats.avgLlmConf')} stroke="var(--color-chart-5)" strokeWidth={1.5} strokeDasharray="4 3" dot={false} />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </GlassCard>
        </div>
      )}
    </div>
  )
}
