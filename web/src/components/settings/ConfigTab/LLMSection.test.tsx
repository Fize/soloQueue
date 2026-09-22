import { describe, expect, it } from 'vitest'

import { normalizeProviderTimeoutMs, validateProviderForm } from './LLMSection'

describe('normalizeProviderTimeoutMs', () => {
  it('preserves zero as disabled instead of restoring a 30 second cap', () => {
    expect(normalizeProviderTimeoutMs(0)).toBe(0)
    expect(normalizeProviderTimeoutMs(undefined)).toBe(0)
  })

  it('preserves an explicit nonzero legacy cap', () => {
    expect(normalizeProviderTimeoutMs(120_000)).toBe(120_000)
  })
})

describe('validateProviderForm', () => {
  it('rejects an empty provider without needing a backend request', () => {
    const result = validateProviderForm({}, '{}')
    expect(result.fieldErrors).toEqual({
      id: 'Provider ID is required.',
      name: 'Display name is required.',
      baseUrl: 'API base URL is required.',
    })
  })

  it('rejects malformed or non-object custom headers', () => {
    expect(validateProviderForm({ id: 'demo', name: 'Demo', baseUrl: 'https://example.test' }, '{').fieldErrors.headers)
      .toContain('Headers must be valid JSON object')
    expect(validateProviderForm({ id: 'demo', name: 'Demo', baseUrl: 'https://example.test' }, '[]').fieldErrors.headers)
      .toBe('Headers must be a JSON object')
  })
})
