import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { StatsTab } from './StatsTab'

const api = vi.hoisted(() => ({
  getStatsOverview: vi.fn(),
  getStatsBreakdown: vi.fn(),
  getStatsEvents: vi.fn(),
  getStatsFilters: vi.fn(),
  getStatsActivity: vi.fn(),
}))

vi.mock('@/lib/api/stats-api', () => api)
const routerApi = vi.hoisted(() => ({ getRouterStats: vi.fn() }))
vi.mock('@/lib/api/chat-api', () => routerApi)

vi.mock('recharts', () => ({
  CartesianGrid: () => null,
  Legend: () => null,
  Line: () => null,
  LineChart: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  Tooltip: () => null,
  XAxis: () => null,
  YAxis: () => null,
}))

const metrics = {
  total_tokens: 125000,
  prompt_tokens: 100000,
  completion_tokens: 25000,
  reasoning_tokens: 5000,
  cache_hit_tokens: 80000,
  cache_miss_tokens: 20000,
  request_count: 42,
  success_count: 40,
  error_count: 2,
  cancelled_count: 0,
  timeout_count: 0,
  success_rate: 40 / 42,
  cache_hit_rate: 0.8,
  p95_duration_ms: 1250,
}

const meta = {
  generated_at: '2026-08-11T10:00:00Z',
  data_from: '2026-07-12T10:00:00Z',
  data_to: '2026-08-11T10:00:00Z',
  timezone: 'Asia/Shanghai',
  bucket_size: 'day' as const,
  coverage: {
    total_rows: 42,
    legacy_rows: 10,
    origin: { known_rows: 32, applicable_rows: 32 },
    task_type: { known_rows: 32, applicable_rows: 32 },
    status: { known_rows: 32, applicable_rows: 32 },
    latency: { known_rows: 32, applicable_rows: 32 },
    cache_detail: { known_rows: 32, applicable_rows: 32 },
    reasoning_detail: { known_rows: 24, applicable_rows: 32 },
  },
}

describe('StatsTab', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    routerApi.getRouterStats.mockResolvedValue([])
    window.history.replaceState(null, '', '/')
    api.getStatsOverview.mockResolvedValue({
      meta,
      summary: metrics,
      comparison: {
        total_tokens: { current: 125000, previous: 100000, change_pct: 25 },
        request_count: { current: 42, previous: 40, change_pct: 5 },
        p95_duration_ms: { current: 1250, previous: 1000, change_pct: 25 },
      },
      series: [
        {
          start: '2026-08-10T16:00:00Z',
          end: '2026-08-11T16:00:00Z',
          metrics,
        },
      ],
      insights: [],
    })
    api.getStatsFilters.mockResolvedValue({
      meta,
      teams: [],
      origins: [],
      usage_types: [],
      task_types: [],
      providers: [],
      models: [],
      statuses: [],
    })
    api.getStatsBreakdown.mockImplementation((dimension: string) =>
      Promise.resolve({ meta, dimension, items: [] })
    )
    api.getStatsActivity.mockResolvedValue({ meta, active_days: 0, total_tokens: 0, points: [] })
    api.getStatsEvents.mockResolvedValue({ meta, items: [], next_cursor: null })
  })

  it('shows decision-ready KPIs and explicit coverage limitations', async () => {
    render(<StatsTab />)

    expect(
      await screen.findByText(
        new Intl.NumberFormat(undefined, {
          notation: 'compact',
          maximumFractionDigits: 1,
        }).format(metrics.total_tokens)
      )
    ).toBeInTheDocument()
    expect(screen.getByText('42')).toBeInTheDocument()
    expect(screen.getByText('95.2%')).toBeInTheDocument()
    expect(screen.getByText('1.3 s')).toBeInTheDocument()
    expect(screen.getByText('80.0%')).toBeInTheDocument()
    expect(screen.queryByText('Estimated Cost')).not.toBeInTheDocument()
    expect(screen.queryByText('Pricing unavailable')).not.toBeInTheDocument()
    expect(screen.getByText('10 legacy')).toBeInTheDocument()
    expect(screen.getAllByTestId('stats-kpi')).toHaveLength(5)

    await waitFor(() => expect(api.getStatsOverview).toHaveBeenCalledTimes(1))
    const query = api.getStatsOverview.mock.calls[0][0]
    expect(query.timezone).toBeTruthy()
    expect(new Date(query.to).getTime() - new Date(query.from).getTime()).toBeGreaterThan(
      29 * 24 * 60 * 60 * 1000
    )
  })

  it('places related task statistics together after the overview and trend with separate units', async () => {
    routerApi.getRouterStats.mockResolvedValueOnce([
      {
        period: '2026-10-26',
        classification_source: 'laya',
        classification_level: 'engineering',
        count: 8,
      },
      {
        period: '2026-10-26',
        classification_source: 'laya_unavailable',
        classification_level: 'general',
        count: 2,
      },
    ])
    api.getStatsBreakdown.mockImplementation((dimension: string) =>
      Promise.resolve({
        meta,
        dimension,
        items:
          dimension === 'task_type'
            ? [
                {
                  key: 'engineering',
                  label: 'engineering',
                  metrics: { ...metrics, request_count: 42, total_tokens: 125000 },
                },
              ]
            : [],
      })
    )
    render(<StatsTab />)
    const success = await screen.findByTestId('laya-success')
    const analysis = screen.getByRole('region', { name: 'Task classification and usage' })
    const laya = within(analysis).getByRole('region', { name: 'Laya Task Classification' })
    const usage = within(analysis).getByRole('region', { name: 'Task types' })
    const trend = screen.getByRole('heading', { name: 'Usage trend' })
    const overview = screen.getAllByTestId('stats-kpi')[0]
    expect(
      overview.compareDocumentPosition(analysis) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(trend.compareDocumentPosition(analysis) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(laya.parentElement).toBe(usage.parentElement)
    expect(within(analysis).getAllByRole('region')).toHaveLength(2)
    expect(success).toHaveTextContent('8')
    expect(within(laya).getByTestId('laya-fallback')).toHaveTextContent('2')
    await waitFor(() => expect(within(usage).getByText('42 Calls')).toBeInTheDocument())
    expect(within(usage).getByText('Engineering')).toBeInTheDocument()
    expect(
      within(usage).getByText(
        `${new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(125000)} Tokens`
      )
    ).toBeInTheDocument()
    expect(within(usage).getByText(/One task can generate multiple LLM calls/)).toBeInTheDocument()
  })

  it('keeps local task analysis visible when there are no LLM task details', async () => {
    api.getStatsOverview.mockResolvedValueOnce({
      meta: {
        ...meta,
        coverage: { ...meta.coverage, task_type: { known_rows: 0, applicable_rows: 0 } },
      },
      summary: { ...metrics, request_count: 0 },
      comparison: {},
      series: [],
      insights: [],
    })
    render(<StatsTab />)
    expect(await screen.findByText('No Laya classifications in this period.')).toBeInTheDocument()
    const analysis = screen.getByRole('region', { name: 'Task classification and usage' })
    expect(
      await within(analysis).findByText('No LLM calls with a known task type in this selection.')
    ).toBeInTheDocument()
    expect(screen.getAllByRole('region', { name: 'Laya Task Classification' })).toHaveLength(1)
  })

  it('distinguishes unavailable LLM task details from an empty result while retaining Laya counts', async () => {
    api.getStatsBreakdown.mockImplementation((dimension: string) =>
      dimension === 'task_type'
        ? Promise.reject(new Error('breakdown unavailable'))
        : Promise.resolve({ meta, dimension, items: [] })
    )
    render(<StatsTab />)
    expect(await screen.findByText('LLM task usage is currently unavailable.')).toBeInTheDocument()
    expect(screen.getByTestId('laya-success')).toHaveTextContent('0')
    expect(
      screen.queryByText('No LLM calls with a known task type in this selection.')
    ).not.toBeInTheDocument()
  })

  it('hides inapplicable reliability panels for legacy-only data', async () => {
    api.getStatsOverview.mockResolvedValueOnce({
      meta: {
        ...meta,
        coverage: {
          total_rows: 42,
          legacy_rows: 42,
          origin: { known_rows: 0, applicable_rows: 0 },
          task_type: { known_rows: 0, applicable_rows: 0 },
          status: { known_rows: 0, applicable_rows: 0 },
          latency: { known_rows: 0, applicable_rows: 0 },
          cache_detail: { known_rows: 0, applicable_rows: 0 },
          reasoning_detail: { known_rows: 0, applicable_rows: 0 },
        },
      },
      summary: { ...metrics, success_rate: null, cache_hit_rate: null, p95_duration_ms: null },
      comparison: {},
      series: [],
      insights: [],
    })

    render(<StatsTab />)

    expect(await screen.findByText(/Historical records include tokens/)).toBeInTheDocument()
    expect(screen.getAllByTestId('stats-kpi')).toHaveLength(2)
    expect(screen.queryByText('Success Rate')).not.toBeInTheDocument()
    expect(screen.queryByText('P95 Latency')).not.toBeInTheDocument()
    expect(screen.queryByText('Cache Hit Ratio')).not.toBeInTheDocument()
  })

  it('shows Laya history even when the LLM statistics request fails', async () => {
    api.getStatsOverview.mockRejectedValueOnce(new Error('LLM stats unavailable'))
    routerApi.getRouterStats.mockResolvedValueOnce([
      {
        period: '2026-10-26',
        classification_source: 'laya',
        classification_level: 'engineering',
        count: 8,
      },
    ])
    window.history.replaceState(null, '', '/?stats_team=team-only')
    render(<StatsTab />)
    expect(await screen.findByTestId('laya-success')).toHaveTextContent('8')
    expect(screen.getByRole('alert')).toHaveTextContent('LLM stats unavailable')
    expect(routerApi.getRouterStats.mock.calls[0][1]).toBe('team-only')
    fireEvent.click(screen.getByRole('button', { name: '24h' }))
    await waitFor(() => expect(routerApi.getRouterStats).toHaveBeenCalledTimes(2))
    const [, , from, to] = routerApi.getRouterStats.mock.calls[1]
    expect(Date.parse(to.replace(' ', 'T') + 'Z') - Date.parse(from.replace(' ', 'T') + 'Z')).toBe(
      24 * 60 * 60 * 1000
    )
  })

  it('advances the rolling query window when refreshed', async () => {
    render(<StatsTab />)

    await waitFor(() => expect(api.getStatsOverview).toHaveBeenCalledTimes(1))
    const firstTo = new Date(api.getStatsOverview.mock.calls[0][0].to).getTime()
    const now = vi.spyOn(Date, 'now').mockReturnValue(firstTo + 60_000)

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    await waitFor(() => expect(api.getStatsOverview).toHaveBeenCalledTimes(2))
    expect(new Date(api.getStatsOverview.mock.calls[1][0].to).getTime()).toBe(firstTo + 60_000)
    await waitFor(() => expect(routerApi.getRouterStats).toHaveBeenCalledTimes(2))
    expect(routerApi.getRouterStats.mock.calls[1][3]).toBe(
      new Date(firstTo + 60_000).toISOString().slice(0, 19).replace('T', ' ')
    )
    now.mockRestore()
  })
})
