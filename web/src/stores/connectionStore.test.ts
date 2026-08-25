import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useConnectionStore } from './connectionStore'

describe('connectionStore', () => {
  beforeEach(() => {
    useConnectionStore.setState({
      mode: 'local',
      remoteUrl: '',
      username: '',
      password: '',
      authState: 'checking',
      authError: null,
    })
  })

  describe('getEffectiveBaseUrl', () => {
    it('returns empty string in local mode (dev)', () => {
      useConnectionStore.setState({ mode: 'local' })
      const url = useConnectionStore.getState().getEffectiveBaseUrl()
      expect(url).toBe('')
    })

    it('returns remote URL stripping trailing slash', () => {
      useConnectionStore.setState({ mode: 'remote', remoteUrl: 'http://example.com/' })
      const url = useConnectionStore.getState().getEffectiveBaseUrl()
      expect(url).toBe('http://example.com')
    })
  })

  it('loads browser-stored connection settings without persisting a password', async () => {
    localStorage.setItem('soloqueue_connection_mode', 'remote')
    localStorage.setItem('soloqueue_remote_url', 'https://remote.example')
    localStorage.setItem('soloqueue_remote_username', 'alice')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }))

    await useConnectionStore.getState().loadConfig()

    expect(useConnectionStore.getState()).toMatchObject({
      mode: 'remote',
      remoteUrl: 'https://remote.example',
      username: 'alice',
      password: '',
    })
    expect(localStorage.getItem('soloqueue_remote_password')).toBeNull()
  })

  it('discovers remote auth independently of connection mode', async () => {
    localStorage.setItem('soloqueue_connection_mode', 'local')
    useConnectionStore.setState({ username: '', password: '' })
    vi.stubGlobal(
      'fetch',
      vi.fn()
        .mockResolvedValueOnce({ ok: true, json: async () => ({ backend_url: '' }) })
        .mockResolvedValueOnce({ ok: true, json: async () => ({ required: true, scheme: 'basic' }) })
    )

    await useConnectionStore.getState().loadConfig()

    expect(useConnectionStore.getState().authState).toBe('required')
    expect(useConnectionStore.getState().getAuthHeader()).toBeUndefined()

    useConnectionStore.getState().setUsername('alice')
    useConnectionStore.getState().setPassword('secret')
    expect(useConnectionStore.getState().getAuthHeader()).toBe(`Basic ${btoa('alice:secret')}`)
  })

  it('recognizes browser-cached Basic Auth on load and after refresh', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ ok: true, json: async () => ({ backend_url: '' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ required: true, authenticated: true, scheme: 'basic' }),
      })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ backend_url: '' }) })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ required: true, authenticated: true, scheme: 'basic' }),
      })
    vi.stubGlobal('fetch', fetchMock)

    await useConnectionStore.getState().loadConfig()
    expect(useConnectionStore.getState()).toMatchObject({
      authState: 'authenticated',
      password: '',
    })
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/api/auth/status',
      expect.objectContaining({ credentials: 'same-origin' })
    )

    // A reload reconstructs the store without a password. The browser's Basic
    // Auth cache must still be enough to avoid a second login gate.
    useConnectionStore.setState({ password: '', authState: 'checking' })
    await useConnectionStore.getState().loadConfig()
    expect(useConnectionStore.getState()).toMatchObject({
      authState: 'authenticated',
      password: '',
    })
  })

  describe('getEffectiveWsUrl', () => {
    it('converts http remote URL to ws', () => {
      useConnectionStore.setState({ mode: 'remote', remoteUrl: 'http://example.com' })
      const url = useConnectionStore.getState().getEffectiveWsUrl()
      expect(url).toBe('ws://example.com/ws')
    })

    it('converts https remote URL to wss', () => {
      useConnectionStore.setState({ mode: 'remote', remoteUrl: 'https://example.com' })
      const url = useConnectionStore.getState().getEffectiveWsUrl()
      expect(url).toBe('wss://example.com/ws')
    })
  })
})
