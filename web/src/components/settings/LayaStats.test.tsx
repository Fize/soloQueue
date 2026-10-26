import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getRouterStats, type RouterStat } from '@/lib/api/chat-api'
import { LayaStats } from './LayaStats'

vi.mock('@/lib/api/chat-api', () => ({ getRouterStats: vi.fn() }))
const from = '2026-10-25T16:00:00.000Z'
const to = '2026-10-26T16:00:00.000Z'
const row = (source: string, task: string, count: number): RouterStat => ({
  period: '2026-10-26',
  classification_source: source,
  classification_level: task,
  count,
})
const rows = [
  row('laya', 'engineering', 7),
  row('laya', 'engineering', 2),
  row('laya', 'research', 3),
  row('laya_unavailable', 'general', 4),
  row('local', 'engineering', 100),
  row('llm', 'general', 200),
]

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(getRouterStats).mockResolvedValue(rows)
})

describe('LayaStats', () => {
  it('counts successful classifications and fallbacks separately and excludes other engines', async () => {
    render(<LayaStats from={from} to={to} team="team-a" />)
    expect(screen.getByRole('status')).toHaveTextContent('Loading')
    expect(await screen.findByTestId('laya-success')).toHaveTextContent('12')
    expect(screen.getByTestId('laya-fallback')).toHaveTextContent('4')
    const table = screen.getByRole('table')
    const engineering = within(table).getByRole('row', { name: /Engineering/ })
    expect(
      within(engineering)
        .getAllByRole('cell')
        .map((cell) => cell.textContent)
    ).toEqual(['Engineering', '9', '0'])
    expect(within(table).getByRole('row', { name: /General/ })).toHaveTextContent('0')
    expect(screen.getByText(/time range and team filters/i)).toBeInTheDocument()
    expect(getRouterStats).toHaveBeenCalledWith(
      'daily',
      'team-a',
      '2026-10-25 16:00:00',
      '2026-10-26 16:00:00',
      expect.any(AbortSignal)
    )
  })

  it('keeps counting rules collapsed until requested', async () => {
    render(<LayaStats from={from} to={to} team="" />)
    await screen.findByTestId('laya-success')
    const rules = screen.getByText(/Rule matches are excluded/)
    expect(rules).not.toBeVisible()
    fireEvent.click(screen.getByText('Counting rules'))
    expect(rules).toBeVisible()
    expect(rules).toHaveTextContent('Fallback types reflect the previous/default task type.')
  })

  it('normalizes offset dates to the UTC timestamp format used by storage', async () => {
    render(<LayaStats from="2026-10-26T00:00:00+08:00" to="2026-10-27T00:00:00+08:00" team="" />)
    await screen.findByTestId('laya-success')
    expect(getRouterStats).toHaveBeenCalledWith(
      'daily',
      undefined,
      '2026-10-25 16:00:00',
      '2026-10-26 16:00:00',
      expect.any(AbortSignal)
    )
  })

  it('accepts null or empty history as zero counts', async () => {
    vi.mocked(getRouterStats).mockResolvedValueOnce(null as unknown as RouterStat[])
    render(<LayaStats from={from} to={to} team="" />)
    expect(await screen.findByText('No Laya classifications in this period.')).toBeInTheDocument()
    expect(screen.getByTestId('laya-success')).toHaveTextContent('0')
    expect(screen.getByTestId('laya-fallback')).toHaveTextContent('0')
  })

  it('shows unavailable rather than zero on failure, then retries', async () => {
    vi.mocked(getRouterStats).mockRejectedValueOnce(new Error('offline'))
    render(<LayaStats from={from} to={to} team="" />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Unable to load Laya statistics.')
    expect(screen.queryByTestId('laya-success')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByTestId('laya-success')).toHaveTextContent('12')
  })

  it('clears old counts on range/team refresh and ignores stale responses', async () => {
    let finish!: (value: RouterStat[]) => void
    vi.mocked(getRouterStats).mockReturnValueOnce(
      new Promise((resolve) => {
        finish = resolve
      })
    )
    const view = render(<LayaStats from={from} to={to} team="old" />)
    const signal = vi.mocked(getRouterStats).mock.calls[0][4]
    view.rerender(<LayaStats from={from} to="2026-10-27T16:00:00Z" team="new" />)
    expect(signal?.aborted).toBe(true)
    expect(await screen.findByTestId('laya-success')).toHaveTextContent('12')
    await act(async () => {
      finish([row('laya', 'general', 999)])
    })
    expect(screen.getByTestId('laya-success')).toHaveTextContent('12')
    view.rerender(<LayaStats from={from} to={to} team="" />)
    await waitFor(() => expect(getRouterStats).toHaveBeenCalledTimes(3))
    expect(vi.mocked(getRouterStats).mock.calls[2][1]).toBeUndefined()
    view.unmount()
    expect(vi.mocked(getRouterStats).mock.calls[2][4]?.aborted).toBe(true)
  })
})
