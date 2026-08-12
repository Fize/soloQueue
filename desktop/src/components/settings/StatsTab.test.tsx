import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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

    expect(await screen.findByText('125K')).toBeInTheDocument()
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

  it('advances the rolling query window when refreshed', async () => {
    render(<StatsTab />)

    await waitFor(() => expect(api.getStatsOverview).toHaveBeenCalledTimes(1))
    const firstTo = new Date(api.getStatsOverview.mock.calls[0][0].to).getTime()
    const now = vi.spyOn(Date, 'now').mockReturnValue(firstTo + 60_000)

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }))

    await waitFor(() => expect(api.getStatsOverview).toHaveBeenCalledTimes(2))
    expect(new Date(api.getStatsOverview.mock.calls[1][0].to).getTime()).toBe(firstTo + 60_000)
    now.mockRestore()
  })
})
