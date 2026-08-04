import { describe, expect, it } from 'vitest'
import { isBackendReady } from './backend-status.mjs'

describe('backend readiness', () => {
  it('does not report a spawned process as ready before health check completion', () => {
    expect(
      isBackendReady({ externalGoInstance: false, backendStartTime: null })
    ).toBe(false)
  })

  it('reports a backend ready after health check completion', () => {
    expect(
      isBackendReady({ externalGoInstance: false, backendStartTime: Date.now() })
    ).toBe(true)
  })

  it('reports a healthy external backend as ready', () => {
    expect(
      isBackendReady({ externalGoInstance: true, backendStartTime: Date.now() })
    ).toBe(true)
  })
})
