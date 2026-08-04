import { describe, it, expect, beforeEach, vi } from 'vitest'
import { useConnectionStore } from './connectionStore'

describe('connectionStore', () => {
  beforeEach(() => {
    delete (window as any).electronAPI
    useConnectionStore.setState({
      mode: 'local',
      remoteUrl: '',
      username: '',
      password: '',
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

  it('hydrates public connection settings from Electron Main without a password', async () => {
    Object.defineProperty(window, 'electronAPI', {
      configurable: true,
      value: {
        getRemoteConfig: vi.fn().mockResolvedValue({
          mode: 'remote',
          remoteUrl: 'https://remote.example',
          username: 'alice',
        }),
      },
    })

    await useConnectionStore.getState().loadConfig()

    expect(useConnectionStore.getState()).toMatchObject({
      mode: 'remote',
      remoteUrl: 'https://remote.example',
      username: 'alice',
      password: '',
    })
    expect(localStorage.getItem('soloqueue_remote_password')).toBeNull()
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
