import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useConnectionStore } from '@/stores/connectionStore'
import { APIError, request, requestForm, requestRootJson, requestText } from './http-client'

describe('http client', () => {
  beforeEach(() => {
    vi.spyOn(globalThis, 'fetch').mockReset()
    useConnectionStore.setState({
      mode: 'local',
      remoteUrl: '',
      username: '',
      password: '',
      backendReady: true,
      backendStatus: { running: true, pid: null, uptime: 0 },
    })
  })

  it('uses the API path and sends no auth in local mode', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ ok: true })))

    await request<{ ok: boolean }>('/status')

    const [url, init] = vi.mocked(fetch).mock.calls[0]
    expect(url).toBe('/api/status')
    expect(new Headers((init as RequestInit).headers).has('Authorization')).toBe(false)
  })

  it('keeps root health checks outside the API prefix', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ status: 'ok' })))

    await requestRootJson<{ status: string }>('/healthz')

    expect(vi.mocked(fetch).mock.calls[0][0]).toBe('/healthz')
  })

  it('preserves text responses', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response('port = 8765'))

    await expect(requestText('/config/toml')).resolves.toBe('port = 8765')
  })

  it('does not set a JSON content type for FormData', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(JSON.stringify({ ok: true })))
    const form = new FormData()
    form.append('file', new Blob(['content']), 'test.txt')

    await requestForm('/session/upload', form, { method: 'POST' })

    const headers = new Headers(vi.mocked(fetch).mock.calls[0][1]?.headers)
    expect(headers.has('Content-Type')).toBe(false)
  })

  it('returns undefined for a 204 response', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }))

    await expect(request('/config/session', { method: 'DELETE' })).resolves.toBeUndefined()
  })

  it('does not expose arbitrary non-JSON error bodies', async () => {
    vi.mocked(fetch).mockResolvedValueOnce(
      new Response('<html>proxy details</html>', { status: 502, statusText: 'Bad Gateway' })
    )

    await expect(request('/status')).rejects.toMatchObject<Partial<APIError>>({
      message: 'Bad Gateway',
      status: 502,
    })
  })
})
