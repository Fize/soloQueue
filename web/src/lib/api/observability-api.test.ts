import { describe, expect, it } from 'vitest'
import { formatExecutionTime, formatExecutionTimeCompact } from './observability-api'

const cases: [number, string, string][] = [
  [0, '0 ms', '0ms'],
  [125, '125 ms', '125ms'],
  [999, '999 ms', '999ms'],
  [1000, '1.0 s', '1.0s'],
  [59999, '59.9 s', '59.9s'],
  [60000, '1m', '1m'],
  [88400, '1m 28s', '1m28s'],
  [3599999, '59m 59s', '59m59s'],
  [3600000, '1h', '1h'],
  [3900000, '1h 5m', '1h5m'],
  [3930000, '1h 5m 30s', '1h5m30s'],
  [86399999, '23h 59m 59s', '23h59m59s'],
  [86400000, '1d', '1d'],
  [90000000, '1d 1h', '1d1h'],
  [183845000, '2d 3h 4m 5s', '2d3h4m5s'],
]

describe('formatExecutionTime', () => {
  it.each(cases)('formats %i ms as %s', (ms, expected) => {
    expect(formatExecutionTime(ms)).toBe(expected)
  })

  it('clamps negative durations to zero', () => {
    expect(formatExecutionTime(-1)).toBe('0 ms')
    expect(formatExecutionTime(-86400000)).toBe('0 ms')
  })

  it('returns a placeholder for non-finite values', () => {
    expect(formatExecutionTime(Number.NaN)).toBe('—')
    expect(formatExecutionTime(Number.POSITIVE_INFINITY)).toBe('—')
    expect(formatExecutionTime(Number.NEGATIVE_INFINITY)).toBe('—')
  })
})

describe('formatExecutionTimeCompact', () => {
  it.each(cases)('formats %i ms compactly', (ms, _standard, compact) => {
    expect(formatExecutionTimeCompact(ms)).toBe(compact)
  })

  it('omits every zero-valued unit', () => {
    expect(formatExecutionTime(3600000)).toBe('1h')
    expect(formatExecutionTime(86400000)).toBe('1d')
    expect(formatExecutionTime(3900000)).toBe('1h 5m')
  })

  it('drops only spaces, never precision', () => {
    for (const [ms] of cases) {
      expect(formatExecutionTimeCompact(ms)).toBe(formatExecutionTime(ms).replaceAll(' ', ''))
    }
  })

  it('clamps negative durations to zero', () => {
    expect(formatExecutionTimeCompact(-1)).toBe('0ms')
  })

  it('returns a placeholder for non-finite values', () => {
    expect(formatExecutionTimeCompact(Number.NaN)).toBe('—')
    expect(formatExecutionTimeCompact(Number.POSITIVE_INFINITY)).toBe('—')
  })
})
