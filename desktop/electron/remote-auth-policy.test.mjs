import { describe, expect, it } from 'vitest'
import {
  buildBasicAuthHeader,
  hasAuthorizationHeader,
  normalizeRemoteOrigin,
  shouldInjectRemoteAuth,
} from './remote-auth-policy.mjs'

describe('remote auth policy', () => {
  it('accepts an origin-only HTTP(S) endpoint', () => {
    expect(normalizeRemoteOrigin('https://remote.example:8443/')).toBe('https://remote.example:8443')
  })

  it('rejects credentials, paths, and unsupported schemes in endpoint config', () => {
    expect(normalizeRemoteOrigin('https://admin:secret@remote.example')).toBeNull()
    expect(normalizeRemoteOrigin('https://remote.example/soloqueue')).toBeNull()
    expect(normalizeRemoteOrigin('file:///tmp/desktop')).toBeNull()
  })

  it('limits auth to backend paths on the configured origin', () => {
    const origin = 'https://remote.example'
    expect(shouldInjectRemoteAuth(`${origin}/api/teams`, origin)).toBe(true)
    expect(shouldInjectRemoteAuth(`${origin}/healthz`, origin)).toBe(true)
    expect(shouldInjectRemoteAuth(`${origin}/`, origin)).toBe(false)
    expect(shouldInjectRemoteAuth('https://other.example/api/teams', origin)).toBe(false)
  })

  it('matches the configured host across http/https protocol switches', () => {
    // The user may edit the URL in the UI (http → https) without re-saving;
    // the renderer then requests https while the stored origin is http.
    // Both must be considered the same backend host for header injection.
    expect(shouldInjectRemoteAuth('https://remote.example/api/teams', 'http://remote.example')).toBe(true)
    expect(shouldInjectRemoteAuth('http://remote.example/api/teams', 'https://remote.example')).toBe(true)
  })

  it('does not match different hosts or ports regardless of protocol', () => {
    expect(shouldInjectRemoteAuth('https://remote.example/api/teams', 'https://other.example')).toBe(false)
    expect(shouldInjectRemoteAuth('https://remote.example:8443/api/teams', 'https://remote.example')).toBe(false)
    expect(shouldInjectRemoteAuth('https://remote.example/api/teams', 'https://remote.example:8443')).toBe(false)
  })

  it('does not overwrite an existing Authorization header', () => {
    expect(hasAuthorizationHeader({ authorization: 'Bearer test' })).toBe(true)
    expect(hasAuthorizationHeader({ 'Content-Type': 'application/json' })).toBe(false)
  })

  it('builds Basic auth only when both fields are present', () => {
    expect(buildBasicAuthHeader('admin', 'secret')).toBe(
      `Basic ${Buffer.from('admin:secret').toString('base64')}`
    )
    expect(buildBasicAuthHeader('admin', '')).toBeNull()
  })
})
