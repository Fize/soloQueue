import { expect, it, vi } from 'vitest'
import { getRouterStats } from './chat-api'
import { request } from './core'

vi.mock('./core', () => ({ request: vi.fn() }))

it('sends router time/team filters and forwards cancellation without unwrapping the array', async () => {
  const rows = [
    {
      period: '2026-10-26',
      classification_source: 'laya',
      classification_level: 'general',
      count: 2,
    },
  ]
  vi.mocked(request).mockResolvedValueOnce(rows)
  const controller = new AbortController()
  expect(
    await getRouterStats(
      'daily',
      '__solo__',
      '2026-10-26 00:00:00',
      '2026-10-27 00:00:00',
      controller.signal
    )
  ).toBe(rows)
  const [path, options] = vi.mocked(request).mock.calls[0]
  const query = new URL(path, 'http://localhost').searchParams
  expect(query.get('timeframe')).toBe('daily')
  expect(query.get('team_id')).toBe('__solo__')
  expect(query.get('from')).toBe('2026-10-26 00:00:00')
  expect(query.get('to')).toBe('2026-10-27 00:00:00')
  expect(options?.signal).toBe(controller.signal)
})
