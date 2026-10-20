import { afterEach, describe, expect, it, vi } from 'vitest'
import { getEmbeddingStatus, runEmbeddingAction } from './config-api'
import { request } from './core'
vi.mock('./core', () => ({ request: vi.fn(), requestText: vi.fn() }))
afterEach(() => vi.clearAllMocks())
describe('embedding lifecycle API contract', () => {
  it('passes cancellation to GET status and returns the response', async () => {
    const status = { state: 'ready' }
    vi.mocked(request).mockResolvedValue(status)
    const options = { signal: new AbortController().signal }
    expect(await getEmbeddingStatus(options)).toBe(status)
    expect(request).toHaveBeenCalledWith('/config/embedding/status', options)
  })
  it.each(['retry', 'cancel', 'reindex'] as const)('POSTs %s with the exact action body', async action => {
    const signal = new AbortController().signal
    vi.mocked(request).mockResolvedValue({ state: 'preparing' })
    await runEmbeddingAction(action, { signal })
    expect(request).toHaveBeenCalledWith('/config/embedding/actions', { signal, method: 'POST', body: JSON.stringify({ action }) })
  })
})
