import { request } from './core'

export type StatsBucketSize = 'hour' | 'day' | 'week' | 'none'
export type StatsDimension = 'usage_type' | 'model' | 'task_type' | 'origin' | 'team' | 'status'

export interface StatsQuery {
  from?: string
  to?: string
  timezone?: string
  team_id?: string
  origin?: string
  usage_type?: string
  task_type?: string
  provider_id?: string
  model_id?: string
  status?: string
}

export interface StatsCoverage {
  total_rows: number
  legacy_rows: number
  cache_coverage_pct: number
  reasoning_coverage_pct: number
}

export interface StatsMeta {
  generated_at: string
  data_from: string
  data_to: string
  timezone: string
  bucket_size: StatsBucketSize
  coverage: StatsCoverage
}

export interface StatsMetrics {
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
  request_count: number
  success_count: number
  error_count: number
  cancelled_count: number
  timeout_count: number
  success_rate: number
  cache_hit_rate: number
  p95_duration_ms: number | null
}

export interface StatsDelta {
  current: number
  previous: number
  change_pct: number | null
}

export interface StatsSeriesPoint {
  start: string
  end: string
  metrics: StatsMetrics
}

export interface StatsInsight {
  id: string
  severity: 'info' | 'warning' | 'critical'
  title: string
  detail: string
  metric: string | null
  change_pct: number | null
}

export interface StatsOverview {
  meta: StatsMeta
  summary: StatsMetrics
  comparison: Record<string, StatsDelta>
  series: StatsSeriesPoint[]
  insights: StatsInsight[]
}

export interface StatsBreakdownItem {
  key: string
  label: string
  metrics: StatsMetrics
}

export interface StatsBreakdown {
  meta: StatsMeta
  dimension: StatsDimension
  items: StatsBreakdownItem[]
}

export interface StatsFilterOption {
  value: string
  label: string
}

export interface StatsFilters {
  meta: StatsMeta
  teams: StatsFilterOption[]
  origins: StatsFilterOption[]
  usage_types: StatsFilterOption[]
  task_types: StatsFilterOption[]
  providers: StatsFilterOption[]
  models: StatsFilterOption[]
  statuses: StatsFilterOption[]
}

export interface StatsActivityPoint {
  date: string
  total_tokens: number
  request_count: number
  level: 0 | 1 | 2 | 3 | 4
}

export interface StatsActivity {
  meta: StatsMeta
  active_days: number
  total_tokens: number
  points: StatsActivityPoint[]
}

export interface StatsEvent {
  call_id: string
  request_id: string | null
  session_id: string | null
  run_id: string | null
  agent_id: string | null
  team_id: string | null
  origin: string
  usage_type: string
  task_type: string
  provider_id: string
  model_id: string
  started_at: string
  finished_at: string
  status: string
  finish_reason: string | null
  error_code: string | null
  retry_count: number
  duration_ms: number
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  total_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
  legacy: boolean
}

export interface StatsEvents {
  meta: StatsMeta
  items: StatsEvent[]
  next_cursor: string | null
}

interface StatsEnvelope<T> {
  data: T | null
  error: string | null
}

function toSearchParams(query: StatsQuery): URLSearchParams {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value) params.set(key, value)
  }
  return params
}

async function statsRequest<T>(path: string, signal?: AbortSignal): Promise<T> {
  const envelope = await request<StatsEnvelope<T>>(path, { signal })
  if (envelope.error || !envelope.data) {
    throw new Error(envelope.error || 'stats_invalid_response: response data is missing')
  }
  return envelope.data
}

export function getStatsOverview(query: StatsQuery, signal?: AbortSignal): Promise<StatsOverview> {
  return statsRequest(`/stats/v2/overview?${toSearchParams(query)}`, signal)
}

export function getStatsBreakdown(
  dimension: StatsDimension,
  query: StatsQuery,
  signal?: AbortSignal
): Promise<StatsBreakdown> {
  const params = toSearchParams(query)
  params.set('dimension', dimension)
  return statsRequest(`/stats/v2/breakdowns?${params}`, signal)
}

export function getStatsFilters(query: StatsQuery, signal?: AbortSignal): Promise<StatsFilters> {
  return statsRequest(`/stats/v2/filters?${toSearchParams(query)}`, signal)
}

export function getStatsActivity(
  query: Omit<StatsQuery, 'from' | 'to'>,
  days = 365,
  signal?: AbortSignal
): Promise<StatsActivity> {
  const params = toSearchParams(query)
  params.set('days', String(days))
  return statsRequest(`/stats/v2/activity?${params}`, signal)
}

export function getStatsEvents(
  query: StatsQuery,
  cursor?: string,
  limit = 50,
  signal?: AbortSignal
): Promise<StatsEvents> {
  const params = toSearchParams(query)
  params.set('limit', String(limit))
  if (cursor) params.set('cursor', cursor)
  return statsRequest(`/stats/v2/events?${params}`, signal)
}
