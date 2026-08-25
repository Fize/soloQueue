import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useConnectionStore } from './connectionStore'

describe('connectionStore', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.restoreAllMocks()
    useConnectionStore.setState({
      mode: 'local',
      remoteUrl: '',
      backendReady: false,
      backendStatus: { running: false, pid: null, uptime: null },
      saving: false,
      isChecking: false,
      connectionError: null,
    })
  })

  it('allows deployment credentials in local mode', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ backend_url: '' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await useConnectionStore.getState().loadConfig()

    expect(fetchMock).toHaveBeenCalledWith('/api/runtime-config', {
      cache: 'no-store',
      credentials: 'include',
    })
    expect(useConnectionStore.getState().getEffectiveBaseUrl()).toBe('')
  })

  it('loads and persists the standalone remote backend URL with deployment credentials', async () => {
    localStorage.setItem('soloqueue_connection_mode', 'remote')
    localStorage.setItem('soloqueue_remote_url', 'https://remote.example/')
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ backend_url: '' }) })
    vi.stubGlobal('fetch', fetchMock)

    await useConnectionStore.getState().loadConfig()

    expect(useConnectionStore.getState()).toMatchObject({
      mode: 'remote',
      remoteUrl: 'https://remote.example/',
    })
    expect(useConnectionStore.getState().getEffectiveBaseUrl()).toBe('https://remote.example')
    expect(fetchMock).toHaveBeenCalledWith('https://remote.example/api/runtime-config', {
      cache: 'no-store',
      credentials: 'include',
    })

    await useConnectionStore.getState().saveConfig()
    expect(localStorage.getItem('soloqueue_connection_mode')).toBe('remote')
    expect(localStorage.getItem('soloqueue_remote_url')).toBe('https://remote.example/')
  })

  it('uses the configured backend returned by standalone web', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ backend_url: 'http://127.0.0.1:57647/' }),
      })
    )

    await useConnectionStore.getState().loadConfig()

    expect(useConnectionStore.getState().getEffectiveBaseUrl()).toBe('http://127.0.0.1:57647')
  })

  describe('getEffectiveWsUrl', () => {
    it('converts http remote URL to ws', () => {
      useConnectionStore.setState({ mode: 'remote', remoteUrl: 'http://example.com' })
      expect(useConnectionStore.getState().getEffectiveWsUrl()).toBe('ws://example.com/ws')
    })

    it('converts https remote URL to wss', () => {
      useConnectionStore.setState({ mode: 'remote', remoteUrl: 'https://example.com' })
      expect(useConnectionStore.getState().getEffectiveWsUrl()).toBe('wss://example.com/ws')
    })
  })
})
