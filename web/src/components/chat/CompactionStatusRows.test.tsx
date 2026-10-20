import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { CompactionStatus } from '@/types'
import { CompactionStatusRows } from './CompactionStatusRows'

const startedAt = '2026-10-16T10:00:00.000Z'
const running: CompactionStatus = { id: 'one', status: 'running', started_at: startedAt }

describe('CompactionStatusRows', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date(startedAt))
  })
  afterEach(() => {
    cleanup()
    vi.useRealTimers()
  })

  it('keeps running visible and removes completion after five seconds without a history entry', () => {
    const { container, rerender } = render(<CompactionStatusRows rows={[running]} />)
    act(() => vi.advanceTimersByTime(60_000))
    expect(screen.getByText('Compressing conversation context…')).toBeVisible()
    rerender(<CompactionStatusRows rows={[{ ...running, status: 'completed', finished_at: new Date().toISOString() }]} />)
    expect(screen.getByText('Context compression completed.')).toBeVisible()
    act(() => vi.advanceTimersByTime(4_999))
    expect(screen.getByText('Context compression completed.')).toBeVisible()
    act(() => vi.advanceTimersByTime(1))
    expect(screen.queryByText('Context compression completed.')).not.toBeInTheDocument()
    expect(container).toBeEmptyDOMElement()
  })

  it('hides older completions without a history entry while retaining errors', () => {
    vi.setSystemTime(new Date('2026-10-16T10:01:00.000Z'))
    render(<CompactionStatusRows rows={[
      { ...running, status: 'completed', finished_at: startedAt },
      { ...running, id: 'two', status: 'failed', error: 'Provider timeout' },
    ]} />)
    expect(screen.queryByText('Context compression completed.')).not.toBeInTheDocument()
    expect(screen.queryByText(/Compression history/)).not.toBeInTheDocument()
    expect(screen.getByText('Provider timeout')).toBeVisible()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cleans up the expiration timer when unmounted', () => {
    const { unmount } = render(<CompactionStatusRows rows={[{ ...running, status: 'completed', finished_at: startedAt }]} />)
    expect(vi.getTimerCount()).toBe(1)
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
