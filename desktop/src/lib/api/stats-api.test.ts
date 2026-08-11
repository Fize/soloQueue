import { beforeEach, describe, expect, it, vi } from 'vitest'

import { getStatsBreakdown, getStatsEvents, getStatsOverview } from './stats-api'
import { request } from './core'

vi.mock('./core', () => ({ request: vi.fn() }))

describe('stats api', () => {
  beforeEach(() => vi.mocked(request).mockReset())

  it('unwraps the v2 envelope and preserves RFC3339 filters', async () => {
    vi.mocked(request).mockResolvedValueOnce({
      data: { summary: { total_tokens: 10 } },
      error: null,
    })
    const result = await getStatsOverview({
      from: '2026-08-10T00:00:00Z',
      to: '2026-08-11T00:00:00Z',
      timezone: 'Asia/Shanghai',
      team_id: 'team-a',
    })

    expect(result.summary.total_tokens).toBe(10)
    expect(request).toHaveBeenCalledWith(expect.stringContaining('/stats/v2/overview?'), {
      signal: undefined,
    })
    const path = vi.mocked(request).mock.calls[0][0]
    expect(path).toContain('timezone=Asia%2FShanghai')
    expect(path).toContain('team_id=team-a')
  })

  it('sends breakdown dimensions and event cursors', async () => {
    vi.mocked(request)
      .mockResolvedValueOnce({ data: { dimension: 'model', items: [] }, error: null })
      .mockResolvedValueOnce({ data: { items: [], next_cursor: null }, error: null })

    await getStatsBreakdown('model', { timezone: 'UTC' })
    await getStatsEvents({ timezone: 'UTC' }, 'opaque-cursor', 25)

    expect(vi.mocked(request).mock.calls[0][0]).toContain('dimension=model')
    expect(vi.mocked(request).mock.calls[1][0]).toContain('cursor=opaque-cursor')
    expect(vi.mocked(request).mock.calls[1][0]).toContain('limit=25')
  })

  it('surfaces contract errors', async () => {
    vi.mocked(request).mockResolvedValueOnce({ data: null, error: 'invalid_time_range: bad range' })
    await expect(getStatsOverview({ timezone: 'UTC' })).rejects.toThrow('invalid_time_range')
  })
})
