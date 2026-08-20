import { describe, it, expect } from 'vitest'
import { getStateBorderClass, getStateBgClass } from './WorkflowStatusBadge'
import type { BadgeState } from './WorkflowStatusBadge'

describe('getStateBorderClass', () => {
  it('returns signal border for running state', () => {
    expect(getStateBorderClass('running')).toBe('border-signal')
  })

  it('returns success border for succeeded state', () => {
    expect(getStateBorderClass('succeeded')).toBe('border-success')
  })

  it('returns primary border for completed state', () => {
    expect(getStateBorderClass('completed')).toBe('border-primary')
  })

  it('returns rose border for failed state', () => {
    expect(getStateBorderClass('failed')).toBe('border-rose-500')
  })

  it('returns warning border for cancelled state', () => {
    expect(getStateBorderClass('cancelled')).toBe('border-warning')
  })

  it('returns muted foreground border for queued/pending states', () => {
    expect(getStateBorderClass('queued')).toBe('border-muted-foreground/40')
    expect(getStateBorderClass('pending')).toBe('border-muted-foreground/40')
  })

  it('returns rose-400 border for timed_out state', () => {
    expect(getStateBorderClass('timed_out')).toBe('border-rose-400')
  })

  it('handles all valid state values without throwing', () => {
    const states: BadgeState[] = [
      'queued', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out',
      'pending', 'completed',
    ]
    for (const state of states) {
      expect(() => getStateBorderClass(state)).not.toThrow()
      expect(getStateBorderClass(state)).toMatch(/^border-/)
    }
  })
})

describe('getStateBgClass', () => {
  it('returns matching bg for each state', () => {
    const pairs: [BadgeState, string][] = [
      ['queued', 'bg-muted-foreground/5'],
      ['running', 'bg-signal/5'],
      ['succeeded', 'bg-success/5'],
      ['failed', 'bg-rose-500/5'],
      ['cancelled', 'bg-warning/5'],
      ['timed_out', 'bg-rose-400/5'],
      ['pending', 'bg-muted-foreground/5'],
      ['completed', 'bg-primary/5'],
    ]
    for (const [state, expected] of pairs) {
      expect(getStateBgClass(state)).toBe(expected)
    }
  })
})
