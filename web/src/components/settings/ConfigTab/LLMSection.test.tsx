import { describe, expect, it } from 'vitest'

import { normalizeProviderTimeoutMs } from './LLMSection'

describe('normalizeProviderTimeoutMs', () => {
  it('preserves zero as disabled instead of restoring a 30 second cap', () => {
    expect(normalizeProviderTimeoutMs(0)).toBe(0)
    expect(normalizeProviderTimeoutMs(undefined)).toBe(0)
  })

  it('preserves an explicit nonzero legacy cap', () => {
    expect(normalizeProviderTimeoutMs(120_000)).toBe(120_000)
  })
})
