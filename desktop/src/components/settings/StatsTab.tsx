import { useEffect, useState, useMemo } from 'react'
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as RechartsTooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import { getTokenStats, getRouterStats, getStatTeams, type TokenStat, type RouterStat } from '@/lib/api'
import { ActivityHeatmap, type ActivityDay } from '@/components/ActivityHeatmap'
import { TrendingUp, GitCommitHorizontal } from 'lucide-react'
import { Select } from '@/components/ui/select'
import { GlassCard } from '@/components/ui/glass-card'

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
  const [teams, setTeams] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    const fetchData = async () => {
      setLoading(true); setError('')
      try {
        const teamParam = teamFilter === 'all' ? undefined : teamFilter
        const [tokenData, routerData, teamsData] = await Promise.all([
          getTokenStats(timeframe, teamParam),
          getRouterStats(timeframe, teamParam),
          getStatTeams(),
        ])
        if (!active) return
        setTokenStats(tokenData || [])
        setRouterStats(routerData || [])
        setTeams(teamsData || [])
      } catch (err: any) {
        if (active) setError(err.message || 'Failed to fetch statistics')
      } finally {
        if (active) setLoading(false)
      }
    }
    fetchData()
    return () => { active = false }
  }, [timeframe, teamFilter])

  const tokenChartData = useMemo(() => {
    const grouped = new Map<string, { period: string; prompt: number; completion: number; cache: number }>()
    for (const row of tokenStats) {
      const p = row.period.split(' ')[0]
      if (!grouped.has(p)) grouped.set(p, { period: p, prompt: 0, completion: 0, cache: 0 })
      const g = grouped.get(p)!
      g.prompt += row.prompt_tokens
      g.completion += row.completion_tokens
      g.cache += row.cache_hit_tokens
    }
    return Array.from(grouped.values()).sort((a, b) => a.period.localeCompare(b.period))
  }, [tokenStats])

  const routerChartData = useMemo(() => {
    const grouped = new Map<string, { period: string; local: number; remote: number }>()
    for (const row of routerStats) {
      const p = row.period.split(' ')[0]
      if (!grouped.has(p)) grouped.set(p, { period: p, local: 0, remote: 0 })
      const g = grouped.get(p)!
      if (row.classification_source === 'local') g.local += row.count
      else g.remote += row.count
    }
    return Array.from(grouped.values()).sort((a, b) => a.period.localeCompare(b.period))
  }, [routerStats])

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
      { value: 'all', label: 'All Teams' },
      { value: '__solo__', label: 'Solo (L1)' },
    ]
    for (const t of teams) opts.push({ value: t, label: t.charAt(0).toUpperCase() + t.slice(1) })
    return opts
  }, [teams])

  const fmtLabel = (p: string) => {
    const parts = p.split('-')
    return parts.length === 3 ? `${parts[1]}/${parts[2]}` : p
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
          Usage Statistics
        </h2>
        <p className="text-sm text-muted-foreground">
          Monitor token consumption trends and router classification patterns.
        </p>
      </div>

      <div className="inline-flex w-fit flex-wrap items-center gap-3 rounded-xl border border-border/70 bg-card/70 px-3 py-2 shadow-sm">
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">Timeframe</span>
          <Select value={timeframe} onChange={setTimeframe} options={[
            { value: 'daily', label: 'Daily' },
            { value: 'weekly', label: 'Weekly' },
            { value: 'monthly', label: 'Monthly' },
          ]} className="w-[126px]" />
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs font-medium text-muted-foreground">Team</span>
          <Select value={teamFilter} onChange={setTeamFilter} options={teamOptions} className="w-[150px]" />
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
            Activity Heatmap
          </h3>
          <p className="text-xs text-muted-foreground mt-0.5">
            Token consumption activity over the past year (darker = more tokens)
          </p>
        </div>
        <ActivityHeatmap data={heatmapData} days={365} loading={heatmapLoading} />
      </GlassCard>

      {!loading && !error && (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          {/* Token Consumption */}
          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div>
              <h3 className="font-semibold text-foreground">Token Consumption</h3>
            </div>
            {tokenChartData.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                No token data available for this period.
              </div>
            ) : (
              <div className="flex-1 h-[250px] text-muted-foreground">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={tokenChartData} margin={{ top: 10, right: 18, left: 8, bottom: 6 }}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="currentColor" strokeOpacity={0.15} />
                    <XAxis dataKey="period" tickFormatter={fmtLabel} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                    <YAxis width={56} tickFormatter={fmtTick} tick={{ fontSize: 12, fill: 'currentColor' }} stroke="currentColor" tickLine={{ stroke: 'currentColor' }} axisLine={{ stroke: 'currentColor' }} />
                    <RechartsTooltip content={<ChartTooltip />} cursor={{ stroke: 'currentColor', strokeOpacity: 0.2 }} />
                    <Legend wrapperStyle={{ fontSize: '12px' }} />
                    <Line type="monotone" dataKey="prompt" name="Prompt" stroke="var(--color-chart-1)" strokeWidth={2} dot={{ r: 3 }} activeDot={{ r: 5 }} />
                    <Line type="monotone" dataKey="completion" name="Completion" stroke="var(--color-chart-2)" strokeWidth={2} dot={{ r: 3 }} activeDot={{ r: 5 }} />
                    <Line type="monotone" dataKey="cache" name="Cache Hits" stroke="var(--color-chart-3)" strokeWidth={2} dot={{ r: 3 }} activeDot={{ r: 5 }} />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </GlassCard>

          {/* Router Classifications */}
          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div>
              <h3 className="font-semibold text-foreground">Router Classifications</h3>
            </div>
            {routerChartData.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                No router data available for this period.
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
                    <Line type="monotone" dataKey="local" name="Local (Fast)" stroke="var(--color-chart-4)" strokeWidth={2} dot={{ r: 3 }} activeDot={{ r: 5 }} />
                    <Line type="monotone" dataKey="remote" name="Remote (LLM)" stroke="var(--color-chart-5)" strokeWidth={2} dot={{ r: 3 }} activeDot={{ r: 5 }} />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            )}
          </GlassCard>
        </div>
      )}
    </div>
  )
}
