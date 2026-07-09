import { useEffect, useState, useMemo } from 'react'
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as RechartsTooltip,
  Legend,
  ResponsiveContainer,
} from 'recharts'
import { getTokenStats, getRouterStats, type TokenStat, type RouterStat } from '@/lib/api'
import { Info } from 'lucide-react'
import { Select } from '@/components/ui/select'
import { GlassCard } from '@/components/ui/glass-card'

export function StatsTab() {
  const [timeframe, setTimeframe] = useState('daily')
  const [teamFilter, setTeamFilter] = useState('all')
  const [tokenStats, setTokenStats] = useState<TokenStat[]>([])
  const [routerStats, setRouterStats] = useState<RouterStat[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    const fetchData = async () => {
      setLoading(true)
      setError('')
      try {
        const teamParam = teamFilter === 'all' ? undefined : teamFilter
        const [tokenData, routerData] = await Promise.all([
          getTokenStats(timeframe, teamParam),
          getRouterStats(timeframe, teamParam),
        ])

        if (!active) return

        setTokenStats(tokenData || [])
        setRouterStats(routerData || [])
      } catch (err: any) {
        if (active) setError(err.message || 'Failed to fetch statistics')
      } finally {
        if (active) setLoading(false)
      }
    }
    fetchData()
    return () => { active = false }
  }, [timeframe, teamFilter])

  // Data processing for Token Chart (group by period, aggregate tokens)
  const tokenChartData = useMemo(() => {
    const grouped = new Map<string, { period: string; prompt: number; completion: number; cache: number }>()
    for (const row of tokenStats) {
      const p = row.period.split(' ')[0] // Just take date part
      if (!grouped.has(p)) {
        grouped.set(p, { period: p, prompt: 0, completion: 0, cache: 0 })
      }
      const g = grouped.get(p)!
      g.prompt += row.prompt_tokens
      g.completion += row.completion_tokens
      g.cache += row.cache_hit_tokens
    }
    return Array.from(grouped.values()).sort((a, b) => a.period.localeCompare(b.period))
  }, [tokenStats])

  // Data processing for Router Chart (group by period, count by source)
  const routerChartData = useMemo(() => {
    const grouped = new Map<string, { period: string; local: number; remote: number }>()
    for (const row of routerStats) {
      const p = row.period.split(' ')[0]
      if (!grouped.has(p)) {
        grouped.set(p, { period: p, local: 0, remote: 0 })
      }
      const g = grouped.get(p)!
      if (row.classification_source === 'local') {
        g.local += row.count
      } else {
        g.remote += row.count
      }
    }
    return Array.from(grouped.values()).sort((a, b) => a.period.localeCompare(b.period))
  }, [routerStats])

  return (
    <div className="space-y-6 animate-in fade-in slide-in-from-bottom-2 duration-300">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg font-semibold text-foreground flex items-center gap-2">
          Usage Statistics
        </h2>
        <p className="text-sm text-muted-foreground">
          View token consumption and router classification metrics across time periods and teams.
        </p>
      </div>

      <div className="flex gap-4 items-center bg-card p-3 rounded-lg border border-border shadow-sm">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-foreground">Timeframe:</span>
          <Select
            value={timeframe}
            onChange={(val) => setTimeframe(val)}
            options={[
              { value: 'daily', label: 'Daily' },
              { value: 'weekly', label: 'Weekly' },
              { value: 'monthly', label: 'Monthly' },
            ]}
          />
        </div>

        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-foreground">Team:</span>
          <Select
            value={teamFilter}
            onChange={(val) => setTeamFilter(val)}
            options={[
              { value: 'all', label: 'All Teams' },
              { value: 'unknown', label: 'Unknown' },
            ]}
          />
        </div>
      </div>

      {error && (
        <div className="p-3 text-sm text-destructive bg-destructive/10 rounded-md border border-destructive/20">
          {error}
        </div>
      )}

      {!loading && !error && (
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-6">
          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div className="flex items-center justify-between">
              <h3 className="font-semibold text-foreground">Token Consumption</h3>
              <Info className="h-4 w-4 text-muted-foreground" />
            </div>
            {tokenChartData.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                No token data available for this period.
              </div>
            ) : (
              <div className="flex-1 h-[250px]">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={tokenChartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="hsl(var(--border))" />
                    <XAxis dataKey="period" tick={{ fontSize: 12 }} stroke="hsl(var(--muted-foreground))" />
                    <YAxis tick={{ fontSize: 12 }} stroke="hsl(var(--muted-foreground))" />
                    <RechartsTooltip
                      contentStyle={{ backgroundColor: 'hsl(var(--card))', borderColor: 'hsl(var(--border))', color: 'hsl(var(--foreground))' }}
                      itemStyle={{ color: 'hsl(var(--foreground))' }}
                    />
                    <Legend wrapperStyle={{ fontSize: '12px' }} />
                    <Bar dataKey="prompt" name="Prompt Tokens" stackId="a" fill="#6366f1" />
                    <Bar dataKey="completion" name="Completion Tokens" stackId="a" fill="#14b8a6" />
                    <Bar dataKey="cache" name="Cache Hits" stackId="a" fill="#f59e0b" />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </GlassCard>

          <GlassCard className="flex flex-col gap-4 min-h-[350px]">
            <div className="flex items-center justify-between">
              <h3 className="font-semibold text-foreground">Router Classifications</h3>
              <Info className="h-4 w-4 text-muted-foreground" />
            </div>
            {routerChartData.length === 0 ? (
              <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
                No router data available for this period.
              </div>
            ) : (
              <div className="flex-1 h-[250px]">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={routerChartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="hsl(var(--border))" />
                    <XAxis dataKey="period" tick={{ fontSize: 12 }} stroke="hsl(var(--muted-foreground))" />
                    <YAxis tick={{ fontSize: 12 }} stroke="hsl(var(--muted-foreground))" />
                    <RechartsTooltip
                      contentStyle={{ backgroundColor: 'hsl(var(--card))', borderColor: 'hsl(var(--border))', color: 'hsl(var(--foreground))' }}
                      itemStyle={{ color: 'hsl(var(--foreground))' }}
                    />
                    <Legend wrapperStyle={{ fontSize: '12px' }} />
                    <Bar dataKey="local" name="Local (Fast Track)" stackId="a" fill="#3b82f6" />
                    <Bar dataKey="remote" name="Remote (LLM)" stackId="a" fill="#8b5cf6" />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            )}
          </GlassCard>
        </div>
      )}
    </div>
  )
}
