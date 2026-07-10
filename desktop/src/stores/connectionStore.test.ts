import { describe, it, expect, beforeEach } from 'vitest'
import { useConnectionStore } from './connectionStore'

describe('connectionStore', () => {
  beforeEach(() => {
    useConnectionStore.setState({
      mode: 'local',
      remoteUrl: '',
      username: '',
      password: '',
    })
  })

  describe('getAuthHeaders', () => {
    it('returns empty object in local mode', () => {
      useConnectionStore.setState({ mode: 'local' })
      const headers = useConnectionStore.getState().getAuthHeaders()
      expect(headers).toEqual({})
    })

    it('returns empty object in remote mode without credentials', () => {
      useConnectionStore.setState({ mode: 'remote', remoteUrl: 'http://example.com' })
      const headers = useConnectionStore.getState().getAuthHeaders()
      expect(headers).toEqual({})
    })

    it('returns empty object in remote mode with username only', () => {
      useConnectionStore.setState({
        mode: 'remote',
        remoteUrl: 'http://example.com',
        username: 'user',
      })
      const headers = useConnectionStore.getState().getAuthHeaders()
      expect(headers).toEqual({})
    })

    it('returns Basic auth header in remote mode with credentials', () => {
      useConnectionStore.setState({
        mode: 'remote',
        remoteUrl: 'http://example.com',
        username: 'admin',
        password: 'secret123',
      })
      const headers = useConnectionStore.getState().getAuthHeaders()
      expect(headers.Authorization).toBe('Basic ' + btoa('admin:secret123'))
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
