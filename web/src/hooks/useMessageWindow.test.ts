import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { INITIAL_VISIBLE_MESSAGES, MOUNT_BATCH, useMessageWindow } from './useMessageWindow'

type IdleHandle = { kind: 'idle'; callback: () => void } | { kind: 'timeout' }

let scheduled: IdleHandle[] = []

beforeEach(() => {
  scheduled = []
  vi.stubGlobal('requestIdleCallback', (callback: () => void) => {
    scheduled.push({ kind: 'idle', callback })
    return scheduled.length
  })
  vi.stubGlobal('cancelIdleCallback', () => {})
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function flushIdle() {
  const pending = scheduled
  scheduled = []
  act(() => {
    for (const entry of pending) if (entry.kind === 'idle') entry.callback()
  })
}

describe('useMessageWindow', () => {
  it('renders the newest messages first and the rest during idle slices', () => {
    const { result } = renderHook(() => useMessageWindow({ total: 50, sessionId: 's1' }))
    expect(result.current.visibleCount).toBe(INITIAL_VISIBLE_MESSAGES)

    flushIdle()
    expect(result.current.visibleCount).toBe(INITIAL_VISIBLE_MESSAGES + MOUNT_BATCH)

    let guard = 0
    while (result.current.visibleCount < 50 && guard++ < 20) flushIdle()
    expect(result.current.visibleCount).toBe(50)
    // Nothing is scheduled once every message is mounted.
    expect(scheduled).toHaveLength(0)
  })

  it('never renders more messages than the transcript holds', () => {
    const { result } = renderHook(() => useMessageWindow({ total: 3, sessionId: 's1' }))
    expect(result.current.visibleCount).toBe(3)
  })

  it('restarts the window when the session changes', () => {
    const { result, rerender } = renderHook(
      ({ total, sessionId }) => useMessageWindow({ total, sessionId }),
      { initialProps: { total: 50, sessionId: 's1' } },
    )
    let guard = 0
    while (result.current.visibleCount < 50 && guard++ < 20) flushIdle()
    expect(result.current.visibleCount).toBe(50)

    rerender({ total: 12, sessionId: 's2' })
    expect(result.current.visibleCount).toBe(12)
  })

  it('grows with the transcript while the stream appends messages', () => {
    const { result, rerender } = renderHook(
      ({ total }) => useMessageWindow({ total, sessionId: 's1' }),
      { initialProps: { total: 2 } },
    )
    expect(result.current.visibleCount).toBe(2)
    rerender({ total: 4 })
    expect(result.current.visibleCount).toBe(4)
  })
})
